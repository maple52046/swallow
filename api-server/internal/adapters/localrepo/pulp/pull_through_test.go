package pulp

import (
	"context"
	"testing"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

func TestEnsureOCISourceCreatesPullThroughCache(t *testing.T) {
	tests := []struct {
		name     string
		source   localrepodomain.Source
		upstream string
	}{
		{
			name:     "Docker Hub is fetched from its registry API host",
			source:   localrepodomain.Source{Name: "docker.io", Format: localrepodomain.FormatOCI, URL: "https://docker.io"},
			upstream: "https://registry-1.docker.io",
		},
		{
			name:     "a source without a URL is fetched from its host",
			source:   localrepodomain.Source{Name: "quay.io", Format: localrepodomain.FormatOCI},
			upstream: "https://quay.io",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakePulp(t)
			repo := f.newRepository(t)
			ctx := context.Background()

			first, err := repo.EnsureSource(ctx, tc.source)
			if err != nil {
				t.Fatalf("EnsureSource(%s) error = %v", tc.source.Name, err)
			}
			wantState(t, "first EnsureSource", first.State, localrepodomain.SyncPending)
			if first.Serving {
				t.Errorf("first EnsureSource Serving = true, want false before the distribution exists")
			}
			remote := f.named(pullThroughRemotes, tc.source.Name)
			if remote == nil || remote.str("url") != tc.upstream {
				t.Fatalf("pull-through remote = %v, want url %s", remote, tc.upstream)
			}
			creates := f.requestsTo("POST", api(pullThroughDistributions))
			if len(creates) != 1 || creates[0].body["base_path"] != tc.source.Name || creates[0].body["remote"] != remote.href() {
				t.Errorf("pull-through distribution creates = %+v, want base path %s with the remote", creates, tc.source.Name)
			}

			f.mu.Lock()
			distribution := f.objects[f.order[pullThroughDistributions][0]]
			distribution["distributions"] = []any{"/pulp/api/v3/distributions/container/container/0100/"}
			f.mu.Unlock()
			second, err := repo.EnsureSource(ctx, tc.source)
			if err != nil {
				t.Fatalf("second EnsureSource(%s) error = %v", tc.source.Name, err)
			}
			wantState(t, "second EnsureSource", second.State, localrepodomain.SyncReady)
			if !second.Serving {
				t.Errorf("second EnsureSource Serving = false, want true once the distribution pulls through")
			}
			if second.ContentCount != 1 {
				t.Errorf("ContentCount = %d, want the one image pulled through", second.ContentCount)
			}
			if got := len(f.requestsTo("POST", api(pullThroughDistributions))); got != 1 {
				t.Errorf("pull-through distribution creates = %d, want still 1", got)
			}
		})
	}
}

func TestEnsureSourceDoesNotRepeatAQueuedCreate(t *testing.T) {
	f := newFakePulp(t)
	repo := f.newRepository(t)
	ctx := context.Background()
	source := localrepodomain.Source{Name: "quay.io", Format: localrepodomain.FormatOCI}

	f.setHold(true)
	for range 3 {
		status, err := repo.EnsureSource(ctx, source)
		if err != nil {
			t.Fatalf("EnsureSource(quay.io) error = %v", err)
		}
		wantState(t, "queued create", status.State, localrepodomain.SyncPending)
	}
	creates := f.requestsTo("POST", api(pullThroughDistributions))
	if len(creates) != 1 || creates[0].cid == "" {
		t.Errorf("pull-through distribution creates = %+v, want one, with a correlation id", creates)
	}

	f.release()
	status, err := repo.EnsureSource(ctx, source)
	if err != nil {
		t.Fatalf("EnsureSource(quay.io) after the create error = %v", err)
	}
	wantState(t, "created", status.State, localrepodomain.SyncReady)
}

func TestPullThroughSourceDoesNotServeAChangedUpstreamUntilItApplies(t *testing.T) {
	f := newFakePulp(t)
	repo := f.newRepository(t)
	ctx := context.Background()
	source := localrepodomain.Source{Name: "docker.io", Format: localrepodomain.FormatOCI, URL: "https://docker.io"}
	for range 2 {
		if _, err := repo.EnsureSource(ctx, source); err != nil {
			t.Fatalf("EnsureSource(docker.io) error = %v", err)
		}
	}

	// A user points the registry at an internal mirror; Pulp's worker has not applied it yet.
	f.setHold(true)
	mirrored := source
	mirrored.URL = "https://mirror.example.internal"
	status, err := repo.EnsureSource(ctx, mirrored)
	if err != nil {
		t.Fatalf("EnsureSource(mirrored docker.io) error = %v", err)
	}
	if status.Serving {
		t.Errorf("Serving while the upstream change is queued = true, want false: it still pulls from the old upstream")
	}

	f.release()
	if status, err = repo.EnsureSource(ctx, mirrored); err != nil {
		t.Fatalf("EnsureSource(mirrored docker.io) after the update error = %v", err)
	}
	if !status.Serving {
		t.Errorf("Serving after the upstream change applied = false, want true")
	}
}

func TestEnsurePythonSourceCreatesPullThroughIndex(t *testing.T) {
	f := newFakePulp(t)
	repo := f.newRepository(t)
	ctx := context.Background()
	source := localrepodomain.Source{Name: "pypi", Format: localrepodomain.FormatPython}

	first, err := repo.EnsureSource(ctx, source)
	if err != nil {
		t.Fatalf("EnsureSource(pypi) error = %v", err)
	}
	wantState(t, "first EnsureSource", first.State, localrepodomain.SyncPending)

	remote := f.named(pythonRemotes, "pypi")
	if remote == nil || remote.str("url") != "https://pypi.org/" || remote.str("policy") != "on_demand" {
		t.Errorf("python remote = %v, want PyPI fetched on demand", remote)
	}
	repository := f.named(pythonRepositories, "pypi")
	distribution := f.named(pythonDistributions, "pypi")
	if distribution == nil || distribution.str("base_path") != "pypi" || distribution.str("repository") != repository.href() ||
		distribution.str("remote") != remote.href() {
		t.Errorf("python distribution = %v, want base path pypi with the repository and the remote", distribution)
	}

	f.mu.Lock()
	pkg := f.store(pythonPackages, map[string]any{"name": "numpy", "version": "2.5.4", "filename": "numpy-2.5.4.whl", "size": 1.0e7})
	f.addVersion(repository.href(), []string{pkg})
	f.mu.Unlock()
	second, err := repo.EnsureSource(ctx, source)
	if err != nil {
		t.Fatalf("second EnsureSource(pypi) error = %v", err)
	}
	wantState(t, "second EnsureSource", second.State, localrepodomain.SyncReady)
	if first.Serving || !second.Serving {
		t.Errorf("Serving = %t then %t, want false until the distribution exists, then true", first.Serving, second.Serving)
	}
	if second.ContentCount != 1 {
		t.Errorf("ContentCount = %d, want the one package pulled through", second.ContentCount)
	}
}
