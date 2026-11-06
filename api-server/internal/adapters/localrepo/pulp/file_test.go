package pulp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

const k0sURL = "https://github.com/k0sproject/k0s/releases/download/v1.36.3+k0s.2/k0s-v1.36.3+k0s.2-amd64"

func k0sFile() localrepodomain.File {
	return localrepodomain.File{
		Path: "k0s/v1.36.3+k0s.2/k0s-v1.36.3+k0s.2-amd64", URL: k0sURL, SHA256: sha256Of("k0s binary"),
	}
}

func TestEnsureFileDownloadsOnceAndReportsReady(t *testing.T) {
	f := newFakePulp(t)
	f.downloads[k0sURL] = fakeDownload{sha256: sha256Of("k0s binary"), size: 262 << 20}
	repo := f.newRepository(t)
	ctx := context.Background()

	first, err := repo.EnsureFile(ctx, k0sFile())
	if err != nil {
		t.Fatalf("EnsureFile(k0s) error = %v", err)
	}
	wantState(t, "first EnsureFile", first.State, localrepodomain.SyncPending)

	repository := f.named(fileRepositories, filesName)
	if repository == nil || repository["autopublish"] != true {
		t.Errorf("file repository = %v, want it published automatically", repository)
	}
	downloads := f.requestsTo("POST", api(fileContent))
	if len(downloads) != 1 || downloads[0].body["file_url"] != k0sURL || downloads[0].body["relative_path"] != k0sFile().Path ||
		downloads[0].body["repository"] != repository.href() || downloads[0].cid == "" {
		t.Fatalf("file downloads = %+v, want one correlated download into the files repository", downloads)
	}

	second, err := repo.EnsureFile(ctx, k0sFile())
	if err != nil {
		t.Fatalf("second EnsureFile(k0s) error = %v", err)
	}
	wantState(t, "second EnsureFile", second.State, localrepodomain.SyncReady)
	if second.SizeBytes != 262<<20 || second.Path != k0sFile().Path {
		t.Errorf("second EnsureFile = %+v, want the file's size", second)
	}
	distribution := f.named(fileDistributions, filesName)
	if distribution == nil || distribution.str("base_path") != filesName || distribution.str("repository") != repository.href() {
		t.Errorf("file distribution = %v, want base path files serving the repository", distribution)
	}
	if got := len(f.requestsTo("POST", api(fileContent))); got != 1 {
		t.Errorf("file downloads = %d, want still 1", got)
	}
}

func TestEnsureFileRemovesMismatchAndRetriesLater(t *testing.T) {
	f := newFakePulp(t)
	f.downloads[k0sURL] = fakeDownload{sha256: sha256Of("something else"), size: 10}
	repo := f.newRepository(t)
	ctx := context.Background()

	if _, err := repo.EnsureFile(ctx, k0sFile()); err != nil {
		t.Fatalf("EnsureFile(k0s) error = %v", err)
	}
	status, err := repo.EnsureFile(ctx, k0sFile())
	if err != nil {
		t.Fatalf("EnsureFile(k0s) after the download error = %v", err)
	}
	wantState(t, "mismatched download", status.State, localrepodomain.SyncFailed)
	if !strings.Contains(status.Message, sha256Of("something else")) || !strings.Contains(status.Message, "downloaded from") {
		t.Errorf("mismatch message = %q, want the downloaded checksum", status.Message)
	}
	repository := f.named(fileRepositories, filesName)
	if removals := f.requestsTo("POST", repository.href()+"modify/"); len(removals) != 1 || removals[0].body["remove_content_units"] == nil {
		t.Fatalf("repository changes = %+v, want the mismatching file removed", removals)
	}
	if held := f.latestContents(repository.href()); len(held) != 0 {
		t.Errorf("files repository holds %v, want the mismatching file gone", held)
	}

	status, err = repo.EnsureFile(ctx, k0sFile())
	if err != nil {
		t.Fatalf("EnsureFile(k0s) after the removal error = %v", err)
	}
	wantState(t, "removed mismatch", status.State, localrepodomain.SyncFailed)
	if got := len(f.requestsTo("POST", api(fileContent))); got != 1 {
		t.Errorf("file downloads within the retry interval = %d, want 1", got)
	}

	f.advance(retryInterval + time.Second)
	f.setDownload(k0sURL, fakeDownload{sha256: sha256Of("k0s binary"), size: 262 << 20})
	status, err = repo.EnsureFile(ctx, k0sFile())
	if err != nil {
		t.Fatalf("EnsureFile(k0s) after the retry interval error = %v", err)
	}
	wantState(t, "retried download", status.State, localrepodomain.SyncPending)
	status, err = repo.EnsureFile(ctx, k0sFile())
	if err != nil {
		t.Fatalf("EnsureFile(k0s) after the retried download error = %v", err)
	}
	wantState(t, "good download", status.State, localrepodomain.SyncReady)
}

