package application

import (
	"context"
	"sort"
	"sync"

	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

// maxConcurrentCatalogReads bounds how many provisioners one Site-wide catalog read contacts at a
// time (provisioning.md "Image catalogs of a Site"), so a Site with many provisioners neither
// opens a burst of provider connections nor waits for them one by one.
const maxConcurrentCatalogReads = 2

// OSImageCatalogItem is one provisioner Integration's OS Image catalog as the Site-wide read
// reports it: the Integration's id, name, and Site — never its endpoint, settings, or credential —
// and its images exactly as ListOSImagesUseCase returns them. Err is why the catalog could not be
// read, with Images then empty; delivery turns it into the person-readable message the contract
// names, so a driver detail never reaches the wire.
type OSImageCatalogItem struct {
	IntegrationID   string
	IntegrationName string
	SiteID          string
	Images          []OSImageItem
	Err             error
}

// ListOSImageCatalogsUseCase reads the OS Image catalog of every provisioner Integration of a Site
// (or of every Site) in one call. OS Images are reference data any User may read, while listing
// Integrations is an admin route (identity-and-access.md §4.2), so this is how a user who is not an
// admin learns which images exist without being told anything else about the Integrations.
//
// One catalog's failure is that catalog's Err, not the call's: the others are still worth showing,
// the way a client reading each Integration in turn would show them.
type ListOSImageCatalogsUseCase struct {
	sites        sitedomain.SiteRepository
	integrations sitedomain.IntegrationRepository
	images       *ListOSImagesUseCase
}

// NewListOSImageCatalogsUseCase wires the Site and Integration stores the read resolves its scope
// with and the per-Integration catalog read it repeats.
func NewListOSImageCatalogsUseCase(
	sites sitedomain.SiteRepository,
	integrations sitedomain.IntegrationRepository,
	images *ListOSImagesUseCase,
) *ListOSImageCatalogsUseCase {
	return &ListOSImageCatalogsUseCase{sites: sites, integrations: integrations, images: images}
}

// Execute returns one catalog per provisioner Integration of siteID (of every Site when siteID is
// empty), enabled or not, ordered by Site id and then Integration name.
//
// An unknown Site is sitedomain.ErrSiteNotFound, and a store failure resolving the scope is
// returned as-is; everything after that is per catalog. At most maxConcurrentCatalogReads
// provisioners are read at a time, each with ctx, so a canceled request stops the reads that have
// not started and lets the running ones end with their own context error.
func (uc *ListOSImageCatalogsUseCase) Execute(ctx context.Context, siteID string) ([]OSImageCatalogItem, error) {
	if siteID != "" {
		if _, err := uc.sites.FindByID(ctx, siteID); err != nil {
			return nil, err
		}
	}
	integrations, err := uc.integrations.List(ctx, sitedomain.IntegrationFilter{
		SiteID: siteID, Kind: sitedomain.IntegrationKindProvisioner,
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(integrations, func(i, j int) bool {
		if integrations[i].SiteID != integrations[j].SiteID {
			return integrations[i].SiteID < integrations[j].SiteID
		}
		return integrations[i].Name < integrations[j].Name
	})

	catalogs := make([]OSImageCatalogItem, len(integrations))
	slots := make(chan struct{}, maxConcurrentCatalogReads)
	var reads sync.WaitGroup
	for i, integration := range integrations {
		catalogs[i] = OSImageCatalogItem{
			IntegrationID: integration.ID, IntegrationName: integration.Name, SiteID: integration.SiteID,
			Images: []OSImageItem{},
		}
		reads.Add(1)
		// Each goroutine writes only its own element, so the slice needs no lock.
		go func(catalog *OSImageCatalogItem) {
			defer reads.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				catalog.Err = ctx.Err()
				return
			}
			defer func() { <-slots }()
			images, err := uc.images.Execute(ctx, catalog.IntegrationID)
			if err != nil {
				catalog.Err = err
				return
			}
			catalog.Images = images
		}(&catalogs[i])
	}
	reads.Wait()
	return catalogs, nil
}
