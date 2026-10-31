package app

import (
	"context"
	"errors"

	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

// SiteReader adapts the site feature's repository to the grouping feature's narrow Exists port,
// translating a site not-found into a false rather than an error so a create can report
// ErrSiteNotFound itself.
type SiteReader struct {
	sites sitedomain.SiteRepository
}

// NewSiteReader builds the grouping feature's view of Site existence.
func NewSiteReader(sites sitedomain.SiteRepository) *SiteReader {
	return &SiteReader{sites: sites}
}

// Exists reports whether the Site is present. A site not-found is a definite false; any other
// error (a transport failure) is propagated so a caller does not treat a failed lookup as absence.
func (r *SiteReader) Exists(ctx context.Context, siteID string) (bool, error) {
	_, err := r.sites.FindByID(ctx, siteID)
	if errors.Is(err, sitedomain.ErrSiteNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
