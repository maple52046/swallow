package infra

import (
	"context"
	"errors"

	infradomain "github.com/maple52046/swallow/internal/infrastructure/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
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

// ServerLocator adapts the server feature's projection to the grouping feature's placement port.
// It maps the server domain's not-found onto the grouping feature's own ErrServerNotFound so the
// grouping domain does not depend on the server error vocabulary.
type ServerLocator struct {
	servers serverdomain.ServerRepository
}

// NewServerLocator builds the grouping feature's view of a Server placement target.
func NewServerLocator(servers serverdomain.ServerRepository) *ServerLocator {
	return &ServerLocator{servers: servers}
}

// Locate resolves a Server id into the Site, provisioner integration, provider machine id, and
// currently observed group names a placement needs. An unknown Server is ErrServerNotFound.
func (r *ServerLocator) Locate(ctx context.Context, serverID string) (*infradomain.ServerPlacementTarget, error) {
	server, err := r.servers.FindByID(ctx, serverID)
	if errors.Is(err, serverdomain.ErrServerNotFound) {
		return nil, infradomain.ErrServerNotFound
	}
	if err != nil {
		return nil, err
	}
	return &infradomain.ServerPlacementTarget{
		ServerID:          server.ID,
		SiteID:            server.Source.SiteID,
		IntegrationID:     server.Source.IntegrationID,
		ProviderMachineID: server.Source.ProviderMachineID,
		ObservedZone:      server.Observed.ProviderZone,
		ObservedPool:      server.Observed.ProviderResourcePool,
	}, nil
}
