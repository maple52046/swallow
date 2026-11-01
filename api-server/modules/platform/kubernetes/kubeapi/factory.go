package kubeapi

import (
	"context"
	"fmt"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// ClientFactory builds the live read/write explorer client of a deployed Kubernetes Platform from
// its swallow-owned credential Integration.
//
// It is separate from the core's ReaderFactory because the explorer is a distinct capability
// (read/write in-cluster resources) with a distinct client type. It is not cached: the explorer is
// used interactively rather than on a poll loop, and holding a stale client past a credential
// change would be worse than paying to build one per request.
type ClientFactory struct {
	connections platformdomain.ConnectionSource
}

var _ platformdomain.KubernetesClientFactory = (*ClientFactory)(nil)

// NewClientFactory constructs the explorer client factory over the core's Connection source.
func NewClientFactory(connections platformdomain.ConnectionSource) *ClientFactory {
	return &ClientFactory{connections: connections}
}

// For resolves a Kubernetes Platform into an explorer client. It errors when the platform has no
// credential Integration (the deployed-but-not-yet-recorded case), when the Integration is not a
// Kubernetes platform Integration, or when the credential is missing; the application layer maps
// these to the explorer-unavailable status.
func (f *ClientFactory) For(ctx context.Context, platform *platformdomain.Platform) (platformdomain.KubernetesClusterClient, error) {
	if platform.Type != platformdomain.PlatformTypeKubernetes {
		return nil, platformdomain.ErrPlatformNotKubernetes
	}
	if platform.IntegrationID == "" {
		return nil, platformdomain.ErrClusterExplorerUnavailable
	}
	conn, err := f.connections.Connection(ctx, platform)
	if err != nil {
		return nil, err
	}
	if conn.ProviderKind != ProviderKind {
		return nil, fmt.Errorf("the integration of Platform %q is not a Kubernetes platform API", platform.Name)
	}
	client, err := NewClient(conn)
	if err != nil {
		return nil, err
	}
	return client, nil
}
