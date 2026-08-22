package domain

import "context"

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
}

// The interfaces below are optional capabilities. The base OSProvisioningProvider is the
// minimum every adapter implements; these are added by adapters whose backend supports
// them and reached by a type assertion. This keeps a new provider from having to stub a
// dozen methods it cannot honour, while still letting swallow refuse — rather than silently
// drop — an action the provider does not offer. See docs/decisions/001.

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