func TestEnsureFileReportsFailedDownload(t *testing.T) {
	f := newFakePulp(t)
	repo := f.newRepository(t)
	ctx := context.Background()

	if _, err := repo.EnsureFile(ctx, k0sFile()); err != nil {
		t.Fatalf("EnsureFile(k0s) error = %v", err)
	}
	status, err := repo.EnsureFile(ctx, k0sFile())
	if err != nil {
		t.Fatalf("EnsureFile(k0s) after the download error = %v", err)
	}
	wantState(t, "failed download", status.State, localrepodomain.SyncFailed)
	if !strings.Contains(status.Message, "Not Found") {
		t.Errorf("failed download message = %q, want Pulp's reason", status.Message)
	}
}

func TestEnsureFileAddsContentPulpAlreadyHolds(t *testing.T) {
	f := newFakePulp(t)
	artifact := f.seed("/artifacts/", map[string]any{"size": 42.0})
	content := f.seed(fileContent, map[string]any{"relative_path": k0sFile().Path, "sha256": k0sFile().SHA256, "artifact": artifact})
	repo := f.newRepository(t)
	ctx := context.Background()

	status, err := repo.EnsureFile(ctx, k0sFile())
	if err != nil {
		t.Fatalf("EnsureFile(k0s) error = %v", err)
	}
	wantState(t, "known content", status.State, localrepodomain.SyncPending)
	repository := f.named(fileRepositories, filesName)
	adds := f.requestsTo("POST", repository.href()+"modify/")
	if len(adds) != 1 || len(anyStrings(adds[0].body["add_content_units"])) != 1 || anyStrings(adds[0].body["add_content_units"])[0] != content {
		t.Errorf("repository changes = %+v, want the known content added", adds)
	}
	if got := len(f.requestsTo("POST", api(fileContent))); got != 0 {
		t.Errorf("file downloads = %d, want none for content Pulp holds", got)
	}

	status, err = repo.EnsureFile(ctx, k0sFile())
	if err != nil {
		t.Fatalf("second EnsureFile(k0s) error = %v", err)
	}
	wantState(t, "added content", status.State, localrepodomain.SyncReady)
	if status.SizeBytes != 42 {
		t.Errorf("SizeBytes = %d, want the artifact's 42", status.SizeBytes)
	}
}

