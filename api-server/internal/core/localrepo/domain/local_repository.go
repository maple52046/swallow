package domain

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
)

// Collection is the store collection Local Repository records live in, one per Site, keyed by the
// Site id. Controllers watch it by this name.
const Collection = "local_repositories"

// SettingContentOrigin is the `repository` Integration setting that holds the origin (scheme, host,
// and port) hosts read content from; the Integration's endpoint is where swallow calls the
// provider's API, which hosts may not be able to reach.
const SettingContentOrigin = "contentOrigin"

// DeclaredByUser is the declaredBy value of a Repository Source a user added to the Site's spec;
// a module's declarations carry DeclaredByModule of its id.
const DeclaredByUser = "user"

// DeclaredByModule is the declaredBy value of what the module with id declares: "module:<id>".
func DeclaredByModule(id string) string {
	return "module:" + id
}

// LocalRepository is one Site's Local Repository resource (desired-state-model.md §4). Its identity
// is the Site: a Site has at most one `repository` Integration, so the record outlives a replaced
// Integration and keeps the user's sources.
//
// Spec is desired and written only by the API (a user); Generation counts its writes, starting at 1
// with the first write and 0 for a record the controller created before any. Status is observed and
// written only by the Local Repository controller. What the Site's repository must hold (the
// effective spec) is Spec merged with the compiled modules' declarations; it is computed, never
// stored, so a release that changes a module's declarations needs no migration.
type LocalRepository struct {
	SiteID     string
	Generation int64
	Spec       Spec
	Status     Status
}

// Spec is the user's part of a Site's Local Repository: how the Site's deployments use it (Mode),
// and the Repository Sources added beyond what the modules declare (the OS package archive, for
// example).
type Spec struct {
	Mode    Mode
	Sources []Source
}

// Mode is how a Site's deployments use its Local Repository (glossary Local Repository Mode). The
// values are wire and store values: renaming one is a model change.
//
//   - ModeUpstream: deployments fetch every artifact from the upstream its module declares and the
//     controller converges nothing. It is the default; a record without a spec reads as it.
//   - ModeCache: deployments fetch from the Local Repository what it serves and the rest from the
//     declared upstreams, decided per artifact at acceptance, and never wait for its content.
//   - ModeOffline: deployments fetch every artifact from the Local Repository and are refused while
//     it does not serve everything, never falling back to the upstreams.
type Mode string

// The Local Repository Modes, each described on Mode.
const (
	ModeUpstream Mode = "upstream"
	ModeCache    Mode = "cache"
	ModeOffline  Mode = "offline"
)

// Valid reports whether m is one of the modes; the zero value is not.
func (m Mode) Valid() bool {
	switch m {
	case ModeUpstream, ModeCache, ModeOffline:
		return true
	}
	return false
}

// Converges reports whether the controller converges a Local Repository in mode m into its
// provider: in cache and offline mode deployments fetch from it, so it must hold the effective
// content; in upstream mode (or an unknown mode) the provider is left alone.
func (m Mode) Converges() bool {
	return m == ModeCache || m == ModeOffline
}

// Status is what the controller last observed of the Site's repository through IntegrationID, at
// ObservedAt, while converging spec generation ObservedGeneration. Sources, Files, and Images record
// each item with the definition it was ensured with (an image's is its reference), so a definition
// changed since then reads as not yet observed. Endpoints are nil until first read; ObservedAt is
// nil until the first complete pass on IntegrationID. Conditions are the controller's judgements
// (ConditionReady, convergence.ConditionRealized, convergence.ConditionRequiresAttention), each for
// the generation it names; a pass that could not complete (the provider unreachable, the
// Integration misconfigured) changes only them, so the items stay as last observed. The zero Status
// means nothing was observed: no Integration, or none since the Integration changed.
type Status struct {
	IntegrationID      string
	ObservedGeneration int64
	Sources            []ObservedSource
	Files              []ObservedFile
	Images             []ObservedImage
	Endpoints          *HostEndpoints
	ObservedAt         *time.Time
	Conditions         []convergence.Condition
}

// ConditionReady is the Local Repository's own condition type: true when deployments that fetch
// artifacts are accepted under the spec's mode. Upstream mode is never ready; cache mode needs the
// Integration enabled, observed, not stopped, and the host addresses read; offline mode needs, beyond
// that, every effective source serving and every file and declared image ready. Its false reason is
// the first thing in the way.
const ConditionReady convergence.ConditionType = "ready"

