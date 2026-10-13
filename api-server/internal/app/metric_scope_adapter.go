package app

import (
	"context"
	"errors"
	"fmt"

	infradomain "github.com/maple52046/swallow/internal/infrastructure/domain"
	monitoringdomain "github.com/maple52046/swallow/internal/monitoring/domain"
	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// metricScopeResolver satisfies the monitoring context's ServerScopeResolver port by reading
// the grouping contexts (Zones, Pools, Platforms) and the Server projection.
//
// It lives in the wiring layer because it bridges four contexts that must not import each
// other: monitoring only knows server_id, the infrastructure context owns the Zone/Pool
// catalog, the platform context owns Platforms, and the Server projection carries the
// observed placement and membership. Membership rules match the published contracts:
//
//   - a Zone or Pool contains the non-absent Servers of its Site whose provider-observed zone or
//     resource pool equals the grouping's name (infrastructure.md: placement is observed on the
//     Server, not stored on the grouping);
//   - a Platform contains the non-absent Servers whose membership axis names it, the same set as
//     GET /servers?platformId=.
//
// Each call lists the Site's Servers once (no pagination) and filters in memory; the repository
// has no placement filter, and a Site's fleet fits comfortably in one read.
type metricScopeResolver struct {
	zones     zoneFinder
	pools     poolFinder
	platforms platformFinder
	servers   serverLister
}

// The narrow views the resolver needs, so tests can supply small fakes instead of whole
// repositories.
type (
	zoneFinder interface {
		FindByID(ctx context.Context, id string) (*infradomain.Zone, error)
	}
	poolFinder interface {
		FindByID(ctx context.Context, id string) (*infradomain.Pool, error)
	}
	platformFinder interface {
		FindByID(ctx context.Context, id string) (*platformdomain.Platform, error)
	}
	serverLister interface {
		List(ctx context.Context, filter serverdomain.ListFilter) (serverdomain.ListResult, error)
	}
)

// ResolveScope returns the ids of the grouping's current non-absent members in repository
// order. An unknown grouping id is monitoringdomain.ErrScopeNotFound; any other lookup failure
// is returned as-is so a transport problem is not mistaken for an empty grouping.
func (r metricScopeResolver) ResolveScope(
	ctx context.Context,
	kind monitoringdomain.ScopeKind,
	id string,
) ([]string, error) {
	switch kind {
	case monitoringdomain.ScopeZone:
		zone, err := r.zones.FindByID(ctx, id)
		if errors.Is(err, infradomain.ErrZoneNotFound) {
			return nil, fmt.Errorf("%w: zone %q", monitoringdomain.ErrScopeNotFound, id)
		}
		if err != nil {
			return nil, err
		}
		return r.siteMembers(ctx, zone.SiteID, func(server *serverdomain.Server) bool {
			return server.Observed.ProviderZone == zone.Name
		})

	case monitoringdomain.ScopePool:
		pool, err := r.pools.FindByID(ctx, id)
		if errors.Is(err, infradomain.ErrPoolNotFound) {
			return nil, fmt.Errorf("%w: pool %q", monitoringdomain.ErrScopeNotFound, id)
		}
		if err != nil {
			return nil, err
		}
		return r.siteMembers(ctx, pool.SiteID, func(server *serverdomain.Server) bool {
			return server.Observed.ProviderResourcePool == pool.Name
		})

	case monitoringdomain.ScopePlatform:
		if _, err := r.platforms.FindByID(ctx, id); errors.Is(err, platformdomain.ErrPlatformNotFound) {
			return nil, fmt.Errorf("%w: platform %q", monitoringdomain.ErrScopeNotFound, id)
		} else if err != nil {
			return nil, err
		}
		result, err := r.servers.List(ctx, serverdomain.ListFilter{PlatformID: id})
		if err != nil {
			return nil, err
		}
		return memberIDs(result.Servers, nil), nil

	default:
		return nil, fmt.Errorf("scope kind %q needs no resolution", kind)
	}
}

// siteMembers lists the Site's non-absent Servers and keeps those the predicate accepts.
func (r metricScopeResolver) siteMembers(
	ctx context.Context,
	siteID string,
	member func(*serverdomain.Server) bool,
) ([]string, error) {
	result, err := r.servers.List(ctx, serverdomain.ListFilter{SiteID: siteID})
	if err != nil {
		return nil, err
	}
	return memberIDs(result.Servers, member), nil
}

// memberIDs collects the ids of the servers the predicate accepts; a nil predicate keeps all.
func memberIDs(servers []*serverdomain.Server, keep func(*serverdomain.Server) bool) []string {
	ids := make([]string, 0, len(servers))
	for _, server := range servers {
		if keep == nil || keep(server) {
			ids = append(ids, server.ID)
		}
	}
	return ids
}
