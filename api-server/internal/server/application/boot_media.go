package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// Boot Media timings. A BMC is slow: mounting an HTTP ISO takes up to a minute on AMI, and a BMC
// answers 503 for minutes after its host posts. The interactive preflight waits a bounded time so
// an HTTP request returns; the Workflow ensure Task can afford to wait out a busy BMC.
const (
	bootMediaProbeTimeout = 90 * time.Second
	// bootMediaPreflightTimeout covers a mount (up to a minute) plus the three-minute settle a
	// fresh mount needs before the host may power on, plus the boot-direction writes.
	bootMediaPreflightTimeout = 6 * time.Minute
	bootMediaClearTimeout     = 2 * time.Minute
	bootMediaReadTimeout      = 45 * time.Second
	bootMediaEnsureTimeout    = 15 * time.Minute
	// bootMediaProgressWriteTimeout bounds one progress write; progress must never hold up or
	// fail the preflight it describes.
	bootMediaProgressWriteTimeout = 5 * time.Second
	// bootMediaResetCheck is how long after a recovery restart the mount is checked again: AMI
	// dropped a fresh mount within a minute of a reset, while the host's POST takes minutes.
	bootMediaResetCheck = 45 * time.Second
)

// libvirtBootOverride is the boot direction libvirt Boot Media applies: the CD-ROM's boot order in
// the domain's persistent definition, which lasts until Boot Media is disabled.
const libvirtBootOverride = "Continuous"

// BootMediaUseCase owns Boot Media for Servers (decisions 047, 049, and 055): the capability probe,
// the enable preflight with a chosen Boot ISO, disable, and the ensure step that runs before every
// inspection and OS Deployment.
//
// The method follows the Server's Power Configuration (BootMediaEndpointSource): redfish drives
// the BMC with the address and account read from the provisioner per call, never stored or logged;
// libvirt uploads the Boot ISO to the virtual machine's Hypervisor and puts it on the domain's
// CD-ROM. It persists only the swallow-owned setting and capabilities through BootMediaStore. Boot
// ISOs are resolved through BootISOResolver; a Server may only use one of its own provisioner
// Integration. Enabling and disabling change the host's boot, so they pass the Server Lock guard
// (for libvirt, the Hypervisor's too); the ensure step runs inside a Workflow that already passed
// it. Authorization (admin) is the delivery layer's job.
type BootMediaUseCase struct {
	servers   serverdomain.ServerRepository
	store     serverdomain.BootMediaStore
	guard     serverdomain.MutationGuard
	endpoints serverdomain.BootMediaEndpointSource
	redfish   serverdomain.RedfishController
	libvirt   serverdomain.LibvirtHost
	isos      serverdomain.BootISOResolver
	now       func() time.Time
	// resetCheck overrides bootMediaResetCheck in tests.
	resetCheck time.Duration
}

// NewBootMediaUseCase wires the use case. guard may be nil only in the worker, which never calls
// SetEnabled.
func NewBootMediaUseCase(
	servers serverdomain.ServerRepository,
	store serverdomain.BootMediaStore,
	guard serverdomain.MutationGuard,
	endpoints serverdomain.BootMediaEndpointSource,
	redfish serverdomain.RedfishController,
	libvirt serverdomain.LibvirtHost,
	isos serverdomain.BootISOResolver,
) *BootMediaUseCase {
	return &BootMediaUseCase{
		servers: servers, store: store, guard: guard, endpoints: endpoints, redfish: redfish, libvirt: libvirt, isos: isos,
		now: func() time.Time { return time.Now().UTC() }, resetCheck: bootMediaResetCheck,
	}
}

// BootMediaView is one Server's Boot Media as the API reports it: the chosen Boot ISO, the
// Server's setting and capabilities, and — when requested — the BMC's or Hypervisor's live state.
type BootMediaView struct {
	Server *serverdomain.Server
	// Image is the Boot ISO the setting names, nil when it names none.
	Image *BootMediaImageView
	// Apply is the enable preflight running now, nil when none is (an abandoned one is dropped).
	Apply     *serverdomain.BootMediaApply
	Live      *serverdomain.BootMediaState
	LiveError string
}

// BootMediaImageView is the chosen Boot ISO and whether the Server's method can use it now.
// Available is false, with Reason, when the ISO was deleted, its file is missing, or — for
// redfish — no base URL is set.
type BootMediaImageView struct {
	ID        string
	Name      string
	URL       string
	Available bool
	Reason    string
}

// BootISORef is the Boot ISO a Workflow froze for an ensure step: its id and, when the
// installation had a base URL, the URL a BMC mounts.
type BootISORef struct {
	ID  string
	URL string
}

// BootMediaProbe is the result of probing a Server's Boot Media methods.
type BootMediaProbe struct {
	Redfish *serverdomain.RedfishCapability
	Libvirt *serverdomain.LibvirtCapability
}

