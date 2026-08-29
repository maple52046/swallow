package application

import (
	"context"
	"errors"
	"fmt"

	clusterdomain "github.com/maple52046/swallow/internal/cluster/domain"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// PolicyChecker refuses operations that contradict a cluster's gpuStackOwner policy.
//
// It lives in the cluster context because the policy does: the operation context must
// not need to know what a GPU operator is in order to refuse installing drivers behind
// one. It satisfies the operation context's PolicyChecker port.
type PolicyChecker struct {
	clusters clusterdomain.ClusterRepository
	servers  serverdomain.ServerRepository
}

func NewPolicyChecker(
	clusters clusterdomain.ClusterRepository,
	servers serverdomain.ServerRepository,
) *PolicyChecker {
	return &PolicyChecker{clusters: clusters, servers: servers}
}

// CheckOperation returns ErrPolicyConflict when the operation would fight a cluster over
// something that cluster owns.
func (c *PolicyChecker) CheckOperation(
	ctx context.Context,
	kind operationdomain.OperationKind,
	clusterID string,
	serverIDs []string,
) error {
	if kind == operationdomain.OperationKindUninstallKubernetes && clusterID == "" {
		return fmt.Errorf("%w: uninstall-kubernetes requires a clusterId",
			operationdomain.ErrPolicyConflict)
	}
	if (kind == operationdomain.OperationKindDeployKubernetes ||
		kind == operationdomain.OperationKindUninstallKubernetes) && clusterID != "" {
		if _, err := c.clusters.FindByID(ctx, clusterID); err != nil {
			if errors.Is(err, clusterdomain.ErrClusterNotFound) {
				return fmt.Errorf("%w: cluster %s no longer exists",
					operationdomain.ErrPolicyConflict, clusterID)
			}
			return err
		}
	}

	// Only driver installation is contested today. Deploying a cluster or configuring
	// Slurm does not touch anything a GPU operator manages.
	if kind != operationdomain.OperationKindInstallGPUDriver {
		return nil
	}

	for _, serverID := range serverIDs {
		server, err := c.servers.FindByID(ctx, serverID)
		if err != nil {
			return err
		}
		if server.Membership == nil || server.Membership.ClusterID == "" {
			// Not in a cluster, so no cluster policy applies. Installing a driver on
			// a spare is exactly what the provisioning-owned mode is for.
			continue
		}

		cluster, err := c.clusters.FindByID(ctx, server.Membership.ClusterID)
		if errors.Is(err, clusterdomain.ErrClusterNotFound) {
			// The membership axis outlived its cluster registration. Nothing to
			// enforce, and the membership sync will clear it.
			continue
		}
		if err != nil {
			return err
		}

		if cluster.GPUStackOwner == clusterdomain.GPUStackOwnerGPUOperator {
			return fmt.Errorf(
				"%w: %s belongs to cluster %q, where the GPU operator owns drivers and DCGM; "+
					"installing them from provisioning as well would leave two owners on the same host",
				operationdomain.ErrPolicyConflict, server.DisplayName(), cluster.Name)
		}
	}

	return nil
}
