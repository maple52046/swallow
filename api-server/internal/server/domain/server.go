// Package domain defines Server: a physical machine swallow manages, projected from a
// provisioner's inventory.
//
// A server is not created by an operator. It comes into being when the reconciler sees
// a machine in a provisioner's inventory that has no server yet. swallow owns the
// server's identity; everything else here is either cached observation or a status
// read from a system that owns it.
//
// See docs/decisions/002-server-identity.md for the reasoning, including the
// reconciler's matching rules and what was rejected.
package domain

import (
	"errors"
	"strings"
	"time"
)

// Source is the external key: which provisioner, at which site, calls this machine
// what. Unique across all servers, and how the reconciler finds a record to update.
type Source struct {
	SiteID        string
	IntegrationID string
	// ProviderMachineID is the provisioner's own identifier, e.g. a MAAS system_id.
	// Opaque: passed back to the provisioner verbatim and never parsed.
	ProviderMachineID string
}

// Hardware identifies the physical machine independently of any provisioner, so that a
// re-enrolled machine is recognised rather than duplicated.
//
// These values are hints, not keys. Cloned virtual machines share a system UUID, and
// physical hardware ships with duplicate or empty DMI fields often enough that a fleet
// will hit it. Matching on them can be ambiguous, and ambiguity is a conflict to
// surface rather than a merge to perform.
type Hardware struct {
	SystemUUID   string
	SerialNumber string
	MACAddresses []string
}

// placeholderIdentifiers are the values firmware reports when a vendor did not fill a
// DMI field in.
//
// They arrive as ordinary strings rather than as empty fields, so treating them
// literally makes every machine in a batch match every other one. MAAS reports
// "Unknown" for the serial of every virtual machine, which is enough on its own to
// collapse a whole lab into one server.
var placeholderIdentifiers = map[string]bool{
	"unknown":                              true,
	"none":                                 true,
	"n/a":                                  true,
	"na":                                   true,
	"not specified":                        true,
	"not available":                        true,
	"not applicable":                       true,
	"default string":                       true,
	"to be filled by o.e.m.":               true,
	"system serial number":                 true,
	"0":                                    true,
	"invalid":                              true,
	"00000000-0000-0000-0000-000000000000": true,
	"ffffffff-ffff-ffff-ffff-ffffffffffff": true,
	"00:00:00:00:00:00":                    true,
}

// meaningfulIdentifier returns value when it actually identifies something, and ""
// otherwise. Comparison is case-insensitive because firmware is inconsistent about it.
func meaningfulIdentifier(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || placeholderIdentifiers[strings.ToLower(trimmed)] {
		return ""
	}
	return trimmed
}

// Normalized returns a copy with placeholder values removed, so that what is stored
// contains only identifiers that mean something.
func (h Hardware) Normalized() Hardware {
	systemUUID, serialNumber, macs := h.Identifiers()
	return Hardware{
		SystemUUID:   systemUUID,
		SerialNumber: serialNumber,
		MACAddresses: macs,
	}
}

// Identifiers returns the hardware identifiers that can be matched on.
//
// Empty and placeholder values are excluded deliberately: a query for "serial number is
// Unknown" matches every machine whose vendor left the field blank, which is the
// opposite of identification.
func (h Hardware) Identifiers() (systemUUID, serialNumber string, macs []string) {
	for _, mac := range h.MACAddresses {
		if meaningful := meaningfulIdentifier(mac); meaningful != "" {
			macs = append(macs, meaningful)
		}
	}
	return meaningfulIdentifier(h.SystemUUID), meaningfulIdentifier(h.SerialNumber), macs
}

// Empty reports whether there is nothing to match on.
func (h Hardware) Empty() bool {
	systemUUID, serial, macs := h.Identifiers()
	return systemUUID == "" && serial == "" && len(macs) == 0
}

// GPU is a GPU as the provisioner detected it. Telemetry is never stored here; only
// what the hardware inventory reports.
type GPU struct {
	Vendor string
	Model  string
	Count  int
}

// Observed holds what the provisioner reports about the machine. Cached, never
// authoritative, and never used as a key.
type Observed struct {
	// Hostname is nullable, mutable, and not unique. Two sites may both have
	// "gpu-node-01".
	Hostname string
	FQDN     string
	// Addresses is empty before commissioning and during a reinstall.
	Addresses            []string
	Architecture         string
	CPUCores             int
	MemoryMiB            int64
	StorageGB            float64
	GPUs                 []GPU
	ProviderZone         string
	ProviderResourcePool string
	// SystemVendor, SystemProduct, and CPUModel describe the physical machine as the
	// provisioner commissioned it, so the fleet can be grouped by hardware generation.
	SystemVendor  string
	SystemProduct string
	CPUModel      string
	// ProviderPod is the VM host a virtual machine belongs to, empty for bare metal.
	ProviderPod string
	// Tags are the provisioner's own labels for the machine. Mirrored because a fleet
	// is routinely filtered by them, e.g. "every machine tagged gpu".
	Tags []string
}

