// Package domain defines Server: a physical machine swallow manages, projected from a
// provisioner's inventory.
//
// A server comes into being when the reconciler sees a machine in a provisioner's
// inventory that has no server yet, whether the provisioner enlisted it, swallow registered
// it, or a host with an existing OS was registered (its Origin). swallow owns the server's
// identity and its Spec (the desired state a provisioner realizes); everything else here is
// cached observation, a status read from a system that owns it, or the convergence status
// its controller writes.
//
// See docs/decisions/002-server-identity.md for the identity reasoning, including the
// reconciler's matching rules, and docs/decisions/071 for the spec and convergence model.
package domain

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
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

// GPUKind separates workload accelerator capacity from graphics controllers that only serve
// a Server's local console or BMC. Provider inventories often call both devices GPUs, so every
// projected GPU must carry this distinction before capacity or monitoring code consumes it.
type GPUKind string

const (
	// GPUKindCompute contributes accelerator capacity and may have workload telemetry.
	GPUKindCompute GPUKind = "compute"
	// GPUKindDisplay is a local-console, BMC, or virtual-machine console graphics controller,
	// not workload capacity.
	GPUKindDisplay GPUKind = "display"
)

// GPU is a provisioner-observed GPU group. Telemetry is never stored here; only hardware
// inventory belongs in the projection. Kind is normalized at the projection boundary, while
// ClassifiedKind keeps documents written before the field existed compatible on read.
type GPU struct {
	Vendor string
	Model  string
	Count  int
	Kind   GPUKind
}

// ClassifyGPUKind applies swallow's fallback when a provider exposes only a generic GPU class.
// Known server-console controllers and the emulated console adapters a hypervisor gives a virtual
// machine are display devices; every other reported GPU remains compute so an unnamed accelerator
// is not silently removed from capacity. Each family is matched by name and by the "vendor:product"
// PCI id a provider falls back to when it cannot name the device.
func ClassifyGPUKind(vendor, model string) GPUKind {
	identity := strings.ToLower(strings.TrimSpace(vendor + " " + model))
	cirrusGD5446 := strings.Contains(identity, "cirrus logic") &&
		(strings.Contains(identity, "gd 5446") || strings.Contains(identity, "gd5446"))
	switch {
	case strings.Contains(identity, "aspeed"), strings.Contains(identity, "1a03:"):
		return GPUKindDisplay
	case cirrusGD5446, strings.Contains(identity, "1013:"):
		return GPUKindDisplay
	case strings.Contains(identity, "matrox") && strings.Contains(identity, "g200"):
		return GPUKindDisplay
	case strings.Contains(identity, "102b:"):
		return GPUKindDisplay
	case strings.Contains(identity, "xgi") && (strings.Contains(identity, "z7") || strings.Contains(identity, "z9")):
		return GPUKindDisplay
	case strings.Contains(identity, "18ca:"):
		return GPUKindDisplay
	// Virtual-machine console adapters: virtio-gpu, QXL, Bochs/QEMU standard VGA, VMware SVGA II,
	// and Hyper-V video. Their vendors also make non-display devices, so the PCI fallbacks name
	// the exact product rather than the vendor prefix.
	case strings.Contains(identity, "virtio") && strings.Contains(identity, "gpu"), strings.Contains(identity, "1af4:1050"):
		return GPUKindDisplay
	case strings.Contains(identity, "qxl"), strings.Contains(identity, "1b36:0100"):
		return GPUKindDisplay
	case strings.Contains(identity, "bochs"), strings.Contains(identity, "qemu virtual video"), strings.Contains(identity, "1234:1111"):
		return GPUKindDisplay
	case strings.Contains(identity, "vmware") && strings.Contains(identity, "svga"), strings.Contains(identity, "15ad:0405"):
		return GPUKindDisplay
	case strings.Contains(identity, "hyper-v") && (strings.Contains(identity, "vga") || strings.Contains(identity, "video")),
		strings.Contains(identity, "1414:5353"):
		return GPUKindDisplay
	default:
		return GPUKindCompute
	}
}

// ClassifiedKind returns a valid kind for both current and legacy GPU projections. An explicit
// valid kind is authoritative; an absent or unknown stored value is reclassified from the
// provider's vendor/model strings so upgrades are correct before the next inventory sweep.
func (g GPU) ClassifiedKind() GPUKind {
	if g.Kind == GPUKindCompute || g.Kind == GPUKindDisplay {
		return g.Kind
	}
	return ClassifyGPUKind(g.Vendor, g.Model)
}