// Get returns the Server's Boot Media. With live, it also reads the BMC or Hypervisor; a live-read
// failure is reported in LiveError rather than failing the request, because the stored setting is
// still meaningful. Errors: ErrServerNotFound.
func (uc *BootMediaUseCase) Get(ctx context.Context, serverID string, live bool) (*BootMediaView, error) {
	server, err := uc.servers.FindByID(ctx, serverID)
	if err != nil {
		return nil, err
	}
	view, err := uc.view(ctx, server)
	if err != nil {
		return nil, err
	}
	if live && view.Image != nil && view.Image.Available {
		readCtx, cancel := context.WithTimeout(ctx, bootMediaReadTimeout)
		defer cancel()
		state, err := uc.read(readCtx, server, BootISORef{ID: view.Image.ID, URL: view.Image.URL})
		if err == nil {
			view.Live = &state
		} else {
			view.LiveError = uc.explain(server, err).Error()
		}
	}
	return view, nil
}

// read reads the live Boot Media state through the Server's method.
func (uc *BootMediaUseCase) read(ctx context.Context, server *serverdomain.Server, iso BootISORef) (serverdomain.BootMediaState, error) {
	endpoint, err := uc.endpoint(ctx, server)
	if err != nil {
		return serverdomain.BootMediaState{}, err
	}
	if endpoint.Method == serverdomain.BootMediaMethodLibvirt {
		login, err := uc.hypervisor(server, endpoint.Libvirt)
		if err != nil {
			return serverdomain.BootMediaState{}, err
		}
		return uc.libvirt.ReadBootMedia(ctx, login, endpoint.Libvirt.Domain, iso.ID)
	}
	return uc.redfish.ReadBootMedia(ctx, *endpoint.BMC, iso.URL)
}

// view assembles the stored part of a BootMediaView, resolving the Boot ISO the setting names
// for the Server's method. A Boot ISO that no longer exists is still reported, unavailable, so
// the operator sees what the setting points at.
func (uc *BootMediaUseCase) view(ctx context.Context, server *serverdomain.Server) (*BootMediaView, error) {
	view := &BootMediaView{Server: server}
	if server.BootMediaApply.Running(uc.now()) {
		view.Apply = server.BootMediaApply
	}
	if server.BootMedia == nil || server.BootMedia.ISOID == "" {
		return view, nil
	}
	id := server.BootMedia.ISOID
	image, err := uc.isos.Resolve(ctx, id)
	switch {
	case errors.Is(err, serverdomain.ErrBootISOUnknown):
		view.Image = &BootMediaImageView{ID: id, URL: uc.isos.URL(id), Reason: "The Boot ISO no longer exists; choose another."}
		return view, nil
	case errors.Is(err, serverdomain.ErrBootMediaNotConfigured) && image != nil:
		view.Image = &BootMediaImageView{ID: image.ID, Name: image.Name, URL: image.URL, Reason: err.Error()}
		if server.BootMediaMethod() == serverdomain.BootMediaMethodLibvirt {
			// libvirt uploads the file; only a missing file makes it unavailable.
			if _, fileErr := uc.isos.File(ctx, id); fileErr == nil {
				view.Image.Available, view.Image.Reason = true, ""
			}
		}
		return view, nil
	case err != nil:
		return nil, err
	}
	view.Image = &BootMediaImageView{ID: image.ID, Name: image.Name, URL: image.URL, Available: true}
	return view, nil
}

// Probe re-probes the Server's Boot Media method now and stores the result. Errors:
// ErrServerNotFound, and a provisioner that could not be reached (the capabilities are then left
// unchanged).
func (uc *BootMediaUseCase) Probe(ctx context.Context, serverID string) (*BootMediaProbe, error) {
	server, err := uc.servers.FindByID(ctx, serverID)
	if err != nil {
		return nil, err
	}
	return uc.probe(ctx, server)
}

// probe classifies one Server's Boot Media method and stores both capabilities. A Server without
// a BMC or whose provisioner withholds the connection gets a stored Redfish capability saying so;
// a virtual machine gets `no_bmc` plus its libvirt probe; a provisioner outage is returned so
// nothing stale is written.
func (uc *BootMediaUseCase) probe(ctx context.Context, server *serverdomain.Server) (*BootMediaProbe, error) {
	probeCtx, cancel := context.WithTimeout(ctx, bootMediaProbeTimeout)
	defer cancel()
	var capability serverdomain.RedfishCapability
	var libvirt *serverdomain.LibvirtCapability
	endpoint, err := uc.endpoint(probeCtx, server)
	switch {
	case errors.Is(err, serverdomain.ErrNoBMC):
		capability = serverdomain.RedfishCapability{Support: serverdomain.RedfishNoBMC, ProbedAt: uc.now(),
			Reason: "The Server has no BMC (no BMC power driver, or the provisioner holds no BMC address)."}
	case errors.Is(err, serverdomain.ErrBMCCredentialUnavailable):
		capability = serverdomain.RedfishCapability{Support: serverdomain.RedfishUnreachable, ProbedAt: uc.now(),
			Reason: "The provisioner did not reveal the BMC connection; its integration account must be an administrator."}
	case err != nil:
		return nil, err
	case endpoint.Method == serverdomain.BootMediaMethodLibvirt:
		capability = serverdomain.RedfishCapability{Support: serverdomain.RedfishNoBMC, ProbedAt: uc.now(),
			Reason: "The Server is a libvirt virtual machine; its Boot Media method is libvirt."}
		probed, err := uc.probeLibvirt(probeCtx, endpoint.Libvirt)
		if err != nil {
			return nil, fmt.Errorf("probe hypervisor of %s: %w", server.DisplayName(), err)
		}
		libvirt = &probed
	default:
		capability, err = uc.redfish.Probe(probeCtx, *endpoint.BMC)
		if err != nil {
			return nil, fmt.Errorf("probe BMC of %s: %w", server.DisplayName(), err)
		}
	}
	if err := uc.store.SetRedfishCapability(ctx, server.ID, &capability); err != nil {
		return nil, fmt.Errorf("save redfish capability of %s: %w", server.DisplayName(), err)
	}
	server.Redfish = &capability
	if libvirt != nil || server.Libvirt != nil {
		if err := uc.store.SetLibvirtCapability(ctx, server.ID, libvirt); err != nil {
			return nil, fmt.Errorf("save libvirt capability of %s: %w", server.DisplayName(), err)
		}
		server.Libvirt = libvirt
	}
	return &BootMediaProbe{Redfish: &capability, Libvirt: libvirt}, nil
}

