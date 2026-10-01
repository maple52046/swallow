package domain

import (
	"context"
	"io"
)

// MachineFilter narrows a machine listing. Zero values mean "no constraint".
type MachineFilter struct {
	// Status matches the normalized lifecycle state.
	Status MachineStatus
	// Keyword is a case-insensitive substring matched against hostname and FQDN.
	Keyword string
}

// DeployRequest describes an OS deployment to start on a single machine.
type DeployRequest struct {
	MachineID string
	// OSSystem is the provider's OS family, e.g. "ubuntu". May be empty when the
	// provider can infer it from DistroSeries.
	OSSystem string
	// DistroSeries is the release to deploy, e.g. "jammy" or "ubuntu/22.04".
	DistroSeries string
	// UserData is optional cloud-init user data in plain text. Adapters apply
	// whatever transport encoding their provider requires.
	UserData string
	// Comment is an optional note for the provider's own event log.
	Comment string
	// Ephemeral asks for the OS to run from memory, leaving the machine's disks
	// untouched.
	//
	// An adapter that does not support this must return a *ProviderError of kind
	// ProviderErrorRejected rather than deploying normally. Dropping the flag would
	// invert the operator's instruction: they asked for a machine that keeps nothing,
	// and a disk installation keeps everything.
	Ephemeral bool
}

// ReleaseRequest describes how a machine should be returned to its provider's
// available pool. Disk erasure is explicit because providers can offer materially
// different speed and security trade-offs.
type ReleaseRequest struct {
	MachineID   string
	Erase       bool
	SecureErase bool
	QuickErase  bool
	// Comment is an optional note for the provider's own event log.
	Comment string
}

// ProviderCapabilities declares what an adapter can express, so that a request the
// provider cannot honour is refused before it is sent rather than silently downgraded,
// and so that a client can hide an action a provisioner does not offer instead of
// presenting a button that always fails.
//
// This is a static property of the adapter, deliberately not a probe: it is consulted on
// the deploy path, where an extra round trip could fail for reasons unrelated to the
// question being asked. Each optional capability corresponds to one of the optional
// interfaces below; an adapter that sets a flag true must implement the matching
// interface.
type ProviderCapabilities struct {
	// EphemeralDeploy reports that DeployRequest.Ephemeral is honoured.
	EphemeralDeploy bool
	// DeploymentReadiness reports that DeploymentTargetValidator is implemented.
	DeploymentReadiness bool
	// NetworkConfiguration reports that NetworkConfigurationProvider is implemented.
	NetworkConfiguration bool
	// Power reports that PowerController is implemented.
	Power bool
	// HardwareValidation reports that HardwareValidator is implemented.
	HardwareValidation bool
	// OperatorState reports that OperatorStateController is implemented.
	OperatorState bool
	// MachineDetail reports that MachineDetailInspector is implemented.
	MachineDetail bool
	// HardwareInventory reports that HardwareInventoryInspector is implemented.
	HardwareInventory bool
	// MachineRemoval reports that MachineRemover is implemented.
	MachineRemoval bool
	// ReleaseOptions reports that ConfigurableMachineReleaser is implemented.
	ReleaseOptions bool
	// ImageRemoval reports that OSImageRemover is implemented.
	ImageRemoval bool
	// ImageUpload reports that OSImageUploader is implemented.
	ImageUpload bool
	// Grouping reports that GroupingController is implemented, i.e. the provider can
	// realize swallow-owned Zone and Pool intent and assign a machine to them.
	Grouping bool
	// Tagging reports that MachineTagController is implemented, i.e. the provider owns machine
	// tags and swallow drives it to create, assign, and unassign them. When false, swallow owns
	// a Server's tags itself (the ServerTagOverlay fallback). See docs/decisions/031.
	Tagging bool
	// SSHKeyRegistration reports that SSHKeyRegistrar is implemented, i.e. the provider holds SSH
	// public keys for the account swallow authenticates as and authorizes them on machines it
	// deploys. When false, swallow's SSH Keys are recorded as unsupported for this provisioner.
	// See docs/decisions/039.
	SSHKeyRegistration bool
}

