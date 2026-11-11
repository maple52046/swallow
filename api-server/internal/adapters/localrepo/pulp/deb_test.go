package pulp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

func ubuntuSource() localrepodomain.Source {
	return localrepodomain.Source{
		Name: "ubuntu", Format: localrepodomain.FormatDeb, URL: "http://archive.ubuntu.com/ubuntu/",
		Suites: []string{"noble", "noble-updates", "noble-security"}, Components: []string{"main", "universe"},
		Architectures: []string{"amd64"},
	}
}

func seedUbuntuUpstream(f *fakePulp) {
	f.upstream["http://archive.ubuntu.com/ubuntu/"] = []object{
		{"package": "chrony", "version": "4.5-1", "architecture": "amd64", "relative_path": "pool/main/c/chrony/chrony_4.5-1_amd64.deb"},
		{"package": "nfs-common", "version": "1:2.6.4", "architecture": "amd64", "relative_path": "pool/main/n/nfs-utils/nfs-common_2.6.4_amd64.deb"},
	}
}

func TestEnsureDebSourceCreatesObjectsAndSyncsOnce(t *testing.T) {
	f := newFakePulp(t)
	signing := f.seedSigningService()
	seedUbuntuUpstream(f)
	repo := f.newRepository(t)
	ctx := context.Background()

	first, err := repo.EnsureSource(ctx, ubuntuSource())
	if err != nil {
		t.Fatalf("EnsureSource(ubuntu) error = %v", err)
	}
	wantState(t, "first EnsureSource", first.State, localrepodomain.SyncPending)
	if first.Serving {
		t.Errorf("first EnsureSource Serving = true, want false before the first sync")
	}

	remote := f.named(debRemotes, "ubuntu")
	if remote == nil || remote.str("url") != "http://archive.ubuntu.com/ubuntu/" || remote.str("policy") != "on_demand" ||
		remote.str("distributions") != "noble noble-updates noble-security" || remote.str("components") != "main universe" ||
		remote.str("architectures") != "amd64" {
		t.Errorf("deb remote = %v, want the source's URL, suites, components, and architectures synced on demand", remote)
	}
	repository := f.named(debRepositories, "ubuntu")
	if repository == nil || repository.str("signing_service") != signing || repository["autopublish"] != true {
		t.Errorf("deb repository = %v, want it signed by %s and published automatically", repository, signing)
	}
	syncs := f.requestsTo("POST", repository.href()+"sync/")
	if len(syncs) != 1 || syncs[0].body["remote"] != remote.href() || syncs[0].body["mirror"] != true {
		t.Fatalf("sync requests = %+v, want one mirror sync from the remote", syncs)
	}

	second, err := repo.EnsureSource(ctx, ubuntuSource())
	if err != nil {
		t.Fatalf("second EnsureSource(ubuntu) error = %v", err)
	}
	wantState(t, "second EnsureSource", second.State, localrepodomain.SyncReady)
	if second.ContentCount != 2 || second.LastSyncedAt == nil || !second.LastSyncedAt.Equal(f.clock()) || !second.Serving {
		t.Errorf("second EnsureSource = %+v, want 2 packages synced at %s and served", second, f.clock())
	}
	distribution := f.named(debDistributions, "ubuntu")
	if distribution == nil || distribution.str("base_path") != "deb/ubuntu" || distribution.str("repository") != repository.href() {
		t.Errorf("deb distribution = %v, want base path deb/ubuntu serving the repository", distribution)
	}
	if got := len(f.requestsTo("POST", repository.href()+"sync/")); got != 1 {
		t.Errorf("sync requests after the second call = %d, want still 1", got)
	}
	if got := len(f.requestsTo("POST", api(debPublications))); got != 0 {
		t.Errorf("publication requests = %d, want none: the repository publishes automatically", got)
	}
}