// probeLibvirt classifies a virtual machine's Hypervisor and domain. Only ctx ending is an error.
func (uc *BootMediaUseCase) probeLibvirt(ctx context.Context, endpoint *serverdomain.LibvirtEndpoint) (serverdomain.LibvirtCapability, error) {
	capability := serverdomain.LibvirtCapability{
		Account: endpoint.Account, Domain: endpoint.Domain, Pool: serverdomain.BootMediaPool, ProbedAt: uc.now(),
	}
	if endpoint.Hypervisor == nil {
		capability.Support = serverdomain.LibvirtNoHypervisor
		capability.Reason = fmt.Sprintf("The virsh address names host %q, which is not a swallow Server of this Site.", endpoint.Host)
		return capability, nil
	}
	capability.HypervisorServerID = endpoint.Hypervisor.ID
	login, err := serverdomain.HypervisorOf(endpoint.Hypervisor, endpoint.Account)
	if err != nil {
		capability.Support, capability.Reason = serverdomain.LibvirtUnreachable, err.Error()
		return capability, nil
	}
	capability.Account = login.Account
	machine, err := uc.libvirt.Domain(ctx, login, endpoint.Domain)
	switch {
	case err == nil:
		capability.Support, capability.CDROM = serverdomain.LibvirtSupported, machine.CDROM
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return capability, err
	case errors.Is(err, serverdomain.ErrDomainNotFound):
		capability.Support, capability.Reason = serverdomain.LibvirtUnsupported, err.Error()
	default:
		capability.Support, capability.Reason = serverdomain.LibvirtUnreachable, err.Error()
	}
	return capability, nil
}

// BootMediaChange is the result of enabling or disabling Boot Media.
type BootMediaChange struct {
	View *BootMediaView
	// Reverted reports, for a disable, whether the BMC or domain was reset (ISO ejected, boot
	// direction cleared); RevertError explains why not. Enabling leaves both empty.
	Reverted    bool
	RevertError string
}