// The interfaces below are optional capabilities. The base OSProvisioningProvider is the
// minimum every adapter implements; these are added by adapters whose backend supports
// them and reached by a type assertion. This keeps a new provider from having to stub a
// dozen methods it cannot honour, while still letting swallow refuse — rather than silently
// drop — an action the provider does not offer. See docs/decisions/001.

// DeploymentTargetValidator checks provider-owned prerequisites before dispatch.
//
// It is deliberately read-only. A MAAS adapter can require a subnet link, but it
// must not choose or create that link on the operator's behalf.
type DeploymentTargetValidator interface {
	// ValidateDeploymentTarget returns nil only when provider-owned prerequisites
	// currently pass. Implementations map remediable state to ProviderErrorRejected,
	// a missing machine to ErrMachineNotFound, and connectivity failures as usual.
	ValidateDeploymentTarget(ctx context.Context, machineID string) error
}

// PowerController controls a machine's power through the provisioner's BMC integration.
type PowerController interface {
	PowerOn(ctx context.Context, machineID string) (*Machine, error)
	PowerOff(ctx context.Context, machineID string) (*Machine, error)
	// QueryPowerState reads the live power state from the BMC rather than the
	// provisioner's cached value.
	QueryPowerState(ctx context.Context, machineID string) (PowerState, error)
}

// HardwareValidator re-runs the provisioner's own commissioning and hardware tests, and
// resolves their outcomes.
type HardwareValidator interface {
	Commission(ctx context.Context, machineID string) (*Machine, error)
	Test(ctx context.Context, machineID string) (*Machine, error)
	// Abort stops an in-progress commissioning, testing, or deployment.
	Abort(ctx context.Context, machineID string) (*Machine, error)
	// OverrideFailedTesting accepts a machine whose tests failed, moving it back to a
	// usable state on an operator's judgement.
	OverrideFailedTesting(ctx context.Context, machineID string) (*Machine, error)
}

// OperatorStateController makes the operator-driven state changes that are neither a
// deployment nor a release: taking a machine out of automation, flagging it, or putting
// it into a recovery environment.
type OperatorStateController interface {
	Lock(ctx context.Context, machineID string) (*Machine, error)
	Unlock(ctx context.Context, machineID string) (*Machine, error)
	MarkBroken(ctx context.Context, machineID string) (*Machine, error)
	MarkFixed(ctx context.Context, machineID string) (*Machine, error)
	EnterRescueMode(ctx context.Context, machineID string) (*Machine, error)
	ExitRescueMode(ctx context.Context, machineID string) (*Machine, error)
}

// HardwareInventoryInspector reads a machine's attached hardware devices that the base
// machine listing does not include, such as GPUs. Separated because it costs a call per
// machine and changes only at commissioning, so it runs on its own slow cadence.
type HardwareInventoryInspector interface {
	ListGPUs(ctx context.Context, machineID string) ([]GPU, error)
}

// MachineDetailInspector returns a live, display-oriented dump of everything the
// provisioner knows about one machine, for a single-machine view.
//
// The return type is deliberately generic rather than provider-specific: it is read one
// machine at a time, never queried across the fleet, so swallow proxies it live rather than
// mirroring it, and carries no schema for it. A second provider fills the same sections
// with its own content.
type MachineDetailInspector interface {
	GetMachineDetail(ctx context.Context, machineID string) (*MachineDetail, error)
}

// MachineEventReader reads the operational history retained by a provisioner for one
// machine. Events remain provider-owned and are proxied live; swallow does not claim
// that this is a complete audit record of every swallow action.
type MachineEventReader interface {
	ListMachineEvents(ctx context.Context, machineID string, limit int) ([]MachineEvent, error)
}

// MachineRemover permanently deletes a Machine from the provisioner's inventory.
//
// Implementations must honour the provider's normal safeguards and must not force a
// deletion implicitly. ErrMachineNotFound means the requested end state is already true;
// callers may finish deleting their local projection.
type MachineRemover interface {
	DeleteMachine(ctx context.Context, machineID string) error
}

