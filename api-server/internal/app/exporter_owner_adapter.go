package app

import (
	"context"
	"errors"

	clusterdomain "github.com/maple52046/swallow/internal/cluster/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// exporterOwnerResolver resolves a server's effective exporter owner from its lock state
// and cluster policy, satisfying the operation context's ExporterOwnerResolver port.
//
// It lives in the app wiring layer because it bridges two contexts — the server's
// provisioning lock and a cluster's exporterOwner policy — without either context
// depending on the other. The resolution order matters: a locked machine is off-limits
// regardless of any cluster it belongs to.
type exporterOwnerResolver struct {
	clusters clusterdomain.ClusterRepository
}

// EffectiveExporterOwner returns "unmanaged" for a locked machine, the cluster's policy
// for a cluster member, and "ansible" otherwise. A membership pointing at a cluster that
// no longer exists falls back to ansible rather than erroring, matching how the policy
// checker treats a stale membership.
func (r exporterOwnerResolver) EffectiveExporterOwner(ctx context.Context, server *serverdomain.Server) (string, error) {
	if server.Provisioning != nil && server.Provisioning.Locked {
		return string(clusterdomain.ExporterOwnerUnmanaged), nil
	}
	if server.Membership != nil && server.Membership.ClusterID != "" {
		cluster, err := r.clusters.FindByID(ctx, server.Membership.ClusterID)
		if errors.Is(err, clusterdomain.ErrClusterNotFound) {
			return string(clusterdomain.ExporterOwnerAnsible), nil
		}
		if err != nil {
			return "", err
		}
		return string(cluster.ExporterOwner), nil
	}
	return string(clusterdomain.ExporterOwnerAnsible), nil
}
