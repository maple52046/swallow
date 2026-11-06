package pulp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

func TestParseImageReference(t *testing.T) {
	digest := "sha256:" + sha256Of("manifest")
	tests := []struct {
		raw  string
		want imageReference
	}{
		{"quay.io/k0sproject/pause:3.10.2-0", imageReference{host: "quay.io", path: "k0sproject/pause", reference: "3.10.2-0"}},
		{"docker.io/grafana/grafana:12.4.12", imageReference{host: "docker.io", path: "grafana/grafana", reference: "12.4.12"}},
		{"busybox", imageReference{host: "docker.io", path: "library/busybox", reference: "latest"}},
		{"rocm/pytorch:latest", imageReference{host: "docker.io", path: "rocm/pytorch", reference: "latest"}},
		{"index.docker.io/library/redis:7", imageReference{host: "docker.io", path: "library/redis", reference: "7"}},
		{"registry.k8s.io/pause@" + digest, imageReference{host: "registry.k8s.io", path: "pause", reference: digest}},
		{"docker.io/grafana/grafana:12.4.12@" + digest, imageReference{host: "docker.io", path: "grafana/grafana", reference: digest}},
		{"localhost/app:1", imageReference{host: "localhost", path: "app", reference: "1"}},
	}
	for _, tc := range tests {
		got, err := parseImageReference(tc.raw)
		if err != nil || got != tc.want {
			t.Errorf("parseImageReference(%q) = %+v, %v; want %+v", tc.raw, got, err, tc.want)
		}
	}
	for _, raw := range []string{"", "quay.io/Upper/Case:1", "quay.io/a/b@sha256:short", "quay.io/a/b:bad tag"} {
		if _, err := parseImageReference(raw); err == nil {
			t.Errorf("parseImageReference(%q) error = nil, want a malformed reference", raw)
		}
	}
}

const grafanaReference = "docker.io/grafana/grafana:12.4.12"

// grafanaBlobs are the blobs of the grafana image the fake registry serves: the config and two
// layers of its linux/amd64 image, and a layer of its arm64 image, which must never be fetched.
type grafanaBlobs struct {
	config, layer, bigLayer, armLayer string
}

// grafanaSize is the total size the amd64 manifest declares for its blobs.
const grafanaSize = 1_200 + 3_000_000 + 83_800_000

// seedGrafana makes the fake registry serve grafanaReference as an index of an arm64 and an amd64
// image. Pulp holds none of its blobs until a test seeds them or the registry streams them.
func seedGrafana(f *fakePulp) grafanaBlobs {
	amd64 := "sha256:" + sha256Of("amd64 manifest")
	blobs := grafanaBlobs{
		config: "sha256:" + sha256Of("config"), layer: "sha256:" + sha256Of("layer"),
		bigLayer: "sha256:" + sha256Of("big layer"), armLayer: "sha256:" + sha256Of("arm layer"),
	}
	f.manifests["docker.io/grafana/grafana/manifests/12.4.12"] = fakeManifest{
		mediaType: "application/vnd.oci.image.index.v1+json",
		body: fmt.Sprintf(`{"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[
			{"digest":"sha256:%s","platform":{"os":"linux","architecture":"arm64"}},
			{"digest":"%s","platform":{"os":"linux","architecture":"amd64"}}]}`, sha256Of("arm manifest"), amd64),
	}
	f.manifests["docker.io/grafana/grafana/manifests/"+amd64] = fakeManifest{
		mediaType: "application/vnd.oci.image.manifest.v1+json",
		body: fmt.Sprintf(`{"config":{"digest":"%s","size":1200},"layers":[{"digest":"%s","size":3000000},{"digest":"%s","size":83800000}]}`,
			blobs.config, blobs.layer, blobs.bigLayer),
	}
	f.blobs[blobs.config], f.blobs[blobs.layer], f.blobs[blobs.bigLayer], f.blobs[blobs.armLayer] = "{}", "layer bytes", "big layer bytes", "arm bytes"
	return blobs
}

// ensureImage calls EnsureImage and fails the test on an error.
func ensureImage(t *testing.T, repo *Repository, reference string) localrepodomain.ImageStatus {
	t.Helper()
	status, err := repo.EnsureImage(context.Background(), reference)
	if err != nil {
		t.Fatalf("EnsureImage(%s) error = %v", reference, err)
	}
	return status
}

