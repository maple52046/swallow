package pulp

import (
	"context"
	"errors"
	"testing"
	"time"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

// fakeIntegrations serves Integrations and credentials from memory; the factory only reads.
type fakeIntegrations struct {
	sitedomain.IntegrationRepository
	integrations    map[string]*sitedomain.Integration
	credentials     map[string]string
	credentialReads int
}

func (f *fakeIntegrations) FindByID(_ context.Context, id string) (*sitedomain.Integration, error) {
	integration, ok := f.integrations[id]
	if !ok {
		return nil, sitedomain.ErrIntegrationNotFound
	}
	copied := *integration
	return &copied, nil
}

func (f *fakeIntegrations) Credential(_ context.Context, id string) (string, error) {
	f.credentialReads++
	credential, ok := f.credentials[id]
	if !ok {
		return "", sitedomain.ErrCredentialNotSet
	}
	return credential, nil
}

func pulpIntegration(id string, settings map[string]string) *sitedomain.Integration {
	return &sitedomain.Integration{
		ID: id, SiteID: "site-1", Kind: sitedomain.IntegrationKindRepository, ProviderKind: ProviderKind,
		Name: "pulp", Endpoint: "http://pulp-web:8080/pulp/api/v3/", Enabled: true, Settings: settings,
		UpdatedAt: time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC),
	}
}

func TestRepositoryFactoryForBuildsAndCachesPulpRepository(t *testing.T) {
	integrations := &fakeIntegrations{
		integrations: map[string]*sitedomain.Integration{
			"int-1": pulpIntegration("int-1", map[string]string{localrepodomain.SettingContentOrigin: "http://10.170.168.20:8080/"}),
		},
		credentials: map[string]string{"int-1": fakePassword},
	}
	factory := NewRepositoryFactory(integrations, nil)

	got, err := factory.For(context.Background(), "int-1")
	if err != nil {
		t.Fatalf("For(int-1) error = %v", err)
	}
	repository, ok := got.(*Repository)
	if !ok {
		t.Fatalf("For(int-1) = %T, want *Repository", got)
	}
	if repository.api.base != "http://pulp-web:8080" {
		t.Errorf("API base = %q, want the endpoint without the API path", repository.api.base)
	}
	if repository.contentOrigin != "http://10.170.168.20:8080" {
		t.Errorf("content origin = %q, want it without the trailing slash", repository.contentOrigin)
	}
	if repository.api.http.Timeout != apiTimeout || repository.registry.Timeout != 0 {
		t.Errorf("timeouts = API %s, registry %s; want %s and none", repository.api.http.Timeout, repository.registry.Timeout, apiTimeout)
	}

	again, err := factory.For(context.Background(), "int-1")
	if err != nil || again != got {
		t.Errorf("second For(int-1) = %p, %v; want the cached repository %p", again, err, got)
	}
	if integrations.credentialReads != 1 {
		t.Errorf("credential reads = %d, want 1 while the Integration is unchanged", integrations.credentialReads)
	}

	integrations.integrations["int-1"].UpdatedAt = integrations.integrations["int-1"].UpdatedAt.Add(time.Minute)
	rebuilt, err := factory.For(context.Background(), "int-1")
	if err != nil || rebuilt == got {
		t.Fatalf("For(int-1) after an update = %p, %v; want a new repository", rebuilt, err)
	}
	if rebuilt.(*Repository).downloads != repository.downloads {
		t.Errorf("rebuilt repository downloads = %p, want the factory's %p shared across Integration changes", rebuilt.(*Repository).downloads, repository.downloads)
	}
}

func TestRepositoryFactoryForRefusesWhatItCannotServe(t *testing.T) {
	origin := map[string]string{localrepodomain.SettingContentOrigin: "http://10.170.168.20:8080"}
	metrics := pulpIntegration("metrics", origin)
	metrics.Kind = sitedomain.IntegrationKindMetrics
	otherProvider := pulpIntegration("other", origin)
	otherProvider.ProviderKind = "nexus"
	badEndpoint := pulpIntegration("bad-endpoint", origin)
	badEndpoint.Endpoint = "ftp://pulp"
	withPath := pulpIntegration("with-path", map[string]string{localrepodomain.SettingContentOrigin: "http://10.0.0.5:8080/content"})

	integrations := &fakeIntegrations{
		integrations: map[string]*sitedomain.Integration{
			"metrics": metrics, "other": otherProvider, "bad-endpoint": badEndpoint,
			"no-origin": pulpIntegration("no-origin", nil), "with-path": withPath,
			"no-credential": pulpIntegration("no-credential", origin),
		},
		credentials: map[string]string{
			"metrics": fakePassword, "other": fakePassword, "bad-endpoint": fakePassword, "no-origin": fakePassword, "with-path": fakePassword,
		},
	}
	factory := NewRepositoryFactory(integrations, nil)

	for _, id := range []string{"metrics", "other", "bad-endpoint", "no-origin", "with-path"} {
		_, err := factory.For(context.Background(), id)
		var providerErr *localrepodomain.ProviderError
		if !errors.As(err, &providerErr) || providerErr.Kind != localrepodomain.ProviderErrorRejected {
			t.Errorf("For(%s) error = %v, want a rejected ProviderError", id, err)
		}
	}
	if _, err := factory.For(context.Background(), "no-credential"); !errors.Is(err, sitedomain.ErrCredentialNotSet) {
		t.Errorf("For(no-credential) error = %v, want ErrCredentialNotSet", err)
	}
	if _, err := factory.For(context.Background(), "missing"); !errors.Is(err, sitedomain.ErrIntegrationNotFound) {
		t.Errorf("For(missing) error = %v, want ErrIntegrationNotFound", err)
	}
}
