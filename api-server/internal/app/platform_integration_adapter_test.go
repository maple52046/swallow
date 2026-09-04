package app

import (
	"context"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	platforminfra "github.com/maple52046/swallow/internal/platform/infra"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

type integrationCleanerRepo struct {
	integrations map[string]*sitedomain.Integration
	deleted      []string
}

func (r *integrationCleanerRepo) Create(
	context.Context,
	*sitedomain.Integration,
	string,
) error {
	return nil
}

func (r *integrationCleanerRepo) FindByID(
	_ context.Context,
	id string,
) (*sitedomain.Integration, error) {
	integration, ok := r.integrations[id]
	if !ok {
		return nil, sitedomain.ErrIntegrationNotFound
	}
	return integration, nil
}

func (r *integrationCleanerRepo) List(
	context.Context,
	sitedomain.IntegrationFilter,
) ([]*sitedomain.Integration, error) {
	return nil, nil
}

func (r *integrationCleanerRepo) Update(context.Context, *sitedomain.Integration) error {
	return nil
}

func (r *integrationCleanerRepo) ReplaceCredential(context.Context, string, string) error {
	return nil
}

func (r *integrationCleanerRepo) Credential(context.Context, string) (string, error) {
	return "", nil
}

func (r *integrationCleanerRepo) UpdateSyncState(
	context.Context,
	string,
	sitedomain.SyncState,
) error {
	return nil
}

func (r *integrationCleanerRepo) Delete(_ context.Context, id string) error {
	r.deleted = append(r.deleted, id)
	return nil
}

func TestManagedPlatformIntegrationCleanerDeletesExplicitOwnedIntegration(t *testing.T) {
	repo := &integrationCleanerRepo{integrations: map[string]*sitedomain.Integration{}}
	cleaner := managedPlatformIntegrationCleaner{integrations: repo}
	platform := &platformdomain.Platform{
		ID: "platform-1", OwnedIntegrationID: "owned-integration",
	}

	if err := cleaner.DeleteForPlatform(context.Background(), platform, false); err != nil {
		t.Fatalf("delete owned integration: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != "owned-integration" {
		t.Fatalf("deleted = %v", repo.deleted)
	}
}

func TestManagedPlatformIntegrationCleanerRequiresCompleteLegacySignature(t *testing.T) {
	matching := &sitedomain.Integration{
		ID: "legacy-owned", SiteID: "site-1",
		Kind:         sitedomain.IntegrationKindPlatform,
		ProviderKind: sitedomain.ProviderKindKubernetes,
		Name:         "lab (deployed)",
		Settings: map[string]string{
			platforminfra.SettingControllerLeaseDiscovery: "true",
		},
	}
	operatorOwned := &sitedomain.Integration{
		ID: "operator-owned", SiteID: "site-1",
		Kind:         sitedomain.IntegrationKindPlatform,
		ProviderKind: sitedomain.ProviderKindKubernetes,
		Name:         "operator registration",
		Settings: map[string]string{
			platforminfra.SettingControllerLeaseDiscovery: "true",
		},
	}
	repo := &integrationCleanerRepo{integrations: map[string]*sitedomain.Integration{
		matching.ID:      matching,
		operatorOwned.ID: operatorOwned,
	}}
	cleaner := managedPlatformIntegrationCleaner{integrations: repo}

	for _, test := range []struct {
		name          string
		integrationID string
		allowLegacy   bool
		wantDeleted   bool
	}{
		{name: "matching legacy deployment", integrationID: matching.ID, allowLegacy: true, wantDeleted: true},
		{name: "operator owned", integrationID: operatorOwned.ID, allowLegacy: true},
		{name: "legacy detection disabled", integrationID: matching.ID},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo.deleted = nil
			platform := &platformdomain.Platform{
				ID: "platform-1", SiteID: "site-1", Name: "lab",
				IntegrationID: test.integrationID,
			}
			if err := cleaner.DeleteForPlatform(
				context.Background(),
				platform,
				test.allowLegacy,
			); err != nil {
				t.Fatalf("clean integration: %v", err)
			}
			if got := len(repo.deleted) == 1; got != test.wantDeleted {
				t.Fatalf("deleted = %v, want deletion %v", repo.deleted, test.wantDeleted)
			}
		})
	}
}
