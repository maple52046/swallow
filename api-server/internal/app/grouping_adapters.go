package app

import (
	"context"
	"errors"

	infradomain "github.com/maple52046/swallow/internal/core/infrastructure/domain"
	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

// groupingTargets implements infradomain.GroupingTargets over the site Integration repository: a
// Site's Zones and Pools are realized on every enabled provisioner Integration of that Site.
type groupingTargets struct {
	integrations sitedomain.IntegrationRepository
}

var _ infradomain.GroupingTargets = groupingTargets{}

func (t groupingTargets) ForSite(ctx context.Context, siteID string) ([]string, error) {
	integrations, err := t.integrations.List(ctx, sitedomain.IntegrationFilter{
		SiteID: siteID, Kind: sitedomain.IntegrationKindProvisioner, EnabledOnly: true,
	})
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(integrations))
	for i, integration := range integrations {
		ids[i] = integration.ID
	}
	return ids, nil
}

func (t groupingTargets) Sites(ctx context.Context) ([]string, error) {
	integrations, err := t.integrations.List(ctx, sitedomain.IntegrationFilter{
		Kind: sitedomain.IntegrationKindProvisioner, EnabledOnly: true,
	})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var sites []string
	for _, integration := range integrations {
		if !seen[integration.SiteID] {
			seen[integration.SiteID] = true
			sites = append(sites, integration.SiteID)
		}
	}
	return sites, nil
}

// serverPlacements implements infradomain.ServerPlacements over the Server spec, translating the
// server domain's errors into the grouping feature's own.
type serverPlacements struct {
	servers serverdomain.ServerRepository
	specs   serverdomain.ServerConvergenceStore
}

var _ infradomain.ServerPlacements = serverPlacements{}

func (p serverPlacements) Read(ctx context.Context, serverID string) (*infradomain.ServerPlacement, error) {
	server, err := p.servers.FindByID(ctx, serverID)
	if errors.Is(err, serverdomain.ErrServerNotFound) {
		return nil, infradomain.ErrServerNotFound
	}
	if err != nil {
		return nil, err
	}
	return &infradomain.ServerPlacement{
		ServerID: server.ID, SiteID: server.Source.SiteID, Generation: server.Generation,
		ZoneID: server.Spec.ZoneID, PoolID: server.Spec.PoolID,
	}, nil
}

// Write re-reads the Server so the tags in its spec are the ones at expectedGeneration; the
// conditional spec write then refuses if anything changed in between.
func (p serverPlacements) Write(ctx context.Context, serverID string, expectedGeneration int64, zoneID, poolID string) (int64, error) {
	server, err := p.servers.FindByID(ctx, serverID)
	if errors.Is(err, serverdomain.ErrServerNotFound) {
		return 0, infradomain.ErrServerNotFound
	}
	if err != nil {
		return 0, err
	}
	if server.Generation != expectedGeneration {
		return 0, infradomain.ErrPlacementConflict
	}
	spec := server.Spec
	spec.ZoneID, spec.PoolID = zoneID, poolID
	generation, err := p.specs.UpdateSpec(ctx, serverID, expectedGeneration, spec)
	switch {
	case errors.Is(err, serverdomain.ErrServerNotFound):
		return 0, infradomain.ErrServerNotFound
	case errors.Is(err, serverdomain.ErrSpecConflict):
		return 0, infradomain.ErrPlacementConflict
	}
	return generation, err
}

// placementReferences implements infradomain.PlacementReferences over the Server specs.
type placementReferences struct {
	specs serverdomain.ServerConvergenceStore
}

var _ infradomain.PlacementReferences = placementReferences{}

func (r placementReferences) CountZone(ctx context.Context, zoneID string) (int, error) {
	return r.specs.CountSpecPlacement(ctx, zoneID, "")
}

func (r placementReferences) CountPool(ctx context.Context, poolID string) (int, error) {
	return r.specs.CountSpecPlacement(ctx, "", poolID)
}

// serverGroupCatalog implements provisioningapp.ServerGroupCatalog over the Zone and Pool
// repositories: a group's name on a provisioner is its realization there, so a Server follows a
// rename only once the catalog controller has renamed the group on its provisioner.
type serverGroupCatalog struct {
	zones infradomain.ZoneRepository
	pools infradomain.PoolRepository
}

var _ provisioningapp.ServerGroupCatalog = serverGroupCatalog{}

func (c serverGroupCatalog) ZoneOn(ctx context.Context, zoneID, integrationID string) (string, bool, bool, error) {
	zone, err := c.zones.FindByID(ctx, zoneID)
	if errors.Is(err, infradomain.ErrZoneNotFound) {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	name := zone.Status.Realization(integrationID).ProviderName
	return name, name != "", true, nil
}

func (c serverGroupCatalog) PoolOn(ctx context.Context, poolID, integrationID string) (string, bool, bool, error) {
	pool, err := c.pools.FindByID(ctx, poolID)
	if errors.Is(err, infradomain.ErrPoolNotFound) {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	name := pool.Status.Realization(integrationID).ProviderName
	return name, name != "", true, nil
}

func (c serverGroupCatalog) ZoneNamed(ctx context.Context, siteID, integrationID, name string) (string, bool, error) {
	zones, err := c.zones.List(ctx, siteID)
	if err != nil {
		return "", false, err
	}
	for _, zone := range zones {
		if zone.Name == name || zone.Status.Realization(integrationID).ProviderName == name {
			return zone.ID, true, nil
		}
	}
	return "", false, nil
}

func (c serverGroupCatalog) PoolNamed(ctx context.Context, siteID, integrationID, name string) (string, bool, error) {
	pools, err := c.pools.List(ctx, siteID)
	if err != nil {
		return "", false, err
	}
	for _, pool := range pools {
		if pool.Name == name || pool.Status.Realization(integrationID).ProviderName == name {
			return pool.ID, true, nil
		}
	}
	return "", false, nil
}
