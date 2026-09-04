package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	platforminfra "github.com/maple52046/swallow/internal/platform/infra"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// DeploymentCredential is what a successful platform deployment produced: how to reach the
// new platform's API and a bearer token to read it with.
type DeploymentCredential struct {
	APIEndpoint   string
	Token         string
	CACertificate string
}

// DeploymentCredentialService records the credential a platform deployment produced by
// creating the platform's read integration and attaching it, then reading membership once.
//
// It is the platform side of the operation completion hook: the composition root adapts a
// successful deploy-kubernetes operation into a call here, so the platform context never
// depends on the operation context.
type DeploymentCredentialService struct {
	platforms    platformdomain.PlatformRepository
	integrations sitedomain.IntegrationRepository
	membership   *MembershipSyncUseCase
}

// NewDeploymentCredentialService constructs the credential recorder.
func NewDeploymentCredentialService(
	platforms platformdomain.PlatformRepository,
	integrations sitedomain.IntegrationRepository,
	membership *MembershipSyncUseCase,
) *DeploymentCredentialService {
	return &DeploymentCredentialService{platforms: platforms, integrations: integrations, membership: membership}
}

// Record attaches a freshly deployed platform's read credential and syncs its membership.
//
// The token is stored as a sealed platform-kind integration credential. Control-plane lease
// discovery is enabled because any supported topology may use dedicated control-plane Servers
// that do not register as Kubernetes nodes; insecureSkipVerify is set only when the deployment returned no CA certificate. A first
// membership read is triggered so the platform's members appear without waiting for the
// background interval; its failure is not fatal because the integration is stored and the
// background sync retries.
func (s *DeploymentCredentialService) Record(ctx context.Context, platformID string, credential DeploymentCredential) error {
	if strings.TrimSpace(credential.APIEndpoint) == "" || strings.TrimSpace(credential.Token) == "" {
		return fmt.Errorf("platform deployment returned no usable credential")
	}
	platform, err := s.platforms.FindByID(ctx, platformID)
	if err != nil {
		return err
	}

	settings := map[string]string{
		platforminfra.SettingControllerLeaseDiscovery: "true",
	}
	if strings.TrimSpace(credential.CACertificate) == "" {
		settings[platforminfra.SettingInsecureSkipVerify] = "true"
	}

	now := time.Now().UTC()
	integration := &sitedomain.Integration{
		ID:           uuid.NewString(),
		SiteID:       platform.SiteID,
		Kind:         sitedomain.IntegrationKindPlatform,
		ProviderKind: sitedomain.ProviderKindKubernetes,
		Name:         platform.Name + " (deployed)",
		Endpoint:     credential.APIEndpoint,
		Enabled:      true,
		Settings:     settings,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.integrations.Create(ctx, integration, credential.Token); err != nil {
		return err
	}

	platform.IntegrationID = integration.ID
	platform.OwnedIntegrationID = integration.ID
	platform.UpdatedAt = now
	if err := s.platforms.Update(ctx, platform); err != nil {
		return err
	}

	_, _ = s.membership.Execute(ctx, platform.ID)
	return nil
}
