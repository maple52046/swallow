package infra

import (
	"context"
	"fmt"
	"sync"
	"time"

	operationdomain "github.com/AFDEAPAC/swallow/internal/operation/domain"
	"github.com/AFDEAPAC/swallow/internal/operation/infra/awx"
	sitedomain "github.com/AFDEAPAC/swallow/internal/site/domain"
)

const defaultTimeout = 30 * time.Second

// Setting keys an operator may set on an automation integration.
const (
	SettingTimeout            = "timeout"
	SettingInsecureSkipVerify = "insecureSkipVerify"
)

// ControllerFactory builds automation controllers from registered integrations.
//
// Cached per integration and invalidated by UpdatedAt, for the same reason as the
// provisioning factory: the poller talks to these continuously and should reuse
// connections.
type ControllerFactory struct {
	integrations sitedomain.IntegrationRepository

	mu     sync.Mutex
	cached map[string]cachedController
}

type cachedController struct {
	controller operationdomain.AutomationController
	updatedAt  time.Time
}

func NewControllerFactory(integrations sitedomain.IntegrationRepository) *ControllerFactory {
	return &ControllerFactory{
		integrations: integrations,
		cached:       make(map[string]cachedController),
	}
}

func (f *ControllerFactory) For(ctx context.Context, integrationID string) (operationdomain.AutomationController, error) {
	integration, err := f.integrations.FindByID(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	if integration.Kind != sitedomain.IntegrationKindAutomation {
		return nil, fmt.Errorf("integration %q is registered as %q, not an automation controller",
			integration.Name, integration.Kind)
	}

	f.mu.Lock()
	if hit, ok := f.cached[integrationID]; ok && hit.updatedAt.Equal(integration.UpdatedAt) {
		f.mu.Unlock()
		return hit.controller, nil
	}
	f.mu.Unlock()

	controller, err := f.build(ctx, integration)
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	f.cached[integrationID] = cachedController{controller: controller, updatedAt: integration.UpdatedAt}
	f.mu.Unlock()

	return controller, nil
}

func (f *ControllerFactory) build(ctx context.Context, integration *sitedomain.Integration) (operationdomain.AutomationController, error) {
	switch integration.ProviderKind {
	case sitedomain.ProviderKindAWX:
		token, err := f.integrations.Credential(ctx, integration.ID)
		if err != nil {
			return nil, err
		}

		timeout := defaultTimeout
		if raw := integration.Setting(SettingTimeout, ""); raw != "" {
			if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
				timeout = parsed
			}
		}

		client, err := awx.NewClient(
			integration.Endpoint,
			token,
			timeout,
			integration.SettingBool(SettingInsecureSkipVerify),
		)
		if err != nil {
			return nil, err
		}
		return awx.NewController(client), nil

	default:
		return nil, fmt.Errorf("unsupported automation controller kind %q", integration.ProviderKind)
	}
}
