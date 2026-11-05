package application

import (
	"context"
	"errors"
	"maps"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	"github.com/maple52046/swallow/internal/core/store/storetest"
)

// credentialIntegrations stores Integrations in memory with their credential and operator
// credential. operatorErr, when set, is what every operator credential read fails with.
type credentialIntegrations struct {
	sitedomain.IntegrationRepository
	items       map[string]*sitedomain.Integration
	credentials map[string]string
	operators   map[string]string
	operatorErr error
}

func newCredentialIntegrations() *credentialIntegrations {
	return &credentialIntegrations{
		items: map[string]*sitedomain.Integration{}, credentials: map[string]string{}, operators: map[string]string{},
	}
}

func (r *credentialIntegrations) ReplaceOperatorCredential(_ context.Context, id, credential string) error {
	if _, ok := r.items[id]; !ok {
		return sitedomain.ErrIntegrationNotFound
	}
	if credential == "" {
		delete(r.operators, id)
		return nil
	}
	r.operators[id] = credential
	return nil
}

func (r *credentialIntegrations) OperatorCredential(_ context.Context, id string) (string, error) {
	if r.operatorErr != nil {
		return "", r.operatorErr
	}
	credential, ok := r.operators[id]
	if !ok {
		return "", sitedomain.ErrCredentialNotSet
	}
	return credential, nil
}

func (r *credentialIntegrations) Create(_ context.Context, integration *sitedomain.Integration, credential string) error {
	r.items[integration.ID] = integration
	r.credentials[integration.ID] = credential
	return nil
}

func (r *credentialIntegrations) FindByID(_ context.Context, id string) (*sitedomain.Integration, error) {
	if integration, ok := r.items[id]; ok {
		return integration, nil
	}
	return nil, sitedomain.ErrIntegrationNotFound
}

func (r *credentialIntegrations) Credential(_ context.Context, id string) (string, error) {
	return r.credentials[id], nil
}

func (r *credentialIntegrations) Update(_ context.Context, integration *sitedomain.Integration) error {
	r.items[integration.ID] = integration
	return nil
}

func (r *credentialIntegrations) ReplaceCredential(_ context.Context, id, credential string) error {
	r.credentials[id] = credential
	return nil
}

// unreachableReaders fails every read, as a cluster that is not up yet would.
type unreachableReaders struct{}

func (unreachableReaders) For(context.Context, *platformdomain.Platform) (platformdomain.PlatformReader, error) {
	return nil, errors.New("not reachable")
}

