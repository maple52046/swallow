package app

import (
	"context"
	"encoding/json"

	clusterdomain "github.com/maple52046/swallow/internal/cluster/domain"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// clusterLifecycleReader adapts durable operation history to the cluster lifecycle port.
type clusterLifecycleReader struct {
	operations operationdomain.ExecutionRepository
}

func (r clusterLifecycleReader) Read(ctx context.Context, clusterIDs []string) (map[string]clusterdomain.LifecycleSnapshot, error) {
	snapshots := make(map[string]clusterdomain.LifecycleSnapshot, len(clusterIDs))
	for _, id := range clusterIDs {
		snapshots[id] = registeredLifecycle()
	}
	if len(clusterIDs) == 0 {
		return snapshots, nil
	}

	result, err := r.operations.List(ctx, operationdomain.ExecutionListFilter{
		ClusterIDs: clusterIDs,
		Kinds: []operationdomain.OperationKind{
			operationdomain.OperationKindDeployKubernetes,
			operationdomain.OperationKindUninstallKubernetes,
		},
	})
	if err != nil {
		return nil, err
	}

	for _, operation := range result.Operations {
		snapshot, expected := snapshots[operation.ClusterID]
		if !expected {
			continue
		}
		projected := &clusterdomain.LifecycleOperation{
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
		snapshots[operation.ClusterID] = snapshot
	}

	for id, snapshot := range snapshots {
		snapshots[id] = deriveLifecycle(snapshot)
	}
	return snapshots, nil
}

// deploymentIntent narrows persisted runner variables back into Cluster vocabulary. The
// general extra-vars map stays private to Operations; malformed historical values produce
// no deployment projection instead of a plausible but incorrect topology.
func deploymentIntent(operation *operationdomain.ExecutionOperation) *clusterdomain.LifecycleDeployment {
	roles := decodeStringMap(operation.ExtraVars["swallow_k0s_roles"])
	if len(roles) == 0 || len(operation.TargetServerIDs) == 0 {
		return nil
	}
	workloadControllers := stringSet(decodeStringSlice(
		operation.ExtraVars["swallow_k0s_workload_controller_ids"],
	))
	assignments := make([]clusterdomain.RoleAssignment, 0, len(operation.TargetServerIDs))
	for _, serverID := range operation.TargetServerIDs {
		role := clusterdomain.NodeRole(roles[serverID])
		if !role.Valid() {
			return nil
		}
		assignments = append(assignments, clusterdomain.RoleAssignment{
			ServerID: serverID, Role: role,
			RunWorkloads: role == clusterdomain.NodeRoleControlPlane && workloadControllers[serverID],
		})
	}
	spec := clusterdomain.DeploymentSpec{RoleAssignments: assignments}
	topology := spec.Topology()
	if topology == "" {
		return nil
	}
	return &clusterdomain.LifecycleDeployment{
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

func registeredLifecycle() clusterdomain.LifecycleSnapshot {
	return clusterdomain.LifecycleSnapshot{
		Origin: clusterdomain.ClusterOriginRegistered,
		State:  clusterdomain.ClusterLifecycleRegistered,
	}
}

func deriveLifecycle(snapshot clusterdomain.LifecycleSnapshot) clusterdomain.LifecycleSnapshot {
	if snapshot.Deployment == nil {
		return registeredLifecycle()
	}
	snapshot.Origin = clusterdomain.ClusterOriginDeployed

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
			snapshot.State = clusterdomain.ClusterLifecycleUninstalling
		case operationdomain.StatusSucceeded:
			snapshot.State = clusterdomain.ClusterLifecycleUninstalled
		default:
			snapshot.State = clusterdomain.ClusterLifecycleUninstallFailed
		}
		return snapshot
	}

	switch status {
	case operationdomain.StatusPending, operationdomain.StatusRunning:
		snapshot.State = clusterdomain.ClusterLifecycleDeploying
	case operationdomain.StatusSucceeded:
		snapshot.State = clusterdomain.ClusterLifecycleActive
	default:
		snapshot.State = clusterdomain.ClusterLifecycleDeployFailed
	}
	return snapshot
}