func TestEnsureDebSourceReportsInFlightSyncWithoutStartingAnother(t *testing.T) {
	f := newFakePulp(t)
	f.seedSigningService()
	repo := f.newRepository(t)
	ctx := context.Background()

	if _, err := repo.EnsureSource(ctx, ubuntuSource()); err != nil {
		t.Fatalf("EnsureSource(ubuntu) error = %v", err)
	}
	// A resync a day later is held in Pulp's queue, then runs.
	f.advance(resyncInterval + time.Minute)
	f.setHold(true)
	status, err := repo.EnsureSource(ctx, ubuntuSource())
	if err != nil {
		t.Fatalf("EnsureSource(ubuntu) after a day error = %v", err)
	}
	wantState(t, "resync", status.State, localrepodomain.SyncPending)
	if status.LastSyncedAt == nil {
		t.Errorf("resync LastSyncedAt = nil, want the previous successful sync")
	}
	if !status.Serving {
		t.Errorf("resync Serving = false, want true: the previous publication is still served")
	}

	status, err = repo.EnsureSource(ctx, ubuntuSource())
	if err != nil {
		t.Fatalf("EnsureSource(ubuntu) while queued error = %v", err)
	}
	wantState(t, "queued sync", status.State, localrepodomain.SyncPending)
	f.startRunning()
	status, err = repo.EnsureSource(ctx, ubuntuSource())
	if err != nil {
		t.Fatalf("EnsureSource(ubuntu) while running error = %v", err)
	}
	wantState(t, "running sync", status.State, localrepodomain.SyncSyncing)
	if !status.Serving {
		t.Errorf("running resync Serving = false, want true")
	}

	repository := f.named(debRepositories, "ubuntu")
	if got := len(f.requestsTo("POST", repository.href()+"sync/")); got != 2 {
		t.Errorf("sync requests = %d, want 2 (the first sync and one resync)", got)
	}
}

func TestEnsureDebSourceReportsFailedSyncAndRetriesLater(t *testing.T) {
	f := newFakePulp(t)
	f.seedSigningService()
	f.syncError["ubuntu"] = "Cannot connect to host archive.ubuntu.com:80 ssl:default [Name or service not known]"
	repo := f.newRepository(t)
	ctx := context.Background()

	if _, err := repo.EnsureSource(ctx, ubuntuSource()); err != nil {
		t.Fatalf("EnsureSource(ubuntu) error = %v", err)
	}
	status, err := repo.EnsureSource(ctx, ubuntuSource())
	if err != nil {
		t.Fatalf("EnsureSource(ubuntu) after the failure error = %v", err)
	}
	wantState(t, "failed sync", status.State, localrepodomain.SyncFailed)
	if !strings.Contains(status.Message, "Cannot connect to host archive.ubuntu.com") || status.LastSyncedAt != nil || status.Serving {
		t.Errorf("failed sync status = %+v, want Pulp's reason, no successful sync, and nothing served", status)
	}

	repository := f.named(debRepositories, "ubuntu")
	f.advance(retryInterval + time.Second)
	f.setSyncError("ubuntu", "")
	status, err = repo.EnsureSource(ctx, ubuntuSource())
	if err != nil {
		t.Fatalf("EnsureSource(ubuntu) after the retry interval error = %v", err)
	}
	wantState(t, "retried sync", status.State, localrepodomain.SyncPending)
	if got := len(f.requestsTo("POST", repository.href()+"sync/")); got != 2 {
		t.Errorf("sync requests = %d, want the failed one and a retry", got)
	}
}

func TestEnsureDebSourceKeepsServingThroughAFailedResync(t *testing.T) {
	f := newFakePulp(t)
	f.seedSigningService()
	repo := f.newRepository(t)
	ctx := context.Background()
	for range 2 {
		if _, err := repo.EnsureSource(ctx, ubuntuSource()); err != nil {
			t.Fatalf("EnsureSource(ubuntu) error = %v", err)
		}
	}

	f.advance(resyncInterval + time.Minute)
	f.setSyncError("ubuntu", "Cannot connect to host archive.ubuntu.com:80")
	for range 2 {
		if _, err := repo.EnsureSource(ctx, ubuntuSource()); err != nil {
			t.Fatalf("EnsureSource(ubuntu) during the resync error = %v", err)
		}
	}
	status, err := repo.EnsureSource(ctx, ubuntuSource())
	if err != nil {
		t.Fatalf("EnsureSource(ubuntu) after the failed resync error = %v", err)
	}
	wantState(t, "failed resync", status.State, localrepodomain.SyncFailed)
	if !status.Serving || status.LastSyncedAt == nil {
		t.Errorf("failed resync status = %+v, want the first sync still served", status)
	}
}

