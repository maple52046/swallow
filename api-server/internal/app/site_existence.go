package app

import (
	"context"
	"errors"

	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

// siteExistence implements monitoringapp.SiteLookup over the Site repository, so the monitoring
// context checks a Site id without importing the site context's application.
type siteExistence struct {
	sites sitedomain.SiteRepository
}

// SiteExists implements monitoringapp.SiteLookup; a store failure is returned, not read as absent.
func (s siteExistence) SiteExists(ctx context.Context, siteID string) (bool, error) {
	_, err := s.sites.FindByID(ctx, siteID)
	if errors.Is(err, sitedomain.ErrSiteNotFound) {
		return false, nil
	}
	return err == nil, err
}
