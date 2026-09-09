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
	orchestrations operationdomain.WorkflowRepository
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
		Kinds: []operationdomain.WorkflowKind{
			operationdomain.WorkflowKindDeployKubernetes,
			operationdomain.WorkflowKindConfigureSlurm,
			operationdomain.WorkflowKindUninstallKubernetes,
			operationdomain.WorkflowKindUninstallSlurm,
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
		case operationdomain.WorkflowKindDeployKubernetes, operationdomain.WorkflowKindConfigureSlurm:
			// A Slurm deployment claims its targets and drives the deploying/active/failed
			// projection the same way k0s does; only the k0s-shaped topology Intent is left
			// nil, since deploymentIntent reads k0s role vars that Slurm does not set.
			projected.Intent = deploymentIntent(operation)
			if snapshot.Deployment == nil {
				snapshot.Deployment = projected
			}
		case operationdomain.WorkflowKindUninstallKubernetes, operationdomain.WorkflowKindUninstallSlurm:
			if snapshot.Uninstall == nil {
				snapshot.Uninstall = projected
			}
		}
		snapshots[operation.PlatformID] = snapshot
	}

	if r.orchestrations != nil {
		v3, _, err := r.orchestrations.List(ctx, operationdomain.WorkflowFilter{PlatformIDs: platformIDs})
		if err != nil {
			return nil, err
		}
		expectedIDs := map[string]bool{}
		for _, id := range platformIDs {
			expectedIDs[id] = true
		}
		for _, operation := range v3 {
			isDeploy := operation.Kind == operationdomain.WorkflowKindDeployKubernetes ||
				operation.Kind == operationdomain.WorkflowKindConfigureSlurm
			isUninstall := operation.Kind == operationdomain.WorkflowKindUninstallKubernetes ||
				operation.Kind == operationdomain.WorkflowKindUninstallSlurm
			if !expectedIDs[operation.PlatformID] || (!isDeploy && !isUninstall) {
				continue
			}
			snapshot := snapshots[operation.PlatformID]
			projected := &platformdomain.LifecycleOperation{ID: operation.ID, Status: lifecycleStatus(operation.Status),
				TargetServerIDs: append([]string(nil), operation.TargetServerIDs...), RequestedAt: operation.RequestedAt}
			if isDeploy {
				// Project the deployment intent per platform type: k0s from its role vars,
				// Slurm from its recorded controller/compute id lists. Slurm needs its own
				// projection because slurmrestd only reports compute (slurmd) nodes as members,
				// so without the recorded controller ids the manager nodes would be invisible to
				// the read model.
				if operation.Kind == operationdomain.WorkflowKindConfigureSlurm {
					projected.Intent = deploymentIntentSlurm(operation)
				} else {
					projected.Intent = deploymentIntentV3(operation)
				}
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

func lifecycleStatus(status operationdomain.WorkflowStatus) string {
	switch status {
	case operationdomain.WorkflowPending, operationdomain.WorkflowWaitingDependency:
		return string(operationdomain.StatusPending)
	case operationdomain.WorkflowRunning, operationdomain.WorkflowWaitingExternal, operationdomain.WorkflowCanceling:
		return string(operationdomain.StatusRunning)
	case operationdomain.WorkflowSucceeded:
		return string(operationdomain.StatusSucceeded)
	case operationdomain.WorkflowCanceled:
		return string(operationdomain.StatusCanceled)
	default:
		return string(operationdomain.StatusFailed)
	}
}

func deploymentIntentV3(operation *operationdomain.Workflow) *platformdomain.LifecycleDeployment {
	extraVars, _ := operation.Intent["extraVars"].(map[string]any)
	legacy := &operationdomain.ExecutionOperation{
		TargetServerIDs: operation.TargetServerIDs,
		ExtraVars:       extraVars,
	}
	return deploymentIntent(legacy)
}

// deploymentIntentSlurm projects a Slurm deployment's per-daemon node roles from the durable
// Operation's recorded controller/compute id lists into the shared LifecycleDeployment shape.
// It exists because slurmrestd only reports compute (slurmd) nodes as members, so a
// controller-only node is otherwise invisible to the read model; this is the intent recorded at
// deploy time, not a live cluster read. Slurm daemons are not mutually exclusive, so a node that
// runs both slurmctld and slurmd is a control-plane assignment that also runs workloads (its
// slurmd compute). Returns nil when the recorded intent is incomplete, so a malformed history
// yields no projection instead of a guessed topology.
func deploymentIntentSlurm(operation *operationdomain.Workflow) *platformdomain.LifecycleDeployment {
	extraVars, _ := operation.Intent["extraVars"].(map[string]any)
	// Trusted-var names owned by the Slurm deploy use case (buildSlurmVars). Referenced as
	// literals here, like the k0s role vars above, because they are the deployment's published
	// contract into the operation intent, not this package's private constants.
	controllerIDs := decodeStringSlice(extraVars["swallow_slurm_controller_ids"])
	computeIDs := decodeStringSlice(extraVars["swallow_slurm_compute_ids"])
	if len(controllerIDs) == 0 || len(operation.TargetServerIDs) == 0 {
		return nil
	}
	controllers := stringSet(controllerIDs)
	compute := stringSet(computeIDs)
	assignments := make([]platformdomain.RoleAssignment, 0, len(operation.TargetServerIDs))
	for _, serverID := range operation.TargetServerIDs {
		isController := controllers[serverID]
		isCompute := compute[serverID]
		if !isController && !isCompute {
			continue
		}
		role := platformdomain.NodeRoleWorker
		runWorkloads := false
		if isController {
			// A Slurm controller maps to the manager (control-plane) role; if it also runs the
			// compute daemon it additionally runs workloads, exactly like a k0s control-plane
			// node that schedules pods.
			role = platformdomain.NodeRoleControlPlane
			runWorkloads = isCompute
		}
		assignments = append(assignments, platformdomain.RoleAssignment{
			ServerID: serverID, Role: role, RunWorkloads: runWorkloads,
		})
	}
	if len(assignments) == 0 {
		return nil
	}
	// More than one controller is a highly available control plane; a single controller is a
	// standalone manager. Slurm has no "multi-node non-HA" manager tier, so those are the only
	// two shapes the read model reports.
	topology := platformdomain.KubernetesTopologyStandalone
	if len(controllerIDs) > 1 {
		topology = platformdomain.KubernetesTopologyHighAvailability
	}
	return &platformdomain.LifecycleDeployment{Topology: topology, RoleAssignments: assignments}
}
