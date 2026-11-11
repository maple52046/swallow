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
	Python: []string{"numpy==2.5.4"},
}}

// The record's observed sources, in readyRecord's order.
const (
	dockerSourceIndex = iota
	pypiSourceIndex
	registrySourceIndex
	ubuntuSourceIndex
)

// readyRecord is site-1's record in offline mode, with every effective source ready and serving,
// and every file and image ready, on repo-site-1.
func readyRecord() localrepodomain.LocalRepository {
	now := time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC)
	ubuntu := debSource("ubuntu", "http://archive.example/ubuntu/", []string{"noble"}, []string{"main"})
	docker := resolverModules[0].Sources[0]
	pypi := localrepodomain.Source{Name: "pypi", Format: localrepodomain.FormatPython, URL: "https://pypi.org/"}
	registry := localrepodomain.Source{Name: "registry.example", Format: localrepodomain.FormatOCI, URL: "https://registry.example"}
	ready := func(source localrepodomain.Source) localrepodomain.ObservedSource {
		return localrepodomain.ObservedSource{Source: source, Status: localrepodomain.SourceStatus{
			Name: source.Name, Format: source.Format, State: localrepodomain.SyncReady, Serving: true,
		}}
	}
	return localrepodomain.LocalRepository{
		SiteID: "site-1", Generation: 1,
		Spec: localrepodomain.Spec{Mode: localrepodomain.ModeOffline, Sources: []localrepodomain.Source{ubuntu}},
		Status: localrepodomain.Status{
			IntegrationID: "repo-site-1", ObservedGeneration: 1, ObservedAt: &now,
			Sources: []localrepodomain.ObservedSource{ready(docker), ready(pypi), ready(registry), ready(ubuntu)},
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
				DebSigningKey: "KEY",
				Registry:      "10.0.0.5:8080",
				RegistryMirrors: map[string]string{
					"quay.io": "http://10.0.0.5:8080/v2/quay.io", "registry.example": "http://10.0.0.5:8080/v2/registry.example",
				},
				PythonIndex: "http://10.0.0.5:8080/pypi/pypi/simple/",
				Files:       "http://10.0.0.5:8080/files/",
			},
		},
	}
}

// withMode returns record in mode.
func withMode(record localrepodomain.LocalRepository, mode localrepodomain.Mode) localrepodomain.LocalRepository {
	record.Spec.Mode = mode
	return record
}

func newTestResolver(record *localrepodomain.LocalRepository, integrations ...*sitedomain.Integration) *Resolver {
	records := localrepotest.NewStore()
	if record != nil {
		records.Put(*record)
	}
	return NewResolver(newFakeIntegrations(integrations...), records, resolverModules)
}

// The trusted var's Local Repository and upstream halves, which the cache mode cases mix.
var (
	localDeb = []any{
		map[string]any{"name": "docker-ce", "uri": "http://10.0.0.5:8080/deb/docker-ce/", "suites": []string{"noble"}, "components": []string{"stable"},
			"signing_key_url": "", "signing_key_fingerprint": ""},
		map[string]any{"name": "ubuntu", "uri": "http://10.0.0.5:8080/deb/ubuntu/", "suites": []string{"noble"}, "components": []string{"main"},
			"signing_key_url": "", "signing_key_fingerprint": ""},
	}
	upstreamDeb = []any{
		map[string]any{"name": "docker-ce", "uri": "https://download.example/ubuntu", "suites": []string{"noble"}, "components": []string{"stable"},
			"signing_key_url": "https://download.example/ubuntu/gpg", "signing_key_fingerprint": "9DC858229FC7DD38854AE2D88D81803C0EBFCD88"},
	}
	localFiles     = map[string]any{"tools/tool-1": "http://10.0.0.5:8080/files/tools/tool-1"}
	upstreamFiles  = map[string]any{"tools/tool-1": "https://files.example/tool-1"}
	localImages    = map[string]any{"registry.example/team/app:1.2": "10.0.0.5:8080/registry.example/team/app:1.2"}
	upstreamImages = map[string]any{"registry.example/team/app:1.2": "registry.example/team/app:1.2"}
)