// The Local Repository's condition reasons, beside the shared convergence reasons
// (convergence.ReasonConverged, convergence.ReasonProviderUnreachable, and
// convergence.ReasonAttentionNotNeeded). They are wire values in the Local Repository contract.
const (
	// ReasonReady: ready is true.
	ReasonReady = "ready"
	// ReasonSyncing: a source, file, or declared image is pending or syncing (for ready in offline
	// mode: a source not serving yet, or a file or image not ready yet).
	ReasonSyncing = "syncing"
	// ReasonSourceFailed, ReasonFileFailed, and ReasonImageFailed: the last fetch of a source, a
	// file, or a declared image failed.
	ReasonSourceFailed = "source_failed"
	ReasonFileFailed   = "file_failed"
	ReasonImageFailed  = "image_failed"
	// ReasonCredentialMissing: the Integration has no credential.
	ReasonCredentialMissing = "credential_missing"
	// ReasonCredentialRejected: the provider refused the Integration's credential.
	ReasonCredentialRejected = "credential_rejected"
	// ReasonContentOriginMissing: the Integration has no SettingContentOrigin, so hosts have no
	// address to read content at.
	ReasonContentOriginMissing = "content_origin_missing"
	// ReasonIntegrationInvalid: the provider adapter refused the Integration's configuration.
	ReasonIntegrationInvalid = "integration_invalid"
	// ReasonNotServing: the spec's mode is upstream, so the Site's deployments fetch from the
	// upstreams and the controller converges nothing.
	ReasonNotServing = "not_serving"
	// ReasonIntegrationDisabled: the Integration is disabled, which pauses the Local Repository.
	ReasonIntegrationDisabled = "integration_disabled"
	// ReasonNotObserved: no pass has completed on the Integration yet.
	ReasonNotObserved = "not_observed"
	// ReasonEndpointsUnknown: the host addresses have not been read yet.
	ReasonEndpointsUnknown = "endpoints_unknown"
)

// ConfigurationReason reports whether a realized reason says the Integration or its provider is
// in the way rather than the content: the reasons a pass that could not complete records.
func ConfigurationReason(reason string) bool {
	switch reason {
	case convergence.ReasonProviderUnreachable, ReasonCredentialMissing, ReasonCredentialRejected,
		ReasonContentOriginMissing, ReasonIntegrationInvalid:
		return true
	}
	return false
}

// ObservedSource is one Repository Source as it was ensured (Source) and what the provider reported
// (Status).
type ObservedSource struct {
	Source Source
	Status SourceStatus
}

// ObservedFile is one File as it was ensured and what the provider reported.
type ObservedFile struct {
	File   File
	Status FileStatus
}

// ObservedImage is one declared image reference as it was ensured and what the provider reported;
// an image whose registry source was not ready yet is recorded pending without asking the provider.
type ObservedImage struct {
	Reference string
	Status    ImageStatus
}

// Equal reports whether two Source definitions are the same, list order included: the provider was
// asked to serve exactly that.
func (s Source) Equal(other Source) bool {
	return s.Name == other.Name && s.Format == other.Format && s.URL == other.URL &&
		slices.Equal(s.Suites, other.Suites) && slices.Equal(s.Components, other.Components) &&
		slices.Equal(s.Architectures, other.Architectures)
}

// Store persists Local Repository records (collection `local_repositories`). Production
// implementations are durable and safe for concurrent use; the in-memory ones are for tests.
//
// The writers are split by field (desired-state-model.md §7.4): PutSpec is the API's and never
// touches Status; SetStatus is the controller's and never touches Spec or Generation.
type Store interface {
	// Get returns the Site's record, or ErrLocalRepositoryNotFound when none exists.
	Get(ctx context.Context, siteID string) (*LocalRepository, error)
	// List returns every record, in no particular order.
	List(ctx context.Context) ([]*LocalRepository, error)
	// PutSpec replaces the Site's spec and increments its generation, but only while the stored
	// generation is expectedGeneration (0 when no record or a record without a spec write exists);
	// otherwise it returns ErrGenerationConflict and writes nothing. It returns the stored record.
	PutSpec(ctx context.Context, siteID string, expectedGeneration int64, spec Spec) (*LocalRepository, error)
	// SetStatus replaces the Site's status, creating a record at generation 0 with an empty spec in
	// ModeUpstream when none exists. A record read without a stored mode reads in ModeUpstream too.
	SetStatus(ctx context.Context, siteID string, status Status) error
	// Delete removes the Site's record; removing one that does not exist is not an error.
	Delete(ctx context.Context, siteID string) error
}

var (
	// ErrLocalRepositoryNotFound means the store holds no record for the Site: no spec was written
	// and the controller has not observed its repository yet. It is a store miss, not a missing
	// repository.
	ErrLocalRepositoryNotFound = errors.New("local repository record not found")
	// ErrNoRepositoryIntegration means the Site has no `repository` Integration, so it has no Local
	// Repository to read or change.
	ErrNoRepositoryIntegration = errors.New("this site has no repository integration")
	// ErrGenerationConflict means a spec write named a generation other than the stored one: someone
	// changed the spec since the writer read it.
	ErrGenerationConflict = errors.New("the local repository spec changed since it was read")
	// ErrInvalidSpec means a spec write is malformed or contradicts what the modules declare; the
	// wrapped message says what.
	ErrInvalidSpec = errors.New("invalid local repository spec")
	// ErrInvalidContentQuery means a content query is malformed; the wrapped message says what.
	ErrInvalidContentQuery = errors.New("invalid local repository content query")
	// ErrLocalRepositoryUnavailable refuses automation that fetches artifacts while the Site's Local
	// Repository is in cache or offline mode and is not ready: its Integration is disabled, its last
	// pass was stopped, it was not observed yet, or (offline mode only) a source, file, or declared
	// image it must serve is not ready yet. A Site in upstream mode, or one without a Local
	// Repository, fetches from the upstreams and is never refused with it. The wrapped message names
	// what to enable, fix, or wait for. It is a state conflict the user resolves outside the request
	// (409).
	ErrLocalRepositoryUnavailable = errors.New("the site's local repository is not ready")
)
