package domain

import (
	"context"
	"errors"
	"fmt"
	"time"
)

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

// BootMediaApplier names what last applied Boot Media to a Server's BMC.
type BootMediaApplier string

const (
	// BootMediaAppliedByPreflight is the operator's enable action on the Server.
	BootMediaAppliedByPreflight BootMediaApplier = "preflight"
	// BootMediaAppliedByEnsure is the ensure-boot-media Task of an OS Deployment.
	BootMediaAppliedByEnsure BootMediaApplier = "ensure"
)

// BootMediaSetting is a Server's Boot Media setting (decision 047): the operator's intent that
// the Server boot the installation's iPXE ISO first, plus the outcome of the last apply.
//
// It is swallow-owned and written only through BootMediaStore; provider reconciliation preserves
// it. It records intent and history, never the BMC's live state — whether the ISO is mounted now
// is read from the BMC (BootMediaState).
type BootMediaSetting struct {
	Enabled   bool
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

// BootMediaState is the BMC's live Boot Media state for one Server, read for one request.
type BootMediaState struct {
	// MediaInserted reports that a virtual CD holds the installation's ISO.
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

// Ready reports that the next boot will start from the installation's ISO.
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

// BMCEndpointSource reads a Server's BMC endpoint from its provisioner (decision 047).
//
// Implementations return ErrNoBMC when the Server has no BMC (a virtual machine, no power driver,
// no address) or its provisioner cannot hand out BMC connections, ErrBMCCredentialUnavailable when
// the provisioner refused to reveal the connection, and ErrBMCConnectionUnavailable (wrapped)
// when it could not be reached. They must not log or retain the password.
type BMCEndpointSource interface {
	BMCEndpoint(ctx context.Context, server *Server) (*BMCEndpoint, error)
}

// RedfishController drives the Redfish Boot Media functions of one BMC (decision 047).
//
// Every method re-discovers the BMC's resources (host System, virtual CD, actions) instead of
// trusting stored paths, retries a busy BMC (HTTP 503, connection resets) until ctx is done, and
// never puts the password into an error. ISO URLs passed in are the installation's Boot Media URL.
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
// repository implements both on the same document. Both methods replace the field (nil clears
// it), touch nothing else, and return ErrServerNotFound for an unknown id.
type BootMediaStore interface {
	SetBootMedia(ctx context.Context, id string, setting *BootMediaSetting) error
	SetRedfishCapability(ctx context.Context, id string, capability *RedfishCapability) error
}

// BootMediaImage is the installation's Boot Media ISO as configured (decision 047): one file,
// served by swallow at one URL that the installation fixes.
type BootMediaImage interface {
	// URL is the absolute HTTP URL BMCs mount; empty when Boot Media is not configured.
	URL() string
	// Available returns nil when the ISO is configured and being served, otherwise an error
	// wrapping ErrBootMediaNotConfigured that says what is missing.
	Available() error
}

var (
	// ErrBootMediaNotConfigured means the installation serves no Boot Media ISO (no file or no
	// base URL configured), so no Server can enable it.
	ErrBootMediaNotConfigured = errors.New("boot media is not configured for this installation")
	// ErrNoBMC means the Server has no BMC swallow can drive.
	ErrNoBMC = errors.New("server has no BMC")
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
		return "This installation does not serve a Boot Media ISO." + detail
	case errors.Is(e.Err, ErrNoBMC):
		return fmt.Sprintf("Server %q has no BMC swallow can drive (a virtual machine, or the provisioner holds no BMC address).", e.Server)
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