// ConfigurableMachineReleaser releases a machine with provider-supported disk
// erasure controls. The base parameterless Release remains available for adapters
// without this optional capability and for backwards-compatible API requests.
type ConfigurableMachineReleaser interface {
	ReleaseWithOptions(ctx context.Context, req ReleaseRequest) (*Machine, error)
}

// OSImageRemover permanently deletes a provider-owned OS image, such as a MAAS
// uploaded custom image.
//
// This is deliberately narrow: only operator-managed custom images are removable this
// way. An image the provider mirrors from an upstream stream (a synced OS release) is not
// swallow's to delete — the provider would re-sync it — so adapters map such a request,
// and an image that does not exist, onto *ProviderError{Kind: ProviderErrorRejected}
// rather than pretending to have deleted something. imageID and architecture identify the
// image exactly as ListOSImages reported them, because one image name can back several
// architectures.
type OSImageRemover interface {
	DeleteOSImage(ctx context.Context, imageID, architecture string) error
}

// UploadOSImageRequest describes a new provider-owned OS image to create from
// operator-supplied content.
//
// Name and Architecture are the operator's intent in swallow-neutral form; the adapter maps
// them into the provider's own vocabulary (for MAAS, the custom-image namespace and a
// "<arch>/generic" architecture). The uploaded artifact is provider-owned exactly like a
// synced one, and whether it is classified as a custom image is provider-determined, never
// chosen here.
type UploadOSImageRequest struct {
	// Name is the operator-chosen image name, before any provider namespacing.
	Name string
	// Architecture is the CPU architecture, e.g. "amd64".
	Architecture string
	// Title is an optional human-readable label; empty means the provider derives one.
	Title string
	// FileType is the provider-validated artifact format, e.g. "tgz". Empty means the
	// provider's own default.
	FileType string
	// Size is the exact byte length of Content. Providers that reserve a resource before the
	// bytes arrive (MAAS) require it up front, so callers must supply the real size.
	Size int64
	// SHA256 is the lowercase hex digest of Content, computed by the caller. Providers verify
	// it on completion, so a mismatch is the provider's rejection, not swallow's.
	SHA256 string
	// Content streams the artifact bytes. The adapter reads it once, sequentially, and must
	// not assume it is seekable; swallow keeps no copy after the provider accepts it.
	Content io.Reader
}

// OSImageUploader creates a new provider-owned OS image from operator-supplied content, such
// as a MAAS uploaded custom image.
//
// It is the creation counterpart to OSImageRemover: swallow drives the provider to store the
// artifact but does not own, mirror, or keep a copy of it (see ADR 027). An adapter that sets
// ProviderCapabilities.ImageUpload must implement this. Implementations map a provider refusal
// (a duplicate name, an unsupported file type) onto *ProviderError{Kind: ProviderErrorRejected}
// and transport or 5xx failures onto ProviderErrorUnavailable, and return the created image as
// the provider reports it so the caller can show the new catalog row without a second read.
type OSImageUploader interface {
	UploadOSImage(ctx context.Context, req UploadOSImageRequest) (*OSImage, error)
}