// waitFor polls condition until it holds, failing the test after a few seconds; the downloads it
// waits for run in the background.
func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// An image Pulp stores whole is ready with the size its amd64 manifest declares, found with one
// lookup of its blobs, and nothing is downloaded.
func TestEnsureImageReportsAStoredImageReady(t *testing.T) {
	f := newFakePulp(t)
	blobs := seedGrafana(f)
	for _, digest := range []string{blobs.config, blobs.layer, blobs.bigLayer} {
		f.seedBlob(digest, true)
	}
	repo := f.newRepository(t)

	got := ensureImage(t, repo, grafanaReference)
	want := localrepodomain.ImageStatus{Reference: grafanaReference, State: localrepodomain.SyncReady, SizeBytes: grafanaSize}
	if got != want {
		t.Errorf("EnsureImage(grafana) = %+v, want %+v", got, want)
	}
	lookups := f.requestsTo("GET", api(containerBlobs))
	if len(lookups) != 1 || lookups[0].query.Get("digest__in") != strings.Join([]string{blobs.config, blobs.layer, blobs.bigLayer}, ",") {
		t.Errorf("blob lookups = %+v, want one naming the amd64 image's blobs", lookups)
	}
	for _, digest := range []string{blobs.config, blobs.layer, blobs.bigLayer, blobs.armLayer} {
		if got := f.blobRequestCount(digest); got != 0 {
			t.Errorf("blob %s requests = %d, want none for a stored image", digest, got)
		}
	}
	if tokens := f.requestsTo("GET", "/token/"); len(tokens) != 1 || tokens[0].query.Get("scope") != "repository:docker.io/grafana/grafana:pull" {
		t.Errorf("token requests = %+v, want one anonymous token for the image's pull scope", tokens)
	}
}

// An image Pulp holds only partly is downloaded in the background, only its missing blobs (one
// Pulp knows only from the upstream, one it does not know), and reads syncing while the download
// runs and ready once Pulp stored them.
func TestEnsureImageDownloadsTheMissingBlobsUntilStored(t *testing.T) {
	f := newFakePulp(t)
	blobs := seedGrafana(f)
	f.seedBlob(blobs.config, true)
	f.seedBlob(blobs.layer, false)
	release := f.holdBlobs(t)
	repo := f.newRepository(t)

	wantState(t, "first EnsureImage", ensureImage(t, repo, grafanaReference).State, localrepodomain.SyncPending)
	waitFor(t, "the missing layer is requested", func() bool { return f.blobRequestCount(blobs.layer) == 1 })
	syncing := ensureImage(t, repo, grafanaReference)
	wantState(t, "EnsureImage while downloading", syncing.State, localrepodomain.SyncSyncing)
	if syncing.SizeBytes != 0 {
		t.Errorf("SizeBytes while downloading = %d, want 0 until the image is stored", syncing.SizeBytes)
	}

	release()
	waitFor(t, "the image is ready", func() bool { return ensureImage(t, repo, grafanaReference).State == localrepodomain.SyncReady })
	for digest, want := range map[string]int{blobs.config: 0, blobs.layer: 1, blobs.bigLayer: 1, blobs.armLayer: 0} {
		if got := f.blobRequestCount(digest); got != want {
			t.Errorf("blob %s requests = %d, want %d", digest, got, want)
		}
	}
}

// A download that fails makes the image failed with the reason until retryInterval has passed,
// without fetching again; the next call after that downloads what is still missing.
func TestEnsureImageRetriesAFailedDownloadAfterRetryInterval(t *testing.T) {
	f := newFakePulp(t)
	blobs := seedGrafana(f)
	f.blobFailures[blobs.bigLayer] = 502
	repo := f.newRepository(t)

	ensureImage(t, repo, grafanaReference)
	var failed localrepodomain.ImageStatus
	waitFor(t, "the download fails", func() bool {
		failed = ensureImage(t, repo, grafanaReference)
		return failed.State == localrepodomain.SyncFailed
	})
	if !strings.Contains(failed.Message, "HTTP 502") || !strings.Contains(failed.Message, "fetched again after "+retryInterval.String()) {
		t.Errorf("failed message = %q, want Pulp's answer and when it is fetched again", failed.Message)
	}

	f.advance(retryInterval - time.Second)
	wantState(t, "EnsureImage within retryInterval", ensureImage(t, repo, grafanaReference).State, localrepodomain.SyncFailed)
	if got := f.blobRequestCount(blobs.bigLayer); got != 1 {
		t.Errorf("big layer requests within retryInterval = %d, want 1", got)
	}

	f.mu.Lock()
	delete(f.blobFailures, blobs.bigLayer)
	f.mu.Unlock()
	f.advance(2 * time.Second)
	wantState(t, "EnsureImage after retryInterval", ensureImage(t, repo, grafanaReference).State, localrepodomain.SyncPending)
	waitFor(t, "the image is ready", func() bool { return ensureImage(t, repo, grafanaReference).State == localrepodomain.SyncReady })
	if got := f.blobRequestCount(blobs.layer); got != 1 {
		t.Errorf("layer requests = %d, want 1: the retry fetches only what is still missing", got)
	}
}