// IsAMDCompute reports whether the group is AMD workload capacity: a compute GPU whose provider
// vendor names AMD, or whose model is the "1002:product" PCI id a provider falls back to when it
// cannot name the device. AMD display controllers are not workload capacity and never match.
func (g GPU) IsAMDCompute() bool {
	if g.ClassifiedKind() != GPUKindCompute {
		return false
	}
	vendor := strings.ToLower(g.Vendor)
	return strings.Contains(vendor, "advanced micro devices") ||
		strings.Contains(vendor, "amd") ||
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(g.Model)), "1002:")
}

// HasAMDComputeGPU reports whether any group is AMD workload capacity (see GPU.IsAMDCompute).
func HasAMDComputeGPU(gpus []GPU) bool {
	for _, gpu := range gpus {
		if gpu.IsAMDCompute() {
			return true
		}
	}
	return false
}

// GPUCountByKind returns physical devices of one classified kind, including legacy groups whose
// stored kind is absent. Display controllers therefore never inflate compute capacity.
func GPUCountByKind(gpus []GPU, kind GPUKind) int {
	total := 0
	for _, gpu := range gpus {
		if gpu.ClassifiedKind() == kind {
			total += gpu.Count
		}
	}
	return total
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
	// Tags are every label the provisioner reports on the machine, automatic and manual.
	// Mirrored because a fleet is routinely filtered by them, e.g. "every machine tagged gpu".
	Tags []string
	// AutomaticTags is the subset of Tags the provisioner computes itself (a MAAS tag with a
	// definition). They are provider-owned and only observed; the rest of Tags are the manual
	// tags that ServerSpec.Tags owns (decision 071).
	AutomaticTags []string
}

// ManualTags returns the observed tags that are not automatic, in observed order: the tags a
// Server's spec owns and its controller converges.
func (o Observed) ManualTags() []string {
	manual := make([]string, 0, len(o.Tags))
	for _, tag := range o.Tags {
		if !slices.Contains(o.AutomaticTags, tag) {
			manual = append(manual, tag)
		}
	}
	return manual
}

// Origin records how a Server came into swallow (decision 071). It only describes the arrival:
// whatever the origin, the Server's desired state belongs to swallow.
type Origin string

const (
	// OriginDiscovered: the provisioner enlisted the machine on its own, for example by network
	// boot. Also the origin of every Server that predates the field.
	OriginDiscovered Origin = "discovered"
	// OriginEnrolled: swallow registered the machine with the provisioner.
	OriginEnrolled Origin = "enrolled"
	// OriginImported: the machine was registered with an OS already installed, without
	// reinstalling it; first observed already deployed.
	OriginImported Origin = "imported"
)

// ServerSpec is the desired state of the provisioner-realized fields of a Server (decision 071),
// written only by the API and by adoption. The controller converges the provisioner to it.
type ServerSpec struct {
	// Tags is the complete set of manual tags the Server must carry. A manual tag present on the
	// machine but not here is removed; automatic tags are never touched.
	Tags []string
}

// ServerConvergence is the status the Server's controller writes: the spec generation its
// conditions were judged for and the conditions themselves. Only the controller writes it.
type ServerConvergence struct {
	ObservedGeneration int64
	Conditions         []convergence.Condition
}

// ProvisioningStatus is the axis owned by the provisioner: can this be deployed, is a
// deployment running, did it fail.
type ProvisioningStatus struct {
	// State is a normalized provisioning state. Branch on this.
	State string
	// StateSince is when swallow first observed State. It is swallow's own record, not the
	// provider's transition time, so it is only as precise as the observation that saw the
	// change; zero means unknown (a projection stored before the field existed). Writers set it
	// through NextStateSince so it survives every re-observation of an unchanged State.
	StateSince time.Time
	// ProviderState is the provisioner's own label. Display only.
	ProviderState string
	// ErrorDescription is the provisioner's own machine-level failure reason (e.g. "Failed to
	// erase disks."), mirrored only for failure states and empty otherwise. Display and
	// diagnostics only; it surfaces why a lifecycle action failed on the failed/broken axis so
	// an operator does not have to read the provider event log to find the reason.
	ErrorDescription string
	PowerState       string
	// OSSystem and DistroSeries describe what is currently deployed.
	OSSystem     string
	DistroSeries string
	// DeployedImageName is the effective display name of the currently deployed OS image,
	// resolved by matching OSSystem/DistroSeries against the provider's image catalog and applying
	// any swallow overlay (custom) name. It is mirrored when a deploy completes (RefreshServer) and
	// refreshed by reconcile and by an image rename, so a freshly deployed Server shows the
	// friendly name promptly rather than only after the next reconcile pass. A mirrored fact
	// carried alongside the other provider observations here, refreshed with ObservedAt, and
	// display only: empty when nothing is deployed or the image cannot be resolved. It exists so
	// the fleet list can show a meaningful image name without a per-request catalog fan-out (see
	// docs/decisions/001 and 009).
	DeployedImageName string
	// DeployedImageDefaultUser is the effective default login user of the currently deployed OS
	// image (its swallow overlay value, else the built-in for its OS family), mirrored exactly
	// like DeployedImageName. Automation logs in to the Server as this user exclusively; empty
	// means no default user is known and automation falls back to the Site SSH user and built-in
	// candidates (decision 039).
	DeployedImageDefaultUser string
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
	// InspectionRequired reports that the provisioner's storage layout for this Server cannot
	// take a disk deployment until the Server is inspected again (for example a UEFI Server
	// whose layout was built while it booted BIOS). Disk deployments are refused while it is
	// set; a memory deployment is unaffected. InspectionRequiredReason explains it to operators.
	InspectionRequired       bool
	InspectionRequiredReason string
	// CommissioningStatus and TestingStatus are the provisioner's own labels for the
	// last hardware inspection and test run. Display only; the normalized State above
	// is what to branch on.
	CommissioningStatus string
	TestingStatus       string
	IntegrationID       string
	ObservedAt          time.Time
}