// ProvisioningStatus is the axis owned by the provisioner: can this be deployed, is a
// deployment running, did it fail.
type ProvisioningStatus struct {
	// State is a normalized provisioning state. Branch on this.
	State string
	// ProviderState is the provisioner's own label. Display only.
	ProviderState string
	PowerState    string
	// OSSystem and DistroSeries describe what is currently deployed.
	OSSystem     string
	DistroSeries string
	// Ephemeral reports that the deployed OS runs from memory, so anything written to
	// the root filesystem is lost on reboot.
	//
	// It belongs on this axis rather than being remembered from the deploy request:
	// the provisioner reports it as a property of the machine, and a machine can be
	// re-deployed the other way round without swallow being involved.
	Ephemeral bool
	// HWEKernel is the provisioner's own kernel label. Display only.
	HWEKernel string
	// Locked reports that the provisioner is refusing state-changing actions on the
	// machine, which explains why a deploy or release can be rejected in a state that
	// would otherwise allow it.
	Locked bool
	// CommissioningStatus and TestingStatus are the provisioner's own labels for the
	// last hardware inspection and test run. Display only; the normalized State above
	// is what to branch on.
	CommissioningStatus string
	TestingStatus       string
	IntegrationID       string
	ObservedAt          time.Time
}

// DeploymentState is Swallow's outcome for the most recent durable OS deployment.
// It is separate from provider lifecycle: an image can be installed but unusable.
type DeploymentState string

const (
	DeploymentDeploying         DeploymentState = "deploying"
	DeploymentVerifying         DeploymentState = "verifying"
	DeploymentSucceeded         DeploymentState = "succeeded"
	DeploymentFailed            DeploymentState = "failed"
	DeploymentRequiresAttention DeploymentState = "requires_attention"
	DeploymentCanceled          DeploymentState = "canceled"
)

// DeploymentStatus materializes one durable provision-os Step on its Server.
// StatusReason is normalized by Swallow and contains no credentials or secrets.
type DeploymentStatus struct {
	State        DeploymentState
	OperationID  string
	StepID       string
	Attempt      int
	Stage        string
	StatusReason string
	StartedAt    time.Time
	FinishedAt   *time.Time
	UpdatedAt    time.Time
}

// MembershipStatus is the axis owned by a platform's own API.
//
// swallow never writes this to express intent. Intent lives in an operation; this is the
// platform's answer, and when the two disagree the platform is right.
type MembershipStatus struct {
	PlatformID string
	// NodeName is the platform's own name for this machine, which is how in-platform
	// metrics are joined back to a server.
	NodeName   string
	Role       string
	State      string
	ObservedAt time.Time
}

// HealthStatus is the axis owned by the metrics store.
//
// It is never persisted. It is filled in at query time by whoever asked, which is why
// a server loaded from the repository always has a nil Health.
type HealthStatus struct {
	State      string
	ObservedAt time.Time
}

// Health axis values.
const (
	HealthUp   = "up"
	HealthDown = "down"
)

// Server is a physical machine swallow manages.
type Server struct {
	// ID is swallow-issued and stable for the machine's whole life in the platform.
	// Every swallow reference uses this and only this.
	ID       string
	Source   Source
	Hardware Hardware
	Observed Observed

	// Deployment is Swallow's verified result. Provider reconciliation preserves it.
	Deployment *DeploymentStatus

	// The three externally owned status axes. Each is nil until its owner has been
	// observed: absent means "not known", not a default state.
	Provisioning *ProvisioningStatus
	Membership   *MembershipStatus
	Health       *HealthStatus

	// Absent means the machine stopped appearing in its provisioner's inventory.
	// Absent servers are never deleted: absence is usually transient, deletion is not.
	Absent     bool
	LastSeenAt time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// DisplayName is the label to show for this server. There is no swallow-owned name, so
// the provisioner's hostname is used, falling back to the identifier.
func (s *Server) DisplayName() string {
	if s.Observed.Hostname != "" {
		return s.Observed.Hostname
	}
	if s.Observed.FQDN != "" {
		return s.Observed.FQDN
	}
	return s.ID
}

// PrimaryAddress returns the first known address, or "" when the provisioner has not
// assigned one yet.
func (s *Server) PrimaryAddress() string {
	if len(s.Observed.Addresses) == 0 {
		return ""
	}
	return s.Observed.Addresses[0]
}

var (
	ErrServerNotFound = errors.New("server not found")
	// ErrAmbiguousHardware means more than one existing server matches a machine's
	// hardware identifiers. Never resolved by guessing: merging two servers is
	// unrecoverable, so this surfaces as a conflict instead.
	ErrAmbiguousHardware = errors.New("hardware identifiers match more than one server")
)
