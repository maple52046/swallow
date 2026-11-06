package pulp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	"github.com/maple52046/swallow/internal/shared/redact"
)

// dockerHub is the host references name Docker Hub by; a reference without a host is on it.
const dockerHub = "docker.io"

const (
	// imageDownloadParallelism is how many images download at once in one process, so the declared
	// images do not all compete for the uplink at the same time.
	imageDownloadParallelism = 2
	// maxManifestBytes bounds a manifest or token answer; real ones are a few kilobytes.
	maxManifestBytes = 4 << 20
	// blobLookupBatch is how many digests one lookup of Pulp's blobs names, which keeps its request
	// line well within what a proxy in front of Pulp accepts.
	blobLookupBatch = 50
)

// manifestAccept lists the manifest media types the adapter understands: an OCI index or Docker
// manifest list, and the image manifests an index resolves to.
const manifestAccept = "application/vnd.oci.image.index.v1+json, " +
	"application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json, " +
	"application/vnd.docker.distribution.manifest.v2+json"

// The platform an index resolves to: the one swallow deploys.
const (
	imageOS   = "linux"
	imageArch = "amd64"
)

// EnsureImage makes Pulp's pull-through cache hold a declared image before the first host pulls it,
// and reports whether it does (localrepodomain.Repository). reference is fully qualified
// ("quay.io/k0sproject/pause:3.10.2-0"); a reference without a host names Docker Hub. The image must
// belong to an oci source the caller ensured (its host is a pull-through distribution).
//
// The call reads the image's manifests through the content origin's registry, as a host would, with
// the anonymous token Pulp's registry issues; an index resolves to its linux/amd64 image, the
// architecture swallow deploys. It then asks Pulp which blobs of that image (config and layers) it
// stores: a blob Pulp knows only from the upstream has no artifact, and a host pulling it would wait
// for the upstream. With every blob stored the image is ready, and SizeBytes is the total of the
// blob sizes the manifest declares.
//
// Otherwise the missing blobs are pulled through Pulp's registry in the background and their bytes
// discarded, since Pulp stores what it streams: pending while the download waits for one of
// imageDownloadParallelism slots, syncing while it runs. A download that failed, or ended with
// blobs still missing, makes the image failed with its reason for retryInterval; the next call after
// that starts it again. The download's state lives only in this process (imageDownloads), so a
// download a restart abandoned is simply started again; whether the image is stored is always
// Pulp's answer.
//
// A malformed reference, an index without a linux/amd64 image, an image the registry does not have,
// and an image Pulp cannot fetch from its upstream (its registry answers 5xx or 429 for it) are
// rejected errors, problems of this image; Pulp unreachable or refusing the credential are the
// unavailable and auth errors of any call.
func (r *Repository) EnsureImage(ctx context.Context, reference string) (localrepodomain.ImageStatus, error) {
	image, err := parseImageReference(reference)
	if err != nil {
		return localrepodomain.ImageStatus{}, rejected("ensure image", fmt.Sprintf("The image reference %q is malformed: %v.", reference, err))
	}
	puller := &registryPuller{http: r.registry, origin: r.contentOrigin, repository: image.host + "/" + image.path}
	resolved, err := puller.resolve(ctx, image.reference)
	if err != nil {
		return localrepodomain.ImageStatus{}, err
	}
	missing, err := r.missingBlobs(ctx, resolved.blobs)
	if err != nil {
		return localrepodomain.ImageStatus{}, err
	}
	key := r.contentOrigin + " " + reference
	if len(missing) == 0 {
		r.downloads.forget(key)
		return localrepodomain.ImageStatus{Reference: reference, State: localrepodomain.SyncReady, SizeBytes: resolved.size}, nil
	}
	state, message := r.downloads.ensure(ctx, key, r.now, func(download context.Context) error {
		for _, digest := range missing {
			if err := puller.drain(download, digest); err != nil {
				return err
			}
		}
		return nil
	})
	return localrepodomain.ImageStatus{Reference: reference, State: state, Message: redact.Text(message)}, nil
}

