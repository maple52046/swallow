package application

import (
	"context"
	"errors"
	"testing"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

// recordingBuilder is a Platform module's reader builder that keeps the Connection it was given.
type recordingBuilder struct {
	platformType platformdomain.PlatformType
	got          *platformdomain.Connection
}

func (b recordingBuilder) PlatformType() platformdomain.PlatformType { return b.platformType }

func (b recordingBuilder) NewReader(conn platformdomain.Connection) (platformdomain.PlatformReader, error) {
	*b.got = conn
	return nil, nil
}

// The factory hands the Platform's Integration to the builder of the Platform's type: endpoint,
// token, timeout, CA, and every setting.
func TestReaderFactoryBuildsTheReaderOfThePlatformType(t *testing.T) {
	integrations := newCredentialIntegrations()
	integrations.items["i1"] = &sitedomain.Integration{
		ID: "i1", Kind: sitedomain.IntegrationKindPlatform, ProviderKind: "widget", Endpoint: "https://api",
		Settings: map[string]string{platformdomain.SettingTimeout: "5s", platformdomain.SettingCACertificate: "PEM", "custom": "x"},
	}
	integrations.credentials["i1"] = "token"
	var got platformdomain.Connection
	factory, err := NewReaderFactory(integrations, recordingBuilder{platformType: "widget", got: &got})
	if err != nil {
		t.Fatalf("NewReaderFactory: %v", err)
	}
	if _, err := factory.For(context.Background(), &platformdomain.Platform{Type: "widget", IntegrationID: "i1"}); err != nil {
		t.Fatalf("For: %v", err)
	}
	if got.ProviderKind != "widget" || got.Endpoint != "https://api" || got.Token != "token" || got.Timeout != 5*time.Second ||
		got.CACertificate != "PEM" || got.InsecureSkipVerify || got.Setting("custom", "") != "x" {
		t.Errorf("connection = %+v", got)
	}

	if _, err := factory.For(context.Background(), &platformdomain.Platform{Type: "other", IntegrationID: "i1"}); !errors.Is(err, platformdomain.ErrUnsupportedPlatformType) {
		t.Errorf("a type without a module: err = %v, want ErrUnsupportedPlatformType", err)
	}
	if _, err := factory.For(context.Background(), &platformdomain.Platform{Type: "widget"}); !errors.Is(err, platformdomain.ErrNoPlatformIntegration) {
		t.Errorf("no Integration: err = %v, want ErrNoPlatformIntegration", err)
	}
	if _, err := NewReaderFactory(integrations, recordingBuilder{platformType: "widget", got: &got}, recordingBuilder{platformType: "widget", got: &got}); err == nil {
		t.Error("two builders for one Platform type must be refused")
	}
}