// Record stores the credential as the Platform's owned read Integration with exactly the settings
// the Platform module decided, under the Platform type as provider kind.
func TestRecordStoresTheModulesCredential(t *testing.T) {
	platforms := &deployFakePlatformRepo{platforms: map[string]*platformdomain.Platform{
		"p1": {ID: "p1", SiteID: "site-1", Name: "lab", Type: platformdomain.PlatformTypeKubernetes},
	}}
	integrations := newCredentialIntegrations()
	service := NewDeploymentCredentialService(&storetest.Transactor{}, platforms, integrations,
		NewMembershipSyncUseCase(platforms, nil, unreachableReaders{}))
	settings := map[string]string{platformdomain.SettingCACertificate: "PEM\n", "moduleKey": "true"}

	err := service.Record(context.Background(), "p1", platformdomain.Credential{
		Endpoint: "https://10.0.0.1:6443", Token: "token", Settings: settings,
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	platform := platforms.platforms["p1"]
	integration, err := integrations.FindByID(context.Background(), platform.IntegrationID)
	if err != nil {
		t.Fatalf("recorded Integration: %v", err)
	}
	if platform.OwnedIntegrationID != integration.ID || integration.ProviderKind != "kubernetes" ||
		integrations.credentials[integration.ID] != "token" || !maps.Equal(integration.Settings, settings) {
		t.Errorf("integration = %+v, owned %q", integration, platform.OwnedIntegrationID)
	}
	if err := service.Record(context.Background(), "p1", platformdomain.Credential{Endpoint: "https://x"}); err == nil {
		t.Error("a credential without a token must be refused")
	}
}

// A rerun of a deployment records its credential again: the Integration the Platform already owns
// is refreshed rather than duplicated, whose name would collide with it.
func TestRecordRefreshesTheOwnedIntegrationOnARerun(t *testing.T) {
	platforms := &deployFakePlatformRepo{platforms: map[string]*platformdomain.Platform{
		"p1": {ID: "p1", SiteID: "site-1", Name: "lab", Type: platformdomain.PlatformTypeSlurm},
	}}
	integrations := newCredentialIntegrations()
	service := NewDeploymentCredentialService(&storetest.Transactor{}, platforms, integrations,
		NewMembershipSyncUseCase(platforms, nil, unreachableReaders{}))
	ctx := context.Background()
	if err := service.Record(ctx, "p1", platformdomain.Credential{Endpoint: "http://a:6820", Token: "old", Settings: map[string]string{"v": "1"}}); err != nil {
		t.Fatalf("first Record: %v", err)
	}
	owned := platforms.platforms["p1"].OwnedIntegrationID
	if err := service.Record(ctx, "p1", platformdomain.Credential{Endpoint: "http://b:6820", Token: "new", Settings: map[string]string{"v": "2"}}); err != nil {
		t.Fatalf("rerun Record: %v", err)
	}
	if len(integrations.items) != 1 || platforms.platforms["p1"].OwnedIntegrationID != owned {
		t.Fatalf("integrations = %d, owned %q -> %q; want the one Integration refreshed", len(integrations.items), owned, platforms.platforms["p1"].OwnedIntegrationID)
	}
	refreshed := integrations.items[owned]
	if refreshed.Endpoint != "http://b:6820" || refreshed.Settings["v"] != "2" || integrations.credentials[owned] != "new" {
		t.Errorf("refreshed = %+v, credential %q", refreshed, integrations.credentials[owned])
	}
}

// A deployment that returns an operator token gets it sealed as the same Integration's operator
// credential, apart from the read credential; one that returns none (a Slurm Platform, or a
// whitespace token) records no operator credential.
func TestRecordStoresTheOperatorCredentialBesideTheReadCredential(t *testing.T) {
	cases := []struct {
		name         string
		operator     string
		wantOperator string
	}{
		{name: "operator token returned", operator: "operator", wantOperator: "operator"},
		{name: "no operator token", operator: ""},
		{name: "blank operator token", operator: "  "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			platforms := &deployFakePlatformRepo{platforms: map[string]*platformdomain.Platform{
				"p1": {ID: "p1", SiteID: "site-1", Name: "lab", Type: platformdomain.PlatformTypeKubernetes},
			}}
			integrations := newCredentialIntegrations()
			service := NewDeploymentCredentialService(&storetest.Transactor{}, platforms, integrations,
				NewMembershipSyncUseCase(platforms, nil, unreachableReaders{}))
			err := service.Record(context.Background(), "p1", platformdomain.Credential{
				Endpoint: "https://10.0.0.1:6443", Token: "reader", OperatorToken: tc.operator,
			})
			if err != nil {
				t.Fatalf("Record: %v", err)
			}
			id := platforms.platforms["p1"].IntegrationID
			operator, ok := integrations.operators[id]
			if integrations.credentials[id] != "reader" || operator != tc.wantOperator || ok != (tc.wantOperator != "") {
				t.Errorf("credential %q, operator credential %q (stored %v); want reader and %q",
					integrations.credentials[id], operator, ok, tc.wantOperator)
			}
		})
	}
}

// A rerun records both credentials from its own result: a new operator token replaces the stored
// one, and a result without one removes it, so the Integration never pairs a read credential with
// an operator credential from an earlier run (which may belong to a cluster since reinstalled).
func TestRecordRefreshKeepsBothCredentialsFromOneDeployment(t *testing.T) {
	platforms := &deployFakePlatformRepo{platforms: map[string]*platformdomain.Platform{
		"p1": {ID: "p1", SiteID: "site-1", Name: "lab", Type: platformdomain.PlatformTypeKubernetes},
	}}
	integrations := newCredentialIntegrations()
	service := NewDeploymentCredentialService(&storetest.Transactor{}, platforms, integrations,
		NewMembershipSyncUseCase(platforms, nil, unreachableReaders{}))
	ctx := context.Background()
	record := func(token, operator string) {
		t.Helper()
		if err := service.Record(ctx, "p1", platformdomain.Credential{Endpoint: "https://a:6443", Token: token, OperatorToken: operator}); err != nil {
			t.Fatalf("Record(%q, %q): %v", token, operator, err)
		}
	}
	record("reader-1", "operator-1")
	owned := platforms.platforms["p1"].OwnedIntegrationID

	record("reader-2", "operator-2")
	if integrations.credentials[owned] != "reader-2" || integrations.operators[owned] != "operator-2" {
		t.Errorf("after a rerun: credential %q, operator credential %q; want reader-2 and operator-2",
			integrations.credentials[owned], integrations.operators[owned])
	}

	record("reader-3", "")
	if _, stale := integrations.operators[owned]; stale || integrations.credentials[owned] != "reader-3" {
		t.Errorf("after a rerun without an operator token: credential %q, operator credential %q; want reader-3 and none",
			integrations.credentials[owned], integrations.operators[owned])
	}
	if len(integrations.items) != 1 {
		t.Errorf("integrations = %d, want the one owned Integration refreshed", len(integrations.items))
	}
}
