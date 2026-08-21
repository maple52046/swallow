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
// The set is deliberately coarse: it carries only the distinctions gdcm acts on
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
// never be managed by gdcm at all; gdcm reads it to decide what can be deployed.
// The two become linked only when an operator imports a Machine, which creates a
// Server carrying a server.ProvisioningSource back-reference.
type Machine struct {
	// ID is the provider-side identifier (a MAAS system_id). Opaque to gdcm:
	// it is passed back to the provider verbatim and never parsed.
	ID string
	// Hostname is the short name the provider knows the machine by.
	Hostname string
	// FQDN is the fully qualified name, when the provider manages DNS for it.
	FQDN string
	// Status is the normalized lifecycle state gdcm reasons about.
	Status MachineStatus
	// ProviderStatus is the provider's own status label, retained because the
	// normalized Status is intentionally coarse. Display and diagnostics only —
	// never branch on this value.
	ProviderStatus string
	PowerState     PowerState
	Architecture   string
	CPUCores       int
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
	// Display only: the vocabulary is the provider's, not gdcm's.
	HWEKernel   string
	IPAddresses []string
	Tags        []string
	// Zone and ResourcePool are the provider's own grouping labels. They are carried
	// through opaquely and are not mapped onto any gdcm hierarchy.
	Zone         string
	ResourcePool string

	// Hardware identifiers, used to recognise the same physical machine after it is
	// re-enrolled under a new provider ID. Any of these may be empty: providers do
	// not all report them, and some hardware does not populate its DMI fields.
	SystemUUID   string
	SerialNumber string
	MACAddresses []string
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
	// ErrProviderNotConfigured means gdcm has no provisioning provider configured.
	// This is a deployment configuration state, not a failure of the provider.
	ErrProviderNotConfigured = errors.New("provisioning provider not configured")

	// ErrMachineNotFound means the provider has no machine with the requested ID.
	ErrMachineNotFound = errors.New("machine not found")

	// ErrIntegrationNotProvisioner means the referenced integration is registered as
	// something other than a provisioner.
	ErrIntegrationNotProvisioner = errors.New("integration is not a provisioner")

	// ErrProviderKindUnsupported means no adapter is built for the integration's
	// provider kind.
	ErrProviderKindUnsupported = errors.New("unsupported provisioning provider kind")
)

// ProviderErrorKind classifies a provider failure so that the delivery layer can
// choose an HTTP status without knowing which provider produced it.
type ProviderErrorKind string

const (
	// ProviderErrorUnavailable means the provider could not be reached, timed
	// out, or returned a server-side failure.
	ProviderErrorUnavailable ProviderErrorKind = "unavailable"
	// ProviderErrorAuth means the provider rejected the credentials gdcm is
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
