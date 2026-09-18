package application

import (
	"context"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// KubernetesExplorerUseCase resolves a deployed Kubernetes Platform into a live cluster client
// and correlates cluster nodes to Server identity.
//
// It enforces the explorer eligibility rules once, in one place: the Platform must be a
// Kubernetes Platform (else ErrPlatformNotKubernetes), must be Swallow-deployed rather than a
// legacy registered record (else ErrClusterExplorerUnavailable), and must have a recorded
// credential (the factory reports ErrClusterExplorerUnavailable when it does not). Every
// explorer request goes through Client so no route can accidentally skip a check. Like the
// Slurm live read (ADR 021) it performs no writes to Swallow state: it never touches the
// membership axis, the sync counters, or the reconcile interval. See
// docs/decisions/032-self-deployed-platform-management.md.
type KubernetesExplorerUseCase struct {
	platforms platformdomain.PlatformRepository
	lifecycle platformdomain.LifecycleReader
	clients   platformdomain.KubernetesClientFactory
	servers   serverdomain.ServerRepository
}

// NewKubernetesExplorerUseCase wires the platform repository, the operation-derived lifecycle
// reader (to prove the Platform is Swallow-deployed), the cluster client factory, and the
// server repository (for node-to-Server correlation).
func NewKubernetesExplorerUseCase(
	platforms platformdomain.PlatformRepository,
	lifecycle platformdomain.LifecycleReader,
	clients platformdomain.KubernetesClientFactory,
	servers serverdomain.ServerRepository,
) *KubernetesExplorerUseCase {
	return &KubernetesExplorerUseCase{platforms: platforms, lifecycle: lifecycle, clients: clients, servers: servers}
}

// Client resolves a Platform into a live Kubernetes explorer client, enforcing eligibility.
// Callers use the returned client for every read/write; it is the only entry point.
func (uc *KubernetesExplorerUseCase) Client(ctx context.Context, platformID string) (platformdomain.KubernetesClusterClient, error) {
	client, _, err := uc.resolve(ctx, platformID)
	return client, err
}

// Nodes lists cluster nodes and fills each node's ServerID by correlating its name and
// addresses to a Server projection at the Platform's Site, reusing the same matching the
// membership sync uses. Correlation failure for one node leaves its ServerID empty rather than
// failing the whole read.
func (uc *KubernetesExplorerUseCase) Nodes(ctx context.Context, platformID string) ([]platformdomain.KubernetesNode, error) {
	client, platform, err := uc.resolve(ctx, platformID)
	if err != nil {
		return nil, err
	}
	nodes, err := client.ListNodes(ctx)
	if err != nil {
		return nil, err
	}

	candidates, err := uc.servers.List(ctx, serverdomain.ListFilter{SiteID: platform.SiteID, IncludeAbsent: true})
	if err != nil {
		return nil, err
	}
	index := buildServerIndex(candidates.Servers)
	for i := range nodes {
		member := platformdomain.Member{Name: nodes[i].Name, Addresses: nodes[i].Addresses}
		if server := index.match(member); server != nil {
			nodes[i].ServerID = server.ID
		}
	}
	return nodes, nil
}

// resolve loads the Platform, enforces the explorer eligibility rules, and builds the client.
func (uc *KubernetesExplorerUseCase) resolve(ctx context.Context, platformID string) (platformdomain.KubernetesClusterClient, *platformdomain.Platform, error) {
	platform, err := uc.platforms.FindByID(ctx, platformID)
	if err != nil {
		return nil, nil, err
	}
	if platform.Type != platformdomain.PlatformTypeKubernetes {
		return nil, nil, platformdomain.ErrPlatformNotKubernetes
	}

	// The explorer is for Swallow-deployed Platforms only. Origin is derived from durable
	// Operation history exactly as the read model derives it: no lifecycle entry means no
	// deployment provenance, i.e. a legacy registered record, which is not eligible.
	origin := platformdomain.PlatformOriginRegistered
	if uc.lifecycle != nil {
		read, err := uc.lifecycle.Read(ctx, []string{platformID})
		if err != nil {
			return nil, nil, err
		}
		if snapshot, ok := read[platformID]; ok {
			origin = snapshot.Origin
		}
	}
	if origin != platformdomain.PlatformOriginDeployed {
		return nil, nil, platformdomain.ErrClusterExplorerUnavailable
	}

	client, err := uc.clients.For(ctx, platform)
	if err != nil {
		return nil, nil, err
	}
	return client, platform, nil
}
