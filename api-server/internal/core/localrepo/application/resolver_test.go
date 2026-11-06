package application

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	"github.com/maple52046/swallow/internal/core/localrepo/domain/localrepotest"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

var resolverModules = []localrepodomain.ModuleDeclaration{{
	ModuleID: "docker-ce",
	Sources:  []localrepodomain.Source{debSource("docker-ce", "https://download.example/ubuntu", []string{"noble"}, []string{"stable"})},
	Files:    []localrepodomain.File{{Path: "tools/tool-1", URL: "https://files.example/tool-1", SHA256: "aa"}},
	Images:   []string{"registry.example/team/app:1.2"},
}}

// readyRecord is site-1's record with every effective source, file, and image ready on repo-site-1.
func readyRecord() localrepodomain.LocalRepository {
	now := time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC)
	ubuntu := debSource("ubuntu", "http://archive.example/ubuntu/", []string{"noble"}, []string{"main"})
	docker := resolverModules[0].Sources[0]
	registry := localrepodomain.Source{Name: "registry.example", Format: localrepodomain.FormatOCI, URL: "https://registry.example"}
	ready := func(source localrepodomain.Source) localrepodomain.ObservedSource {
		return localrepodomain.ObservedSource{Source: source, Status: localrepodomain.SourceStatus{Name: source.Name, Format: source.Format, State: localrepodomain.SyncReady}}
	}
	return localrepodomain.LocalRepository{
		SiteID: "site-1", Generation: 1, Spec: localrepodomain.Spec{Sources: []localrepodomain.Source{ubuntu}},
		Status: localrepodomain.Status{
			IntegrationID: "repo-site-1", ObservedGeneration: 1, ObservedAt: &now,
			Sources: []localrepodomain.ObservedSource{ready(docker), ready(registry), ready(ubuntu)},
			Files: []localrepodomain.ObservedFile{{File: resolverModules[0].Files[0], Status: localrepodomain.FileStatus{
				Path: "tools/tool-1", State: localrepodomain.SyncReady,
			}}},
			Images: []localrepodomain.ObservedImage{{Reference: "registry.example/team/app:1.2", Status: localrepodomain.ImageStatus{
				Reference: "registry.example/team/app:1.2", State: localrepodomain.SyncReady, SizeBytes: 4096,
			}}},
			Endpoints: &localrepodomain.HostEndpoints{
				Deb: []localrepodomain.DebSource{
					{Name: "ubuntu", URI: "http://10.0.0.5:8080/deb/ubuntu/", Suites: []string{"noble"}, Components: []string{"main"}},
					{Name: "unmanaged", URI: "http://10.0.0.5:8080/deb/unmanaged/"},
					{Name: "docker-ce", URI: "http://10.0.0.5:8080/deb/docker-ce/", Suites: []string{"noble"}, Components: []string{"stable"}},
				},
				DebSigningKey:   "KEY",
				Registry:        "10.0.0.5:8080",
				RegistryMirrors: map[string]string{"quay.io": "http://10.0.0.5:8080/v2/quay.io"},
				Files:           "http://10.0.0.5:8080/files/",
			},
		},
	}
}

func newTestResolver(record *localrepodomain.LocalRepository, integrations ...*sitedomain.Integration) *Resolver {
	records := localrepotest.NewStore()
	if record != nil {
		records.Put(*record)
	}
	return NewResolver(newFakeIntegrations(integrations...), records, resolverModules)
}

// A ready Local Repository yields the trusted var with every key, its deb list limited to the
// effective deb sources in name order.
func TestResolverHostVarsOfAReadyRepository(t *testing.T) {
	record := readyRecord()
	vars, err := newTestResolver(&record, repositoryIntegrationOf("site-1")).HostVars(context.Background(), "site-1")
	if err != nil {
		t.Fatalf("HostVars: %v", err)
	}
	want := map[string]any{"swallow_local_repository": map[string]any{
		"deb": []any{
			map[string]any{"name": "docker-ce", "uri": "http://10.0.0.5:8080/deb/docker-ce/", "suites": []string{"noble"}, "components": []string{"stable"}},
			map[string]any{"name": "ubuntu", "uri": "http://10.0.0.5:8080/deb/ubuntu/", "suites": []string{"noble"}, "components": []string{"main"}},
		},
		"deb_signing_key":  "KEY",
		"registry":         "10.0.0.5:8080",
		"registry_mirrors": map[string]any{"quay.io": "http://10.0.0.5:8080/v2/quay.io"},
		"python_index":     "",
		"files":            "http://10.0.0.5:8080/files/",
	}}
	if !reflect.DeepEqual(vars, want) {
		t.Errorf("HostVars = %#v\nwant %#v", vars, want)
	}
}