// An offline Local Repository that is ready yields the trusted var with every key: its deb list
// limited to the effective deb sources in name order, all signed with the Site's key, and every
// declared file, image, registry mirror, and Python package through the provider.
func TestResolverArtifactVarsOfAReadyOfflineRepository(t *testing.T) {
	record := readyRecord()
	vars, err := newTestResolver(&record, repositoryIntegrationOf("site-1")).ArtifactVars(context.Background(), "site-1")
	if err != nil {
		t.Fatalf("ArtifactVars: %v", err)
	}
	want := map[string]any{"swallow_artifacts": map[string]any{
		"source":          "local_repository",
		"apt":             "local_repository",
		"deb":             localDeb,
		"deb_signing_key": "KEY",
		"registry":        "10.0.0.5:8080",
		"registry_mirrors": map[string]any{
			"quay.io": "http://10.0.0.5:8080/v2/quay.io", "registry.example": "http://10.0.0.5:8080/v2/registry.example",
		},
		"python_index": "http://10.0.0.5:8080/pypi/pypi/simple/",
		"files":        localFiles,
		"images":       localImages,
	}}
	if !reflect.DeepEqual(vars, want) {
		t.Errorf("ArtifactVars = %#v\nwant %#v", vars, want)
	}
}

// An offline source whose resync runs, or failed, still serves its last synchronization, so the
// deployment is accepted from the Local Repository.
func TestResolverAcceptsAnOfflineSourceThatServesWhileItResyncs(t *testing.T) {
	for _, state := range []localrepodomain.SyncState{localrepodomain.SyncSyncing, localrepodomain.SyncFailed} {
		record := readyRecord()
		record.Status.Sources[ubuntuSourceIndex].Status.State = state
		vars, err := newTestResolver(&record, repositoryIntegrationOf("site-1")).ArtifactVars(context.Background(), "site-1")
		if err != nil {
			t.Fatalf("ArtifactVars with ubuntu %s but serving: %v", state, err)
		}
		if got := vars["swallow_artifacts"].(map[string]any)["apt"]; got != "local_repository" {
			t.Errorf("ArtifactVars with ubuntu %s apt = %v, want local_repository", state, got)
		}
	}
}