// missingBlobs returns the digests, in the given order, of the blobs Pulp holds no artifact for:
// blobs it does not know, and blobs its pull-through knows only from the upstream (it records a
// blob's upstream on the first request and stores the blob once it streamed it whole).
func (r *Repository) missingBlobs(ctx context.Context, digests []string) ([]string, error) {
	stored := map[string]bool{}
	for batch := range slices.Chunk(digests, blobLookupBatch) {
		found, err := r.api.list(ctx, containerBlobs, url.Values{
			"digest__in": {strings.Join(batch, ",")}, "limit": {listLimit}, "fields": {"digest,artifact"},
		})
		if err != nil {
			return nil, err
		}
		for _, blob := range found.Results {
			if blob.str("artifact") != "" {
				stored[blob.str("digest")] = true
			}
		}
	}
	var missing []string
	for _, digest := range digests {
		if !stored[digest] {
			missing = append(missing, digest)
		}
	}
	return missing, nil
}

// imageDownloads are the image downloads of one process. A RepositoryFactory holds one and shares
// it with every Repository it builds, so the parallelism bound and what is downloading hold across
// Integration changes. Downloads are keyed by content origin and reference.
//
// Nothing here is persisted: it only keeps one process from starting a download twice and paces the
// retries of a failed one. A new process starts empty, and whether an image is stored is read from
// Pulp on every call. It is safe for concurrent use.
type imageDownloads struct {
	slots chan struct{}

	mu    sync.Mutex
	byKey map[string]*imageDownload
}

// imageDownload is one image's download: in flight until finished is set, holding a slot while
// running, and once finished, failure is the reason the image is still incomplete.
type imageDownload struct {
	running  bool
	finished time.Time
	failure  string
}

func newImageDownloads() *imageDownloads {
	return &imageDownloads{slots: make(chan struct{}, imageDownloadParallelism), byKey: map[string]*imageDownload{}}
}

// ensure reports the download of key, starting it with run when none is in flight and the last one
// did not finish less than retryInterval ago: pending while it waits for a slot, syncing while it
// runs, failed with the reason of a recent one. The caller has found the image incomplete, so a
// recent download that read every blob also reads as failed.
//
// run executes in a goroutine that outlives the call on purpose: it is detached from ctx's
// cancellation but keeps its values, waits for a slot without a deadline (every slot holder is
// bounded), and gets a context that ends after downloadTimeout. The goroutine ends when run
// returns; its failure is logged and kept for the next call. A process that stops abandons it.
func (d *imageDownloads) ensure(ctx context.Context, key string, now func() time.Time, run func(context.Context) error) (localrepodomain.SyncState, string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if current, ok := d.byKey[key]; ok {
		switch {
		case current.finished.IsZero() && current.running:
			return localrepodomain.SyncSyncing, ""
		case current.finished.IsZero():
			return localrepodomain.SyncPending, ""
		case now().Sub(current.finished) < retryInterval:
			return localrepodomain.SyncFailed, current.failure
		}
	}
	download := &imageDownload{}
	d.byKey[key] = download
	background := context.WithoutCancel(ctx)
	go func() {
		d.slots <- struct{}{}
		defer func() { <-d.slots }()
		d.mu.Lock()
		download.running = true
		d.mu.Unlock()

		bounded, cancel := context.WithTimeout(background, downloadTimeout)
		err := run(bounded)
		cancel()
		failure := downloadFailure(err)
		if err != nil {
			slog.Warn("downloading an image through Pulp stopped", "image", key, "error", redact.Error(err))
		}
		d.mu.Lock()
		download.running, download.finished, download.failure = false, now(), failure
		d.mu.Unlock()
	}()
	return localrepodomain.SyncPending, ""
}

// forget drops what is known of key's download once the image is stored. A download still in flight
// is left to finish: it holds its slot until then.
func (d *imageDownloads) forget(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if current, ok := d.byKey[key]; ok && !current.finished.IsZero() {
		delete(d.byKey, key)
	}
}