// SetEnabled enables or disables Boot Media on the Server. isoID names the Boot ISO to enable
// with and is ignored when disabling.
//
// Enabling is the preflight: it requires a Boot ISO of the Server's own provisioner Integration
// that its method can use (redfish needs it served at a URL, libvirt only its file) and an
// unlocked Server with a method, re-probes it, attaches the chosen Boot ISO and directs the boot at
// it, verifies both, and only then saves the setting as enabled with that Boot ISO. Enabling an
// enabled Server re-applies it; enabling with another Boot ISO switches to it. A failed preflight
// records its reason on the setting (so the Server page shows it) without changing whether it is
// enabled or which Boot ISO it names.
//
// While the preflight runs it is recorded on the Server with its current phase (Get reports it
// as Apply), so any client can follow it; only one runs per Server, and a disable waits for it.
//
// Disabling saves the setting first — the operator's intent must not depend on the BMC — then
// makes a best-effort attempt to detach the ISO and clear the boot direction, reported in the
// result.
//
// Errors: ErrServerNotFound, ErrBootISORequired, ErrBootISOUnknown, ErrBootISOWrongIntegration,
// ErrBootMediaApplying, the Server Lock errors, a *HypervisorError, and a *BootMediaError wrapping
// ErrBootMediaNotConfigured, ErrNoBMC, ErrNoHypervisor, ErrBMCCredentialUnavailable,
// ErrBMCUnreachable, ErrRedfishUnsupported, or ErrBootMediaRejected.
func (uc *BootMediaUseCase) SetEnabled(ctx context.Context, serverID string, enabled bool, isoID string) (*BootMediaChange, error) {
	server, err := uc.servers.FindByID(ctx, serverID)
	if err != nil {
		return nil, err
	}
	var image *serverdomain.BootISOImage
	var unserved error
	if enabled {
		if isoID == "" {
			return nil, serverdomain.ErrBootISORequired
		}
		image, err = uc.isos.Resolve(ctx, isoID)
		switch {
		case errors.Is(err, serverdomain.ErrBootMediaNotConfigured):
			// The method decides whether a Boot ISO without a URL is usable; a missing file is not.
			if _, fileErr := uc.isos.File(ctx, isoID); fileErr != nil {
				return nil, &serverdomain.BootMediaError{Err: serverdomain.ErrBootMediaNotConfigured, Server: server.DisplayName(), Detail: fileErr.Error()}
			}
			unserved = err
		case err != nil:
			return nil, err
		}
		if image.IntegrationID != server.Source.IntegrationID {
			return nil, fmt.Errorf("%w: Boot ISO %q chains to another provisioner than Server %q's", serverdomain.ErrBootISOWrongIntegration, image.Name, server.DisplayName())
		}
	}
	if err := uc.guard.RequireUnlocked(ctx, []string{server.ID}); err != nil {
		return nil, err
	}
	if !enabled {
		if server.BootMediaApply.Running(uc.now()) {
			return nil, serverdomain.ErrBootMediaApplying
		}
		return uc.disable(ctx, server)
	}

	// Mongo keeps milliseconds; StartedAt identifies this apply in later writes, so it must
	// compare equal after a round trip.
	started := uc.now().Truncate(time.Millisecond)
	progress := serverdomain.BootMediaApply{
		ISOID: image.ID, Phase: serverdomain.BootMediaPhaseProbing, StartedAt: started, PhaseStartedAt: started,
	}
	if err := uc.store.BeginBootMediaApply(ctx, server.ID, &progress, started.Add(-serverdomain.BootMediaApplyStaleAfter)); err != nil {
		return nil, err
	}
	defer uc.endApply(server.ID, started)
	ctx = serverdomain.WithBootMediaPhaseReporter(ctx, uc.phaseReporter(ctx, server.ID, progress))

	previous := server.BootMedia
	probed, err := uc.probe(ctx, server)
	if err != nil {
		return nil, uc.explain(server, err)
	}
	var mode string
	if probed.Libvirt != nil {
		mode, err = uc.enableLibvirt(ctx, server, probed.Libvirt, image.ID)
	} else {
		mode, err = uc.enableRedfish(ctx, server, previous, probed.Redfish, image, unserved)
	}
	if err != nil {
		uc.recordFailure(ctx, server, previous, err)
		return nil, err
	}
	now := uc.now()
	setting := &serverdomain.BootMediaSetting{
		Enabled: true, ISOID: image.ID, UpdatedAt: now,
		LastAppliedAt: &now, LastAppliedBy: serverdomain.BootMediaAppliedByPreflight, BootOverride: mode,
	}
	if err := uc.store.SetBootMedia(ctx, server.ID, setting); err != nil {
		return nil, fmt.Errorf("save boot media of %s: %w", server.DisplayName(), err)
	}
	server.BootMedia = setting
	view, err := uc.view(ctx, server)
	if err != nil {
		return nil, err
	}
	return &BootMediaChange{View: view}, nil
}

// enableRedfish is the Redfish part of the preflight: a supported BMC and a Boot ISO served at a
// URL, then the mount and boot override.
func (uc *BootMediaUseCase) enableRedfish(ctx context.Context, server *serverdomain.Server, previous *serverdomain.BootMediaSetting,
	capability *serverdomain.RedfishCapability, image *serverdomain.BootISOImage, unserved error) (string, error) {
	if capability.Support != serverdomain.RedfishSupported {
		sentinel := serverdomain.ErrRedfishUnsupported
		switch capability.Support {
		case serverdomain.RedfishNoBMC:
			sentinel = serverdomain.ErrNoBMC
		case serverdomain.RedfishUnreachable:
			sentinel = serverdomain.ErrBMCUnreachable
		}
		return "", &serverdomain.BootMediaError{Err: sentinel, Server: server.DisplayName(), Detail: capability.Reason}
	}
	if unserved != nil {
		return "", &serverdomain.BootMediaError{Err: serverdomain.ErrBootMediaNotConfigured, Server: server.DisplayName(), Detail: unserved.Error()}
	}
	applyCtx, cancel := context.WithTimeout(ctx, bootMediaPreflightTimeout)
	defer cancel()
	mode, err := uc.apply(applyCtx, server, previous, image.URL)
	if err != nil {
		return "", uc.explain(server, err)
	}
	return mode, nil
}

// enableLibvirt is the libvirt part of the preflight: a swallow Hypervisor that is unlocked, then
// the upload and the CD-ROM's boot order. A Hypervisor the probe could not use is tried again by
// the apply, whose error says precisely what failed.
func (uc *BootMediaUseCase) enableLibvirt(ctx context.Context, server *serverdomain.Server, capability *serverdomain.LibvirtCapability, isoID string) (string, error) {
	if capability.Support == serverdomain.LibvirtNoHypervisor {
		return "", &serverdomain.BootMediaError{Err: serverdomain.ErrNoHypervisor, Server: server.DisplayName(), Detail: capability.Reason}
	}
	if err := uc.guard.RequireUnlocked(ctx, []string{capability.HypervisorServerID}); err != nil {
		return "", err
	}
	applyCtx, cancel := context.WithTimeout(ctx, bootMediaPreflightTimeout)
	defer cancel()
	serverdomain.ReportBootMediaPhase(applyCtx, serverdomain.BootMediaPhaseMounting, nil)
	if err := uc.applyLibvirt(applyCtx, server, isoID); err != nil {
		return "", uc.explain(server, err)
	}
	return libvirtBootOverride, nil
}

