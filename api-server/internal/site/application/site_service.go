// Package application coordinates site and integration management.
//
// Sites and integrations are plain CRUD with no workflow to name, so each aggregate
// gets one service with methods rather than a separate use case type per operation.
// The one place with real logic is deletion, which has to refuse to strand the records
// that reference what is being deleted.
package application

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	sitedomain "github.com/AFDEAPAC/swallow/internal/site/domain"
)

type SiteService struct {
	sites        sitedomain.SiteRepository
	integrations sitedomain.IntegrationRepository
}

func NewSiteService(
	sites sitedomain.SiteRepository,
	integrations sitedomain.IntegrationRepository,
) *SiteService {
	return &SiteService{sites: sites, integrations: integrations}
}

type SiteItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type CreateSiteInput struct {
	Name        string
	Description string
}

func (s *SiteService) Create(ctx context.Context, input CreateSiteInput) (*SiteItem, error) {
	now := time.Now().UTC()
	site := &sitedomain.Site{
		ID:          uuid.NewString(),
		Name:        strings.TrimSpace(input.Name),
		Description: input.Description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.sites.Create(ctx, site); err != nil {
		return nil, err
	}

	item := toSiteItem(site)
	return &item, nil
}

func (s *SiteService) List(ctx context.Context) ([]SiteItem, error) {
	sites, err := s.sites.List(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]SiteItem, 0, len(sites))
	for _, site := range sites {
		items = append(items, toSiteItem(site))
	}
	return items, nil
}

func (s *SiteService) Get(ctx context.Context, id string) (*SiteItem, error) {
	site, err := s.sites.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	item := toSiteItem(site)
	return &item, nil
}

type UpdateSiteInput struct {
	Name        *string
	Description *string
}

func (s *SiteService) Update(ctx context.Context, id string, input UpdateSiteInput) (*SiteItem, error) {
	site, err := s.sites.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		site.Name = strings.TrimSpace(*input.Name)
	}
	if input.Description != nil {
		site.Description = *input.Description
	}
	site.UpdatedAt = time.Now().UTC()

	if err := s.sites.Update(ctx, site); err != nil {
		return nil, err
	}

	item := toSiteItem(site)
	return &item, nil
}

// Delete refuses while integrations still reference the site. Cascading would remove
// the integrations and, through them, orphan every server projected from them — a lot
// of history to lose to one click.
func (s *SiteService) Delete(ctx context.Context, id string) error {
	if _, err := s.sites.FindByID(ctx, id); err != nil {
		return err
	}

	integrations, err := s.integrations.List(ctx, sitedomain.IntegrationFilter{SiteID: id})
	if err != nil {
		return err
	}
	if len(integrations) > 0 {
		return sitedomain.ErrSiteHasIntegrations
	}

	return s.sites.Delete(ctx, id)
}

func toSiteItem(site *sitedomain.Site) SiteItem {
	return SiteItem{
		ID:          site.ID,
		Name:        site.Name,
		Description: site.Description,
		CreatedAt:   site.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   site.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
