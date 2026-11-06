package pulp

import (
	"context"
	"testing"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

func TestRemoveDebSourceCancelsItsSyncAndDeletesItsObjects(t *testing.T) {
	f := newFakePulp(t)
	f.seedSigningService()
	repo := f.newRepository(t)
	ctx := context.Background()
	if _, err := repo.EnsureSource(ctx, ubuntuSource()); err != nil {
		t.Fatalf("EnsureSource(ubuntu) error = %v", err)
	}
	remote, repository, distribution := f.named(debRemotes, "ubuntu"), f.named(debRepositories, "ubuntu"), f.named(debDistributions, "ubuntu")

	// A resync is running when the source is removed.
	f.setHold(true)
	f.mu.Lock()
	sync := f.addTask("pulp_deb.app.tasks.synchronizing.synchronize", "", []string{repository.href(), remote.href()}, func() ([]string, string) { return nil, "" })
	f.mu.Unlock()
	f.startRunning()
	f.setHold(false)

	if err := repo.RemoveSource(ctx, "ubuntu", localrepodomain.FormatDeb); err != nil {
		t.Fatalf("RemoveSource(ubuntu) error = %v", err)
	}
	if got := f.object(sync).str("state"); got != "canceled" {
		t.Errorf("running sync state = %q, want canceled before the repository is deleted", got)
	}
	for _, href := range []string{distribution.href(), repository.href(), remote.href()} {
		if f.object(href) != nil {
			t.Errorf("%s still exists after RemoveSource", href)
		}
	}
	deletes := f.requestsTo("DELETE", "")
	if len(deletes) != 3 || deletes[0].path != distribution.href() {
		t.Errorf("deletes = %+v, want the distribution first, then the repository and the remote", deletes)
	}

	if err := repo.RemoveSource(ctx, "ubuntu", localrepodomain.FormatDeb); err != nil {
		t.Errorf("RemoveSource(ubuntu) again error = %v, want nil for a source that is gone", err)
	}
}

func TestRemoveOCISourceDeletesTheImagesPulledThroughIt(t *testing.T) {
	f := newFakePulp(t)
	repo := f.newRepository(t)
	ctx := context.Background()
	source := localrepodomain.Source{Name: "docker.io", Format: localrepodomain.FormatOCI}
	if _, err := repo.EnsureSource(ctx, source); err != nil {
		t.Fatalf("EnsureSource(docker.io) error = %v", err)
	}
	childRepo := f.seed(containerRepositories, map[string]any{"name": "docker.io/grafana/grafana"})
	childRemote := f.seed("/remotes/container/container/", map[string]any{"name": "docker.io/grafana/grafana"})
	child := f.seed("/distributions/container/container/", map[string]any{
		"name": "docker.io/grafana/grafana", "base_path": "docker.io/grafana/grafana", "repository": childRepo, "remote": childRemote,
	})
	f.mu.Lock()
	parent := f.objects[f.order[pullThroughDistributions][0]]
	parent["distributions"] = []any{child}
	f.mu.Unlock()
	parentRemote := f.named(pullThroughRemotes, "docker.io")

	if err := repo.RemoveSource(ctx, "docker.io", localrepodomain.FormatOCI); err != nil {
		t.Fatalf("RemoveSource(docker.io) error = %v", err)
	}
	for _, href := range []string{child, childRepo, childRemote, parent.href(), parentRemote.href()} {
		if f.object(href) != nil {
			t.Errorf("%s still exists after RemoveSource", href)
		}
	}
}

func TestRemoveSourceOfNothingIsNoError(t *testing.T) {
	repo := newFakePulp(t).newRepository(t)
	for _, format := range []localrepodomain.Format{localrepodomain.FormatDeb, localrepodomain.FormatOCI, localrepodomain.FormatPython, localrepodomain.FormatRPM} {
		if err := repo.RemoveSource(context.Background(), "missing", format); err != nil {
			t.Errorf("RemoveSource(missing, %s) error = %v, want nil", format, err)
		}
	}
	if err := repo.RemoveSource(context.Background(), "files", localrepodomain.FormatFile); err == nil {
		t.Errorf("RemoveSource(files, file) error = nil, want a rejection: files are not sources")
	}
}