// applyLibvirt resolves the virtual machine's Hypervisor and the Boot ISO's file and applies it.
func (uc *BootMediaUseCase) applyLibvirt(ctx context.Context, server *serverdomain.Server, isoID string) error {
	endpoint, err := uc.endpoint(ctx, server)
	if err != nil {
		return err
	}
	if endpoint.Method != serverdomain.BootMediaMethodLibvirt {
		return serverdomain.ErrNoHypervisor
	}
	login, err := uc.hypervisor(server, endpoint.Libvirt)
	if err != nil {
		return err
	}
	file, err := uc.isos.File(ctx, isoID)
	if err != nil {
		return err
	}
	_, err = uc.libvirt.ApplyBootMedia(ctx, login, endpoint.Libvirt.Domain, *file)
	return err
}

// mountedURL is the URL the BMC was last told to mount for a setting: its Boot ISO's, or — for a
// setting enabled before Boot ISOs existed — the retired installation ISO's. It is "" when the
// setting never enabled Boot Media.
func (uc *BootMediaUseCase) mountedURL(setting *serverdomain.BootMediaSetting) string {
	if setting == nil || (setting.ISOID == "" && setting.LastAppliedAt == nil) {
		return ""
	}
	return uc.isos.URL(setting.ISOID)
}

// disable saves the disabled setting, keeping the last Boot ISO as history, then tries to reset
// the BMC or the domain.
func (uc *BootMediaUseCase) disable(ctx context.Context, server *serverdomain.Server) (*BootMediaChange, error) {
	previous := server.BootMedia
	setting := &serverdomain.BootMediaSetting{Enabled: false, UpdatedAt: uc.now()}
	if previous != nil {
		setting.ISOID = previous.ISOID
		setting.LastAppliedAt, setting.LastAppliedBy, setting.BootOverride = previous.LastAppliedAt, previous.LastAppliedBy, previous.BootOverride
	}
	if err := uc.store.SetBootMedia(ctx, server.ID, setting); err != nil {
		return nil, fmt.Errorf("save boot media of %s: %w", server.DisplayName(), err)
	}
	server.BootMedia = setting
	view, err := uc.view(ctx, server)
	if err != nil {
		return nil, err
	}
	change := &BootMediaChange{View: view}
	clearCtx, cancel := context.WithTimeout(ctx, bootMediaClearTimeout)
	defer cancel()
	if err := uc.clear(clearCtx, server, previous); err != nil {
		change.RevertError = uc.explain(server, err).Error()
		return change, nil
	}
	change.Reverted = true
	return change, nil
}

// clear detaches the ISO and clears the boot direction through the Server's method.
func (uc *BootMediaUseCase) clear(ctx context.Context, server *serverdomain.Server, previous *serverdomain.BootMediaSetting) error {
	endpoint, err := uc.endpoint(ctx, server)
	if err != nil {
		return err
	}
	if endpoint.Method == serverdomain.BootMediaMethodLibvirt {
		login, err := uc.hypervisor(server, endpoint.Libvirt)
		if err != nil {
			return err
		}
		if err := uc.guard.RequireUnlocked(ctx, []string{login.ServerID}); err != nil {
			return err
		}
		return uc.libvirt.ClearBootMedia(ctx, login, endpoint.Libvirt.Domain)
	}
	return uc.redfish.ClearBootMedia(ctx, *endpoint.BMC, uc.mountedURL(previous))
}

// EnsureOutcome reports what an ensure step did.
type EnsureOutcome struct {
	// Skipped means Boot Media is no longer enabled on the Server, so nothing was done.
	Skipped bool
	// WasReady reports that the next boot already started from the ISO, before the step changed
	// anything.
	WasReady bool
	Mode     string
}

// Ensure is the ensure-boot-media Task of an inspection or OS Deployment: when the Server still
// has Boot Media enabled it reads the live state, then re-applies the ISO and boot direction
// unconditionally — a BIOS can consume or re-sort them at every boot, and a provisioner's own
// power-on overrides one-time settings, so "already ready" is not trusted to survive until the
// host boots. iso is the Boot ISO frozen when the Workflow was created: redfish mounts its URL,
// and libvirt uploads its file (the setting's Boot ISO for a Workflow that froze only a URL). A
// failure is recorded on the setting and returned. Errors: ErrServerNotFound, the provisioner
// errors, a *HypervisorError, and a *BootMediaError.
func (uc *BootMediaUseCase) Ensure(ctx context.Context, serverID string, iso BootISORef) (EnsureOutcome, error) {
	server, err := uc.servers.FindByID(ctx, serverID)
	if err != nil {
		return EnsureOutcome{}, err
	}
	if server.BootMedia == nil || !server.BootMedia.Enabled {
		return EnsureOutcome{Skipped: true}, nil
	}
	if iso.ID == "" {
		iso.ID = server.BootMedia.ISOID
	}
	ensureCtx, cancel := context.WithTimeout(ctx, bootMediaEnsureTimeout)
	defer cancel()
	outcome, err := uc.ensure(ensureCtx, server, iso)
	if err != nil {
		failure := uc.explain(server, err)
		uc.recordFailure(ctx, server, server.BootMedia, failure)
		return outcome, failure
	}
	now := uc.now()
	setting := *server.BootMedia
	setting.LastAppliedAt, setting.LastAppliedBy, setting.BootOverride = &now, serverdomain.BootMediaAppliedByEnsure, outcome.Mode
	setting.LastError, setting.LastErrorAt = "", nil
	if err := uc.store.SetBootMedia(ctx, server.ID, &setting); err != nil {
		// The ISO is attached; losing the history line must not fail the Workflow.
		slog.Warn("record boot media ensure", "serverId", server.ID, "error", err)
	}
	server.BootMedia = &setting
	return outcome, nil
}