// GroupingController realizes swallow-owned Zone and Pool intent in a provisioner that
// supports machine grouping (for MAAS: physical zones and resource pools), and assigns a
// machine to a zone or pool. It is the provider side of decision 029: swallow owns the
// Zone/Pool catalog, and this capability drives the provisioner when it can express the
// same grouping.
//
// The controller speaks swallow's currency — group names — and hides any provider-internal
// identifier. Create is deliberately *ensure* semantics: a group the provider already holds
// (MAAS ships a `default` zone and pool) is not an error, so a swallow create over an existing
// provider group succeeds. Delete of a group the provider does not have is likewise satisfied;
// a provider that refuses to delete a group still in use maps that refusal to
// ProviderErrorRejected. Rename maps the previous name to the new one.
//
// Assignment returns the machine as the provider reports it immediately afterwards so the
// caller can echo the effective zone/pool without a second read; an unknown machine maps to
// ErrMachineNotFound. An adapter that sets ProviderCapabilities.Grouping must implement this.
type GroupingController interface {
	// EnsureZone creates the named zone, treating an existing one as already satisfied.
	EnsureZone(ctx context.Context, name, description string) error
	// RenameZone changes a zone's name (and description) from currentName to newName.
	RenameZone(ctx context.Context, currentName, newName, description string) error
	// DeleteZone removes the named zone, treating a missing one as already satisfied.
	DeleteZone(ctx context.Context, name string) error

	// EnsurePool creates the named resource pool, treating an existing one as satisfied.
	EnsurePool(ctx context.Context, name, description string) error
	// RenamePool changes a pool's name (and description) from currentName to newName.
	RenamePool(ctx context.Context, currentName, newName, description string) error
	// DeletePool removes the named resource pool, treating a missing one as satisfied.
	DeletePool(ctx context.Context, name string) error

	// SetMachineZone assigns machineID to the named zone; an empty name is refused because
	// a machine always has a zone in providers that support them.
	SetMachineZone(ctx context.Context, machineID, zoneName string) (*Machine, error)
	// SetMachinePool assigns machineID to the named resource pool; an empty name is refused
	// for the same reason as SetMachineZone.
	SetMachinePool(ctx context.Context, machineID, poolName string) (*Machine, error)
}

// MachineTagController lets swallow drive a provider that owns machine tags: it lists the
// provider's tags, creates a manual tag, and assigns or unassigns a tag across a batch of
// machines. It is the provider side of decision 031's capability-first rule for tags — when a
// provisioner advertises Tagging, swallow drives it here; when it does not, swallow owns the tags
// itself through the ServerTagOverlay fallback.
//
// The controller speaks tag names, swallow's currency, not any provider-internal identifier.
// EnsureTag is idempotent create of a *manual* tag: a tag the provider already holds is treated
// as satisfied. Add/Remove are batch-native (they take every machine id at once) because a
// provider that owns tags globally — MAAS assigns one tag to many machines in a single
// update_nodes call — must not be walked one machine at a time. A provider tag that is computed
// from a definition (a MAAS automatic tag) is read-only: it is reported with Editable=false by
// ListTags, and an attempt to Add/Remove it is refused by the provider and surfaced as a
// *ProviderError{Kind: ProviderErrorRejected} rather than silently dropped. An adapter that sets
// ProviderCapabilities.Tagging must implement this.
type MachineTagController interface {
	// ListTags returns every tag the provider knows, each flagged Editable when swallow may
	// assign or unassign it (a manual tag with no definition).
	ListTags(ctx context.Context) ([]MachineTag, error)
	// EnsureTag creates the named manual tag, treating an existing tag as already satisfied.
	EnsureTag(ctx context.Context, name string) error
	// AddTag assigns the named tag to every machine in machineIDs in one provider call. An empty
	// machineIDs is a no-op.
	AddTag(ctx context.Context, name string, machineIDs []string) error
	// RemoveTag unassigns the named tag from every machine in machineIDs in one provider call.
	// An empty machineIDs is a no-op.
	RemoveTag(ctx context.Context, name string, machineIDs []string) error
}

// ProviderSSHKey is one SSH public key a provisioner holds for the account swallow authenticates
// as. ID is the provider's own opaque identifier, needed to remove the key later; PublicKey is the
// authorized_keys line exactly as the provider reports it, which callers compare by key material
// (type and base64 blob) rather than by comment.
type ProviderSSHKey struct {
	ID        string
	PublicKey string
}

// SSHKeyRegistrar lets swallow realize its SSH Keys in a provisioner that injects account SSH keys
// into the machines it deploys (for MAAS, the API-key owner's keys under /account/prefs/sshkeys/,
// written into the deployed image's default user by cloud-init). It is the provider side of
// decision 039: swallow owns the key records; this capability makes a provider deployment
// authorize them.
//
// The registrar affects only deployments the provider starts after a key is added; it never
// changes machines already deployed. RemoveSSHKey treats a key the provider no longer holds as
// already satisfied. A provider refusal (a malformed or duplicate key) maps to
// *ProviderError{Kind: ProviderErrorRejected} and transport failures to ProviderErrorUnavailable.
// An adapter that sets ProviderCapabilities.SSHKeyRegistration must implement this.
type SSHKeyRegistrar interface {
	// ListSSHKeys returns every SSH key the provider holds for swallow's account, including keys
	// an operator added outside swallow.
	ListSSHKeys(ctx context.Context) ([]ProviderSSHKey, error)
	// AddSSHKey registers one authorized_keys line and returns the provider's record of it.
	AddSSHKey(ctx context.Context, publicKey string) (ProviderSSHKey, error)
	// RemoveSSHKey deletes the provider key with this identifier; a missing key is satisfied.
	RemoveSSHKey(ctx context.Context, keyID string) error
}

