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
//
// For builds the client from the read credential, which on a cluster the current
// k0s_cluster_credential role deployed is the read-only swallow-reader ServiceAccount; ForOperator
// builds it from the operator credential, the cluster-admin swallow-operator ServiceAccount. Which
// one a request gets is the core's decision (the explorer use case), never this factory's.
type ClientFactory struct {
	connections platformdomain.OperatorConnectionSource
}

var (
	_ platformdomain.KubernetesClientFactory                                       = (*ClientFactory)(nil)
	_ platformdomain.OperatorClientFactory[platformdomain.KubernetesClusterClient] = (*ClientFactory)(nil)
)

// NewClientFactory constructs the explorer client factory over the core's Connection source, which
// resolves both the read and the operator Connection (the core's ReaderFactory).
func NewClientFactory(connections platformdomain.OperatorConnectionSource) *ClientFactory {
	return &ClientFactory{connections: connections}
}

// For resolves a Kubernetes Platform into an explorer client authenticated with its read
// credential. It errors when the platform has no credential Integration (the
// deployed-but-not-yet-recorded case), when the Integration is not a Kubernetes platform
// Integration, or when the credential is missing; the application layer maps these to the
// explorer-unavailable status.
func (f *ClientFactory) For(ctx context.Context, platform *platformdomain.Platform) (platformdomain.KubernetesClusterClient, error) {
	return f.client(ctx, platform, f.connections.Connection)
}

// ForOperator resolves a Kubernetes Platform into an explorer client authenticated with its
// operator credential, or with its read credential when the Integration holds none (a Platform
// deployed before the operator ServiceAccount existed, whose read credential is cluster-admin). It
// has the same errors as For.
func (f *ClientFactory) ForOperator(ctx context.Context, platform *platformdomain.Platform) (platformdomain.KubernetesClusterClient, error) {
	return f.client(ctx, platform, f.connections.OperatorConnection)
}

// client checks that the Platform can have an explorer client before resolving its Connection, so
// no credential is decrypted for a Platform that cannot use it.
func (f *ClientFactory) client(
	ctx context.Context,
	platform *platformdomain.Platform,
	connection func(ctx context.Context, platform *platformdomain.Platform) (platformdomain.Connection, error),
) (platformdomain.KubernetesClusterClient, error) {
	if platform.Type != platformdomain.PlatformTypeKubernetes {
		return nil, platformdomain.ErrPlatformNotKubernetes
	}
	if platform.IntegrationID == "" {
		return nil, platformdomain.ErrClusterExplorerUnavailable
	}
	conn, err := connection(ctx, platform)
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