func (uc *BootMediaUseCase) ensure(ctx context.Context, server *serverdomain.Server, iso BootISORef) (EnsureOutcome, error) {
	endpoint, err := uc.endpoint(ctx, server)
	if err != nil {
		return EnsureOutcome{}, err
	}
	outcome := EnsureOutcome{}
	if endpoint.Method == serverdomain.BootMediaMethodLibvirt {
		login, err := uc.hypervisor(server, endpoint.Libvirt)
		if err != nil {
			return outcome, err
		}
		if iso.ID == "" {
			return outcome, fmt.Errorf("%w: no Boot ISO is chosen", serverdomain.ErrBootMediaNotConfigured)
		}
		file, err := uc.isos.File(ctx, iso.ID)
		if err != nil {
			return outcome, err
		}
		if state, err := uc.libvirt.ReadBootMedia(ctx, login, endpoint.Libvirt.Domain, iso.ID); err == nil {
			outcome.WasReady = state.Ready()
		}
		if _, err := uc.libvirt.ApplyBootMedia(ctx, login, endpoint.Libvirt.Domain, *file); err != nil {
			return outcome, err
		}
		outcome.Mode = libvirtBootOverride
		return outcome, nil
	}
	if iso.URL == "" {
		return outcome, fmt.Errorf("%w: the Boot ISO had no URL when the Workflow was accepted; set the installation's Boot Media base URL", serverdomain.ErrBootMediaNotConfigured)
	}
	if state, err := uc.redfish.ReadBootMedia(ctx, *endpoint.BMC, iso.URL); err == nil {
		outcome.WasReady = state.Ready()
	}
	mode, _, err := uc.redfish.ApplyBootMedia(ctx, *endpoint.BMC, iso.URL)
	if err != nil {
		return outcome, err
	}
	outcome.Mode = mode
	return outcome, nil
}

// EnableApplied applies Boot Media with isoID to a virtual machine that virtual-machine enrollment
// registered and saves it as enabled, so its inspection and every OS deployment re-apply it. It is
// not the preflight: it records no progress and checks no Server Lock, because the enrollment
// Workflow holds the Hypervisor and the Server was projected moments ago. It stores the probes
// first so the Server reports the libvirt method. Errors: ErrServerNotFound, ErrBootISOUnknown,
// ErrBootISOWrongIntegration, a *HypervisorError, and a *BootMediaError.
func (uc *BootMediaUseCase) EnableApplied(ctx context.Context, serverID, isoID string) error {
	server, err := uc.servers.FindByID(ctx, serverID)
	if err != nil {
		return err
	}
	file, err := uc.isos.File(ctx, isoID)
	if err != nil {
		return uc.explain(server, err)
	}
	if file.IntegrationID != server.Source.IntegrationID {
		return fmt.Errorf("%w: Boot ISO %q chains to another provisioner than Server %q's", serverdomain.ErrBootISOWrongIntegration, file.Name, server.DisplayName())
	}
	probed, err := uc.probe(ctx, server)
	if err != nil {
		return uc.explain(server, err)
	}
	if probed.Libvirt == nil || probed.Libvirt.Support != serverdomain.LibvirtSupported {
		reason := "The Server's power settings do not name a swallow hypervisor."
		if probed.Libvirt != nil {
			reason = probed.Libvirt.Reason
		}
		return &serverdomain.BootMediaError{Err: serverdomain.ErrNoHypervisor, Server: server.DisplayName(), Detail: reason}
	}
	applyCtx, cancel := context.WithTimeout(ctx, bootMediaPreflightTimeout)
	defer cancel()
	if err := uc.applyLibvirt(applyCtx, server, isoID); err != nil {
		return uc.explain(server, err)
	}
	now := uc.now()
	setting := &serverdomain.BootMediaSetting{
		Enabled: true, ISOID: isoID, UpdatedAt: now,
		LastAppliedAt: &now, LastAppliedBy: serverdomain.BootMediaAppliedByEnsure, BootOverride: libvirtBootOverride,
	}
	if err := uc.store.SetBootMedia(ctx, server.ID, setting); err != nil {
		return fmt.Errorf("save boot media of %s: %w", server.DisplayName(), err)
	}
	server.BootMedia = setting
	return nil
}

