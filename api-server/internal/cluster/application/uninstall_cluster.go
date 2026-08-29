package application

import (
	"context"
	"errors"
	"fmt"

	clusterdomain "github.com/maple52046/swallow/internal/cluster/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// UninstallService validates a cluster and starts its idempotent k0s removal operation.
type UninstallService struct {
	clusters  clusterdomain.ClusterRepository
	servers   serverdomain.ServerRepository
	lifecycle clusterdomain.LifecycleReader
	launcher  clusterdomain.UninstallLauncher
}

// NewUninstallService constructs the cluster uninstall use case.
func NewUninstallService(
	clusters clusterdomain.ClusterRepository,
	servers serverdomain.ServerRepository,
	lifecycle clusterdomain.LifecycleReader,
	launcher clusterdomain.UninstallLauncher,
) *UninstallService {
	return &UninstallService{
		clusters: clusters, servers: servers, lifecycle: lifecycle, launcher: launcher,
	}
}

// UninstallClusterInput identifies the cluster and requesting operator.
type UninstallClusterInput struct {
	ClusterID   string
	RequestedBy string
}

// UninstallClusterResult is the accepted uninstall operation.
type UninstallClusterResult struct {
	ClusterID   string `json:"clusterId"`
	OperationID string `json:"operationId"`
}

// Uninstall validates durable provenance and launches against the original target snapshot.
func (s *UninstallService) Uninstall(
	ctx context.Context,
	input UninstallClusterInput,
) (*UninstallClusterResult, error) {
	cluster, err := s.clusters.FindByID(ctx, input.ClusterID)
	if err != nil {
		return nil, err
	}
	if cluster.Type != clusterdomain.ClusterTypeKubernetes {
		return nil, fmt.Errorf("%w: only Kubernetes clusters deployed by Swallow can be uninstalled",
			clusterdomain.ErrClusterNotDeployManaged)
	}
	if s.lifecycle == nil {
		return nil, clusterdomain.ErrClusterNotDeployManaged
	}
	snapshots, err := s.lifecycle.Read(ctx, []string{cluster.ID})
	if err != nil {
		return nil, err
	}
	snapshot, ok := snapshots[cluster.ID]
	if !ok || snapshot.Origin != clusterdomain.ClusterOriginDeployed || snapshot.Deployment == nil {
		return nil, clusterdomain.ErrClusterNotDeployManaged
	}

	retryOf := ""
	switch snapshot.State {
	case clusterdomain.ClusterLifecycleDeploying, clusterdomain.ClusterLifecycleUninstalling:
		return nil, fmt.Errorf("%w: cluster lifecycle is %s",
			clusterdomain.ErrClusterUninstallConflict, snapshot.State)
	case clusterdomain.ClusterLifecycleUninstalled:
		return nil, clusterdomain.ErrClusterAlreadyUninstalled
	case clusterdomain.ClusterLifecycleUninstallFailed:
		if snapshot.Uninstall != nil {
			retryOf = snapshot.Uninstall.ID
		}
	}

	targetIDs := append([]string(nil), snapshot.Deployment.TargetServerIDs...)
	if len(targetIDs) == 0 {
		return nil, fmt.Errorf("%w: deployment operation has no target snapshot",
			clusterdomain.ErrClusterUninstallConflict)
	}
	for _, targetID := range targetIDs {
		server, targetErr := s.servers.FindByID(ctx, targetID)
		if errors.Is(targetErr, serverdomain.ErrServerNotFound) {
			return nil, fmt.Errorf("%w: deployment target %s no longer exists",
				clusterdomain.ErrClusterUninstallConflict, targetID)
		}
		if targetErr != nil {
			return nil, targetErr
		}
		if server.Absent {
			return nil, fmt.Errorf("%w: deployment target %s is absent",
				clusterdomain.ErrClusterUninstallConflict, server.DisplayName())
		}
		if server.Provisioning != nil && server.Provisioning.Locked {
			return nil, fmt.Errorf("%w: deployment target %s is locked",
				clusterdomain.ErrClusterUninstallConflict, server.DisplayName())
		}
	}

	operationID, err := s.launcher.LaunchUninstall(ctx, clusterdomain.UninstallLaunch{
		Cluster: cluster, TargetServerIDs: targetIDs,
		RestoreExporters:   cluster.ExporterOwner == clusterdomain.ExporterOwnerK8s,
		RetryOfOperationID: retryOf, RequestedBy: input.RequestedBy,
	})
	if err != nil {
		return nil, err
	}
	return &UninstallClusterResult{ClusterID: cluster.ID, OperationID: operationID}, nil
}