func TestEnsureDebSourceUpdatesChangedRemoteAndResyncs(t *testing.T) {
	f := newFakePulp(t)
	f.seedSigningService()
	repo := f.newRepository(t)
	ctx := context.Background()
	for range 2 {
		if _, err := repo.EnsureSource(ctx, ubuntuSource()); err != nil {
			t.Fatalf("EnsureSource(ubuntu) error = %v", err)
		}
	}

	f.advance(time.Hour)
	changed := ubuntuSource()
	changed.Components = []string{"main", "universe", "restricted"}
	status, err := repo.EnsureSource(ctx, changed)
	if err != nil {
		t.Fatalf("EnsureSource(changed ubuntu) error = %v", err)
	}
	wantState(t, "changed source", status.State, localrepodomain.SyncPending)
	if status.Serving {
		t.Errorf("changed source Serving = true, want false until it is synced with the new components")
	}
	remote := f.named(debRemotes, "ubuntu")
	patches := f.requestsTo("PATCH", remote.href())
	if len(patches) != 1 || patches[0].body["components"] != "main universe restricted" || len(patches[0].body) != 1 {
		t.Errorf("remote updates = %+v, want one update of the components only", patches)
	}
	if patches[0].cid == "" {
		t.Errorf("remote update carries no correlation id")
	}
	repository := f.named(debRepositories, "ubuntu")
	if got := len(f.requestsTo("POST", repository.href()+"sync/")); got != 2 {
		t.Errorf("sync requests = %d, want a resync after the remote changed", got)
	}
	if status, err = repo.EnsureSource(ctx, changed); err != nil {
		t.Fatalf("EnsureSource(changed ubuntu) after the resync error = %v", err)
	}
	if !status.Serving {
		t.Errorf("changed source Serving after its resync = false, want true")
	}

	// The same suites in another order are not a change.
	reordered := changed
	reordered.Suites = []string{"noble-security", "noble", "noble-updates"}
	if _, err := repo.EnsureSource(ctx, reordered); err != nil {
		t.Fatalf("EnsureSource(reordered ubuntu) error = %v", err)
	}
	if got := len(f.requestsTo("PATCH", remote.href())); got != 1 {
		t.Errorf("remote updates after reordering the suites = %d, want still 1", got)
	}
}

func TestEnsureDebSourceDoesNotChurnOnAValuePulpStoresDifferently(t *testing.T) {
	f := newFakePulp(t)
	f.seedSigningService()
	f.normalizeURLs = true
	f.seed(debRemotes, withDownloadTimeouts(map[string]any{
		"name": "ubuntu", "url": "http://archive.ubuntu.com/ubuntu", "distributions": "noble noble-updates noble-security",
		"components": "main universe", "architectures": "amd64", "policy": "on_demand",
	}))
	repo := f.newRepository(t)
	ctx := context.Background()

	for range 4 {
		if _, err := repo.EnsureSource(ctx, ubuntuSource()); err != nil {
			t.Fatalf("EnsureSource(ubuntu) error = %v", err)
		}
	}
	remote := f.named(debRemotes, "ubuntu")
	if got := len(f.requestsTo("PATCH", remote.href())); got != 1 {
		t.Errorf("remote updates = %d, want 1 within the retry interval", got)
	}
	repository := f.named(debRepositories, "ubuntu")
	if got := len(f.requestsTo("POST", repository.href()+"sync/")); got != 1 {
		t.Errorf("sync requests = %d, want 1: an update that changes nothing must not resync", got)
	}
}