// NextStateSince returns the StateSince for a new observation of state at now, given the
// projection it replaces. An unchanged state keeps the earlier time, so re-reading a Server every
// reconcile pass never restarts its running time; a changed state, a first observation, or an
// earlier time that was never recorded starts at now.
func NextStateSince(previous *ProvisioningStatus, state string, now time.Time) time.Time {
	if previous != nil && previous.State == state && !previous.StateSince.IsZero() {
		return previous.StateSince
	}
	return now
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
	State       DeploymentState
	OperationID string
	StepID      string
	Attempt     int
	// Code is the failed Step's stable, machine-branchable error code (for example
	// "deployment_address_unavailable"). It lets a client render a concise root cause
	// without parsing the verbose StatusReason. Empty for a non-failed deployment.
	Code         string
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
	ID string
	// Origin is how the Server arrived, set when it is first projected.
	Origin Origin
	// Generation counts spec changes; 0 means the Server has no spec yet and is adopted from its
	// first observation (desired-state-model.md §6.5).
	Generation  int64
	Spec        ServerSpec
	Convergence ServerConvergence
	Source      Source
	Hardware    Hardware
	Observed    Observed

	// Deployment is Swallow's verified result. Provider reconciliation preserves it.
	Deployment *DeploymentStatus

	// PlatformClaim is the Platform whose accepted deployment targets this Server, nil when none
	// does. It is swallow-owned, written only through PlatformClaimStore, and preserved by
	// provider reconciliation.
	PlatformClaim *PlatformClaim

	// DefaultUser is the Server Default User an operator set on this Server (decision 045), empty
	// when none is set. It is swallow-owned and verified by a Deployment Key login before it is
	// saved; provider reconciliation preserves it. It belongs to the current OS installation and is
	// cleared when swallow starts a new OS deployment or the Server is observed without an OS. Read
	// it through EffectiveDefaultUser, which falls back to the OS Image's default user.
	DefaultUser string

	// BootMedia is the Server's Boot Media setting (decision 047), nil when never set. Redfish is
	// the latest Redfish capability probe of its BMC, nil before the first probe. Libvirt is the
	// latest probe of its Hypervisor (decision 055), nil unless its power driver is virsh.
	// BootMediaApply is the enable preflight recorded as running, nil when none is (check Running:
	// an abandoned one lingers). All are swallow-owned, written only through BootMediaStore, and
	// preserved by provider reconciliation.
	BootMedia      *BootMediaSetting
	Redfish        *RedfishCapability
	Libvirt        *LibvirtCapability
	BootMediaApply *BootMediaApply

	// The three externally owned status axes. Each is nil until its owner has been
	// observed: absent means "not known", not a default state.
	Provisioning *ProvisioningStatus
	Membership   *MembershipStatus
	Health       *HealthStatus

	// Absent means the machine stopped appearing in its provisioner's inventory.
	// Absent servers are never deleted: absence is usually transient, deletion is not. The
	// controller reports it as realized=false with reason provider_missing.
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
	// ErrSpecConflict means a spec write expected a generation the Server no longer has: someone
	// else changed the spec first. The caller re-reads and retries or reports a conflict.
	ErrSpecConflict = errors.New("server spec changed concurrently")
	// ErrAmbiguousHardware means more than one existing server matches a machine's
	// hardware identifiers. Never resolved by guessing: merging two servers is
	// unrecoverable, so this surfaces as a conflict instead.
	ErrAmbiguousHardware = errors.New("hardware identifiers match more than one server")
)
