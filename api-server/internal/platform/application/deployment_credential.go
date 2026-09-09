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

// SlurmDeploymentCredential is what a successful Slurm deployment produced: the slurmrestd
// base URL, a JWT to read it with, and the slurmrestd API version the controller exposes.
type SlurmDeploymentCredential struct {
	Endpoint   string
	Token      string
	APIVersion string
}

// RecordSlurm attaches a freshly deployed Slurm platform's read credential and syncs its
// membership. Unlike the Kubernetes path it registers a ProviderKindSlurm integration whose
// endpoint is slurmrestd and whose token is a Slurm JWT, and it pins the slurmrestd API
// version when the run reported one so the reader queries the matching endpoint version.
// insecureSkipVerify is set because slurmrestd is commonly reached over plain HTTP or a
// self-signed endpoint inside the site, and membership is read-only. A first membership read
// is triggered so members appear without waiting for the interval; its failure is not fatal
// because the integration is stored and the background sync retries.
func (s *DeploymentCredentialService) RecordSlurm(ctx context.Context, platformID string, credential SlurmDeploymentCredential) error {
	if strings.TrimSpace(credential.Endpoint) == "" || strings.TrimSpace(credential.Token) == "" {
		return fmt.Errorf("slurm deployment returned no usable credential")
	}
	platform, err := s.platforms.FindByID(ctx, platformID)
	if err != nil {
		return err
	}

	settings := map[string]string{
		platforminfra.SettingInsecureSkipVerify: "true",
	}
	if version := strings.TrimSpace(credential.APIVersion); version != "" {
		settings[platforminfra.SettingSlurmAPIVersion] = version
	}

	now := time.Now().UTC()
	integration := &sitedomain.Integration{
		ID:           uuid.NewString(),
		SiteID:       platform.SiteID,
		Kind:         sitedomain.IntegrationKindPlatform,
		ProviderKind: sitedomain.ProviderKindSlurm,
		Name:         platform.Name + " (deployed)",
		Endpoint:     credential.Endpoint,
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
