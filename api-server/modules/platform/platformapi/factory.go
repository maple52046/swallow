package platformapi

import (
	"context"
	"fmt"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

const defaultTimeout = 30 * time.Second

// ReaderFactory builds platform readers from a platform's integration.
//
// Not cached, unlike the provisioning and automation factories: membership is read once
// per interval rather than continuously, so there is no connection reuse to protect and
// no reason to hold a stale client.
type ReaderFactory struct {
	integrations sitedomain.IntegrationRepository
}

func NewReaderFactory(integrations sitedomain.IntegrationRepository) *ReaderFactory {
	return &ReaderFactory{integrations: integrations}
}

func (f *ReaderFactory) For(ctx context.Context, platform *platformdomain.Platform) (platformdomain.PlatformReader, error) {
	if platform.IntegrationID == "" {
		return nil, platformdomain.ErrNoPlatformIntegration
	}

	integration, err := f.integrations.FindByID(ctx, platform.IntegrationID)
	if err != nil {
		return nil, err
	}
	if integration.Kind != sitedomain.IntegrationKindPlatform {
		return nil, fmt.Errorf("integration %q is registered as %q, not a platform API",
			integration.Name, integration.Kind)
	}

	token, err := f.integrations.Credential(ctx, integration.ID)
	if err != nil {
		return nil, err
	}

	timeout := defaultTimeout
	if raw := integration.Setting(platformdomain.SettingTimeout, ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			timeout = parsed
		}
	}
	insecure := integration.SettingBool(platformdomain.SettingInsecureSkipVerify)

	switch platform.Type {
	case platformdomain.PlatformTypeKubernetes:
		controllerLeases := integration.SettingBool(platformdomain.SettingControllerLeaseDiscovery)
		return NewKubernetesReader(integration.Endpoint, token, timeout, insecure, controllerLeases)

	case platformdomain.PlatformTypeSlurm:
		return NewSlurmReader(
			integration.Endpoint,
			token,
			integration.Setting(platformdomain.SettingSlurmAPIVersion, DefaultSlurmAPIVersion),
			timeout,
			insecure,
		)

	default:
		return nil, fmt.Errorf("%w: %q", platformdomain.ErrUnsupportedPlatformType, platform.Type)
	}
}

// KubernetesClientFactory builds a live read/write Kubernetes explorer client from a
// deployed Kubernetes Platform's Swallow-owned credential Integration.
//
// It is separate from ReaderFactory because the explorer is a distinct capability (read/write
// in-cluster resources) with a distinct client type. Like ReaderFactory it is not cached: the
// explorer is used interactively rather than on a poll loop, and holding a stale client past a
// credential change would be worse than paying to build one per request.
type KubernetesClientFactory struct {
	integrations sitedomain.IntegrationRepository
}

// NewKubernetesClientFactory constructs the explorer client factory.
func NewKubernetesClientFactory(integrations sitedomain.IntegrationRepository) *KubernetesClientFactory {
	return &KubernetesClientFactory{integrations: integrations}
}

// For resolves a Kubernetes Platform into an explorer client. It errors when the platform has
// no credential Integration (the deployed-but-not-yet-recorded case), when the integration is
// not a Kubernetes platform integration, or when the credential is missing — the application
// layer maps these to the explorer-unavailable status.
func (f *KubernetesClientFactory) For(ctx context.Context, platform *platformdomain.Platform) (platformdomain.KubernetesClusterClient, error) {
	if platform.Type != platformdomain.PlatformTypeKubernetes {
		return nil, platformdomain.ErrPlatformNotKubernetes
	}
	if platform.IntegrationID == "" {
		return nil, platformdomain.ErrClusterExplorerUnavailable
	}

	integration, err := f.integrations.FindByID(ctx, platform.IntegrationID)
	if err != nil {
		return nil, err
	}
	if integration.Kind != sitedomain.IntegrationKindPlatform || integration.ProviderKind != sitedomain.ProviderKindKubernetes {
		return nil, fmt.Errorf("integration %q is not a Kubernetes platform API", integration.Name)
	}

	token, err := f.integrations.Credential(ctx, integration.ID)
	if err != nil {
		return nil, err
	}

	timeout := defaultTimeout
	if raw := integration.Setting(platformdomain.SettingTimeout, ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			timeout = parsed
		}
	}
	insecure := integration.SettingBool(platformdomain.SettingInsecureSkipVerify)

	return NewKubernetesClient(integration.Endpoint, token, timeout, insecure)
}
