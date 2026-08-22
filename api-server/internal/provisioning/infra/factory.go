package infra

import (
	"context"
	"fmt"
	"sync"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	"github.com/maple52046/swallow/internal/provisioning/infra/maas"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// defaultTimeout bounds a single provider API call when the integration does not
// override it.
const defaultTimeout = 30 * time.Second

// Setting keys an operator may set on a provisioner integration.
const (
	SettingTimeout            = "timeout"
	SettingInsecureSkipVerify = "insecureSkipVerify"
)

// ProviderFactory builds provisioning providers from registered integrations.
//
// Providers are cached per integration and invalidated by the integration's UpdatedAt,
// which changes whenever its endpoint, settings, or credential change. Without the
// cache every request would construct a fresh HTTP transport and TLS configuration,
// which throws away connection reuse against a system polled continuously.
type ProviderFactory struct {
	integrations sitedomain.IntegrationRepository

	mu     sync.Mutex
	cached map[string]cachedProvider
}

type cachedProvider struct {
	provider  provisioningdomain.OSProvisioningProvider
	updatedAt time.Time
}

func NewProviderFactory(integrations sitedomain.IntegrationRepository) *ProviderFactory {
	return &ProviderFactory{
		integrations: integrations,
		cached:       make(map[string]cachedProvider),
	}
}

func (f *ProviderFactory) For(ctx context.Context, integrationID string) (provisioningdomain.OSProvisioningProvider, error) {
	integration, err := f.integrations.FindByID(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	if integration.Kind != sitedomain.IntegrationKindProvisioner {
		return nil, fmt.Errorf("%w: %q is registered as %q",
			provisioningdomain.ErrIntegrationNotProvisioner, integration.Name, integration.Kind)
	}

	f.mu.Lock()
	if hit, ok := f.cached[integrationID]; ok && hit.updatedAt.Equal(integration.UpdatedAt) {
		f.mu.Unlock()
		return hit.provider, nil
	}
	f.mu.Unlock()

	provider, err := f.build(ctx, integration)
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	f.cached[integrationID] = cachedProvider{provider: provider, updatedAt: integration.UpdatedAt}
	f.mu.Unlock()

	return provider, nil
}

func (f *ProviderFactory) build(ctx context.Context, integration *sitedomain.Integration) (provisioningdomain.OSProvisioningProvider, error) {
	switch integration.ProviderKind {
	case sitedomain.ProviderKindMAAS:
		credential, err := f.integrations.Credential(ctx, integration.ID)
		if err != nil {
			return nil, err
		}

		timeout := defaultTimeout
		if raw := integration.Setting(SettingTimeout, ""); raw != "" {
			if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
				timeout = parsed
			}
		}

		client, err := maas.NewClient(
			integration.Endpoint,
			credential,
			timeout,
			integration.SettingBool(SettingInsecureSkipVerify),
		)
		if err != nil {
			return nil, err
		}
		return maas.NewProvider(client), nil

	default:
		return nil, fmt.Errorf("%w: %q",
			provisioningdomain.ErrProviderKindUnsupported, integration.ProviderKind)
	}
}
