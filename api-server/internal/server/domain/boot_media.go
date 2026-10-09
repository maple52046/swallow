package domain

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// BootMediaMethod is how a Server's Boot ISO is attached (decisions 047 and 055), decided by its
// Power Configuration. The values are the published `method` of the Boot Media API.
type BootMediaMethod string

const (
	// BootMediaMethodRedfish mounts the Boot ISO as Redfish virtual media on the Server's BMC.
	BootMediaMethodRedfish BootMediaMethod = "redfish"
	// BootMediaMethodLibvirt puts the Boot ISO on a virtual machine's CD-ROM on its Hypervisor.
	BootMediaMethodLibvirt BootMediaMethod = "libvirt"
)

// BootMediaMethod is the method the stored probes found: libvirt when the libvirt probe found a
// Hypervisor, else redfish when the Redfish probe found a BMC, else "".
func (s *Server) BootMediaMethod() BootMediaMethod {
	switch {
	case s.Libvirt != nil && s.Libvirt.Support != LibvirtNoHypervisor:
		return BootMediaMethodLibvirt
	case s.Redfish != nil && s.Redfish.Support != RedfishNoBMC:
		return BootMediaMethodRedfish
	default:
		return ""
	}
}

// RedfishSupport is the outcome of swallow's Redfish capability probe of a Server's BMC
// (decision 047). The values are the published `redfish.support` of the Boot Media API.
type RedfishSupport string

const (
	// RedfishSupported means the Redfish service answered at the BMC address, a host System was
	// identified, and it offers a virtual CD and a boot source override: Boot Media can be enabled.
	RedfishSupported RedfishSupport = "supported"
	// RedfishUnsupported means Redfish answered but the host System lacks virtual media or boot
	// override (or swallow could not tell which System is the host).
	RedfishUnsupported RedfishSupport = "unsupported"
	// RedfishUnreachable means no Redfish service answered at the BMC address, or it refused the
	// provisioner-held account.
	RedfishUnreachable RedfishSupport = "unreachable"
	// RedfishNoBMC means the Server has no BMC to probe: a virtual machine, or the provisioner holds
	// no BMC address for it.
	RedfishNoBMC RedfishSupport = "no_bmc"
)

// RedfishCapability is what one probe of a Server's BMC found. It is swallow-owned data with a
// probe time (decision 047): the BMC owns the truth, and a firmware update can change it, so it
// is refreshed by the capability sweep and on demand, and used only to gate and explain Boot
// Media — every apply re-discovers the BMC's resources rather than trusting stored paths.
//
// It never holds a credential. ServiceRoot is the Redfish root URL without userinfo.
type RedfishCapability struct {
	Support RedfishSupport
	// Reason explains, in operator terms, why Support is not RedfishSupported. Empty otherwise.
	Reason          string
	ServiceRoot     string
	Vendor          string
	Product         string
	RedfishVersion  string
	FirmwareVersion string
	// SystemID is the Redfish ComputerSystem identified as the host (for example "Self"); a GPU
	// baseboard or another non-host System is never chosen.
	SystemID     string
	VirtualMedia bool
	// BootOverrideModes are the BootSourceOverrideEnabled values the System allows besides
	// Disabled, a subset of "Once" and "Continuous".
	BootOverrideModes []string
	ProbedAt          time.Time
}

// Stale reports whether the probe is older than window at now, so a sweep re-probes it.
func (c *RedfishCapability) Stale(now time.Time, window time.Duration) bool {
	return c == nil || now.Sub(c.ProbedAt) >= window
}

// LibvirtSupport is the outcome of swallow's libvirt probe of a virtual machine (decision 055).
// The values are the published `libvirt.support` of the Boot Media API.
type LibvirtSupport string

const (
	// LibvirtSupported means the Hypervisor answered and has the domain: Boot Media can be enabled.
	LibvirtSupported LibvirtSupport = "supported"
	// LibvirtUnsupported means the Hypervisor answered but has no such domain.
	LibvirtUnsupported LibvirtSupport = "unsupported"
	// LibvirtUnreachable means the Hypervisor could not be reached, refused the Deployment Key, or
	// the account may not use libvirt.
	LibvirtUnreachable LibvirtSupport = "unreachable"
	// LibvirtNoHypervisor means the Power Configuration's host is not a swallow Server of the Site.
	LibvirtNoHypervisor LibvirtSupport = "no_hypervisor"
)

