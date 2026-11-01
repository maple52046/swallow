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

// credentialIntegrations stores Integrations in memory with their credential.
type credentialIntegrations struct {
	sitedomain.IntegrationRepository
	items       map[string]*sitedomain.Integration
	credentials map[string]string
}

func newCredentialIntegrations() *credentialIntegrations {
	return &credentialIntegrations{items: map[string]*sitedomain.Integration{}, credentials: map[string]string{}}
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
