package app

import (
	"context"
	"errors"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	platforminfra "github.com/maple52046/swallow/internal/platform/infra"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// managedPlatformIntegrationCleaner removes only integrations proven to be Swallow-owned.
type managedPlatformIntegrationCleaner struct {
	integrations sitedomain.IntegrationRepository
}

func (c managedPlatformIntegrationCleaner) DeleteForPlatform(
	ctx context.Context,
	platform *platformdomain.Platform,
	allowLegacySignature bool,
) error {
	integrationID := platform.OwnedIntegrationID
	if integrationID == "" && allowLegacySignature && platform.IntegrationID != "" {
		integration, err := c.integrations.FindByID(ctx, platform.IntegrationID)
		if errors.Is(err, sitedomain.ErrIntegrationNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if integration.SiteID != platform.SiteID ||
			integration.Kind != sitedomain.IntegrationKindPlatform ||
			integration.ProviderKind != sitedomain.ProviderKindKubernetes ||
			integration.Name != platform.Name+" (deployed)" ||
			integration.Settings[platforminfra.SettingControllerLeaseDiscovery] != "true" {
			return nil
		}
		integrationID = integration.ID
	}
	if integrationID == "" {
		return nil
	}
	if err := c.integrations.Delete(ctx, integrationID); err != nil &&
		!errors.Is(err, sitedomain.ErrIntegrationNotFound) {
		return err
	}
	return nil
}