// LibvirtCapability is what one probe of a virtual machine's Hypervisor found. Like
// RedfishCapability it is swallow-owned, refreshed by the capability sweep and on demand, and used
// only to gate and explain Boot Media.
type LibvirtCapability struct {
	Support LibvirtSupport
	// Reason explains, in operator terms, why Support is not LibvirtSupported. Empty otherwise.
	Reason             string
	HypervisorServerID string
	Account            string
	Domain             string
	Pool               string
	CDROM              bool
	ProbedAt           time.Time
}

// LibvirtEndpoint is how swallow reaches one virtual machine for one call, read from its Power
// Configuration (`qemu+ssh://<account>@<host>/system`, power ID the domain).
type LibvirtEndpoint struct {
	// Hypervisor is the swallow Server the address's host names; nil when the host is not a
	// swallow Server of the Site. HypervisorOf turns it into a login.
	Hypervisor *Server
	Host       string
	// Account is the address's SSH account, empty when it names none.
	Account string
	Domain  string
}

// BootMediaEndpoint is the method a Server's Power Configuration gives it, with what that method
// needs: BMC for redfish, Libvirt for libvirt.
type BootMediaEndpoint struct {
	Method  BootMediaMethod
	BMC     *BMCEndpoint
	Libvirt *LibvirtEndpoint
}

// BootMediaApplier names what last applied Boot Media to a Server's BMC.
type BootMediaApplier string

const (
	// BootMediaAppliedByPreflight is the operator's enable action on the Server.
	BootMediaAppliedByPreflight BootMediaApplier = "preflight"
	// BootMediaAppliedByEnsure is the ensure-boot-media Task of an OS Deployment.
	BootMediaAppliedByEnsure BootMediaApplier = "ensure"
)

// BootMediaSetting is a Server's Boot Media setting (decisions 047 and 049): the operator's
// intent that the Server boot a chosen Boot ISO first, plus the outcome of the last apply.
//
// It is swallow-owned and written only through BootMediaStore; provider reconciliation preserves
// it. It records intent and history, never the BMC's live state — whether the ISO is mounted now
// is read from the BMC (BootMediaState).
type BootMediaSetting struct {
	Enabled bool
	// ISOID is the chosen Boot ISO, one of the Server's own provisioner Integration. It is empty
	// on a setting enabled before Boot ISOs existed (decision 049); such a setting needs one
	// chosen, and a disabled setting keeps the last one only as history.
	ISOID     string
	UpdatedAt time.Time
	// LastAppliedAt and LastAppliedBy record the last apply that left the BMC ready.
	LastAppliedAt *time.Time
	LastAppliedBy BootMediaApplier
	// BootOverride is the BootSourceOverrideEnabled mode the BMC accepted at the last apply:
	// "Continuous" when the BMC allows it, otherwise "Once".
	BootOverride string
	// LastError is the operator-facing reason the most recent apply failed, cleared by a
	// successful one. It contains no credential.
	LastError   string
	LastErrorAt *time.Time
}

// BootMediaPhase is one step of an enable preflight, in the order they run. The values are the
// published `apply.phase` of the Boot Media API. Steps that are not needed are skipped.
type BootMediaPhase string

const (
	// BootMediaPhaseProbing re-probes the BMC's Redfish capability.
	BootMediaPhaseProbing BootMediaPhase = "probing"
	// BootMediaPhaseEjecting ejects the previously mounted Boot ISO when switching to another.
	BootMediaPhaseEjecting BootMediaPhase = "ejecting"
	// BootMediaPhaseMounting mounts the Boot ISO and waits until the BMC reports it inserted.
	BootMediaPhaseMounting BootMediaPhase = "mounting"
	// BootMediaPhaseSettling is the fixed wait a fresh mount needs before the host may power on;
	// it is the only phase with a known end.
	BootMediaPhaseSettling BootMediaPhase = "settling"
	// BootMediaPhaseDirecting directs the next boots at the virtual CD.
	BootMediaPhaseDirecting BootMediaPhase = "directing"
	// BootMediaPhaseVerifying reads the mount and the boot direction back.
	BootMediaPhaseVerifying BootMediaPhase = "verifying"
)

// BootMediaApplyStaleAfter is how old a recorded apply may be before it is taken as abandoned
// (its API process stopped): longer than any preflight can run, so a live one is never mistaken
// for stale.
const BootMediaApplyStaleAfter = 10 * time.Minute