// MachineDetail is a provider-neutral, display-oriented view of one machine: labelled
// fields grouped into sections, plus tabular data like disks and NICs. It holds strings
// because it is for display, not for swallow to reason about.
type MachineDetail struct {
	Sections []DetailSection
	Tables   []DetailTable
}

// DetailSection is a titled group of label/value fields.
type DetailSection struct {
	Title  string
	Fields []DetailField
}

// DetailField is one labelled value.
type DetailField struct {
	Label string
	Value string
}

// DetailTable is titled tabular data whose columns are provider-defined.
type DetailTable struct {
	Title   string
	Columns []string
	Rows    [][]string
}

// MachineEvent is the provider-neutral portion of one provisioner event. Adapters normalize
// OccurredAt to RFC3339 when the provider format is known and otherwise retain the raw value
// so one malformed event cannot hide the otherwise usable history.
type MachineEvent struct {
	ID          string
	Level       string
	Type        string
	Description string
	Actor       string
	OccurredAt  string
}

// OSProvisioningProvider is the port an OS provisioning backend must implement
// to be usable by swallow.
//
// Implementations live under infra/ and own all translation between provider
// vocabulary and this package's types. In particular they must map:
//   - transport failures and provider 5xx onto *ProviderError{Kind: ProviderErrorUnavailable}
//   - rejected credentials onto *ProviderError{Kind: ProviderErrorAuth}
//   - refused-but-understood requests onto *ProviderError{Kind: ProviderErrorRejected}
//   - unknown machine identifiers onto ErrMachineNotFound
//
// Deploy and Release are asynchronous on every provider swallow targets: they
// return once the provider has accepted the request, and progress is observed by
// re-reading the machine.
type OSProvisioningProvider interface {
	// Name is the stable provider kind, e.g. "maas". It selects this adapter and is
	// recorded against the integration, so it must not change between releases.
	Name() string

	// Probe reports reachability and advertised version without side effects.
	Probe(ctx context.Context) (ProviderInfo, error)

	// Capabilities reports which optional deploy behaviours this adapter honours.
	// Synchronous and side-effect free: it describes the adapter, not the remote system.
	Capabilities() ProviderCapabilities

	// ListMachines returns every machine matching filter.
	ListMachines(ctx context.Context, filter MachineFilter) ([]*Machine, error)

	// GetMachine returns one machine, or ErrMachineNotFound.
	GetMachine(ctx context.Context, machineID string) (*Machine, error)

	// ListOSImages returns the images the provider can currently deploy.
	ListOSImages(ctx context.Context) ([]*OSImage, error)

	// Deploy starts an OS deployment and returns the machine as the provider
	// reports it immediately afterwards.
	Deploy(ctx context.Context, req DeployRequest) (*Machine, error)

	// Release returns a machine to the provider's available pool. Providers
	// generally require this before a deployed machine can be redeployed.
	Release(ctx context.Context, machineID string) (*Machine, error)
}

// ProviderFactory resolves a registered integration into a usable provider.
//
// A fleet has one provisioner per site, so which provider to talk to is a per-request
// question answered from stored integration data rather than from process
// configuration. The factory owns credential retrieval, so no use case handles one.
type ProviderFactory interface {
	// For returns a provider for the integration, or ErrIntegrationNotProvisioner
	// when the integration plays a different role, or ErrProviderKindUnsupported
	// when no adapter exists for its product.
	For(ctx context.Context, integrationID string) (OSProvisioningProvider, error)
}
