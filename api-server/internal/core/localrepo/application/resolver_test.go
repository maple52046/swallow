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
	SigningKeys: map[string]localrepodomain.SigningKey{
		"docker-ce": {URL: "https://download.example/ubuntu/gpg", Fingerprint: "9DC858229FC7DD38854AE2D88D81803C0EBFCD88"},
	},
	Files:  []localrepodomain.File{{Path: "tools/tool-1", URL: "https://files.example/tool-1", SHA256: "aa"}},
	Images: []string{"registry.example/team/app:1.2"},
}}

// readyRecord is site-1's record serving deployments, with every effective source, file, and image
// ready on repo-site-1.
func readyRecord() localrepodomain.LocalRepository {
	now := time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC)
	ubuntu := debSource("ubuntu", "http://archive.example/ubuntu/", []string{"noble"}, []string{"main"})
	docker := resolverModules[0].Sources[0]
	registry := localrepodomain.Source{Name: "registry.example", Format: localrepodomain.FormatOCI, URL: "https://registry.example"}
	ready := func(source localrepodomain.Source) localrepodomain.ObservedSource {
		return localrepodomain.ObservedSource{Source: source, Status: localrepodomain.SourceStatus{Name: source.Name, Format: source.Format, State: localrepodomain.SyncReady}}
	}
	return localrepodomain.LocalRepository{
		SiteID: "site-1", Generation: 1,
		Spec: localrepodomain.Spec{ServeDeployments: true, Sources: []localrepodomain.Source{ubuntu}},
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

// A ready Local Repository that serves deployments yields the trusted var with every key: its deb
// list limited to the effective deb sources in name order, all signed with the Site's key, and every
// declared file and image through the provider.
func TestResolverArtifactVarsOfAReadyRepository(t *testing.T) {
	record := readyRecord()
	vars, err := newTestResolver(&record, repositoryIntegrationOf("site-1")).ArtifactVars(context.Background(), "site-1")
	if err != nil {
		t.Fatalf("ArtifactVars: %v", err)
	}
	want := map[string]any{"swallow_artifacts": map[string]any{
		"source": "local_repository",
		"deb": []any{
			map[string]any{"name": "docker-ce", "uri": "http://10.0.0.5:8080/deb/docker-ce/", "suites": []string{"noble"}, "components": []string{"stable"},
				"signing_key_url": "", "signing_key_fingerprint": ""},
			map[string]any{"name": "ubuntu", "uri": "http://10.0.0.5:8080/deb/ubuntu/", "suites": []string{"noble"}, "components": []string{"main"},
				"signing_key_url": "", "signing_key_fingerprint": ""},
		},
		"deb_signing_key":  "KEY",
		"registry":         "10.0.0.5:8080",
		"registry_mirrors": map[string]any{"quay.io": "http://10.0.0.5:8080/v2/quay.io"},
		"python_index":     "",
		"files":            map[string]any{"tools/tool-1": "http://10.0.0.5:8080/files/tools/tool-1"},
		"images":           map[string]any{"registry.example/team/app:1.2": "10.0.0.5:8080/registry.example/team/app:1.2"},
	}}
	if !reflect.DeepEqual(vars, want) {
		t.Errorf("ArtifactVars = %#v\nwant %#v", vars, want)
	}
}

// A Site whose Local Repository does not serve deployments, or that has none, fetches from the
// upstreams the modules declare, whatever the Local Repository's state: only the modules' deb
// sources (not the user's) with their signing keys, every file at its URL and image at its own
// reference, and no Local Repository address.
func TestResolverArtifactVarsFromTheUpstreams(t *testing.T) {
	wantUpstream := map[string]any{"swallow_artifacts": map[string]any{
		"source": "upstream",
		"deb": []any{
			map[string]any{"name": "docker-ce", "uri": "https://download.example/ubuntu", "suites": []string{"noble"}, "components": []string{"stable"},
				"signing_key_url": "https://download.example/ubuntu/gpg", "signing_key_fingerprint": "9DC858229FC7DD38854AE2D88D81803C0EBFCD88"},
		},
		"deb_signing_key":  "",
		"registry":         "",
		"registry_mirrors": map[string]any{},
		"python_index":     "",
		"files":            map[string]any{"tools/tool-1": "https://files.example/tool-1"},
		"images":           map[string]any{"registry.example/team/app:1.2": "registry.example/team/app:1.2"},
	}}
	notServing := readyRecord()
	notServing.Spec.ServeDeployments = false
	notServingNorReady := readyRecord()
	notServingNorReady.Spec.ServeDeployments = false
	notServingNorReady.Status.Sources[2].Status.State = localrepodomain.SyncFailed
	disabled := repositoryIntegrationOf("site-1")
	disabled.Enabled = false

	for _, tc := range []struct {
		name        string
		record      *localrepodomain.LocalRepository
		integration *sitedomain.Integration
	}{
		{"no Integration", nil, nil},
		{"no Integration, a spec left from an earlier one", &notServing, nil},
		{"spec never written", nil, repositoryIntegrationOf("site-1")},
		{"ready but not serving", &notServing, repositoryIntegrationOf("site-1")},
		{"not serving and a source failed", &notServingNorReady, repositoryIntegrationOf("site-1")},
		{"not serving and the Integration disabled", &notServingNorReady, disabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var integrations []*sitedomain.Integration
			if tc.integration != nil {
				integrations = append(integrations, tc.integration)
			}
			vars, err := newTestResolver(tc.record, integrations...).ArtifactVars(context.Background(), "site-1")
			if err != nil {
				t.Fatalf("ArtifactVars: %v", err)
			}
			if !reflect.DeepEqual(vars, wantUpstream) {
				t.Errorf("ArtifactVars = %#v\nwant %#v", vars, wantUpstream)
			}
		})
	}
}

// While the Local Repository serves deployments, anything short of ready is
// ErrLocalRepositoryUnavailable naming what to enable, fix, or wait for; it never falls back to the
// upstreams.
func TestResolverRefusesAServingRepositoryThatIsNotReady(t *testing.T) {
	disabled := repositoryIntegrationOf("site-1")
	disabled.Enabled = false
	unobserved := localrepodomain.LocalRepository{SiteID: "site-1", Generation: 1, Spec: localrepodomain.Spec{ServeDeployments: true}}
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
		{"disabled", &syncing, disabled, "is disabled; enable it"},
		{"never observed", &unobserved, repositoryIntegrationOf("site-1"), "has not been observed yet"},
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
			_, err := newTestResolver(tc.record, tc.integration).ArtifactVars(context.Background(), "site-1")
			if !errors.Is(err, localrepodomain.ErrLocalRepositoryUnavailable) {
				t.Fatalf("ArtifactVars error = %v, want ErrLocalRepositoryUnavailable", err)
			}
			if !strings.Contains(err.Error(), tc.mentions) {
				t.Errorf("ArtifactVars error = %q, want it to mention %q", err, tc.mentions)
			}
		})
	}
}