// BootMediaApply is an enable preflight running on a Server, recorded so any client — another
// tab, a reloaded page, the CLI — can follow it and so a second one is refused. It is written
// only through BootMediaStore, separately from the setting, and removed when the preflight ends.
type BootMediaApply struct {
	ISOID          string
	Phase          BootMediaPhase
	StartedAt      time.Time
	PhaseStartedAt time.Time
	// PhaseEndsAt is set only for a phase with a known end (settling).
	PhaseEndsAt *time.Time
}

// Running reports whether the apply is still in flight at now, rather than abandoned.
func (a *BootMediaApply) Running(now time.Time) bool {
	return a != nil && now.Sub(a.StartedAt) < BootMediaApplyStaleAfter
}

// BootMediaPhaseReporter receives each phase an apply enters; endsAt is set only for a phase with
// a known end. It must return quickly and never fail the apply.
type BootMediaPhaseReporter func(phase BootMediaPhase, endsAt *time.Time)

type phaseReporterKey struct{}

// WithBootMediaPhaseReporter returns ctx carrying report, so a RedfishController reports the
// phases of the call it makes with that context without its signature naming progress.
func WithBootMediaPhaseReporter(ctx context.Context, report BootMediaPhaseReporter) context.Context {
	return context.WithValue(ctx, phaseReporterKey{}, report)
}

// ReportBootMediaPhase tells ctx's reporter, if any, that the apply entered phase.
func ReportBootMediaPhase(ctx context.Context, phase BootMediaPhase, endsAt *time.Time) {
	if report, ok := ctx.Value(phaseReporterKey{}).(BootMediaPhaseReporter); ok && report != nil {
		report(phase, endsAt)
	}
}

// BootMediaState is the BMC's live Boot Media state for one Server, read for one request.
type BootMediaState struct {
	// MediaInserted reports that a virtual CD holds the Boot ISO.
	MediaInserted bool
	// MediaImage is the image the BMC reports for that CD, verbatim (BMCs rewrite URLs).
	MediaImage string
	// OverrideEnabled and OverrideTarget are the System's BootSourceOverrideEnabled and
	// BootSourceOverrideTarget as the BMC reports them.
	OverrideEnabled string
	OverrideTarget  string
	// OverrideReady reports that the override boots the virtual CD (once or continuously).
	OverrideReady bool
}

// Ready reports that the next boot will start from the Boot ISO.
func (s BootMediaState) Ready() bool {
	return s.MediaInserted && s.OverrideReady
}

// BMCEndpoint is how swallow reaches one Server's BMC for one call: the provisioner-held address
// and account (read live, never stored) plus the Server's hardware UUID, which identifies the
// host System among several (a GPU baseboard is a System too).
type BMCEndpoint struct {
	Address    string
	Username   string
	Password   string
	PowerType  string
	SystemHint string
	HostUUID   string
}

// BootMediaEndpointSource reads a Server's Boot Media method and endpoint from its provisioner's
// Power Configuration (decisions 047, 054, and 055): a bmc-family driver gives redfish with the
// BMC's endpoint, and a virsh driver gives libvirt with the Hypervisor its address names.
//
// Implementations return ErrNoBMC when the Server has neither (no power driver, another driver,
// no address) or its provisioner cannot hand out power settings, ErrBMCCredentialUnavailable when
// the provisioner refused to reveal them, and ErrBMCConnectionUnavailable (wrapped) when it could
// not be reached. They must not log or retain the password.
type BootMediaEndpointSource interface {
	BootMediaEndpoint(ctx context.Context, server *Server) (*BootMediaEndpoint, error)
}

