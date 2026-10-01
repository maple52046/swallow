// Package domain defines the provider-agnostic vocabulary for OS provisioning.
//
// The types here describe what every OS provisioning backend must be able to
// express. Provider-specific vocabulary (MAAS system IDs, MAAS status codes,
// and so on) is translated into these types by the adapters under infra/, so
// that no application or delivery code is aware of which provider is in use.
package domain

import "errors"

// MachineStatus is the provider-agnostic lifecycle state of a provisionable machine.
//
// The set is deliberately coarse: it carries only the distinctions swallow acts on
// (can it be deployed, is it being deployed, did it fail). Adapters map their own
// richer vocabulary onto these values and preserve the original label in
// Machine.ProviderStatus, so a provider gaining new states never invents a new
// MachineStatus value here.
type MachineStatus string

const (
	// MachineStatusNew means the provider has discovered the machine but it is
	// not yet ready to deploy (not commissioned).
	MachineStatusNew MachineStatus = "new"
	// MachineStatusCommissioning means the provider is inspecting the hardware.
	MachineStatusCommissioning MachineStatus = "commissioning"
	// MachineStatusReady means the machine can accept a deployment.
	MachineStatusReady MachineStatus = "ready"
	// MachineStatusAllocated means the machine is reserved but not yet deployed.
	MachineStatusAllocated MachineStatus = "allocated"
	// MachineStatusDeploying means an OS deployment is in progress.
	MachineStatusDeploying MachineStatus = "deploying"
	// MachineStatusDeployed means an OS is installed and running.
	MachineStatusDeployed MachineStatus = "deployed"
	// MachineStatusReleasing means the machine is being returned to the pool.
	MachineStatusReleasing MachineStatus = "releasing"
	// MachineStatusTesting means the provider is running hardware tests.
	MachineStatusTesting MachineStatus = "testing"
	// MachineStatusRescue means the machine is in a provider rescue mode.
	MachineStatusRescue MachineStatus = "rescue"
	// MachineStatusBroken means the provider has marked the machine unusable.
	MachineStatusBroken MachineStatus = "broken"
	// MachineStatusFailed means the last lifecycle operation failed.
	MachineStatusFailed MachineStatus = "failed"
	// MachineStatusRetired means the machine is withdrawn from service.
	MachineStatusRetired MachineStatus = "retired"
	// MachineStatusUnknown means the provider reported a state this version
	// does not recognise.
	MachineStatusUnknown MachineStatus = "unknown"
)

// PowerState is the provider-agnostic power state of a machine.
type PowerState string

const (
	PowerStateOn      PowerState = "on"
	PowerStateOff     PowerState = "off"
	PowerStateError   PowerState = "error"
	PowerStateUnknown PowerState = "unknown"
)

