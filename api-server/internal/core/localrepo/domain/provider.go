package domain

import (
	"context"
	"strings"
	"time"

	"github.com/maple52046/swallow/internal/shared/redact"
)

// Format is the kind of content a Repository Source carries, in the core's neutral words: OS
// packages of the two package formats (deb, rpm), container images (oci), Python packages (python),
// and single files (file). The values are stored and on the API, so they never change.
type Format string

const (
	FormatDeb    Format = "deb"
	FormatRPM    Format = "rpm"
	FormatOCI    Format = "oci"
	FormatPython Format = "python"
	FormatFile   Format = "file"
)

// Formats lists every Format, for validation and for telling a user what they may have meant.
var Formats = []Format{FormatDeb, FormatRPM, FormatOCI, FormatPython, FormatFile}

// Valid reports whether f is one of Formats.
func (f Format) Valid() bool {
	for _, format := range Formats {
		if f == format {
			return true
		}
	}
	return false
}

// Source is one upstream a Local Repository mirrors or caches (glossary: Repository Source). Name
// identifies it within the Site and becomes part of the address hosts read it at, so it is stable.
// Suites, Components, and Architectures narrow an OS package source and are empty for the other
// formats; a source whose content is pulled through on demand (oci, python) is addressed by URL
// alone.
type Source struct {
	Name          string
	Format        Format
	URL           string
	Suites        []string
	Components    []string
	Architectures []string
}

// File is one file a Local Repository serves at Path, fetched from URL and verified against SHA256
// (lower-case hex). Path is relative to the repository's file root and is part of the address hosts
// read it at.
type File struct {
	Path   string
	URL    string
	SHA256 string
}

// SyncState is a Repository Source's, File's, or declared image's sync state as the provider
// reports it. pending: the provider accepted the item but has not started fetching it; syncing: a
// fetch runs; ready: the content is served; failed: the last fetch failed and Message says why. The
// values are on the API.
type SyncState string

const (
	SyncPending SyncState = "pending"
	SyncSyncing SyncState = "syncing"
	SyncReady   SyncState = "ready"
	SyncFailed  SyncState = "failed"
)

// Settled reports whether the provider has finished with the item for now: ready or failed. An
// unsettled item is still being fetched, so its controller looks at it again soon.
func (s SyncState) Settled() bool {
	return s == SyncReady || s == SyncFailed
}

// SourceStatus is the provider's report on one Repository Source. LastSyncedAt is when its content
// last synced successfully, nil before the first success; ContentCount is how many items (packages,
// images, files) it serves, 0 when unknown.
type SourceStatus struct {
	Name         string
	Format       Format
	State        SyncState
	Message      string
	LastSyncedAt *time.Time
	ContentCount int
}

// FileStatus is the provider's report on one File; SizeBytes is 0 until the file was fetched.
type FileStatus struct {
	Path      string
	State     SyncState
	Message   string
	SizeBytes int64
}

// ImageStatus is the provider's report on one declared container image (a fully qualified
// reference). It is ready only once the provider stores every blob of the image (its config and
// layers; of an index, those of the variant swallow deploys), so that no host pull reaches the
// upstream; an image the provider merely knows the manifest of is still being fetched. SizeBytes is
// the total size of those blobs, 0 until known.
type ImageStatus struct {
	Reference string
	State     SyncState
	Message   string
	SizeBytes int64
}

// DebSource is how a host reads one deb Repository Source: the source's name, the URI the provider
// publishes it at, and the suites and components it publishes.
type DebSource struct {
	Name       string
	URI        string
	Suites     []string
	Components []string
}

// HostEndpoints are the addresses a host reads the Local Repository's content at, as the provider
// publishes them: Deb, one entry per deb source it serves; DebSigningKey, the ASCII-armored public
// key the deb metadata is signed with ("" when unsigned); Registry, the "host:port" of the container
// registry; RegistryMirrors, the pull-through mirror URL of each upstream registry host; PythonIndex,
// the URL of the Python simple index; Files, the URL prefix a File's Path is appended to. A field is
// empty when the provider serves nothing of that kind. None of them carries a credential.
type HostEndpoints struct {
	Deb             []DebSource
	DebSigningKey   string
	Registry        string
	RegistryMirrors map[string]string
	PythonIndex     string
	Files           string
}

// ContentQuery asks for one page of a Local Repository's content of Format, optionally only from
// Source (a Repository Source name) and only items whose name contains Search. Offset and Limit
// select the page; Limit is positive.
type ContentQuery struct {
	Format Format
	Source string
	Search string
	Offset int
	Limit  int
}