// RedfishController drives the Redfish Boot Media functions of one BMC (decision 047).
//
// Every method re-discovers the BMC's resources (host System, virtual CD, actions) instead of
// trusting stored paths, retries a busy BMC (HTTP 503, connection resets) until ctx is done, and
// never puts the password into an error. ISO URLs passed in are Boot ISO URLs (decision 049).
type RedfishController interface {
	// Probe classifies the BMC. It reports unreachable or unsupported BMCs in the result rather
	// than as an error; it returns an error only when ctx ended before an answer.
	Probe(ctx context.Context, endpoint BMCEndpoint) (RedfishCapability, error)
	// ReadBootMedia reports the live state for isoURL without changing anything.
	ReadBootMedia(ctx context.Context, endpoint BMCEndpoint, isoURL string) (BootMediaState, error)
	// ApplyBootMedia makes the next boots start from isoURL: it enables the BMC's remote media
	// service when a vendor requires it, mounts isoURL on a virtual CD unless already mounted, and
	// sets a boot override to that CD — Continuous when allowed, else Once — then reads the result
	// back. It is idempotent. It returns the override mode applied and the verified state.
	ApplyBootMedia(ctx context.Context, endpoint BMCEndpoint, isoURL string) (string, BootMediaState, error)
	// ClearBootMedia ejects isoURL from every virtual CD holding it and disables a boot override
	// that targets the virtual CD. A BMC that already has neither is satisfied.
	ClearBootMedia(ctx context.Context, endpoint BMCEndpoint, isoURL string) error
	// MountBootMedia mounts isoURL on the virtual CD at once, for a host that is already booting:
	// without the settle ApplyBootMedia gives a mount that a power-on follows, and without
	// changing the boot direction. A mounted ISO is left alone. It returns the state read back.
	MountBootMedia(ctx context.Context, endpoint BMCEndpoint, isoURL string) (BootMediaState, error)
	// ResetHost restarts the host through Redfish (ForceRestart, or On when it is off). It is used
	// only to recover a deployment boot that missed the ISO.
	ResetHost(ctx context.Context, endpoint BMCEndpoint) error
}

// BootMediaStore persists the swallow-owned Boot Media fields of a Server. It is separate from
// ServerRepository so the many repository fakes need not implement it; the production Mongo
// repository implements both on the same document. Every method touches only its own field and
// returns ErrServerNotFound for an unknown id.
type BootMediaStore interface {
	// SetBootMedia, SetRedfishCapability, and SetLibvirtCapability replace their field; nil clears
	// it.
	SetBootMedia(ctx context.Context, id string, setting *BootMediaSetting) error
	SetRedfishCapability(ctx context.Context, id string, capability *RedfishCapability) error
	SetLibvirtCapability(ctx context.Context, id string, capability *LibvirtCapability) error
	// BeginBootMediaApply records apply atomically unless one started at or after staleBefore is
	// recorded, in which case it returns ErrBootMediaApplying.
	BeginBootMediaApply(ctx context.Context, id string, apply *BootMediaApply, staleBefore time.Time) error
	// UpdateBootMediaApply replaces the recorded apply that started at apply.StartedAt; it does
	// nothing when that one is no longer recorded.
	UpdateBootMediaApply(ctx context.Context, id string, apply *BootMediaApply) error
	// EndBootMediaApply removes the apply that started at startedAt, leaving a newer one alone.
	EndBootMediaApply(ctx context.Context, id string, startedAt time.Time) error
}

// BootISOImage is a Boot ISO as Boot Media uses it (decision 049): which one, which provisioner
// Integration's rack it chains to, and the URL BMCs mount it at.
type BootISOImage struct {
	ID            string
	Name          string
	IntegrationID string
	URL           string
}

// BootISOFile is a Boot ISO's file on this installation, for a method that uploads it (libvirt)
// rather than having a BMC mount its URL.
type BootISOFile struct {
	ID            string
	Name          string
	IntegrationID string
	Path          string
	Size          int64
}

// BootISOResolver resolves the Boot ISO a Boot Media setting names. Boot ISOs are built and
// owned by the provisioning context; this port is the server context's narrow view of them.
type BootISOResolver interface {
	// Resolve returns the Boot ISO with id. It returns ErrBootISOUnknown when none has the id. When
	// the ISO exists but is not served (its file is missing, or no Boot Media base URL is
	// configured) it returns the image together with an error wrapping ErrBootMediaNotConfigured
	// that says why, so callers can still name it.
	Resolve(ctx context.Context, id string) (*BootISOImage, error)
	// File returns the Boot ISO's file. It returns ErrBootISOUnknown when none has the id, and an
	// error wrapping ErrBootMediaNotConfigured when the file is missing. It needs no base URL.
	File(ctx context.Context, id string) (*BootISOFile, error)
	// URL is where BMCs mount id's ISO, whether or not it still exists, so a previously mounted
	// ISO can be ejected. For an empty id it is the URL of the retired installation ISO that a
	// setting enabled before Boot ISOs had mounted. It is "" when no base URL is configured.
	URL(id string) string
}

