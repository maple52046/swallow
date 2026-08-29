package app

import (
	"context"

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