// A malformed reference, an index without a linux/amd64 image, an image the registry does not have,
// and an image Pulp cannot fetch from its upstream are rejections of that image; a content origin
// that cannot be reached at all is not.
func TestEnsureImageRefusesWhatCannotBeFetched(t *testing.T) {
	f := newFakePulp(t)
	f.manifests["quay.io/k0sproject/arm-only/manifests/1"] = fakeManifest{
		mediaType: "application/vnd.oci.image.index.v1+json",
		body:      fmt.Sprintf(`{"manifests":[{"digest":"sha256:%s","platform":{"os":"linux","architecture":"arm64"}}]}`, sha256Of("arm")),
	}
	f.manifestFailures["registry.k8s.io/nfd/node-feature-discovery/manifests/v0.18.3"] = 502
	repo := f.newRepository(t)

	for _, tc := range []struct {
		name, reference, mentions string
		status                    int
	}{
		{"malformed", "quay.io/a/b:bad tag", "is malformed", 0},
		{"unknown image", "quay.io/missing/image:1", "The registry has no image quay.io/missing/image:1 (MANIFEST_UNKNOWN).", 404},
		{"no amd64 variant", "quay.io/k0sproject/arm-only:1", "has no linux/amd64 variant", 0},
		{"upstream unreachable", "registry.k8s.io/nfd/node-feature-discovery:v0.18.3", "could not fetch the image registry.k8s.io/nfd/node-feature-discovery:v0.18.3", 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := repo.EnsureImage(context.Background(), tc.reference)
			var providerErr *localrepodomain.ProviderError
			if !errors.As(err, &providerErr) || providerErr.Kind != localrepodomain.ProviderErrorRejected || !strings.Contains(providerErr.Detail, tc.mentions) {
				t.Fatalf("EnsureImage(%s) error = %v, want a rejection mentioning %q", tc.reference, err, tc.mentions)
			}
			if tc.status != 0 && !isStatus(err, tc.status) {
				t.Errorf("EnsureImage(%s) error = %v, want it to carry Pulp's HTTP %d", tc.reference, err, tc.status)
			}
		})
	}

	unreachable := f.newRepository(t)
	unreachable.contentOrigin = "http://127.0.0.1:1"
	_, err := unreachable.EnsureImage(context.Background(), grafanaReference)
	var providerErr *localrepodomain.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != localrepodomain.ProviderErrorUnavailable {
		t.Errorf("EnsureImage through an unreachable origin error = %v, want an unavailable ProviderError", err)
	}
}

// The Repositories of one factory share its downloads, so a second one reports the download in
// flight instead of starting it again, while a new process, which knows of no download, starts again
// one the previous process abandoned.
func TestEnsureImageRestartsADownloadAnAbandonedProcessLeft(t *testing.T) {
	f := newFakePulp(t)
	blobs := seedGrafana(f)
	release := f.holdBlobs(t)
	shared := newImageDownloads()

	ensureImage(t, f.newRepositorySharing(t, shared), grafanaReference)
	waitFor(t, "the first process requests the config", func() bool { return f.blobRequestCount(blobs.config) == 1 })
	wantState(t, "EnsureImage of the same factory", ensureImage(t, f.newRepositorySharing(t, shared), grafanaReference).State, localrepodomain.SyncSyncing)
	if got := f.blobRequestCount(blobs.config); got != 1 {
		t.Errorf("config requests after the same factory asked again = %d, want 1", got)
	}

	restarted := f.newRepository(t)
	wantState(t, "EnsureImage of a new process", ensureImage(t, restarted, grafanaReference).State, localrepodomain.SyncPending)
	waitFor(t, "the new process requests the config", func() bool { return f.blobRequestCount(blobs.config) == 2 })
	release()
	waitFor(t, "the image is ready", func() bool { return ensureImage(t, restarted, grafanaReference).State == localrepodomain.SyncReady })
}