var (
	// ErrBootMediaNotConfigured means the Boot ISO a Server would use is not served (none chosen,
	// its file missing, or no Boot Media base URL configured), so Boot Media cannot be applied.
	ErrBootMediaNotConfigured = errors.New("boot media is not configured for this server")
	// ErrBootISORequired means enabling Boot Media named no Boot ISO.
	ErrBootISORequired = errors.New("a boot ISO must be chosen to enable boot media")
	// ErrBootISOUnknown means the named Boot ISO does not exist.
	ErrBootISOUnknown = errors.New("boot ISO not found")
	// ErrBootISOWrongIntegration means the Boot ISO chains to another provisioner Integration's
	// rack than the Server's, so the Server could never reach its own provisioner with it.
	ErrBootISOWrongIntegration = errors.New("the boot ISO belongs to another provisioner integration")
	// ErrNoBMC means the Server has no BMC swallow can drive.
	ErrNoBMC = errors.New("server has no BMC")
	// ErrNoHypervisor means a virtual machine's virsh power settings name a host that is not a
	// swallow Server of its Site, so swallow cannot reach its libvirt.
	ErrNoHypervisor = errors.New("the virtual machine's hypervisor is not a swallow server")
	// ErrBMCCredentialUnavailable means the provisioner would not reveal the BMC connection.
	ErrBMCCredentialUnavailable = errors.New("the provisioner did not reveal the BMC connection")
	// ErrBMCConnectionUnavailable means the provisioner could not be reached to read the BMC
	// connection; retrying later may succeed.
	ErrBMCConnectionUnavailable = errors.New("the provisioner could not be reached for the BMC connection")
	// ErrBMCUnreachable means the BMC's Redfish service could not be reached, stayed busy, or
	// rejected the account.
	ErrBMCUnreachable = errors.New("the BMC's Redfish service is unreachable")
	// ErrRedfishUnsupported means the BMC lacks a Redfish function Boot Media needs.
	ErrRedfishUnsupported = errors.New("the BMC does not support redfish boot media")
	// ErrBootMediaRejected means the BMC refused to mount the ISO or set the boot override.
	ErrBootMediaRejected = errors.New("the BMC refused the boot media")
	// ErrBootMediaApplying means an enable preflight is already running on the Server.
	ErrBootMediaApplying = errors.New("boot media is already being applied to this server; wait until it finishes")
)

// BootMediaError explains a failed Boot Media action in operator terms — which Server and what
// the BMC said — while Unwrap keeps the sentinel for status mapping. Detail never holds a
// credential.
type BootMediaError struct {
	Err    error
	Server string
	Detail string
}

// Error is the operator-facing sentence returned as error.message.
func (e *BootMediaError) Error() string {
	detail := ""
	if e.Detail != "" {
		detail = " " + e.Detail
	}
	switch {
	case errors.Is(e.Err, ErrBootMediaNotConfigured):
		return fmt.Sprintf("Boot Media of Server %q has no Boot ISO it can use.%s", e.Server, detail)
	case errors.Is(e.Err, ErrNoBMC):
		return fmt.Sprintf("Server %q has no Boot Media method: no BMC swallow can drive and no swallow hypervisor in its power settings.", e.Server)
	case errors.Is(e.Err, ErrNoHypervisor):
		return fmt.Sprintf("The virsh power settings of Server %q do not name a swallow hypervisor.%s", e.Server, detail)
	case errors.Is(e.Err, ErrBMCCredentialUnavailable):
		return fmt.Sprintf("The provisioner did not reveal the BMC connection of Server %q; its integration account must be a provisioner administrator.", e.Server)
	case errors.Is(e.Err, ErrBMCUnreachable):
		return fmt.Sprintf("The BMC of Server %q could not be reached over Redfish.%s", e.Server, detail)
	case errors.Is(e.Err, ErrRedfishUnsupported):
		return fmt.Sprintf("The BMC of Server %q does not support Redfish Boot Media.%s", e.Server, detail)
	case errors.Is(e.Err, ErrBootMediaRejected):
		return fmt.Sprintf("The BMC of Server %q refused the Boot Media.%s", e.Server, detail)
	default:
		return fmt.Sprintf("Boot Media on Server %q failed: %v", e.Server, e.Err)
	}
}

// Unwrap exposes the sentinel so delivery maps the status with errors.Is.
func (e *BootMediaError) Unwrap() error { return e.Err }
