package application

import (
	"context"
	"fmt"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

// defaultPlatformAPITimeout bounds a platform API call when the Integration sets no timeout.
const defaultPlatformAPITimeout = 30 * time.Second

// ReaderFactory resolves a Platform's Integration into a Connection and builds the reader of the
// Platform's type with the ReaderBuilder its Platform module provides.
//
// Not cached, unlike the provisioning and automation factories: membership is read once per
// interval rather than continuously, so there is no connection reuse to protect and no reason to
// hold a stale client.
type ReaderFactory struct {
	integrations sitedomain.IntegrationRepository
	builders     map[platformdomain.PlatformType]platformdomain.ReaderBuilder
}

var (
	_ platformdomain.ReaderFactory    = (*ReaderFactory)(nil)
	_ platformdomain.ConnectionSource = (*ReaderFactory)(nil)
)

// NewReaderFactory builds the factory over the Platform modules' reader builders. Two builders for
// one Platform type are an error.
func NewReaderFactory(integrations sitedomain.IntegrationRepository, builders ...platformdomain.ReaderBuilder) (*ReaderFactory, error) {
	byType := make(map[platformdomain.PlatformType]platformdomain.ReaderBuilder, len(builders))
	for _, builder := range builders {
		if _, exists := byType[builder.PlatformType()]; exists {
			return nil, fmt.Errorf("two platform readers for Platform type %q", builder.PlatformType())
		}
		byType[builder.PlatformType()] = builder
	}
	return &ReaderFactory{integrations: integrations, builders: byType}, nil
}

// For builds the reader of the Platform's type.
func (f *ReaderFactory) For(ctx context.Context, platform *platformdomain.Platform) (platformdomain.PlatformReader, error) {
	builder, ok := f.builders[platform.Type]
	if !ok {
		return nil, fmt.Errorf("%w: %q", platformdomain.ErrUnsupportedPlatformType, platform.Type)
	}
	conn, err := f.Connection(ctx, platform)
	if err != nil {
		return nil, err
	}
	return builder.NewReader(conn)
}

// Connection reads the Platform's Integration: it must be a platform Integration, and its sealed
// credential is the token.
func (f *ReaderFactory) Connection(ctx context.Context, platform *platformdomain.Platform) (platformdomain.Connection, error) {
	if platform.IntegrationID == "" {
		return platformdomain.Connection{}, platformdomain.ErrNoPlatformIntegration
	}
	integration, err := f.integrations.FindByID(ctx, platform.IntegrationID)
	if err != nil {
		return platformdomain.Connection{}, err
	}
	if integration.Kind != sitedomain.IntegrationKindPlatform {
		return platformdomain.Connection{}, fmt.Errorf("integration %q is registered as %q, not a platform API",
			integration.Name, integration.Kind)
	}
	token, err := f.integrations.Credential(ctx, integration.ID)
	if err != nil {
		return platformdomain.Connection{}, err
	}
	timeout := defaultPlatformAPITimeout
	if raw := integration.Setting(platformdomain.SettingTimeout, ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			timeout = parsed
		}
	}
	return platformdomain.Connection{
		ProviderKind:       integration.ProviderKind,
		Endpoint:           integration.Endpoint,
		Token:              token,
		Timeout:            timeout,
		CACertificate:      integration.Setting(platformdomain.SettingCACertificate, ""),
		InsecureSkipVerify: integration.SettingBool(platformdomain.SettingInsecureSkipVerify),
		Settings:           integration.Settings,
	}, nil
}
