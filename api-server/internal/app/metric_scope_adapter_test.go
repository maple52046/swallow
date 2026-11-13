package app

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	infradomain "github.com/maple52046/swallow/internal/core/infrastructure/domain"
	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

type scopeZones map[string]*infradomain.Zone

func (z scopeZones) FindByID(_ context.Context, id string) (*infradomain.Zone, error) {
	if zone, ok := z[id]; ok {
		return zone, nil
	}
	return nil, infradomain.ErrZoneNotFound
}

type scopePools map[string]*infradomain.Pool

func (p scopePools) FindByID(_ context.Context, id string) (*infradomain.Pool, error) {
	if pool, ok := p[id]; ok {
		return pool, nil
	}
	return nil, infradomain.ErrPoolNotFound
}

type scopePlatforms map[string]*platformdomain.Platform

func (p scopePlatforms) FindByID(_ context.Context, id string) (*platformdomain.Platform, error) {
	if platform, ok := p[id]; ok {
		return platform, nil
	}
	return nil, platformdomain.ErrPlatformNotFound
}

// scopeServers answers List with the servers matching the Site, Pool, and Platform filters, which
// is all the resolver relies on; absent servers are assumed already excluded by the repository.
type scopeServers []*serverdomain.Server

func (s scopeServers) List(_ context.Context, filter serverdomain.ListFilter) (serverdomain.ListResult, error) {
	var matches []*serverdomain.Server
	for _, server := range s {
		if filter.SiteID != "" && server.Source.SiteID != filter.SiteID {
			continue
		}
		if filter.RestrictPools && !slices.Contains(filter.PoolIDs, server.Spec.PoolID) {
			continue
		}
		if filter.PlatformID != "" && (server.Membership == nil || server.Membership.PlatformID != filter.PlatformID) {
			continue
		}
		matches = append(matches, server)
	}
	return serverdomain.ListResult{Servers: matches, Total: len(matches)}, nil
}

// scopeServer is a Server of siteID observed in zone and provider resource pool providerPool, placed
// in the Pool poolID, and a member of platformID when that is not empty.
func scopeServer(id, siteID, zone, providerPool, poolID, platformID string) *serverdomain.Server {
	server := &serverdomain.Server{
		ID:       id,
		Source:   serverdomain.Source{SiteID: siteID},
		Spec:     serverdomain.ServerSpec{PoolID: poolID},
		Observed: serverdomain.Observed{ProviderZone: zone, ProviderResourcePool: providerPool},
	}
	if platformID != "" {
		server.Membership = &serverdomain.MembershipStatus{PlatformID: platformID}
	}
	return server
}

// A Zone's members are the Servers of its own Site observed in it, so a same-named zone of another
// Site does not leak in; a Pool's members are the Servers placed in it in any Site, whatever
// resource pool the provisioner reports.
func TestMetricScopeResolver_ResolvesGroupings(t *testing.T) {
	resolver := metricScopeResolver{
		zones:     scopeZones{"zone-a": {ID: "zone-a", SiteID: "site-1", Name: "rack-a"}},
		pools:     scopePools{"pool-r": {ID: "pool-r", Name: "research"}},
		platforms: scopePlatforms{"plat-1": {ID: "plat-1"}},
		servers: scopeServers{
			scopeServer("srv-1", "site-1", "rack-a", "research", "pool-r", "plat-1"),
			scopeServer("srv-2", "site-1", "rack-b", "research", "", ""),
			scopeServer("srv-3", "site-2", "rack-a", "default", "pool-r", "plat-1"),
		},
	}

	tests := []struct {
		kind monitoringdomain.ScopeKind
		id   string
		want []string
	}{
		{monitoringdomain.ScopeZone, "zone-a", []string{"srv-1"}},
		{monitoringdomain.ScopePool, "pool-r", []string{"srv-1", "srv-3"}},
		{monitoringdomain.ScopePlatform, "plat-1", []string{"srv-1", "srv-3"}},
	}
	for _, tc := range tests {
		got, err := resolver.ResolveScope(context.Background(), tc.kind, tc.id)
		if err != nil {
			t.Fatalf("ResolveScope(%s, %s): %v", tc.kind, tc.id, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ResolveScope(%s, %s) = %v, want %v", tc.kind, tc.id, got, tc.want)
		}
	}
}

func TestMetricScopeResolver_UnknownGroupingIsScopeNotFound(t *testing.T) {
	resolver := metricScopeResolver{
		zones: scopeZones{}, pools: scopePools{}, platforms: scopePlatforms{}, servers: scopeServers{},
	}
	for _, kind := range []monitoringdomain.ScopeKind{
		monitoringdomain.ScopeZone, monitoringdomain.ScopePool, monitoringdomain.ScopePlatform,
	} {
		if _, err := resolver.ResolveScope(context.Background(), kind, "missing"); !errors.Is(err, monitoringdomain.ErrScopeNotFound) {
			t.Errorf("ResolveScope(%s, missing) error = %v, want ErrScopeNotFound", kind, err)
		}
	}
}
