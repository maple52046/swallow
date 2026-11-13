package application

import (
	"context"
	"errors"
	"fmt"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// PoolResolver derives the Pool a Platform belongs to (decision 082; identity-and-access.md §5):
// the one Pool every member of its spec and every Server it still claims is placed in, "" when
// they are in different Pools or in none. It is the one implementation of that rule; the Platform
// responses, authorization, and the placement guard all use it. It holds no state and is safe for
// concurrent use.
type PoolResolver struct {
	platforms platformdomain.PlatformRepository
	claims    serverdomain.PlatformClaimStore
	servers   serverdomain.ServerRepository
}

// NewPoolResolver returns the resolver over the Platform, claim, and Server stores.
func NewPoolResolver(platforms platformdomain.PlatformRepository, claims serverdomain.PlatformClaimStore, servers serverdomain.ServerRepository) PoolResolver {
	return PoolResolver{platforms: platforms, claims: claims, servers: servers}
}

// PoolOf returns the Pool of the Platform with platformID and whether it exists.
func (r PoolResolver) PoolOf(ctx context.Context, platformID string) (string, bool, error) {
	platform, err := r.platforms.FindByID(ctx, platformID)
	if errors.Is(err, platformdomain.ErrPlatformNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	pool, err := r.Pool(ctx, platform)
	return pool, true, err
}

// Pool returns the Pool of platform.
func (r PoolResolver) Pool(ctx context.Context, platform *platformdomain.Platform) (string, error) {
	ids, err := r.claims.ClaimedServerIDs(ctx, platform.ID)
	if err != nil {
		return "", fmt.Errorf("read claimed servers: %w", err)
	}
	if platform.Spec != nil {
		for _, member := range platform.Spec.Members {
			ids = append(ids, member.ServerID)
		}
	}
	if len(ids) == 0 {
		return "", nil
	}
	result, err := r.servers.List(ctx, serverdomain.ListFilter{IDs: ids, IncludeAbsent: true})
	if err != nil {
		return "", fmt.Errorf("read member placements: %w", err)
	}
	pools := make(map[string]string, len(result.Servers))
	for _, server := range result.Servers {
		pools[server.ID] = server.Spec.PoolID
	}
	return platformdomain.PoolOfMembers(ids, pools), nil
}
