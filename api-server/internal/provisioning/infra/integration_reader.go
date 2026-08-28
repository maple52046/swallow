package infra

import (
	"context"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// IntegrationReader adapts the site-owned repositories to provisioning's narrow port.
type IntegrationReader struct {
	integrations sitedomain.IntegrationRepository
	sites        sitedomain.SiteRepository
}

// NewIntegrationReader creates the provisioning view of integrations.
func NewIntegrationReader(
	integrations sitedomain.IntegrationRepository,
	sites sitedomain.SiteRepository,
) *IntegrationReader {
	return &IntegrationReader{integrations: integrations, sites: sites}
}

// Find returns one integration only when it is a provisioner.
func (r *IntegrationReader) Find(ctx context.Context, id string) (*provisioningdomain.ProvisionerIntegration, error) {
	integration, err := r.integrations.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if integration.Kind != sitedomain.IntegrationKindProvisioner {
		return nil, provisioningdomain.ErrIntegrationNotProvisioner
	}
	return &provisioningdomain.ProvisionerIntegration{ID: integration.ID, SiteID: integration.SiteID}, nil
}

// ListBySite returns provisioner integrations for an existing site.
func (r *IntegrationReader) ListBySite(ctx context.Context, siteID string) ([]provisioningdomain.ProvisionerIntegration, error) {
	if _, err := r.sites.FindByID(ctx, siteID); err != nil {
		return nil, err
	}
	integrations, err := r.integrations.List(ctx, sitedomain.IntegrationFilter{
		SiteID: siteID,
		Kind:   sitedomain.IntegrationKindProvisioner,
	})
	if err != nil {
		return nil, err
	}
	items := make([]provisioningdomain.ProvisionerIntegration, 0, len(integrations))
	for _, integration := range integrations {
		items = append(items, provisioningdomain.ProvisionerIntegration{
			ID: integration.ID, SiteID: integration.SiteID,
		})
	}
	return items, nil
}