// A Site in upstream mode, or one without a Local Repository, fetches from the upstreams the
// modules declare, whatever the Local Repository's state: only the modules' deb sources (not the
// user's) with their signing keys, every file at its URL and image at its own reference, and no
// Local Repository address.
func TestResolverArtifactVarsFromTheUpstreams(t *testing.T) {
	wantUpstream := map[string]any{"swallow_artifacts": map[string]any{
		"source":           "upstream",
		"apt":              "upstream",
		"deb":              upstreamDeb,
		"deb_signing_key":  "",
		"registry":         "",
		"registry_mirrors": map[string]any{},
		"python_index":     "",
		"files":            upstreamFiles,
		"images":           upstreamImages,
	}}
	upstream := withMode(readyRecord(), localrepodomain.ModeUpstream)
	upstreamNorReady := withMode(readyRecord(), localrepodomain.ModeUpstream)
	upstreamNorReady.Status.Sources[ubuntuSourceIndex].Status = localrepodomain.SourceStatus{Name: "ubuntu", Format: localrepodomain.FormatDeb, State: localrepodomain.SyncFailed}
	disabled := repositoryIntegrationOf("site-1")
	disabled.Enabled = false

	for _, tc := range []struct {
		name        string
		record      *localrepodomain.LocalRepository
		integration *sitedomain.Integration
	}{
		{"no Integration", nil, nil},
		{"no Integration, a spec left from an earlier one", &upstream, nil},
		{"spec never written", nil, repositoryIntegrationOf("site-1")},
		{"ready but upstream", &upstream, repositoryIntegrationOf("site-1")},
		{"upstream and a source failed", &upstreamNorReady, repositoryIntegrationOf("site-1")},
		{"upstream and the Integration disabled", &upstreamNorReady, disabled},
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

// In cache mode each artifact goes through the Local Repository exactly when the recorded status
// says the provider serves it, and otherwise comes from the modules' declarations; apt goes to one
// side as a whole, and an image follows its registry source whether or not it was fetched ahead.
func TestResolverArtifactVarsInCacheMode(t *testing.T) {
	notServing := func(record *localrepodomain.LocalRepository, index int, state localrepodomain.SyncState) {
		record.Status.Sources[index].Status.State, record.Status.Sources[index].Status.Serving = state, false
	}

	nothingYet := withMode(readyRecord(), localrepodomain.ModeCache)
	for index := range nothingYet.Status.Sources {
		notServing(&nothingYet, index, localrepodomain.SyncSyncing)
	}
	nothingYet.Status.Files[0].Status.State = localrepodomain.SyncPending
	nothingYet.Status.Images[0].Status.State = localrepodomain.SyncPending

	ubuntuFirstSync := withMode(readyRecord(), localrepodomain.ModeCache)
	notServing(&ubuntuFirstSync, ubuntuSourceIndex, localrepodomain.SyncSyncing)
	ubuntuFirstSync.Status.Files[0].Status.State = localrepodomain.SyncSyncing
	ubuntuFirstSync.Status.Images[0].Status.State = localrepodomain.SyncPending

	ubuntuResync := withMode(readyRecord(), localrepodomain.ModeCache)
	ubuntuResync.Status.Sources[ubuntuSourceIndex].Status.State = localrepodomain.SyncSyncing

	everything := withMode(readyRecord(), localrepodomain.ModeCache)

	allUpstream := map[string]any{
		"source": "cache", "apt": "upstream", "deb": upstreamDeb, "deb_signing_key": "",
		"registry": "", "registry_mirrors": map[string]any{}, "python_index": "",
		"files": upstreamFiles, "images": upstreamImages,
	}
	registryMirror := map[string]any{"registry.example": "http://10.0.0.5:8080/v2/registry.example"}
	for _, tc := range []struct {
		name   string
		record localrepodomain.LocalRepository
		want   map[string]any
	}{
		{"nothing served yet", nothingYet, allUpstream},
		{"the OS archive's first sync runs", ubuntuFirstSync, map[string]any{
			"source": "cache", "apt": "upstream", "deb": upstreamDeb, "deb_signing_key": "",
			"registry": "10.0.0.5:8080", "registry_mirrors": registryMirror,
			"python_index": "http://10.0.0.5:8080/pypi/pypi/simple/",
			"files":        upstreamFiles, "images": localImages,
		}},
		{"the OS archive resyncs while it serves", ubuntuResync, map[string]any{
			"source": "cache", "apt": "local_repository", "deb": localDeb, "deb_signing_key": "KEY",
			"registry": "10.0.0.5:8080", "registry_mirrors": registryMirror,
			"python_index": "http://10.0.0.5:8080/pypi/pypi/simple/",
			"files":        localFiles, "images": localImages,
		}},
		{"everything served", everything, map[string]any{
			"source": "cache", "apt": "local_repository", "deb": localDeb, "deb_signing_key": "KEY",
			"registry": "10.0.0.5:8080", "registry_mirrors": registryMirror,
			"python_index": "http://10.0.0.5:8080/pypi/pypi/simple/",
			"files":        localFiles, "images": localImages,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vars, err := newTestResolver(&tc.record, repositoryIntegrationOf("site-1")).ArtifactVars(context.Background(), "site-1")
			if err != nil {
				t.Fatalf("ArtifactVars: %v", err)
			}
			if got := vars["swallow_artifacts"]; !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ArtifactVars = %#v\nwant %#v", got, tc.want)
			}
		})
	}
}

// In offline mode anything short of ready is ErrLocalRepositoryUnavailable naming what to enable,
// fix, or wait for; it never falls back to the upstreams. In cache mode only what keeps the Local
// Repository itself from being used refuses; the content never does.
func TestResolverRefusesARepositoryThatIsNotReady(t *testing.T) {
	disabled := repositoryIntegrationOf("site-1")
	disabled.Enabled = false
	unobserved := localrepodomain.LocalRepository{SiteID: "site-1", Generation: 1, Spec: localrepodomain.Spec{Mode: localrepodomain.ModeOffline}}
	syncing := readyRecord()
	syncing.Status.Sources[ubuntuSourceIndex].Status.State, syncing.Status.Sources[ubuntuSourceIndex].Status.Serving = localrepodomain.SyncSyncing, false
	failedSource := readyRecord()
	failedSource.Status.Sources[dockerSourceIndex].Status = localrepodomain.SourceStatus{
		Name: "docker-ce", Format: localrepodomain.FormatDeb, State: localrepodomain.SyncFailed, Message: "Cannot connect to download.example",
	}
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
		record      localrepodomain.LocalRepository
		integration *sitedomain.Integration
		mentions    string
		// cacheAccepts says whether the same record in cache mode is accepted.
		cacheAccepts bool
	}{
		{"disabled", syncing, disabled, "is disabled; enable it", false},
		{"never observed", unobserved, repositoryIntegrationOf("site-1"), "has not been observed yet", false},
		{"source not serving yet", syncing, repositoryIntegrationOf("site-1"), "source ubuntu is syncing", true},
		{"failed source not serving", failedSource, repositoryIntegrationOf("site-1"), "source docker-ce failed: Cannot connect", true},
		{"failed file", failed, repositoryIntegrationOf("site-1"), "file tools/tool-1 failed: checksum mismatch", true},
		{"changed since observed", changed, repositoryIntegrationOf("site-1"), "source ubuntu is pending", true},
		{"observed on another Integration", otherIntegration, repositoryIntegrationOf("site-1"), "has not been observed yet", false},
		{"no endpoints", noEndpoints, repositoryIntegrationOf("site-1"), "addresses have not been read yet", false},
		{"syncing image", syncingImage, repositoryIntegrationOf("site-1"), "image registry.example/team/app:1.2 is syncing", true},
		{"failed image", failedImage, repositoryIntegrationOf("site-1"), "image registry.example/team/app:1.2 failed: The download of the image", true},
		{"image not observed yet", unobservedImage, repositoryIntegrationOf("site-1"), "image registry.example/team/app:1.2 is pending", true},
		{"last pass stopped by the Integration", rejected, repositoryIntegrationOf("site-1"), "refused the credential", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newTestResolver(&tc.record, tc.integration).ArtifactVars(context.Background(), "site-1")
			if !errors.Is(err, localrepodomain.ErrLocalRepositoryUnavailable) {
				t.Fatalf("offline ArtifactVars error = %v, want ErrLocalRepositoryUnavailable", err)
			}
			if !strings.Contains(err.Error(), tc.mentions) {
				t.Errorf("offline ArtifactVars error = %q, want it to mention %q", err, tc.mentions)
			}

			cache := withMode(tc.record, localrepodomain.ModeCache)
			_, err = newTestResolver(&cache, tc.integration).ArtifactVars(context.Background(), "site-1")
			switch {
			case tc.cacheAccepts && err != nil:
				t.Errorf("cache ArtifactVars error = %v, want the deployment accepted", err)
			case !tc.cacheAccepts && !errors.Is(err, localrepodomain.ErrLocalRepositoryUnavailable):
				t.Errorf("cache ArtifactVars error = %v, want ErrLocalRepositoryUnavailable", err)
			case !tc.cacheAccepts && !strings.Contains(err.Error(), tc.mentions):
				t.Errorf("cache ArtifactVars error = %q, want it to mention %q", err, tc.mentions)
			}
		})
	}
}
