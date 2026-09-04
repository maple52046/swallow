package app

import (
	"context"
	"encoding/json"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
)

// platformLifecycleReader adapts durable operation history to the platform lifecycle port.
type platformLifecycleReader struct {
	operations     operationdomain.ExecutionRepository
	orchestrations operationdomain.OrchestrationRepository
}

func (r platformLifecycleReader) Read(ctx context.Context, platformIDs []string) (map[string]platformdomain.LifecycleSnapshot, error) {
	snapshots := make(map[string]platformdomain.LifecycleSnapshot, len(platformIDs))
	for _, id := range platformIDs {
		snapshots[id] = registeredLifecycle()
	}
	if len(platformIDs) == 0 {
		return snapshots, nil
	}

	result, err := r.operations.List(ctx, operationdomain.ExecutionListFilter{
		PlatformIDs: platformIDs,
		Kinds: []operationdomain.OperationKind{
			operationdomain.OperationKindDeployKubernetes,
			operationdomain.OperationKindUninstallKubernetes,
		},
	})
	if err != nil {
		return nil, err
	}

	for _, operation := range result.Operations {
		snapshot, expected := snapshots[operation.PlatformID]
		if !expected {
			continue
		}
		projected := &platformdomain.LifecycleOperation{
			ID:              operation.ID,
			Status:          string(operation.Execution.Status),
			TargetServerIDs: append([]string(nil), operation.TargetServerIDs...),
			RequestedAt:     operation.RequestedAt,
		}
		switch operation.Kind {
		case operationdomain.OperationKindDeployKubernetes:
			projected.Intent = deploymentIntent(operation)
			if snapshot.Deployment == nil {
				snapshot.Deployment = projected
			}
		case operationdomain.OperationKindUninstallKubernetes:
			if snapshot.Uninstall == nil {
				snapshot.Uninstall = projected
			}
		}
		snapshots[operation.PlatformID] = snapshot
	}

	if r.orchestrations != nil {
		v3, _, err := r.orchestrations.List(ctx, operationdomain.OrchestrationFilter{PlatformIDs: platformIDs})
		if err != nil {
			return nil, err
		}
		expectedIDs := map[string]bool{}
		for _, id := range platformIDs {
			expectedIDs[id] = true
		}
		for _, operation := range v3 {
			if !expectedIDs[operation.PlatformID] || (operation.Kind != operationdomain.OperationKindDeployKubernetes && operation.Kind != operationdomain.OperationKindUninstallKubernetes) {
				continue
			}
			snapshot := snapshots[operation.PlatformID]
			projected := &platformdomain.LifecycleOperation{ID: operation.ID, Status: lifecycleStatus(operation.Status),
				TargetServerIDs: append([]string(nil), operation.TargetServerIDs...), RequestedAt: operation.RequestedAt}
			if operation.Kind == operationdomain.OperationKindDeployKubernetes {
				projected.Intent = deploymentIntentV3(operation)
				if snapshot.Deployment == nil || snapshot.Deployment.RequestedAt.Before(projected.RequestedAt) {
					snapshot.Deployment = projected
				}
			} else if snapshot.Uninstall == nil || snapshot.Uninstall.RequestedAt.Before(projected.RequestedAt) {
				snapshot.Uninstall = projected
			}
			snapshots[operation.PlatformID] = snapshot
		}
	}

	for id, snapshot := range snapshots {
		snapshots[id] = deriveLifecycle(snapshot)
	}
	return snapshots, nil
}