// Machine is a provisionable unit in an OS provisioning provider's inventory.
//
// A Machine is not a server.Server. It lives in the provider's inventory and may
// never be managed by swallow at all; swallow reads it to decide what can be deployed.
// The two become linked only when an operator imports a Machine, which creates a
// Server carrying a server.ProvisioningSource back-reference.
type Machine struct {
	// ID is the provider-side identifier (a MAAS system_id). Opaque to swallow:
	// it is passed back to the provider verbatim and never parsed.
	ID string
	// Hostname is the short name the provider knows the machine by.
	Hostname string
	// FQDN is the fully qualified name, when the provider manages DNS for it.
	FQDN string
	// Status is the normalized lifecycle state swallow reasons about.
	Status MachineStatus
	// ProviderStatus is the provider's own status label, retained because the
	// normalized Status is intentionally coarse. Display and diagnostics only —
	// never branch on this value.
	ProviderStatus string
	// ErrorDescription is the provider's own machine-level failure reason (e.g. "Failed to
	// erase disks."), populated only for failure states (failed/broken/rescue) and empty
	// otherwise. Display and diagnostics only; it exists so an operator can see why a lifecycle
	// action failed without digging through the provider event log. Never branch on it.
	ErrorDescription string
	PowerState       PowerState
	Architecture     string
	CPUCores         int
	// MemoryMiB is usable RAM in mebibytes. Mebibytes rather than megabytes
	// because that is the unit providers report and operators see in the
	// provider's own UI; converting would make the two screens disagree.
	MemoryMiB int64
	// StorageGB is total disk capacity in decimal gigabytes.
	StorageGB float64
	// OSSystem and DistroSeries describe what is currently deployed. Both are
	// empty on a machine that has never been deployed.
	OSSystem     string
	DistroSeries string
	// Ephemeral reports that the deployed OS runs from memory and leaves the disks
	// untouched, so everything on the root filesystem is lost on reboot.
	//
	// This is not cosmetic. An ephemerally deployed machine is indistinguishable from
	// a disk-installed one by state, OS, and release alone, while anything written to
	// it — a driver, a package, a config file — silently un-happens on the next boot.
	Ephemeral bool
	// HWEKernel is the provider's own label for the running kernel, e.g. "ga-24.04".
	// Display only: the vocabulary is the provider's, not swallow's.
	HWEKernel   string
	IPAddresses []string
	Tags        []string
	// Zone and ResourcePool are the provider's own grouping labels for this machine,
	// observed here as the provider currently reports them. They are the observed effect
	// of a grouping assignment, not the swallow-owned catalog: swallow's managed Zone and
	// Pool concepts live in the infrastructure feature, and swallow drives this provider
	// through the optional GroupingController capability. See docs/decisions/029.
	Zone         string
	ResourcePool string

	// SystemVendor, SystemProduct, and CPUModel describe the physical machine as the
	// provisioner commissioned it. Mirrored so a fleet can be queried by hardware
	// generation without opening each machine; see decision 001.
	SystemVendor  string
	SystemProduct string
	CPUModel      string
	// Pod is the provider's name for the VM host a virtual machine belongs to, empty
	// for bare metal.
	Pod string
	// Locked reports that the provisioner is refusing state-changing actions on the
	// machine, which is why a deploy or release may be rejected even when the state
	// otherwise allows it.
	Locked bool
	// CommissioningStatus and TestingStatus are the provisioner's own labels for the
	// last hardware inspection and test run, e.g. "Passed" or "Failed". Display only.
	CommissioningStatus string
	TestingStatus       string
	// GPUs is the machine's attached GPU inventory. It is empty on a plain machine
	// listing: providers report attached devices through a separate call, so it is
	// filled by the inventory sweep rather than by every reconcile pass.
	GPUs []GPU

	// Hardware identifiers, used to recognise the same physical machine after it is
	// re-enrolled under a new provider ID. Any of these may be empty: providers do
	// not all report them, and some hardware does not populate its DMI fields.
	SystemUUID   string
	SerialNumber string
	MACAddresses []string
}

// GPU is a GPU as the provisioner's hardware inventory reports it. Telemetry never
// belongs here — only what commissioning detected. Identical GPUs are collapsed into one
// entry with a count, because a machine with eight of one model is the common case and
// eight identical rows are noise.
type GPU struct {
	Vendor string
	Model  string
	Count  int
}

// PrimaryIP returns the address to register a machine under, or "" when the
// provider has not assigned one yet (typical before commissioning).
func (m *Machine) PrimaryIP() string {
	if len(m.IPAddresses) == 0 {
		return ""
	}
	return m.IPAddresses[0]
}

// OSImage is an operating system that a provider is currently able to deploy.
type OSImage struct {
	// ID is the value to pass back as DeployRequest.DistroSeries, e.g. "ubuntu/jammy".
	ID string
	// Name is the label the provider presents to operators.
	Name string
	// OSSystem is the OS family, e.g. "ubuntu".
	OSSystem string
	// Release is the release within the family, e.g. "jammy".
	Release string
	// Architecture is the CPU architecture the image targets, e.g. "amd64".
	Architecture string
	// SizeBytes is the provider-reported size of the current complete image artifact.
	// Zero means the provider did not report a usable size.
	SizeBytes int64
	// Complete reports whether the provider has fully staged this image so it can be
	// deployed. An incomplete image — for example a custom upload whose boot or kernel
	// resources are still missing — is refused by the deploy preflight with a clear reason
	// instead of being handed to the provider only to fail opaquely mid-install.
	Complete bool
}

