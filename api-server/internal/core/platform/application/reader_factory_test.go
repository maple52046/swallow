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

// The read Connection, and so every reader, carries the read credential even when the Integration
// holds an operator credential; OperatorConnection carries the operator credential with the same
// endpoint and settings, and falls back to the read credential only when the Integration holds no
// operator credential. Any other failure to read the operator credential is returned, not
// downgraded to the read credential.
func TestReaderFactorySelectsTheCredentialForTheAccess(t *testing.T) {
	storeDown := errors.New("store unavailable")
	cases := []struct {
		name        string
		operator    string
		operatorErr error
		wantRead    string
		wantOperate string
		wantErr     error
	}{
		{name: "operator credential stored", operator: "operator", wantRead: "reader", wantOperate: "operator"},
		{name: "deployed before the operator credential", wantRead: "reader", wantOperate: "reader"},
		{name: "operator credential unreadable", operatorErr: storeDown, wantRead: "reader", wantErr: storeDown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			integrations := newCredentialIntegrations()
			integrations.items["i1"] = &sitedomain.Integration{
				ID: "i1", Kind: sitedomain.IntegrationKindPlatform, ProviderKind: "widget", Endpoint: "https://api",
				Settings: map[string]string{platformdomain.SettingCACertificate: "PEM"},
			}
			integrations.credentials["i1"] = "reader"
			if tc.operator != "" {
				integrations.operators["i1"] = tc.operator
			}
			integrations.operatorErr = tc.operatorErr
			var built platformdomain.Connection
			factory, err := NewReaderFactory(integrations, recordingBuilder{platformType: "widget", got: &built})
			if err != nil {
				t.Fatalf("NewReaderFactory: %v", err)
			}
			platform := &platformdomain.Platform{Type: "widget", IntegrationID: "i1"}
			ctx := context.Background()

			if _, err := factory.For(ctx, platform); err != nil || built.Token != tc.wantRead {
				t.Errorf("For: reader token %q, err %v; want %q", built.Token, err, tc.wantRead)
			}
			read, err := factory.Connection(ctx, platform)
			if err != nil || read.Token != tc.wantRead {
				t.Errorf("Connection = %q, %v; want %q", read.Token, err, tc.wantRead)
			}
			operate, err := factory.OperatorConnection(ctx, platform)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("OperatorConnection error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && (operate.Token != tc.wantOperate || operate.Endpoint != read.Endpoint || operate.CACertificate != "PEM") {
				t.Errorf("OperatorConnection = %+v, want token %q on the read Connection's endpoint and CA", operate, tc.wantOperate)
			}
		})
	}

	factory, err := NewReaderFactory(newCredentialIntegrations())
	if err != nil {
		t.Fatalf("NewReaderFactory: %v", err)
	}
	if _, err := factory.OperatorConnection(context.Background(), &platformdomain.Platform{Type: "widget"}); !errors.Is(err, platformdomain.ErrNoPlatformIntegration) {
		t.Errorf("OperatorConnection without an Integration: err = %v, want ErrNoPlatformIntegration", err)
	}
}

// widgetClient stands in for a Platform module's live client; its value names the factory method
// that built it.
type widgetClient string

// widgetFactory is the shape clientForAccess accepts, for widgetClient.
type widgetFactory = interface {
	For(context.Context, *platformdomain.Platform) (widgetClient, error)
}

// readOnlyFactory has no operator capability: it can only build the read client.
type readOnlyFactory struct{ calls *[]string }

func (f readOnlyFactory) For(context.Context, *platformdomain.Platform) (widgetClient, error) {
	*f.calls = append(*f.calls, "For")
	return "read", nil
}

// operatorFactory builds both clients.
type operatorFactory struct{ readOnlyFactory }

func (f operatorFactory) ForOperator(context.Context, *platformdomain.Platform) (widgetClient, error) {
	*f.calls = append(*f.calls, "ForOperator")
	return "operator", nil
}

// ConnectionOperate builds the operator client and ConnectionRead the read client; a factory
// without the operator capability gives its read client for both, so an operate request can only
// be denied by the Platform, never run with more than the read credential.
func TestClientForAccessUsesTheOperatorClientOnlyForOperate(t *testing.T) {
	withOperator := func(calls *[]string) widgetFactory { return operatorFactory{readOnlyFactory{calls}} }
	readOnly := func(calls *[]string) widgetFactory { return readOnlyFactory{calls} }
	cases := []struct {
		name    string
		access  platformdomain.ConnectionAccess
		factory func(calls *[]string) widgetFactory
		want    widgetClient
	}{
		{name: "read", access: platformdomain.ConnectionRead, factory: withOperator, want: "read"},
		{name: "operate", access: platformdomain.ConnectionOperate, factory: withOperator, want: "operator"},
		{name: "operate without the capability", access: platformdomain.ConnectionOperate, factory: readOnly, want: "read"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			got, err := clientForAccess(context.Background(), tc.factory(&calls), &platformdomain.Platform{}, tc.access)
			if err != nil || got != tc.want || len(calls) != 1 {
				t.Errorf("clientForAccess(%v) = %q, %v after %v; want %q from one build", tc.access, got, err, calls, tc.want)
			}
		})
	}
}