// Anything short of ready is ErrLocalRepositoryUnavailable naming what to register, enable, or wait
// for.
func TestResolverRefusesARepositoryThatIsNotReady(t *testing.T) {
	disabled := repositoryIntegrationOf("site-1")
	disabled.Enabled = false
	syncing := readyRecord()
	syncing.Status.Sources[2].Status.State = localrepodomain.SyncSyncing
	failed := readyRecord()
	failed.Status.Files[0].Status = localrepodomain.FileStatus{Path: "tools/tool-1", State: localrepodomain.SyncFailed, Message: "checksum mismatch"}
	changed := readyRecord()
	changed.Spec.Sources[0].Suites = []string{"noble", "noble-updates"}
	otherIntegration := readyRecord()
	otherIntegration.Status.IntegrationID = "repo-previous"
	noEndpoints := readyRecord()
	noEndpoints.Status.Endpoints = nil
	syncingImage := readyRecord()
	syncingImage.Status.Images[0].Status = localrepodomain.ImageStatus{Reference: "registry.example/team/app:1.2", State: localrepodomain.SyncSyncing}
	failedImage := readyRecord()
	failedImage.Status.Images[0].Status = localrepodomain.ImageStatus{
		Reference: "registry.example/team/app:1.2", State: localrepodomain.SyncFailed, Message: "The download of the image through Pulp received nothing for 5m0s.",
	}
	unobservedImage := readyRecord()
	unobservedImage.Status.Images = nil
	rejected := readyRecord()
	rejected.Status.Conditions = []convergence.Condition{{
		Type: convergence.ConditionRealized, Status: convergence.ConditionFalse, Reason: localrepodomain.ReasonCredentialRejected,
		Message: "the provider refused the credential of the repository integration local; replace it",
	}}

	for _, tc := range []struct {
		name        string
		record      *localrepodomain.LocalRepository
		integration *sitedomain.Integration
		mentions    string
	}{
		{"no Integration", nil, nil, "no repository integration; register one"},
		{"disabled", &syncing, disabled, "is disabled; enable it"},
		{"never observed", nil, repositoryIntegrationOf("site-1"), "has not been observed yet"},
		{"syncing source", &syncing, repositoryIntegrationOf("site-1"), "source ubuntu is syncing"},
		{"failed file", &failed, repositoryIntegrationOf("site-1"), "file tools/tool-1 failed: checksum mismatch"},
		{"changed since observed", &changed, repositoryIntegrationOf("site-1"), "source ubuntu is pending"},
		{"observed on another Integration", &otherIntegration, repositoryIntegrationOf("site-1"), "has not been observed yet"},
		{"no endpoints", &noEndpoints, repositoryIntegrationOf("site-1"), "addresses have not been read yet"},
		{"syncing image", &syncingImage, repositoryIntegrationOf("site-1"), "image registry.example/team/app:1.2 is syncing"},
		{"failed image", &failedImage, repositoryIntegrationOf("site-1"), "image registry.example/team/app:1.2 failed: The download of the image"},
		{"image not observed yet", &unobservedImage, repositoryIntegrationOf("site-1"), "image registry.example/team/app:1.2 is pending"},
		{"last pass stopped by the Integration", &rejected, repositoryIntegrationOf("site-1"), "refused the credential"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var integrations []*sitedomain.Integration
			if tc.integration != nil {
				integrations = append(integrations, tc.integration)
			}
			_, err := newTestResolver(tc.record, integrations...).HostVars(context.Background(), "site-1")
			if !errors.Is(err, localrepodomain.ErrLocalRepositoryUnavailable) {
				t.Fatalf("HostVars error = %v, want ErrLocalRepositoryUnavailable", err)
			}
			if !strings.Contains(err.Error(), tc.mentions) {
				t.Errorf("HostVars error = %q, want it to mention %q", err, tc.mentions)
			}
		})
	}
}