// downloadFailure is the client-safe sentence of why an image is still incomplete after its download
// ended with err; nil means Pulp streamed every missing blob without storing it.
func downloadFailure(err error) string {
	again := fmt.Sprintf(" It is fetched again after %s.", retryInterval)
	var providerErr *localrepodomain.ProviderError
	switch {
	case err == nil:
		return "Pulp served every missing blob of the image but did not store them all." + again
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("The download of the image through Pulp did not finish within %s.", downloadTimeout) + again
	case errors.As(err, &providerErr) && providerErr.Detail != "":
		return strings.TrimSuffix(providerErr.Detail, ".") + "." + again
	default:
		return "The download of the image through Pulp stopped: " + redact.Text(err.Error()) + "." + again
	}
}

// imageReference is a parsed image reference: the registry host as references name it, the
// repository path at that host, and the tag or digest.
type imageReference struct {
	host      string
	path      string
	reference string
}

var (
	imagePathPattern = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*)*$`)
	imageTagPattern  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
	imageDigest      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// parseImageReference splits raw the way container runtimes do: the first path segment is a
// registry host when it holds a '.' or ':' or is "localhost", otherwise the image is on Docker Hub,
// where a one-segment path is an official image under library/.
func parseImageReference(raw string) (imageReference, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return imageReference{}, errors.New("it is empty")
	}
	ref := imageReference{reference: "latest"}
	if at := strings.Index(name, "@"); at >= 0 {
		name, ref.reference = name[:at], name[at+1:]
		if !imageDigest.MatchString(ref.reference) {
			return imageReference{}, errors.New("the digest is not sha256:<64 hex digits>")
		}
		// A digest names the image on its own; a tag beside it ("name:tag@sha256:...") is ignored.
		if colon := strings.LastIndex(name, ":"); colon > strings.LastIndex(name, "/") {
			name = name[:colon]
		}
	} else if colon := strings.LastIndex(name, ":"); colon > strings.LastIndex(name, "/") {
		name, ref.reference = name[:colon], name[colon+1:]
		if !imageTagPattern.MatchString(ref.reference) {
			return imageReference{}, errors.New("the tag holds characters a tag cannot")
		}
	}
	ref.host, ref.path = dockerHub, name
	if first, rest, found := strings.Cut(name, "/"); found && (strings.ContainsAny(first, ".:") || first == "localhost") {
		ref.host, ref.path = strings.ToLower(first), rest
	}
	if ref.host == "index.docker.io" {
		ref.host = dockerHub
	}
	if ref.host == dockerHub && !strings.Contains(ref.path, "/") {
		ref.path = "library/" + ref.path
	}
	if !imagePathPattern.MatchString(ref.path) {
		return imageReference{}, errors.New("the repository path holds characters a path cannot")
	}
	return ref, nil
}

// registryPuller pulls one image repository through Pulp's registry API at origin. It obtains an
// anonymous bearer token the first time the registry asks for one and reuses it for the image's
// later requests; it is used by one goroutine at a time (EnsureImage, then its download).
type registryPuller struct {
	http       *http.Client
	origin     string
	repository string
	token      string
}

// manifestDocument is the part of an index or image manifest the adapter reads.
type manifestDocument struct {
	MediaType string `json:"mediaType"`
	Manifests []struct {
		Digest   string `json:"digest"`
		Platform struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
		} `json:"platform"`
	} `json:"manifests"`
	Config manifestBlob   `json:"config"`
	Layers []manifestBlob `json:"layers"`
}

// manifestBlob is a blob an image manifest names, with the size it declares.
type manifestBlob struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// resolvedImage is the image a reference resolves to: the digests of its blobs (config first, then
// layers, each once) and their total declared size.
type resolvedImage struct {
	blobs []string
	size  int64
}

// resolve reads the manifest of reference, following an index to its linux/amd64 image, and
// returns that image's blobs.
func (p *registryPuller) resolve(ctx context.Context, reference string) (resolvedImage, error) {
	manifest, err := p.manifest(ctx, reference)
	if err != nil {
		return resolvedImage{}, err
	}
	if len(manifest.Manifests) > 0 {
		digest := ""
		for _, entry := range manifest.Manifests {
			if entry.Platform.OS == imageOS && entry.Platform.Architecture == imageArch {
				digest = entry.Digest
				break
			}
		}
		if digest == "" {
			return resolvedImage{}, rejected("ensure image", fmt.Sprintf("The image %s has no %s/%s variant.", p.image(reference), imageOS, imageArch))
		}
		if manifest, err = p.manifest(ctx, digest); err != nil {
			return resolvedImage{}, err
		}
	}
	var resolved resolvedImage
	for _, blob := range append([]manifestBlob{manifest.Config}, manifest.Layers...) {
		if blob.Digest == "" || slices.Contains(resolved.blobs, blob.Digest) {
			continue
		}
		resolved.blobs = append(resolved.blobs, blob.Digest)
		resolved.size += blob.Size
	}
	return resolved, nil
}

// image names the repository's image of reference (a tag or a digest) as a reader writes it.
func (p *registryPuller) image(reference string) string {
	if strings.HasPrefix(reference, "sha256:") {
		return p.repository + "@" + reference
	}
	return p.repository + ":" + reference
}

// manifest reads the manifest of reference (a tag or a digest). Pulp's pull-through fetches it from
// the upstream on the first request and stores it, which also creates the image's repository and
// remote. Pulp's registry refusing it is a problem of this image (imageProblem).
func (p *registryPuller) manifest(ctx context.Context, reference string) (manifestDocument, error) {
	resp, err := p.get(ctx, "/manifests/"+reference, manifestAccept)
	if err != nil {
		return manifestDocument{}, p.imageProblem(err, reference)
	}
	defer resp.Body.Close()
	var manifest manifestDocument
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxManifestBytes)).Decode(&manifest); err != nil {
		return manifestDocument{}, fmt.Errorf("decode manifest %s of %s: %w", reference, p.repository, err)
	}
	return manifest, nil
}

// imageProblem makes Pulp's registry answering a manifest request with 404 (the registry has no such
// image: MANIFEST_UNKNOWN, NAME_UNKNOWN) or with 5xx or 429 (its pull-through could not fetch the
// image from the upstream, or the upstream throttled it) a rejection of this image, so it fails
// alone while the rest of the repository converges. Any other failure, Pulp unreachable or refusing
// the anonymous pull or its token service failing, is returned as it is.
func (p *registryPuller) imageProblem(err error, reference string) error {
	operation := "GET /v2/" + p.repository + "/manifests/" + reference
	var providerErr *localrepodomain.ProviderError
	var apiErr *apiError
	if !errors.As(err, &providerErr) || providerErr.Operation != operation || !errors.As(err, &apiErr) {
		return err
	}
	codes := registryErrorCodes(apiErr.Body)
	var detail string
	switch {
	case apiErr.StatusCode == http.StatusNotFound && codes != "":
		detail = fmt.Sprintf("The registry has no image %s (%s).", p.image(reference), codes)
	case apiErr.StatusCode == http.StatusNotFound:
		detail = fmt.Sprintf("The registry has no image %s.", p.image(reference))
	case apiErr.StatusCode >= http.StatusInternalServerError, apiErr.StatusCode == http.StatusTooManyRequests:
		answer := fmt.Sprintf("HTTP %d", apiErr.StatusCode)
		if codes != "" {
			answer += ": " + codes
		}
		detail = fmt.Sprintf("Pulp could not fetch the image %s from its upstream registry (%s); it is tried again later.", p.image(reference), answer)
	default:
		return err
	}
	return &localrepodomain.ProviderError{
		Kind: localrepodomain.ProviderErrorRejected, Detail: detail, Endpoint: redact.URL(p.origin), Operation: operation, Err: err,
	}
}

// registryErrorCodes joins the codes of a registry error answer ({"errors": [{"code": ...}]}), for
// example "MANIFEST_UNKNOWN", or is "" when the body holds none.
func registryErrorCodes(body string) string {
	var answer struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if json.Unmarshal([]byte(body), &answer) != nil {
		return ""
	}
	var codes []string
	for _, entry := range answer.Errors {
		if entry.Code != "" {
			codes = append(codes, entry.Code)
		}
	}
	return strings.Join(codes, ", ")
}

// drain reads one blob to its end and discards it. A blob that delivers no byte for downloadStall
// is abandoned with an unavailable error.
func (p *registryPuller) drain(ctx context.Context, digest string) error {
	blobCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stalled atomic.Bool
	watchdog := time.AfterFunc(downloadStall, func() {
		stalled.Store(true)
		cancel()
	})
	defer watchdog.Stop()

	resp, err := p.get(blobCtx, "/blobs/"+digest, "*/*")
	if err != nil {
		if stalled.Load() {
			return p.stalled(digest)
		}
		return err
	}
	defer resp.Body.Close()
	buffer := make([]byte, 64<<10)
	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			watchdog.Reset(downloadStall)
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			if stalled.Load() {
				return p.stalled(digest)
			}
			return fmt.Errorf("read blob %s of %s: %w", digest, p.repository, err)
		}
	}
}

// stalled is the error of a blob download the watchdog ended.
func (p *registryPuller) stalled(digest string) error {
	return &localrepodomain.ProviderError{
		Kind:      localrepodomain.ProviderErrorUnavailable,
		Detail:    fmt.Sprintf("The download of the image through Pulp received nothing for %s.", downloadStall),
		Endpoint:  redact.URL(p.origin),
		Operation: "GET /v2/" + p.repository + "/blobs/" + digest,
	}
}

// get requests path beneath the image repository's registry URL. A 401 is answered once with an
// anonymous token from the challenge's realm and the request repeated, which also renews a token
// that expired during a long download. The caller closes the answer's body.
func (p *registryPuller) get(ctx context.Context, path, accept string) (*http.Response, error) {
	target := p.origin + "/v2/" + p.repository + path
	operation := "GET /v2/" + p.repository + path
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, fmt.Errorf("build registry request: %w", err)
		}
		req.Header.Set("Accept", accept)
		if p.token != "" {
			req.Header.Set("Authorization", "Bearer "+p.token)
		}
		resp, err := p.http.Do(req)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, translateError(err, p.origin, operation)
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			challenge := resp.Header.Get("WWW-Authenticate")
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyBytes))
			resp.Body.Close()
			if p.token, err = p.fetchToken(ctx, challenge); err != nil {
				return nil, err
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
			resp.Body.Close()
			return nil, translateError(&apiError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(raw))}, p.origin, operation)
		}
		return resp, nil
	}
}

var challengeParameter = regexp.MustCompile(`([A-Za-z]+)="([^"]*)"`)

