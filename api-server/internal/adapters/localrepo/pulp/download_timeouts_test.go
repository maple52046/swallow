package pulp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

// wantTimeouts are downloadTimeouts as the fake Pulp holds them after decoding a request.
var wantTimeouts = map[string]any{"total_timeout": 21600.0, "sock_read_timeout": 300.0}

// Every remote a source creates carries the download timeouts, and a file download asks Pulp for
// them, so no download is cut at Pulp's 300-second default.
func TestNewRemotesAndFileDownloadsCarryTheDownloadTimeouts(t *testing.T) {
	f := newFakePulp(t)
	f.seedSigningService()
	f.downloads[k0sURL] = fakeDownload{sha256: sha256Of("k0s binary"), size: 262 << 20}
	repo := f.newRepository(t)
	ctx := context.Background()

	for _, source := range []localrepodomain.Source{
		ubuntuSource(),
		{Name: "quay.io", Format: localrepodomain.FormatOCI},
		{Name: "pypi", Format: localrepodomain.FormatPython},
	} {
		if _, err := repo.EnsureSource(ctx, source); err != nil {
			t.Fatalf("EnsureSource(%s) error = %v", source.Name, err)
		}
	}
	for collection, name := range map[string]string{debRemotes: "ubuntu", pullThroughRemotes: "quay.io", pythonRemotes: "pypi"} {
		remote := f.named(collection, name)
		if remote == nil || remote["total_timeout"] != wantTimeouts["total_timeout"] || remote["sock_read_timeout"] != wantTimeouts["sock_read_timeout"] {
			t.Errorf("remote %s%s = %v, want the download timeouts %v", collection, name, remote, wantTimeouts)
		}
	}

	if _, err := repo.EnsureFile(ctx, k0sFile()); err != nil {
		t.Fatalf("EnsureFile(k0s) error = %v", err)
	}
	downloads := f.requestsTo("POST", api(fileContent))
	if len(downloads) != 1 || !reflect.DeepEqual(downloads[0].body["downloader_config"], wantTimeouts) {
		t.Errorf("file downloads = %+v, want one with downloader_config %v", downloads, wantTimeouts)
	}
}

// An existing pull-through remote without the timeouts is updated, and so is the remote Pulp
// created for an image pulled through it; the remotes of another registry are left alone. Once
// updated, nothing is sent again.
func TestExistingRemotesConvergeToTheDownloadTimeouts(t *testing.T) {
	f := newFakePulp(t)
	unset := func(name string) map[string]any {
		return map[string]any{"name": name, "url": "https://quay.io", "total_timeout": nil, "sock_read_timeout": nil}
	}
	pullThrough := f.seed(pullThroughRemotes, unset("quay.io"))
	imageRemote := f.seed(containerRemotes, unset("quay.io/k0sproject/pause"))
	otherRegistry := f.seed(containerRemotes, unset("docker.io/library/busybox"))
	repo := f.newRepository(t)
	source := localrepodomain.Source{Name: "quay.io", Format: localrepodomain.FormatOCI}
	ctx := context.Background()

	if _, err := repo.EnsureSource(ctx, source); err != nil {
		t.Fatalf("EnsureSource(quay.io) error = %v", err)
	}
	for _, href := range []string{pullThrough, imageRemote} {
		patches := f.requestsTo("PATCH", href)
		if len(patches) != 1 || !reflect.DeepEqual(patches[0].body, wantTimeouts) {
			t.Errorf("updates of %s = %+v, want one with %v", href, patches, wantTimeouts)
		}
	}
	if patches := f.requestsTo("PATCH", otherRegistry); len(patches) != 0 {
		t.Errorf("updates of another registry's image remote = %+v, want none", patches)
	}

	status, err := repo.EnsureSource(ctx, source)
	if err != nil {
		t.Fatalf("second EnsureSource(quay.io) error = %v", err)
	}
	wantState(t, "second EnsureSource", status.State, localrepodomain.SyncReady)
	if got := len(f.requestsTo("PATCH", pullThrough)) + len(f.requestsTo("PATCH", imageRemote)); got != 2 {
		t.Errorf("updates after convergence = %d, want still 2", got)
	}
}

// Pulp returns a float field with a fraction, which counts as the whole number the adapter sends;
// null does not.
func TestSameValueComparesPulpFloatsAsNumbers(t *testing.T) {
	var returned object
	if err := json.Unmarshal([]byte(`{"total_timeout": 21600.0, "sock_read_timeout": null}`), &returned); err != nil {
		t.Fatal(err)
	}
	want := downloadTimeouts()
	if !sameValue(returned["total_timeout"], want["total_timeout"]) {
		t.Errorf("sameValue(%v, %v) = false, want 21600.0 to equal 21600", returned["total_timeout"], want["total_timeout"])
	}
	if sameValue(returned["sock_read_timeout"], want["sock_read_timeout"]) {
		t.Errorf("sameValue(null, %v) = true, want an unset timeout to differ", want["sock_read_timeout"])
	}
}
