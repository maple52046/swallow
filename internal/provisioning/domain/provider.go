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
// provider cannot honour is refused before it is sent rather than silently downgraded.
//
// This is a static property of the adapter, deliberately not a probe: it is consulted on
// the deploy path, where an extra round trip could fail for reasons unrelated to the
// question being asked.
type ProviderCapabilities struct {
	// EphemeralDeploy reports that DeployRequest.Ephemeral is honoured.
	EphemeralDeploy bool
}

// OSProvisioningProvider is the port an OS provisioning backend must implement
// to be usable by gdcm.
//
// Implementations live under infra/ and own all translation between provider
// vocabulary and this package's types. In particular they must map:
//   - transport failures and provider 5xx onto *ProviderError{Kind: ProviderErrorUnavailable}
//   - rejected credentials onto *ProviderError{Kind: ProviderErrorAuth}
//   - refused-but-understood requests onto *ProviderError{Kind: ProviderErrorRejected}
//   - unknown machine identifiers onto ErrMachineNotFound
//
// Deploy and Release are asynchronous on every provider gdcm targets: they
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