// fetchToken answers a registry's bearer challenge (`Bearer realm="...",service="...",scope="..."`)
// with an anonymous token request to the realm. Pulp issues anonymous pull tokens for public
// distributions, which pull-through distributions are.
func (p *registryPuller) fetchToken(ctx context.Context, challenge string) (string, error) {
	const keyword = "bearer "
	if !strings.HasPrefix(strings.ToLower(challenge), keyword) {
		return "", rejected("ensure image", "Pulp's registry refused the anonymous pull without offering a bearer token.")
	}
	parameters := map[string]string{}
	for _, match := range challengeParameter.FindAllStringSubmatch(challenge[len(keyword):], -1) {
		parameters[strings.ToLower(match[1])] = match[2]
	}
	realm, err := url.Parse(parameters["realm"])
	if err != nil || (realm.Scheme != "http" && realm.Scheme != "https") || realm.Host == "" {
		return "", rejected("ensure image", "Pulp's registry named no usable token realm.")
	}
	query := realm.Query()
	for _, key := range []string{"service", "scope"} {
		if value := parameters[key]; value != "" {
			query.Set(key, value)
		}
	}
	realm.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm.String(), nil)
	if err != nil {
		return "", fmt.Errorf("build registry token request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := p.http.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", translateError(err, p.origin, "GET registry token")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return "", translateError(&apiError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(raw))}, p.origin, "GET registry token")
	}
	var answer struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxManifestBytes)).Decode(&answer); err != nil {
		return "", fmt.Errorf("decode registry token: %w", err)
	}
	if answer.Token != "" {
		return answer.Token, nil
	}
	if answer.AccessToken != "" {
		return answer.AccessToken, nil
	}
	return "", rejected("ensure image", "Pulp's registry answered the token request without a token.")
}