// RecoverDeploymentBoot is called by a running OS deployment of a Server with Boot Media enabled
// whose host may not boot the ISO after the provisioner powered it on. isoURL is the URL frozen
// in the Workflow. It returns true when it changed the BMC or the host.
//
// Shortly after the power-on (notBooted false) the host is still in its POST: when the BMC
// reports the ISO no longer mounted — AMI MegaRAC can drop a fresh mount at a host power-on or
// reset — it mounts the ISO at once, without a settle and without a restart, so the BIOS finds it
// at its boot-device scan.
//
// Long after the power-on (notBooted true) the host has not reached the provisioner — the BIOS
// can boot the disk although the BMC reports the virtual media group first — so it re-applies
// Boot Media (mount, boot order, and a one-time boot to the virtual CD) and restarts the host
// through Redfish, not through the provisioner, so the provisioner's deployment boot starts from
// the ISO. A mount the restart dropped is mounted again while the host POSTs. A restart
// interrupts whatever the host was booting, so the caller must restart a deployment attempt at
// most once.
//
// A Server without Boot Media, and a libvirt virtual machine — whose persistent definition nothing
// undoes — are left alone. Errors: the endpoint and controller errors, wrapped as BootMediaError.
func (uc *BootMediaUseCase) RecoverDeploymentBoot(ctx context.Context, serverID, isoURL string, notBooted bool) (bool, error) {
	server, err := uc.servers.FindByID(ctx, serverID)
	if err != nil {
		return false, err
	}
	if server.BootMedia == nil || !server.BootMedia.Enabled || isoURL == "" {
		return false, nil
	}
	recoverCtx, cancel := context.WithTimeout(ctx, bootMediaEnsureTimeout)
	defer cancel()
	endpoint, err := uc.endpoint(recoverCtx, server)
	if err != nil {
		return false, uc.explain(server, err)
	}
	if endpoint.Method != serverdomain.BootMediaMethodRedfish {
		return false, nil
	}
	bmc := *endpoint.BMC
	mode := server.BootMedia.BootOverride
	if notBooted {
		if mode, _, err = uc.redfish.ApplyBootMedia(recoverCtx, bmc, isoURL); err != nil {
			failure := uc.explain(server, err)
			uc.recordFailure(ctx, server, server.BootMedia, failure)
			return false, failure
		}
		if err := uc.redfish.ResetHost(recoverCtx, bmc); err != nil {
			return false, uc.explain(server, err)
		}
		if err := sleepContext(recoverCtx, uc.resetCheck); err != nil {
			return true, err
		}
	}
	state, err := uc.redfish.ReadBootMedia(recoverCtx, bmc, isoURL)
	if err != nil {
		return notBooted, uc.explain(server, err)
	}
	if state.MediaInserted && !notBooted {
		return false, nil
	}
	if !state.MediaInserted {
		if _, err := uc.redfish.MountBootMedia(recoverCtx, bmc, isoURL); err != nil {
			failure := uc.explain(server, err)
			uc.recordFailure(ctx, server, server.BootMedia, failure)
			return notBooted, failure
		}
	}
	now := uc.now()
	setting := *server.BootMedia
	setting.LastAppliedAt, setting.LastAppliedBy, setting.BootOverride = &now, serverdomain.BootMediaAppliedByEnsure, mode
	setting.LastError, setting.LastErrorAt = "", nil
	if err := uc.store.SetBootMedia(ctx, server.ID, &setting); err != nil {
		slog.Warn("record boot media recovery", "serverId", server.ID, "error", err)
	}
	return true, nil
}

// sleepContext waits d or until ctx ends.
func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ProbeStale probes, with bounded concurrency, every present Server whose capability is missing
// or older than window — the enrollment-time detection: a Server the reconciler just created has
// no capability, so the next pass probes it. Failures are logged per Server and never stop the
// pass; the count of probed Servers is returned.
func (uc *BootMediaUseCase) ProbeStale(ctx context.Context, window time.Duration, concurrency int) (int, error) {
	result, err := uc.servers.List(ctx, serverdomain.ListFilter{})
	if err != nil {
		return 0, err
	}
	now := uc.now()
	var stale []*serverdomain.Server
	for _, server := range result.Servers {
		if server.Provisioning != nil && server.Redfish.Stale(now, window) {
			stale = append(stale, server)
		}
	}
	if concurrency < 1 {
		concurrency = 1
	}
	slots := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, server := range stale {
		select {
		case <-ctx.Done():
			wg.Wait()
			return len(stale), ctx.Err()
		case slots <- struct{}{}:
		}
		wg.Add(1)
		go func(server *serverdomain.Server) {
			defer wg.Done()
			defer func() { <-slots }()
			if _, err := uc.probe(ctx, server); err != nil {
				slog.Warn("boot media capability probe", "serverId", server.ID, "server", server.DisplayName(), "error", err)
			}
		}(server)
	}
	wg.Wait()
	return len(stale), nil
}

// apply applies isoURL through the BMC. A different ISO the previous setting had mounted is
// ejected first, best effort: a BMC whose virtual CDs are all taken refuses a new mount, and a
// failure to eject is reported by the mount that follows.
func (uc *BootMediaUseCase) apply(ctx context.Context, server *serverdomain.Server, previous *serverdomain.BootMediaSetting, isoURL string) (string, error) {
	endpoint, err := uc.endpoint(ctx, server)
	if err != nil {
		return "", err
	}
	if endpoint.Method != serverdomain.BootMediaMethodRedfish {
		return "", serverdomain.ErrNoBMC
	}
	if old := uc.mountedURL(previous); old != "" && old != isoURL {
		serverdomain.ReportBootMediaPhase(ctx, serverdomain.BootMediaPhaseEjecting, nil)
		if err := uc.redfish.ClearBootMedia(ctx, *endpoint.BMC, old); err != nil {
			slog.Warn("eject previous boot ISO", "serverId", server.ID, "error", err)
		}
	}
	mode, _, err := uc.redfish.ApplyBootMedia(ctx, *endpoint.BMC, isoURL)
	return mode, err
}

