package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/AFDEAPAC/swallow/internal/shared/wire"
	sitedomain "github.com/AFDEAPAC/swallow/internal/site/domain"
)

// ServerCounter reports how many servers were projected from an integration, so that
// deleting one that still has servers can be refused.
//
// An interface rather than a direct dependency on the server repository: the site
// context needs one number, not the server model.
type ServerCounter interface {
	CountByIntegration(ctx context.Context, integrationID string) (int, error)
}

type IntegrationService struct {
	integrations sitedomain.IntegrationRepository
	sites        sitedomain.SiteRepository
	servers      ServerCounter
}

func NewIntegrationService(
	integrations sitedomain.IntegrationRepository,
	sites sitedomain.SiteRepository,
	servers ServerCounter,
) *IntegrationService {
	return &IntegrationService{integrations: integrations, sites: sites, servers: servers}
}

// IntegrationItem is the API shape. It has no credential field of any kind, not even a
// redacted one: a field that sometimes holds a secret eventually holds one somewhere it
// should not.
type IntegrationItem struct {
	ID           string            `json:"id"`
	SiteID       string            `json:"siteId"`
	Kind         string            `json:"kind"`
	ProviderKind string            `json:"providerKind"`
	Name         string            `json:"name"`
	Endpoint     string            `json:"endpoint"`
	Enabled      bool              `json:"enabled"`
	Settings     map[string]string `json:"settings"`
	// HasCredential reports whether a credential is stored, which an operator needs
	// to know without the value being readable.
	HasCredential bool          `json:"hasCredential"`
	Sync          SyncStateItem `json:"sync"`
	CreatedAt     string        `json:"createdAt"`
	UpdatedAt     string        `json:"updatedAt"`
}

// SyncStateItem exposes freshness as part of the API. A caller must be able to tell
// "last synced 14 minutes ago" from "up to date" without guessing.
type SyncStateItem struct {
	LastStartedAt   *string `json:"lastStartedAt"`
	LastSucceededAt *string `json:"lastSucceededAt"`
	LastError       *string `json:"lastError"`
}

type CreateIntegrationInput struct {
	SiteID       string
	Kind         string
	ProviderKind string
	Name         string
	Endpoint     string
	Credential   string
	Settings     map[string]string
	Enabled      *bool
}

// ErrInvalidIntegration covers the validation failures the delivery layer turns into a
// 400 with the message attached.
var ErrInvalidIntegration = errors.New("invalid integration")

func (s *IntegrationService) Create(ctx context.Context, input CreateIntegrationInput) (*IntegrationItem, error) {
	kind := sitedomain.IntegrationKind(strings.TrimSpace(input.Kind))
	if !kind.Valid() {
		return nil, fmt.Errorf("%w: kind must be one of %v", ErrInvalidIntegration, sitedomain.ValidIntegrationKinds)
	}
	if !sitedomain.ValidProviderKind(kind, input.ProviderKind) {
		return nil, fmt.Errorf("%w: providerKind for kind %q must be one of %v",
			ErrInvalidIntegration, kind, sitedomain.ProviderKindsFor(kind))
	}
	if strings.TrimSpace(input.Name) == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidIntegration)
	}
	if strings.TrimSpace(input.Endpoint) == "" {
		return nil, fmt.Errorf("%w: endpoint is required", ErrInvalidIntegration)
	}

	if _, err := s.sites.FindByID(ctx, input.SiteID); err != nil {
		return nil, err
	}

	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}

	now := time.Now().UTC()
	integration := &sitedomain.Integration{
		ID:           uuid.NewString(),
		SiteID:       input.SiteID,
		Kind:         kind,
		ProviderKind: input.ProviderKind,
		Name:         strings.TrimSpace(input.Name),
		Endpoint:     strings.TrimSpace(input.Endpoint),
		Enabled:      enabled,
		Settings:     input.Settings,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.integrations.Create(ctx, integration, input.Credential); err != nil {
		return nil, err
	}

	item := toIntegrationItem(integration, input.Credential != "")
	return &item, nil
}

