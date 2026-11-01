package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	"github.com/maple52046/swallow/internal/core/store"
)

// DeploymentCredentialService records the credential a platform deployment produced by
// creating the platform's read integration and attaching it, then reading membership once.
//
// It is the platform side of the operation completion hook: the composition root adapts a
// successful deploy-kubernetes operation into a call here, so the platform context never
// depends on the operation context.
type DeploymentCredentialService struct {
	transactor   store.Transactor
	platforms    platformdomain.PlatformRepository
	integrations sitedomain.IntegrationRepository
	membership   *MembershipSyncUseCase
}

// NewDeploymentCredentialService constructs the credential recorder. The Integration and the
// Platform that names it are written in one transaction through transactor.
func NewDeploymentCredentialService(
	transactor store.Transactor,
	platforms platformdomain.PlatformRepository,
	integrations sitedomain.IntegrationRepository,
	membership *MembershipSyncUseCase,
) *DeploymentCredentialService {
	return &DeploymentCredentialService{transactor: transactor, platforms: platforms, integrations: integrations, membership: membership}
}

// attach stores integration with its sealed token and makes it the Platform's owned read
// Integration, in one transaction: a failure leaves neither an orphaned Integration nor a
// Platform naming one that does not exist, so the completion hook can simply retry.
func (s *DeploymentCredentialService) attach(ctx context.Context, platform *platformdomain.Platform, integration *sitedomain.Integration, token string) error {
	return s.transactor.InTransaction(ctx, func(ctx context.Context) error {
		if err := s.integrations.Create(ctx, integration, token); err != nil {
			return err
		}
		platform.IntegrationID = integration.ID
		platform.OwnedIntegrationID = integration.ID
		platform.UpdatedAt = integration.UpdatedAt
		return s.platforms.Update(ctx, platform)
	})
}

// refresh replaces the endpoint, settings, and credential of the Integration the Platform already
// owns, in one transaction, so a rerun of its deployment records the credential again without a
// second Integration of the same name. It reports false when the Platform owns none.
func (s *DeploymentCredentialService) refresh(
	ctx context.Context,
	platform *platformdomain.Platform,
	credential platformdomain.Credential,
	settings map[string]string,
	now time.Time,
) (bool, error) {
	if platform.OwnedIntegrationID == "" {
		return false, nil
	}
	existing, err := s.integrations.FindByID(ctx, platform.OwnedIntegrationID)
	if errors.Is(err, sitedomain.ErrIntegrationNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	existing.Endpoint = credential.Endpoint
	existing.Settings = settings
	existing.UpdatedAt = now
	return true, s.transactor.InTransaction(ctx, func(ctx context.Context) error {
		if err := s.integrations.Update(ctx, existing); err != nil {
			return err
		}
		if err := s.integrations.ReplaceCredential(ctx, existing.ID, credential.Token); err != nil {
			return err
		}
		if platform.IntegrationID == existing.ID {
			return nil
		}
		platform.IntegrationID = existing.ID
		platform.UpdatedAt = now
		return s.platforms.Update(ctx, platform)
	})
}

// Record attaches a freshly deployed Platform's read credential and syncs its membership. A
// Platform that already owns its read Integration (a rerun of its deployment) gets that
// Integration refreshed instead.
//
// The token is stored as a sealed platform-kind Integration credential whose provider kind is the
// Platform type, which selects the Platform module's reader, and the Credential's settings are the
// Integration's: the module that read the credential decided them. A first membership read is
// triggered so the Platform's members appear without waiting for the background interval; its
// failure is not fatal because the Integration is stored and the background sync retries.
func (s *DeploymentCredentialService) Record(ctx context.Context, platformID string, credential platformdomain.Credential) error {
	if strings.TrimSpace(credential.Endpoint) == "" || strings.TrimSpace(credential.Token) == "" {
		return fmt.Errorf("platform deployment returned no usable credential")
	}
	platform, err := s.platforms.FindByID(ctx, platformID)
	if err != nil {
		return err
	}
	settings := make(map[string]string, len(credential.Settings))
	for key, value := range credential.Settings {
		settings[key] = value
	}
	now := time.Now().UTC()
	if refreshed, err := s.refresh(ctx, platform, credential, settings, now); err != nil || refreshed {
		if err != nil {
			return err
		}
		_, _ = s.membership.Execute(ctx, platform.ID)
		return nil
	}
	integration := &sitedomain.Integration{
		ID:           uuid.NewString(),
		SiteID:       platform.SiteID,
		Kind:         sitedomain.IntegrationKindPlatform,
		ProviderKind: string(platform.Type),
		Name:         platform.Name + " (deployed)",
		Endpoint:     credential.Endpoint,
		Enabled:      true,
		Settings:     settings,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.attach(ctx, platform, integration, credential.Token); err != nil {
		return err
	}
	// After the commit: the first membership read calls the Platform's API.
	_, _ = s.membership.Execute(ctx, platform.ID)
	return nil
}
