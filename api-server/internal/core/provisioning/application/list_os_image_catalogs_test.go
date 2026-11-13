package application

import (
	"context"
	"errors"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

// catalogSiteRepo knows the Sites of a test; only FindByID is used by the catalog read.
type catalogSiteRepo struct {
	sitedomain.SiteRepository
	sites map[string]bool
}

func (r catalogSiteRepo) FindByID(_ context.Context, id string) (*sitedomain.Site, error) {
	if !r.sites[id] {
		return nil, sitedomain.ErrSiteNotFound
	}
	return &sitedomain.Site{ID: id}, nil
}

// catalogIntegrationRepo filters its Integrations like the store does; only List is used.
type catalogIntegrationRepo struct {
	sitedomain.IntegrationRepository
	integrations []*sitedomain.Integration
}

func (r catalogIntegrationRepo) List(_ context.Context, filter sitedomain.IntegrationFilter) ([]*sitedomain.Integration, error) {
	var matches []*sitedomain.Integration
	for _, integration := range r.integrations {
		if (filter.SiteID == "" || integration.SiteID == filter.SiteID) && (filter.Kind == "" || integration.Kind == filter.Kind) {
			matches = append(matches, integration)
		}
	}
	return matches, nil
}

// catalogProviderFactory opens one provider per Integration id and fails the ids in failing.
type catalogProviderFactory struct {
	providers map[string]provisioningdomain.OSProvisioningProvider
	failing   map[string]error
}

func (f catalogProviderFactory) For(_ context.Context, integrationID string) (provisioningdomain.OSProvisioningProvider, error) {
	if err := f.failing[integrationID]; err != nil {
		return nil, err
	}
	return f.providers[integrationID], nil
}

func newCatalogUseCase(factory catalogProviderFactory) *ListOSImageCatalogsUseCase {
	integrations := catalogIntegrationRepo{integrations: []*sitedomain.Integration{
		{ID: "west", SiteID: "site-b", Kind: sitedomain.IntegrationKindProvisioner, Name: "maas-west"},
		{ID: "repo", SiteID: "site-a", Kind: sitedomain.IntegrationKindRepository, Name: "pulp"},
		{ID: "south", SiteID: "site-a", Kind: sitedomain.IntegrationKindProvisioner, Name: "maas-south"},
		{ID: "north", SiteID: "site-a", Kind: sitedomain.IntegrationKindProvisioner, Name: "maas-north"},
	}}
	images := NewListOSImagesUseCase(factory, newOSImageMetadataRepoFake(), newOSImageVerificationRepoFake())
	return NewListOSImageCatalogsUseCase(catalogSiteRepo{sites: map[string]bool{"site-a": true, "site-b": true}}, integrations, images)
}

// Every provisioner of the Site is read, in Site and name order, and one that cannot be read is
// that catalog's error while the others keep their images.
func TestListOSImageCatalogsReadsEveryProvisionerOfASite(t *testing.T) {
	unreachable := errors.New("provider unreachable")
	uc := newCatalogUseCase(catalogProviderFactory{
		providers: map[string]provisioningdomain.OSProvisioningProvider{
			"north": &osImageBaseProvider{images: []*provisioningdomain.OSImage{{ID: "ubuntu/noble", Name: "Ubuntu 24.04", OSSystem: "ubuntu", Release: "noble", Architecture: "amd64"}}},
		},
		failing: map[string]error{"south": unreachable},
	})

	catalogs, err := uc.Execute(context.Background(), "site-a")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(catalogs) != 2 || catalogs[0].IntegrationID != "north" || catalogs[1].IntegrationID != "south" {
		t.Fatalf("catalogs = %+v, want the Site's two provisioners, maas-north then maas-south", catalogs)
	}
	north, south := catalogs[0], catalogs[1]
	if north.Err != nil || len(north.Images) != 1 || north.Images[0].ID != "ubuntu/noble" || north.IntegrationName != "maas-north" || north.SiteID != "site-a" {
		t.Errorf("maas-north = %+v, want its one image and no error", north)
	}
	if !errors.Is(south.Err, unreachable) || south.Images == nil || len(south.Images) != 0 {
		t.Errorf("maas-south = %+v, want the provider error and an empty (non-nil) image list", south)
	}
}

// Without a Site every Site's provisioners are read; an unknown Site is refused before any read.
func TestListOSImageCatalogsScope(t *testing.T) {
	uc := newCatalogUseCase(catalogProviderFactory{providers: map[string]provisioningdomain.OSProvisioningProvider{
		"north": &osImageBaseProvider{}, "south": &osImageBaseProvider{}, "west": &osImageBaseProvider{},
	}})

	catalogs, err := uc.Execute(context.Background(), "")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var ids []string
	for _, catalog := range catalogs {
		ids = append(ids, catalog.IntegrationID)
	}
	if len(ids) != 3 || ids[0] != "north" || ids[1] != "south" || ids[2] != "west" {
		t.Errorf("catalog ids = %v, want [north south west] (site-a by name, then site-b)", ids)
	}

	if _, err := uc.Execute(context.Background(), "site-z"); !errors.Is(err, sitedomain.ErrSiteNotFound) {
		t.Errorf("Execute(unknown Site) error = %v, want ErrSiteNotFound", err)
	}
}