func (s *IntegrationService) List(ctx context.Context, siteID, kind string) ([]IntegrationItem, error) {
	integrations, err := s.integrations.List(ctx, sitedomain.IntegrationFilter{
		SiteID: siteID,
		Kind:   sitedomain.IntegrationKind(kind),
	})
	if err != nil {
		return nil, err
	}

	items := make([]IntegrationItem, 0, len(integrations))
	for _, integration := range integrations {
		items = append(items, toIntegrationItem(integration, s.hasCredential(ctx, integration.ID)))
	}
	return items, nil
}

func (s *IntegrationService) Get(ctx context.Context, id string) (*IntegrationItem, error) {
	integration, err := s.integrations.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	item := toIntegrationItem(integration, s.hasCredential(ctx, id))
	return &item, nil
}

type UpdateIntegrationInput struct {
	Name     *string
	Endpoint *string
	Enabled  *bool
	Settings map[string]string
}

func (s *IntegrationService) Update(ctx context.Context, id string, input UpdateIntegrationInput) (*IntegrationItem, error) {
	integration, err := s.integrations.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		if strings.TrimSpace(*input.Name) == "" {
			return nil, fmt.Errorf("%w: name cannot be empty", ErrInvalidIntegration)
		}
		integration.Name = strings.TrimSpace(*input.Name)
	}
	if input.Endpoint != nil {
		if strings.TrimSpace(*input.Endpoint) == "" {
			return nil, fmt.Errorf("%w: endpoint cannot be empty", ErrInvalidIntegration)
		}
		integration.Endpoint = strings.TrimSpace(*input.Endpoint)
	}
	if input.Enabled != nil {
		integration.Enabled = *input.Enabled
	}
	if input.Settings != nil {
		integration.Settings = input.Settings
	}
	integration.UpdatedAt = time.Now().UTC()

	if err := s.integrations.Update(ctx, integration); err != nil {
		return nil, err
	}

	item := toIntegrationItem(integration, s.hasCredential(ctx, id))
	return &item, nil
}

// ReplaceCredential is the only way a credential changes. There is no read counterpart.
func (s *IntegrationService) ReplaceCredential(ctx context.Context, id, credential string) error {
	if credential == "" {
		return fmt.Errorf("%w: credential cannot be empty", ErrInvalidIntegration)
	}
	if _, err := s.integrations.FindByID(ctx, id); err != nil {
		return err
	}
	return s.integrations.ReplaceCredential(ctx, id, credential)
}

// Delete refuses while servers are still projected from this integration. Those
// servers' identities are keyed on it, so removing it would strand them.
func (s *IntegrationService) Delete(ctx context.Context, id string) error {
	if _, err := s.integrations.FindByID(ctx, id); err != nil {
		return err
	}

	if s.servers != nil {
		count, err := s.servers.CountByIntegration(ctx, id)
		if err != nil {
			return err
		}
		if count > 0 {
			return sitedomain.ErrIntegrationHasServers
		}
	}

	return s.integrations.Delete(ctx, id)
}

// hasCredential reports credential presence without exposing the value. A failure to
// read is reported as absent rather than propagated: this field is informational, and
// a listing should not fail because one credential cannot be decrypted.
func (s *IntegrationService) hasCredential(ctx context.Context, id string) bool {
	_, err := s.integrations.Credential(ctx, id)
	return err == nil
}

func toIntegrationItem(integration *sitedomain.Integration, hasCredential bool) IntegrationItem {
	settings := integration.Settings
	if settings == nil {
		settings = map[string]string{}
	}

	return IntegrationItem{
		ID:            integration.ID,
		SiteID:        integration.SiteID,
		Kind:          string(integration.Kind),
		ProviderKind:  integration.ProviderKind,
		Name:          integration.Name,
		Endpoint:      integration.Endpoint,
		Enabled:       integration.Enabled,
		Settings:      settings,
		HasCredential: hasCredential,
		Sync: SyncStateItem{
			LastStartedAt:   timePtr(integration.Sync.LastStartedAt),
			LastSucceededAt: timePtr(integration.Sync.LastSucceededAt),
			LastError:       wire.String(integration.Sync.LastError),
		},
		CreatedAt: wire.Time(integration.CreatedAt),
		UpdatedAt: wire.Time(integration.UpdatedAt),
	}
}

func timePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return wire.TimePtr(*t)
}