// ProviderInfo identifies a configured provider and reports what it advertises
// about itself. Returned by OSProvisioningProvider.Probe.
type ProviderInfo struct {
	// Name is the stable provider identifier, e.g. "maas".
	Name string
	// DisplayName is the human-readable product name.
	DisplayName string
	// Version is the provider's reported version, or "" when it does not expose one.
	Version string
}

var (
	// ErrProviderNotConfigured means swallow has no provisioning provider configured.
	// This is a deployment configuration state, not a failure of the provider.
	ErrProviderNotConfigured = errors.New("provisioning provider not configured")

	// ErrMachineNotFound means the provider has no machine with the requested ID.
	ErrMachineNotFound = errors.New("machine not found")

	// ErrSSHKeyRegistration means swallow's Deployment Key could not be registered in a
	// key-capable provisioner right before a deployment, so the deployment was not started: a
	// Server deployed now would not authorize swallow (decision 039). It is retryable once the
	// provisioner recovers.
	ErrSSHKeyRegistration = errors.New("ssh key registration in the provisioner failed")

	// ErrDeploymentKeyMissing means the installation has no Deployment Key, so an OS deployment
	// would produce a Server swallow cannot log in to. The key is created at installation
	// (`swallow-api deployment-key ensure`, run by swallowctl install/upgrade), never by the API
	// process; the deployment is refused before any provider write (decision 039).
	ErrDeploymentKeyMissing = errors.New("no deployment key exists; run `swallow-api deployment-key ensure` (swallowctl install and upgrade run it)")

	// ErrIntegrationNotProvisioner means the referenced integration is registered as
	// something other than a provisioner.
	ErrIntegrationNotProvisioner = errors.New("integration is not a provisioner")

	// ErrProviderKindUnsupported means no adapter is built for the integration's
	// provider kind.
	ErrProviderKindUnsupported = errors.New("unsupported provisioning provider kind")

	// ErrInvalidReleaseRequest marks release options whose combination cannot be
	// interpreted safely, such as asking for secure erase without enabling erasure.
	ErrInvalidReleaseRequest = errors.New("invalid release request")

	// ErrServerMutationConflict means the requested state change conflicts with
	// protection or already-active work on the Server.
	ErrServerMutationConflict = errors.New("server mutation conflict")
)

// ProviderErrorKind classifies a provider failure so that the delivery layer can
// choose an HTTP status without knowing which provider produced it.
type ProviderErrorKind string

const (
	// ProviderErrorUnavailable means the provider could not be reached, timed
	// out, or returned a server-side failure.
	ProviderErrorUnavailable ProviderErrorKind = "unavailable"
	// ProviderErrorAuth means the provider rejected the credentials swallow is
	// configured with. This is an operator configuration problem, never an
	// indication about the caller's own credentials.
	ProviderErrorAuth ProviderErrorKind = "auth"
	// ProviderErrorRejected means the provider understood the request and
	// refused it, e.g. deploying to a machine that is not ready.
	ProviderErrorRejected ProviderErrorKind = "rejected"
)

// ProviderError is a failure reported by, or encountered while reaching, a provider.
type ProviderError struct {
	Kind ProviderErrorKind
	// Detail explains the failure and is surfaced to API clients verbatim.
	// Adapters must keep credentials and internal URLs out of it.
	Detail string
	// Err is the underlying transport or decoding error. For logs only.
	Err error
}

func (e *ProviderError) Error() string {
	if e.Detail != "" {
		return string(e.Kind) + ": " + e.Detail
	}
	return string(e.Kind)
}

func (e *ProviderError) Unwrap() error { return e.Err }
