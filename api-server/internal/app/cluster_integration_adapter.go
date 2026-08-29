package app

import (
	"context"
	"errors"

	clusterdomain "github.com/maple52046/swallow/internal/cluster/domain"
	clusterinfra "github.com/maple52046/swallow/internal/cluster/infra"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// managedClusterIntegrationCleaner removes only integrations proven to be Swallow-owned.
type managedClusterIntegrationCleaner struct {
	integrations sitedomain.IntegrationRepository
}

func (c managedClusterIntegrationCleaner) DeleteForCluster(
	ctx context.Context,
	cluster *clusterdomain.Cluster,
	allowLegacySignature bool,
) error {
	integrationID := cluster.OwnedIntegrationID
	if integrationID == "" && allowLegacySignature && cluster.IntegrationID != "" {
		integration, err := c.integrations.FindByID(ctx, cluster.IntegrationID)
		if errors.Is(err, sitedomain.ErrIntegrationNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if integration.SiteID != cluster.SiteID ||
			integration.Kind != sitedomain.IntegrationKindCluster ||
			integration.ProviderKind != sitedomain.ProviderKindKubernetes ||
			integration.Name != cluster.Name+" (deployed)" ||
			integration.Settings[clusterinfra.SettingControllerLeaseDiscovery] != "true" {
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