func TestEnsureFilePublishesARepositoryFilledWithoutPublishing(t *testing.T) {
	f := newFakePulp(t)
	artifact := f.seed("/artifacts/", map[string]any{"size": 42.0})
	content := f.seed(fileContent, map[string]any{"relative_path": k0sFile().Path, "sha256": k0sFile().SHA256, "artifact": artifact})
	repoHref := f.seed(fileRepositories, map[string]any{"name": filesName, "autopublish": false})
	f.mu.Lock()
	f.addVersion(repoHref, []string{content})
	f.mu.Unlock()
	repo := f.newRepository(t)
	ctx := context.Background()

	status, err := repo.EnsureFile(ctx, k0sFile())
	if err != nil {
		t.Fatalf("EnsureFile(k0s) error = %v", err)
	}
	wantState(t, "unpublished file", status.State, localrepodomain.SyncPending)
	if publishes := f.requestsTo("POST", api(filePublications)); len(publishes) != 1 || publishes[0].body["repository"] != repoHref {
		t.Errorf("publication requests = %+v, want one of the files repository", publishes)
	}

	status, err = repo.EnsureFile(ctx, k0sFile())
	if err != nil {
		t.Fatalf("second EnsureFile(k0s) error = %v", err)
	}
	wantState(t, "published file", status.State, localrepodomain.SyncReady)
	if got := len(f.requestsTo("POST", api(fileContent))); got != 0 {
		t.Errorf("file downloads = %d, want none for a file the repository holds", got)
	}
}

func TestEnsureFileWaitsForAnotherDownload(t *testing.T) {
	f := newFakePulp(t)
	repo := f.newRepository(t)
	ctx := context.Background()
	other := localrepodomain.File{Path: "helm/helm-v3.20.0-linux-amd64.tar.gz", URL: "https://get.helm.sh/helm-v3.20.0-linux-amd64.tar.gz", SHA256: sha256Of("helm")}
	repoHref := f.seed(fileRepositories, map[string]any{"name": filesName, "autopublish": true})
	f.seed(fileDistributions, map[string]any{"name": filesName, "base_path": filesName, "repository": repoHref, "publication": nil})

	f.setHold(true)
	if _, err := repo.EnsureFile(ctx, other); err != nil {
		t.Fatalf("EnsureFile(helm) error = %v", err)
	}
	f.startRunning()
	status, err := repo.EnsureFile(ctx, other)
	if err != nil {
		t.Fatalf("EnsureFile(helm) while it downloads error = %v", err)
	}
	wantState(t, "running download", status.State, localrepodomain.SyncSyncing)

	status, err = repo.EnsureFile(ctx, k0sFile())
	if err != nil {
		t.Fatalf("EnsureFile(k0s) while helm downloads error = %v", err)
	}
	wantState(t, "waiting download", status.State, localrepodomain.SyncPending)
	if got := len(f.requestsTo("POST", api(fileContent))); got != 1 {
		t.Errorf("file downloads = %d, want only the one in progress", got)
	}
}

func TestEnsureFileRefusesMalformedFiles(t *testing.T) {
	f := newFakePulp(t)
	repo := f.newRepository(t)
	for name, file := range map[string]localrepodomain.File{
		"absolute path":  {Path: "/etc/passwd", URL: k0sURL, SHA256: sha256Of("x")},
		"parent segment": {Path: "k0s/../../x", URL: k0sURL, SHA256: sha256Of("x")},
		"ftp URL":        {Path: "k0s/x", URL: "ftp://example.com/x", SHA256: sha256Of("x")},
		"URL with user":  {Path: "k0s/x", URL: "https://user:pass@example.com/x", SHA256: sha256Of("x")},
		"short checksum": {Path: "k0s/x", URL: k0sURL, SHA256: "abc"},
	} {
		_, err := repo.EnsureFile(context.Background(), file)
		var providerErr *localrepodomain.ProviderError
		if !errors.As(err, &providerErr) || providerErr.Kind != localrepodomain.ProviderErrorRejected {
			t.Errorf("EnsureFile(%s) error = %v, want a rejected ProviderError", name, err)
		}
		if strings.Contains(err.Error(), "pass@") {
			t.Errorf("EnsureFile(%s) error %q shows the URL's password", name, err)
		}
	}
	if got := len(f.requestsTo("POST", "")); got != 0 {
		t.Errorf("Pulp writes = %d, want none for refused files", got)
	}
}