func TestEnsureDebSourceAdoptsRepositoryPublishedByHand(t *testing.T) {
	f := newFakePulp(t)
	signing := f.seedSigningService()
	// What an earlier, manual setup leaves: an unsigned repository synced once without
	// automatic publishing, and a distribution pinned to an unsigned publication.
	remote := f.seed(debRemotes, withDownloadTimeouts(map[string]any{
		"name": "docker-ce", "url": "https://download.docker.com/linux/ubuntu", "distributions": "noble",
		"components": "stable", "architectures": "amd64", "policy": "on_demand",
	}))
	repoHref := f.seed(debRepositories, map[string]any{"name": "docker-ce", "autopublish": false, "signing_service": nil})
	f.mu.Lock()
	f.addTask("pulp_deb.app.tasks.synchronizing.synchronize", "", []string{repoHref, remote}, func() ([]string, string) {
		return []string{f.addVersion(repoHref, nil)}, ""
	})
	publication := f.store(debPublications, map[string]any{"repository": repoHref, "signing_service": nil})
	f.mu.Unlock()
	distribution := f.seed(debDistributions, map[string]any{"name": "docker-ce", "base_path": "deb/docker-ce", "publication": publication, "repository": nil})

	repo := f.newRepository(t)
	source := localrepodomain.Source{
		Name: "docker-ce", Format: localrepodomain.FormatDeb, URL: "https://download.docker.com/linux/ubuntu",
		Suites: []string{"noble"}, Components: []string{"stable"}, Architectures: []string{"amd64"},
	}
	status, err := repo.EnsureSource(context.Background(), source)
	if err != nil {
		t.Fatalf("EnsureSource(docker-ce) error = %v", err)
	}
	wantState(t, "adopted source", status.State, localrepodomain.SyncPending)
	if status.Serving {
		t.Errorf("adopted source Serving = true, want false while its newest publication is unsigned")
	}

	if got := f.object(repoHref); got.str("signing_service") != signing || got["autopublish"] != true {
		t.Errorf("repository after adoption = %v, want it signed and published automatically", got)
	}
	if got := f.object(distribution); got.str("repository") != repoHref || got["publication"] != nil {
		t.Errorf("distribution after adoption = %v, want it to serve the repository instead of a fixed publication", got)
	}
	publishes := f.requestsTo("POST", api(debPublications))
	if len(publishes) != 1 || publishes[0].body["signing_service"] != signing || publishes[0].body["structured"] != true ||
		publishes[0].body["simple"] != false {
		t.Errorf("publication requests = %+v, want one signed structured publication", publishes)
	}

	status, err = repo.EnsureSource(context.Background(), source)
	if err != nil {
		t.Fatalf("second EnsureSource(docker-ce) error = %v", err)
	}
	wantState(t, "published source", status.State, localrepodomain.SyncReady)
	if !status.Serving {
		t.Errorf("published source Serving = false, want true once a signed publication is served")
	}
}

func TestEnsureDebSourceRefusesWhatItCannotServe(t *testing.T) {
	f := newFakePulp(t)
	repo := f.newRepository(t)
	ctx := context.Background()

	noSuites := ubuntuSource()
	noSuites.Suites = nil
	badName := ubuntuSource()
	badName.Name = "deb/ubuntu"
	rpm := ubuntuSource()
	rpm.Format = localrepodomain.FormatRPM
	for name, source := range map[string]localrepodomain.Source{
		"no suites": noSuites, "a path as name": badName, "rpm": rpm, "no signing service": ubuntuSource(),
	} {
		_, err := repo.EnsureSource(ctx, source)
		var providerErr *localrepodomain.ProviderError
		if !errors.As(err, &providerErr) || providerErr.Kind != localrepodomain.ProviderErrorRejected {
			t.Errorf("EnsureSource(%s) error = %v, want a rejected ProviderError", name, err)
		}
	}
	if got := len(f.requestsTo("POST", api(debRemotes))); got != 0 {
		t.Errorf("remote creates = %d, want none for refused sources", got)
	}
}