// phaseReporter records each phase the preflight that started as progress enters. Writes are
// best effort and detached from the preflight's deadline, so a slow or failed write only makes
// the reported progress lag.
func (uc *BootMediaUseCase) phaseReporter(ctx context.Context, serverID string, progress serverdomain.BootMediaApply) serverdomain.BootMediaPhaseReporter {
	detached := context.WithoutCancel(ctx)
	return func(phase serverdomain.BootMediaPhase, endsAt *time.Time) {
		progress.Phase, progress.PhaseStartedAt, progress.PhaseEndsAt = phase, uc.now().Truncate(time.Millisecond), endsAt
		writeCtx, cancel := context.WithTimeout(detached, bootMediaProgressWriteTimeout)
		defer cancel()
		current := progress
		if err := uc.store.UpdateBootMediaApply(writeCtx, serverID, &current); err != nil {
			slog.Warn("record boot media progress", "serverId", serverID, "phase", phase, "error", err)
		}
	}
}

// endApply removes the record of the preflight that started at startedAt, whatever its outcome.
// It runs detached from the request: a client that gave up must not leave the Server looking
// busy until the record goes stale.
func (uc *BootMediaUseCase) endApply(serverID string, startedAt time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), bootMediaProgressWriteTimeout)
	defer cancel()
	if err := uc.store.EndBootMediaApply(ctx, serverID, startedAt); err != nil {
		slog.Warn("end boot media progress", "serverId", serverID, "error", err)
	}
}

// endpoint resolves the Server's Boot Media method and endpoint, filling the host UUID used to
// pick a BMC's System. Which method a Server has is the endpoint source's answer, from the power
// adapter of its Power Configuration (decisions 054 and 055); this use case does not guess it.
func (uc *BootMediaUseCase) endpoint(ctx context.Context, server *serverdomain.Server) (*serverdomain.BootMediaEndpoint, error) {
	endpoint, err := uc.endpoints.BootMediaEndpoint(ctx, server)
	if err != nil {
		return nil, err
	}
	if endpoint.BMC != nil && endpoint.BMC.HostUUID == "" {
		endpoint.BMC.HostUUID = server.Hardware.SystemUUID
	}
	return endpoint, nil
}

// hypervisor is the login to a virtual machine's Hypervisor, or ErrNoHypervisor when its power
// settings name no swallow Server.
func (uc *BootMediaUseCase) hypervisor(server *serverdomain.Server, endpoint *serverdomain.LibvirtEndpoint) (serverdomain.HypervisorLogin, error) {
	if endpoint.Hypervisor == nil {
		return serverdomain.HypervisorLogin{}, &serverdomain.BootMediaError{Err: serverdomain.ErrNoHypervisor, Server: server.DisplayName(),
			Detail: fmt.Sprintf("Host %q is not a swallow Server of this Site.", endpoint.Host)}
	}
	return serverdomain.HypervisorOf(endpoint.Hypervisor, endpoint.Account)
}

// recordFailure stores the failure reason on the setting, keeping whether it is enabled. A write
// failure is only logged: the operator still gets the original error.
func (uc *BootMediaUseCase) recordFailure(ctx context.Context, server *serverdomain.Server, previous *serverdomain.BootMediaSetting, failure error) {
	now := uc.now()
	setting := serverdomain.BootMediaSetting{UpdatedAt: now}
	if previous != nil {
		setting = *previous
	}
	setting.LastError, setting.LastErrorAt = failure.Error(), &now
	if err := uc.store.SetBootMedia(ctx, server.ID, &setting); err != nil {
		slog.Warn("record boot media failure", "serverId", server.ID, "error", err)
		return
	}
	server.BootMedia = &setting
}

// detailed is implemented by controller errors that carry the BMC's own explanation.
type detailed interface{ Detail() string }

// explain wraps a controller or endpoint error into the operator-facing BootMediaError for the
// Server; errors that already are one or a HypervisorError, and unexpected errors, pass through as
// they are.
func (uc *BootMediaUseCase) explain(server *serverdomain.Server, err error) error {
	var already *serverdomain.BootMediaError
	var hypervisor *serverdomain.HypervisorError
	if errors.As(err, &already) || errors.As(err, &hypervisor) {
		return err
	}
	for _, sentinel := range []error{
		serverdomain.ErrBootMediaNotConfigured, serverdomain.ErrNoBMC, serverdomain.ErrNoHypervisor, serverdomain.ErrBMCCredentialUnavailable,
		serverdomain.ErrBMCUnreachable, serverdomain.ErrRedfishUnsupported, serverdomain.ErrBootMediaRejected,
	} {
		if errors.Is(err, sentinel) {
			detail := ""
			var d detailed
			switch {
			case errors.As(err, &d):
				detail = d.Detail()
			case errors.Is(sentinel, serverdomain.ErrBootMediaNotConfigured) && err.Error() != sentinel.Error():
				detail = err.Error()
			}
			return &serverdomain.BootMediaError{Err: sentinel, Server: server.DisplayName(), Detail: detail}
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &serverdomain.BootMediaError{Err: serverdomain.ErrBMCUnreachable, Server: server.DisplayName(), Detail: "The BMC did not answer in time."}
	}
	return err
}
