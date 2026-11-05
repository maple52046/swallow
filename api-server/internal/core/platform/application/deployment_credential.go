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

// PlatformIntegrations is the Integration store the platform context records a deployed Platform's
// credentials in and reads them back from: a Site's IntegrationRepository together with the
// operator credential the store keeps beside each Integration's credential.
//
// Implementations seal both credentials at rest and never put either on the Integration struct;
// production uses the Mongo Integration repository, and in-memory implementations are for tests.
type PlatformIntegrations interface {
	sitedomain.IntegrationRepository
	sitedomain.OperatorCredentialRepository
}

// DeploymentCredentialService records the credential a platform deployment produced by
// creating the platform's read integration and attaching it, then reading membership once.
//
// It is the platform side of the operation completion hook: the composition root adapts a
// successful deploy-kubernetes operation into a call here, so the platform context never
// depends on the operation context.
type DeploymentCredentialService struct {
	transactor   store.Transactor
	platforms    platformdomain.PlatformRepository
	integrations PlatformIntegrations
	membership   *MembershipSyncUseCase
}

// NewDeploymentCredentialService constructs the credential recorder. The Integration, both of its
// credentials, and the Platform that names it are written in one transaction through transactor.
func NewDeploymentCredentialService(
	transactor store.Transactor,
	platforms platformdomain.PlatformRepository,
	integrations PlatformIntegrations,
	membership *MembershipSyncUseCase,
) *DeploymentCredentialService {
	return &DeploymentCredentialService{transactor: transactor, platforms: platforms, integrations: integrations, membership: membership}
}

// attach stores integration with its sealed credential and operator credential and makes it the
// Platform's owned read Integration, in one transaction: a failure leaves neither an orphaned
// Integration, nor one holding only some of the credentials, nor a Platform naming one that does
// not exist, so the completion hook can simply retry.
func (s *DeploymentCredentialService) attach(
	ctx context.Context,
	platform *platformdomain.Platform,
	integration *sitedomain.Integration,
	credential platformdomain.Credential,
) error {
	return s.transactor.InTransaction(ctx, func(ctx context.Context) error {
		if err := s.integrations.Create(ctx, integration, credential.Token); err != nil {
			return err
		}
		if credential.OperatorToken != "" {
			if err := s.integrations.ReplaceOperatorCredential(ctx, integration.ID, credential.OperatorToken); err != nil {
				return err
			}
		}
		platform.IntegrationID = integration.ID
		platform.OwnedIntegrationID = integration.ID
		platform.UpdatedAt = integration.UpdatedAt
		return s.platforms.Update(ctx, platform)
	})
}

// refresh replaces the endpoint, settings, and both credentials of the Integration the Platform
// already owns, in one transaction, so a rerun of its deployment records the credentials again
// without a second Integration of the same name. It reports false when the Platform owns none.
//
// The operator credential is replaced even when the new result has none, which removes the stored
// one: the Integration's two credentials always come from the same deployment, because one left
// from an earlier run may belong to a cluster or ServiceAccount the newer run replaced. Without an
// operator credential the Platform's operator requests use the new read credential
// (OperatorConnectionSource).
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
		if err := s.integrations.ReplaceOperatorCredential(ctx, existing.ID, credential.OperatorToken); err != nil {
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

// Record attaches a freshly deployed Platform's credentials and syncs its membership. A Platform
// that already owns its read Integration (a rerun of its deployment) gets that Integration
// refreshed instead.
//
// The token is stored as a sealed platform-kind Integration credential whose provider kind is the
// Platform type, which selects the Platform module's reader, and the Credential's settings are the
// Integration's: the module that read the credential decided them. The operator token, when the
// deployment returned one, is sealed as the same Integration's operator credential in the same
// transaction; neither is ever logged or returned. A first membership read is triggered so the
// Platform's members appear without waiting for the background interval; its failure is not fatal
// because the Integration is stored and the background sync retries.
func (s *DeploymentCredentialService) Record(ctx context.Context, platformID string, credential platformdomain.Credential) error {
	if strings.TrimSpace(credential.Endpoint) == "" || strings.TrimSpace(credential.Token) == "" {
		return fmt.Errorf("platform deployment returned no usable credential")
	}
	if strings.TrimSpace(credential.OperatorToken) == "" {
		credential.OperatorToken = ""
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
	if err := s.attach(ctx, platform, integration, credential); err != nil {
		return err
	}
	// After the commit: the first membership read calls the Platform's API.
	_, _ = s.membership.Execute(ctx, platform.ID)
	return nil
}
