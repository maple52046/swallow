package infra

import (
	"context"

	infradomain "github.com/maple52046/swallow/internal/infrastructure/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// GroupingRealizer realizes swallow-owned Zone/Pool intent in a Site's provisioner (decision 029).
//
// It reuses the provisioning feature's ProviderFactory rather than opening its own transport, so
// there is a single source of truth for talking to a provisioner. All provider vocabulary stays
// inside the provisioning adapter; this type only resolves which provisioner a Site uses and
// whether it can group, then delegates to the optional GroupingController capability.
type GroupingRealizer struct {
	integrations sitedomain.IntegrationRepository
	providers    provisioningdomain.ProviderFactory
}

// NewGroupingRealizer wires the realizer to the site integration repository (to find a Site's
// provisioner) and the provisioning provider factory (to build and cache the provider client).
func NewGroupingRealizer(
	integrations sitedomain.IntegrationRepository,
	providers provisioningdomain.ProviderFactory,
) *GroupingRealizer {
	return &GroupingRealizer{integrations: integrations, providers: providers}
}

// The catalog methods below satisfy the domain GroupingRealizer contract by delegating to
// realizeBySite: they resolve the Site's grouping-capable provisioner and run one controller
// operation, returning realized=false (not an error) when the Site has no such provisioner.

func (r *GroupingRealizer) EnsureZone(ctx context.Context, siteID, name, description string) (bool, error) {
	return r.realizeBySite(ctx, siteID, func(gc provisioningdomain.GroupingController) error {
		return gc.EnsureZone(ctx, name, description)
	})
}

func (r *GroupingRealizer) RenameZone(ctx context.Context, siteID, currentName, newName, description string) (bool, error) {
	return r.realizeBySite(ctx, siteID, func(gc provisioningdomain.GroupingController) error {
		return gc.RenameZone(ctx, currentName, newName, description)
	})
}

func (r *GroupingRealizer) DeleteZone(ctx context.Context, siteID, name string) (bool, error) {
	return r.realizeBySite(ctx, siteID, func(gc provisioningdomain.GroupingController) error {
		return gc.DeleteZone(ctx, name)
	})
}

func (r *GroupingRealizer) EnsurePool(ctx context.Context, siteID, name, description string) (bool, error) {
	return r.realizeBySite(ctx, siteID, func(gc provisioningdomain.GroupingController) error {
		return gc.EnsurePool(ctx, name, description)
	})
}

func (r *GroupingRealizer) RenamePool(ctx context.Context, siteID, currentName, newName, description string) (bool, error) {
	return r.realizeBySite(ctx, siteID, func(gc provisioningdomain.GroupingController) error {
		return gc.RenamePool(ctx, currentName, newName, description)
	})
}

func (r *GroupingRealizer) DeletePool(ctx context.Context, siteID, name string) (bool, error) {
	return r.realizeBySite(ctx, siteID, func(gc provisioningdomain.GroupingController) error {
		return gc.DeletePool(ctx, name)
	})
}

// AssignServerZone drives the Server's own provisioner to set its zone. Unlike catalog writes, a
// provisioner without the grouping capability is an error, because a placement always targets one.
func (r *GroupingRealizer) AssignServerZone(ctx context.Context, integrationID, providerMachineID, zoneName string) error {
	gc, err := r.controllerFor(ctx, integrationID)
	if err != nil {
		return err
	}
	_, err = gc.SetMachineZone(ctx, providerMachineID, zoneName)
	return err
}

// AssignServerPool drives the Server's own provisioner to set its resource pool.
func (r *GroupingRealizer) AssignServerPool(ctx context.Context, integrationID, providerMachineID, poolName string) error {
	gc, err := r.controllerFor(ctx, integrationID)
	if err != nil {
		return err
	}
	_, err = gc.SetMachinePool(ctx, providerMachineID, poolName)
	return err
}

// realizeBySite resolves the Site's grouping-capable provisioner and runs op against it.
//
// It returns (false, nil) when the Site has no grouping-capable provisioner, which is how a
// swallow-only Zone/Pool stays valid: catalog management does not require a provider. A provider
// that is present but fails the operation returns (false, err) carrying the provider error.
func (r *GroupingRealizer) realizeBySite(
	ctx context.Context,
	siteID string,
	op func(provisioningdomain.GroupingController) error,
) (bool, error) {
	gc, err := r.siteController(ctx, siteID)
	if err != nil {
		return false, err
	}
	if gc == nil {
		return false, nil
	}
	if err := op(gc); err != nil {
		return false, err
	}
	return true, nil
}

// siteController returns the grouping controller for the Site's provisioner, or nil when the Site
// has no provisioner or its provisioner cannot group. A provisioner that exists but cannot be
// built (for example a missing credential) returns an error, so a real misconfiguration is
// surfaced rather than mistaken for "no capable provisioner".
func (r *GroupingRealizer) siteController(ctx context.Context, siteID string) (provisioningdomain.GroupingController, error) {
	integrations, err := r.integrations.List(ctx, sitedomain.IntegrationFilter{
		SiteID:      siteID,
		Kind:        sitedomain.IntegrationKindProvisioner,
		EnabledOnly: true,
	})
	if err != nil {
		return nil, err
	}
	if len(integrations) == 0 {
		return nil, nil
	}

	// A Site has one provisioner (glossary: MAAS, one per site); use the first enabled one.
	provider, err := r.providers.For(ctx, integrations[0].ID)
	if err != nil {
		return nil, err
	}
	gc, ok := provider.(provisioningdomain.GroupingController)
	if !ok || !provider.Capabilities().Grouping {
		return nil, nil
	}
	return gc, nil
}

// controllerFor builds the grouping controller for one specific provisioner integration, used by
// Server placement where the exact integration is already known from the Server's source. A
// provisioner that cannot group is ErrProviderGroupingUnsupported.
func (r *GroupingRealizer) controllerFor(ctx context.Context, integrationID string) (provisioningdomain.GroupingController, error) {
	provider, err := r.providers.For(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	gc, ok := provider.(provisioningdomain.GroupingController)
	if !ok || !provider.Capabilities().Grouping {
		return nil, infradomain.ErrProviderGroupingUnsupported
	}
	return gc, nil
}
