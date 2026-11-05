package application

import (
	"context"
	"errors"
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
// Readers are always built from the read credential: membership and every other background read
// go through For, which never decrypts the operator credential. OperatorConnection is the only
// path to the operator credential, for a Platform module's client of an admin's request.
//
// Not cached, unlike the provisioning and automation factories: membership is read once per
// interval rather than continuously, so there is no connection reuse to protect and no reason to
// hold a stale client.
type ReaderFactory struct {
	integrations PlatformIntegrations
	builders     map[platformdomain.PlatformType]platformdomain.ReaderBuilder
}

var (
	_ platformdomain.ReaderFactory            = (*ReaderFactory)(nil)
	_ platformdomain.OperatorConnectionSource = (*ReaderFactory)(nil)
)

// NewReaderFactory builds the factory over the Platform modules' reader builders and the store of
// both credentials of their Integrations. Two builders for one Platform type are an error.
func NewReaderFactory(integrations PlatformIntegrations, builders ...platformdomain.ReaderBuilder) (*ReaderFactory, error) {
	byType := make(map[platformdomain.PlatformType]platformdomain.ReaderBuilder, len(builders))
	for _, builder := range builders {
		if _, exists := byType[builder.PlatformType()]; exists {
			return nil, fmt.Errorf("two platform readers for Platform type %q", builder.PlatformType())
		}
		byType[builder.PlatformType()] = builder
	}
	return &ReaderFactory{integrations: integrations, builders: byType}, nil
}

// For builds the reader of the Platform's type from its read Connection.
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
// credential is the token. It is the ConnectionRead Connection.
func (f *ReaderFactory) Connection(ctx context.Context, platform *platformdomain.Platform) (platformdomain.Connection, error) {
	return f.connection(ctx, platform, f.integrations.Credential)
}

// OperatorConnection reads the Platform's Integration like Connection, but its token is the
// Integration's operator credential, or the credential when the Integration holds no operator
// credential (see platformdomain.OperatorConnectionSource). It is the ConnectionOperate Connection:
// a caller uses it only for an admin's request that needs that access, never in the background.
func (f *ReaderFactory) OperatorConnection(ctx context.Context, platform *platformdomain.Platform) (platformdomain.Connection, error) {
	return f.connection(ctx, platform, f.operatorCredential)
}

// operatorCredential returns the Integration's operator credential, or its credential when it holds
// no operator credential. Only ErrCredentialNotSet falls back: any other failure to read the
// operator credential is returned, so a store error never silently downgrades to the read token.
func (f *ReaderFactory) operatorCredential(ctx context.Context, integrationID string) (string, error) {
	token, err := f.integrations.OperatorCredential(ctx, integrationID)
	if errors.Is(err, sitedomain.ErrCredentialNotSet) {
		return f.integrations.Credential(ctx, integrationID)
	}
	return token, err
}

// clientForAccess builds a Platform module's live client for access: ConnectionOperate uses the
// factory's ForOperator when it has the capability (platformdomain.OperatorClientFactory), and
// everything else uses For.
//
// This is the one place an explorer request's access becomes a credential, so a read can never
// carry the operator credential. A factory without the capability gets For for ConnectionOperate
// too: its client authenticates with the read credential, so the Platform may deny the request but
// it never runs with more than reads already have.
func clientForAccess[C any](
	ctx context.Context,
	factory interface {
		For(ctx context.Context, platform *platformdomain.Platform) (C, error)
	},
	platform *platformdomain.Platform,
	access platformdomain.ConnectionAccess,
) (C, error) {
	if access == platformdomain.ConnectionOperate {
		if operators, ok := factory.(platformdomain.OperatorClientFactory[C]); ok {
			return operators.ForOperator(ctx, platform)
		}
	}
	return factory.For(ctx, platform)
}

// connection builds the Connection of the Platform's Integration with the token credential reads.
func (f *ReaderFactory) connection(
	ctx context.Context,
	platform *platformdomain.Platform,
	credential func(ctx context.Context, integrationID string) (string, error),
) (platformdomain.Connection, error) {
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
	token, err := credential(ctx, integration.ID)
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