// ContentItem is one item a Local Repository serves. Which fields are set depends on the format:
// Version and Architecture for packages, Path for files and images, SizeBytes when known.
type ContentItem struct {
	Name         string
	Version      string
	Architecture string
	Source       string
	Path         string
	SizeBytes    int64
}

// ContentPage is one page of content and Total, the number of items matching the query across all
// pages.
type ContentPage struct {
	Items []ContentItem
	Total int
}

// Repository is one Site's Local Repository provider. Ensure* are idempotent and never block on a
// sync: they create or update what serves the item, start a sync when it is new, changed or due, and
// report the current state.
//
// EnsureImage makes the provider hold a declared image (a fully qualified reference) through the
// pull-through source of its registry host, which the caller ensured first, so that the first host
// pulling it does not wait for the upstream. Like the other Ensure* it never waits for the
// download: it reports ready once the provider stores the whole image, and otherwise starts or
// keeps fetching what is missing and reports pending, syncing, or failed (a recent fetch failed;
// it is fetched again after a while). A fetch interrupted by a restart is started again by the next
// call. A problem of the image itself (a malformed reference, an image the registry does not have,
// an upstream the provider cannot fetch it from) is a rejected error.
// RemoveSource stops serving a source swallow ensured before and removes what the provider created
// for it; removing one that does not exist is not an error. Endpoints reports the addresses hosts
// read content at. Browse reads one page of content live.
//
// Errors are *ProviderError (rejected, auth, or unavailable) carrying the original cause, or the
// ctx's error. Implementations are safe for concurrent use and respect ctx cancellation.
type Repository interface {
	EnsureSource(ctx context.Context, source Source) (SourceStatus, error)
	EnsureFile(ctx context.Context, file File) (FileStatus, error)
	EnsureImage(ctx context.Context, reference string) (ImageStatus, error)
	RemoveSource(ctx context.Context, name string, format Format) error
	Endpoints(ctx context.Context) (HostEndpoints, error)
	Browse(ctx context.Context, query ContentQuery) (ContentPage, error)
}

// RepositoryFactory returns the Repository of a `repository` Integration. It returns
// sitedomain.ErrIntegrationNotFound for an unknown Integration, sitedomain.ErrCredentialNotSet when
// the Integration has no credential, and a *ProviderError (rejected) for an Integration no adapter
// of this build serves. It does not contact the provider.
type RepositoryFactory interface {
	For(ctx context.Context, integrationID string) (Repository, error)
}

// ProviderErrorKind classifies a Repository failure so callers decide between retrying and stopping
// without knowing which provider produced it.
type ProviderErrorKind string

const (
	// ProviderErrorUnavailable means the provider could not be reached, timed out, or failed on its
	// side; it is transient and retried.
	ProviderErrorUnavailable ProviderErrorKind = "unavailable"
	// ProviderErrorAuth means the provider refused the Integration's credential: a configuration
	// problem of the Integration, retried after it is fixed.
	ProviderErrorAuth ProviderErrorKind = "auth"
	// ProviderErrorRejected means the provider understood the request and refused it, for example a
	// source URL it cannot use; repeating the same request fails the same way.
	ProviderErrorRejected ProviderErrorKind = "rejected"
)

// ProviderError is a failure reported by, or met while reaching, a Local Repository provider.
// Detail is the adapter's sentence for a user, shown verbatim in an item's message and in API
// errors, so it names no credential. Endpoint (without userinfo or query values) and Operation (a
// short name of the call, "ensure source") are evidence for logs; Err is the original failure, kept
// so callers can errors.As its cause.
type ProviderError struct {
	Kind      ProviderErrorKind
	Detail    string
	Endpoint  string
	Operation string
	Err       error
}

// Error is the kind, the detail, and the failing call: for example "unavailable: the repository
// could not be reached (ensure source at http://repo:8080: connection refused)". It is redacted
// (redact.Text, the endpoint through redact.URL), so it is safe to log.
func (e *ProviderError) Error() string {
	sentence := string(e.Kind)
	if e.Detail != "" {
		sentence += ": " + e.Detail
	}
	call := e.Operation
	if endpoint := redact.URL(e.Endpoint); endpoint != "" {
		call = strings.TrimSpace(call + " at " + endpoint)
	}
	switch {
	case call != "" && e.Err != nil:
		sentence += " (" + call + ": " + e.Err.Error() + ")"
	case call != "":
		sentence += " (" + call + ")"
	case e.Err != nil:
		sentence += " (" + e.Err.Error() + ")"
	}
	return redact.Text(sentence)
}

// Unwrap returns the original failure, so errors.Is and errors.As reach the cause (a transport
// error, the ctx's error) through the classification.
func (e *ProviderError) Unwrap() error { return e.Err }