// deploymentIntent narrows persisted runner variables back into Platform vocabulary. The
// general extra-vars map stays private to Operations; malformed historical values produce
// no deployment projection instead of a plausible but incorrect topology.
func deploymentIntent(operation *operationdomain.ExecutionOperation) *platformdomain.LifecycleDeployment {
	roles := decodeStringMap(operation.ExtraVars["swallow_k0s_roles"])
	if len(roles) == 0 || len(operation.TargetServerIDs) == 0 {
		return nil
	}
	workloadControllers := stringSet(decodeStringSlice(
		operation.ExtraVars["swallow_k0s_workload_controller_ids"],
	))
	assignments := make([]platformdomain.RoleAssignment, 0, len(operation.TargetServerIDs))
	for _, serverID := range operation.TargetServerIDs {
		role := platformdomain.NodeRole(roles[serverID])
		if !role.Valid() {
			return nil
		}
		assignments = append(assignments, platformdomain.RoleAssignment{
			ServerID: serverID, Role: role,
			RunWorkloads: role == platformdomain.NodeRoleControlPlane && workloadControllers[serverID],
		})
	}
	spec := platformdomain.DeploymentSpec{RoleAssignments: assignments}
	topology := spec.Topology()
	if topology == "" {
		return nil
	}
	return &platformdomain.LifecycleDeployment{
		Topology: topology, RoleAssignments: assignments,
	}
}

func decodeStringMap(value any) map[string]string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var decoded map[string]string
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil
	}
	return decoded
}

func decodeStringSlice(value any) []string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var decoded []string
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil
	}
	return decoded
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func registeredLifecycle() platformdomain.LifecycleSnapshot {
	return platformdomain.LifecycleSnapshot{
		Origin: platformdomain.PlatformOriginRegistered,
		State:  platformdomain.PlatformLifecycleRegistered,
	}
}

func deriveLifecycle(snapshot platformdomain.LifecycleSnapshot) platformdomain.LifecycleSnapshot {
	if snapshot.Deployment == nil {
		return registeredLifecycle()
	}
	snapshot.Origin = platformdomain.PlatformOriginDeployed

	latest := snapshot.Deployment
	isUninstall := false
	if snapshot.Uninstall != nil && !snapshot.Uninstall.RequestedAt.Before(snapshot.Deployment.RequestedAt) {
		latest = snapshot.Uninstall
		isUninstall = true
	}
	snapshot.OperationID = latest.ID

	status := operationdomain.Status(latest.Status)
	if isUninstall {
		switch status {
		case operationdomain.StatusPending, operationdomain.StatusRunning:
			snapshot.State = platformdomain.PlatformLifecycleUninstalling
		case operationdomain.StatusSucceeded:
			snapshot.State = platformdomain.PlatformLifecycleUninstalled
		default:
			snapshot.State = platformdomain.PlatformLifecycleUninstallFailed
		}
		return snapshot
	}

	switch status {
	case operationdomain.StatusPending, operationdomain.StatusRunning:
		snapshot.State = platformdomain.PlatformLifecycleDeploying
	case operationdomain.StatusSucceeded:
		snapshot.State = platformdomain.PlatformLifecycleActive
	default:
		snapshot.State = platformdomain.PlatformLifecycleDeployFailed
	}
	return snapshot
}

func lifecycleStatus(status operationdomain.OrchestrationStatus) string {
	switch status {
	case operationdomain.OrchestrationPending, operationdomain.OrchestrationWaitingDependency:
		return string(operationdomain.StatusPending)
	case operationdomain.OrchestrationRunning, operationdomain.OrchestrationWaitingExternal, operationdomain.OrchestrationCanceling:
		return string(operationdomain.StatusRunning)
	case operationdomain.OrchestrationSucceeded:
		return string(operationdomain.StatusSucceeded)
	case operationdomain.OrchestrationCanceled:
		return string(operationdomain.StatusCanceled)
	default:
		return string(operationdomain.StatusFailed)
	}
}

func deploymentIntentV3(operation *operationdomain.OperationV3) *platformdomain.LifecycleDeployment {
	extraVars, _ := operation.Intent["extraVars"].(map[string]any)
	legacy := &operationdomain.ExecutionOperation{
		TargetServerIDs: operation.TargetServerIDs,
		ExtraVars:       extraVars,
	}
	return deploymentIntent(legacy)
}
