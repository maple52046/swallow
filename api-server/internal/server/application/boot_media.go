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
	// bootMediaResetCheck is how long after a recovery restart the mount is checked again: AMI
	// dropped a fresh mount within a minute of a reset, while the host's POST takes minutes.
	bootMediaResetCheck = 45 * time.Second
)

// BootMediaUseCase owns Boot Media for Servers (decision 047): the Redfish capability probe, the
// enable preflight, disable, and the ensure step that runs before every OS Deployment.
//
// It reads each BMC's address and account from the provisioner per call (BMCEndpointSource) and
// never stores or logs them; it persists only the swallow-owned setting and capability through
// BootMediaStore. Enabling and disabling change the host's boot, so they pass the Server Lock
// guard; the ensure step runs inside an OS Deployment Workflow that already passed it.
// Authorization (admin) is the delivery layer's job.
type BootMediaUseCase struct {
	servers   serverdomain.ServerRepository
	store     serverdomain.BootMediaStore
	guard     serverdomain.MutationGuard
	endpoints serverdomain.BMCEndpointSource
	redfish   serverdomain.RedfishController
	image     serverdomain.BootMediaImage
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
	endpoints serverdomain.BMCEndpointSource,
	redfish serverdomain.RedfishController,
	image serverdomain.BootMediaImage,
) *BootMediaUseCase {
	return &BootMediaUseCase{
		servers: servers, store: store, guard: guard, endpoints: endpoints, redfish: redfish, image: image,
		now: func() time.Time { return time.Now().UTC() }, resetCheck: bootMediaResetCheck,
	}
}

// BootMediaView is one Server's Boot Media as the API reports it: the installation ISO, the
// Server's setting and capability, and — when requested — the BMC's live state.
type BootMediaView struct {
	Server         *serverdomain.Server
	ImageURL       string
	ImageAvailable bool
	ImageReason    string
	Live           *serverdomain.BootMediaState
	LiveError      string
}

// Get returns the Server's Boot Media. With live, it also reads the BMC; a live-read failure is
// reported in LiveError rather than failing the request, because the stored setting is still
// meaningful. Errors: ErrServerNotFound.
func (uc *BootMediaUseCase) Get(ctx context.Context, serverID string, live bool) (*BootMediaView, error) {
	server, err := uc.servers.FindByID(ctx, serverID)
	if err != nil {
		return nil, err
	}
	view := uc.view(server)
	if live && view.ImageAvailable {
		readCtx, cancel := context.WithTimeout(ctx, bootMediaReadTimeout)
		defer cancel()
		endpoint, err := uc.endpoint(readCtx, server)
		if err == nil {
			var state serverdomain.BootMediaState
			state, err = uc.redfish.ReadBootMedia(readCtx, *endpoint, view.ImageURL)
			if err == nil {
				view.Live = &state
			}
		}
		if err != nil {
			view.LiveError = uc.explain(server, err).Error()
		}
	}
	return view, nil
}

// view assembles the stored part of a BootMediaView.
func (uc *BootMediaUseCase) view(server *serverdomain.Server) *BootMediaView {
	view := &BootMediaView{Server: server, ImageURL: uc.image.URL()}
	if err := uc.image.Available(); err != nil {
		view.ImageReason = err.Error()
	} else {
		view.ImageAvailable = true
	}
	return view
}

// Probe re-probes the Server's BMC now and stores the result. Errors: ErrServerNotFound, and a
// provisioner that could not be reached (the capability is then left unchanged).
func (uc *BootMediaUseCase) Probe(ctx context.Context, serverID string) (*serverdomain.RedfishCapability, error) {
	server, err := uc.servers.FindByID(ctx, serverID)
	if err != nil {
		return nil, err
	}
	return uc.probe(ctx, server)
}

// probe classifies one Server's BMC and stores the capability. A Server without a BMC or whose
// provisioner withholds the connection gets a stored capability saying so; a provisioner outage
// is returned so nothing stale is written.
func (uc *BootMediaUseCase) probe(ctx context.Context, server *serverdomain.Server) (*serverdomain.RedfishCapability, error) {
	probeCtx, cancel := context.WithTimeout(ctx, bootMediaProbeTimeout)
	defer cancel()
	var capability serverdomain.RedfishCapability
	endpoint, err := uc.endpoint(probeCtx, server)
	switch {
	case errors.Is(err, serverdomain.ErrNoBMC):
		capability = serverdomain.RedfishCapability{Support: serverdomain.RedfishNoBMC, ProbedAt: uc.now(),
			Reason: "The Server has no BMC (a virtual machine, or the provisioner holds no BMC address)."}
	case errors.Is(err, serverdomain.ErrBMCCredentialUnavailable):
		capability = serverdomain.RedfishCapability{Support: serverdomain.RedfishUnreachable, ProbedAt: uc.now(),
			Reason: "The provisioner did not reveal the BMC connection; its integration account must be an administrator."}
	case err != nil:
		return nil, err
	default:
		capability, err = uc.redfish.Probe(probeCtx, *endpoint)
		if err != nil {
			return nil, fmt.Errorf("probe BMC of %s: %w", server.DisplayName(), err)
		}
	}
	if err := uc.store.SetRedfishCapability(ctx, server.ID, &capability); err != nil {
		return nil, fmt.Errorf("save redfish capability of %s: %w", server.DisplayName(), err)
	}
	server.Redfish = &capability
	return &capability, nil
}

// BootMediaChange is the result of enabling or disabling Boot Media.
type BootMediaChange struct {
	View *BootMediaView
	// Reverted reports, for a disable, whether the BMC was reset (ISO ejected, override cleared);
	// RevertError explains why not. Enabling leaves both empty.
	Reverted    bool
	RevertError string
}

// SetEnabled enables or disables Boot Media on the Server.
//
// Enabling is the preflight: it requires the installation ISO, an unlocked Server with a BMC,
// re-probes the BMC, mounts the ISO and sets the boot override, verifies the BMC reports both,
// and only then saves the setting as enabled. A failed preflight records its reason on the
// setting (so the Server page shows it) without changing whether it is enabled.
//
// Disabling saves the setting first — the operator's intent must not depend on the BMC — then
// makes a best-effort attempt to eject the ISO and clear the override, reported in the result.
//
// Errors: ErrServerNotFound, the Server Lock errors, and a *BootMediaError wrapping
// ErrBootMediaNotConfigured, ErrNoBMC, ErrBMCCredentialUnavailable, ErrBMCUnreachable,
// ErrRedfishUnsupported, or ErrBootMediaRejected.
func (uc *BootMediaUseCase) SetEnabled(ctx context.Context, serverID string, enabled bool) (*BootMediaChange, error) {
	server, err := uc.servers.FindByID(ctx, serverID)
	if err != nil {
		return nil, err
	}
	if enabled {
		if err := uc.image.Available(); err != nil {
			return nil, &serverdomain.BootMediaError{Err: serverdomain.ErrBootMediaNotConfigured, Server: server.DisplayName(), Detail: err.Error()}
		}
	}
	if err := uc.guard.RequireUnlocked(ctx, []string{server.ID}); err != nil {
		return nil, err
	}
	if !enabled {
		return uc.disable(ctx, server)
	}

	previous := server.BootMedia
	capability, err := uc.probe(ctx, server)
	if err != nil {
		return nil, uc.explain(server, err)
	}
	if capability.Support != serverdomain.RedfishSupported {
		sentinel := serverdomain.ErrRedfishUnsupported
		switch capability.Support {
		case serverdomain.RedfishNoBMC:
			sentinel = serverdomain.ErrNoBMC
		case serverdomain.RedfishUnreachable:
			sentinel = serverdomain.ErrBMCUnreachable
		}
		failure := &serverdomain.BootMediaError{Err: sentinel, Server: server.DisplayName(), Detail: capability.Reason}
		uc.recordFailure(ctx, server, previous, failure)
		return nil, failure
	}

	applyCtx, cancel := context.WithTimeout(ctx, bootMediaPreflightTimeout)
	defer cancel()
	mode, err := uc.apply(applyCtx, server)
	if err != nil {
		failure := uc.explain(server, err)
		uc.recordFailure(ctx, server, previous, failure)
		return nil, failure
	}
	now := uc.now()
	setting := &serverdomain.BootMediaSetting{
		Enabled: true, UpdatedAt: now,
		LastAppliedAt: &now, LastAppliedBy: serverdomain.BootMediaAppliedByPreflight, BootOverride: mode,
	}
	if err := uc.store.SetBootMedia(ctx, server.ID, setting); err != nil {
		return nil, fmt.Errorf("save boot media of %s: %w", server.DisplayName(), err)
	}
	server.BootMedia = setting
	return &BootMediaChange{View: uc.view(server)}, nil
}

// disable saves the disabled setting, then tries to reset the BMC.
func (uc *BootMediaUseCase) disable(ctx context.Context, server *serverdomain.Server) (*BootMediaChange, error) {
	setting := &serverdomain.BootMediaSetting{Enabled: false, UpdatedAt: uc.now()}
	if previous := server.BootMedia; previous != nil {
		setting.LastAppliedAt, setting.LastAppliedBy, setting.BootOverride = previous.LastAppliedAt, previous.LastAppliedBy, previous.BootOverride
	}
	if err := uc.store.SetBootMedia(ctx, server.ID, setting); err != nil {
		return nil, fmt.Errorf("save boot media of %s: %w", server.DisplayName(), err)
	}
	server.BootMedia = setting
	change := &BootMediaChange{View: uc.view(server)}
	clearCtx, cancel := context.WithTimeout(ctx, bootMediaClearTimeout)
	defer cancel()
	endpoint, err := uc.endpoint(clearCtx, server)
	if err == nil {
		err = uc.redfish.ClearBootMedia(clearCtx, *endpoint, uc.image.URL())
	}
	if err != nil {
		change.RevertError = uc.explain(server, err).Error()
		return change, nil
	}
	change.Reverted = true
	return change, nil
}

// EnsureOutcome reports what an ensure step did.
type EnsureOutcome struct {
	// Skipped means Boot Media is no longer enabled on the Server, so nothing was done.
	Skipped bool
	// WasReady reports that the BMC already booted the ISO next, before the step changed anything.
	WasReady bool
	Mode     string
}

// Ensure is the ensure-boot-media Task of an OS Deployment: when the Server still has Boot Media
// enabled it reads the BMC, then re-applies the mount and override unconditionally — a BIOS can
// consume or re-sort them at every boot, and a provisioner's own power-on overrides one-time
// settings, so "already ready" is not trusted to survive until the deploy boots. isoURL is the
// URL frozen when the Workflow was created. A failure is recorded on the setting and returned.
// Errors: ErrServerNotFound, the provisioner errors, and a *BootMediaError.
func (uc *BootMediaUseCase) Ensure(ctx context.Context, serverID, isoURL string) (EnsureOutcome, error) {
	server, err := uc.servers.FindByID(ctx, serverID)
	if err != nil {
		return EnsureOutcome{}, err
	}
	if server.BootMedia == nil || !server.BootMedia.Enabled {
		return EnsureOutcome{Skipped: true}, nil
	}
	ensureCtx, cancel := context.WithTimeout(ctx, bootMediaEnsureTimeout)
	defer cancel()
	endpoint, err := uc.endpoint(ensureCtx, server)
	if err != nil {
		failure := uc.explain(server, err)
		uc.recordFailure(ctx, server, server.BootMedia, failure)
		return EnsureOutcome{}, failure
	}
	outcome := EnsureOutcome{}
	if state, err := uc.redfish.ReadBootMedia(ensureCtx, *endpoint, isoURL); err == nil {
		outcome.WasReady = state.Ready()
	}
	mode, _, err := uc.redfish.ApplyBootMedia(ensureCtx, *endpoint, isoURL)
	if err != nil {
		failure := uc.explain(server, err)
		uc.recordFailure(ctx, server, server.BootMedia, failure)
		return outcome, failure
	}
	outcome.Mode = mode
	now := uc.now()
	setting := *server.BootMedia
	setting.LastAppliedAt, setting.LastAppliedBy, setting.BootOverride = &now, serverdomain.BootMediaAppliedByEnsure, mode
	setting.LastError, setting.LastErrorAt = "", nil
	if err := uc.store.SetBootMedia(ctx, server.ID, &setting); err != nil {
		// The BMC is ready; losing the history line must not fail the deployment.
		slog.Warn("record boot media ensure", "serverId", server.ID, "error", err)
	}
	return outcome, nil
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
// A Server without Boot Media is left alone. Errors: the endpoint and controller errors, wrapped
// as BootMediaError.
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
	mode := server.BootMedia.BootOverride
	if notBooted {
		if mode, _, err = uc.redfish.ApplyBootMedia(recoverCtx, *endpoint, isoURL); err != nil {
			failure := uc.explain(server, err)
			uc.recordFailure(ctx, server, server.BootMedia, failure)
			return false, failure
		}
		if err := uc.redfish.ResetHost(recoverCtx, *endpoint); err != nil {
			return false, uc.explain(server, err)
		}
		if err := sleepContext(recoverCtx, uc.resetCheck); err != nil {
			return true, err
		}
	}
	state, err := uc.redfish.ReadBootMedia(recoverCtx, *endpoint, isoURL)
	if err != nil {
		return notBooted, uc.explain(server, err)
	}
	if state.MediaInserted && !notBooted {
		return false, nil
	}
	if !state.MediaInserted {
		if _, err := uc.redfish.MountBootMedia(recoverCtx, *endpoint, isoURL); err != nil {
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
				slog.Warn("redfish capability probe", "serverId", server.ID, "server", server.DisplayName(), "error", err)
			}
		}(server)
	}
	wg.Wait()
	return len(stale), nil
}

// apply resolves the endpoint and applies the installation ISO.
func (uc *BootMediaUseCase) apply(ctx context.Context, server *serverdomain.Server) (string, error) {
	endpoint, err := uc.endpoint(ctx, server)
	if err != nil {
		return "", err
	}
	mode, _, err := uc.redfish.ApplyBootMedia(ctx, *endpoint, uc.image.URL())
	return mode, err
}

// endpoint resolves the Server's BMC endpoint and fills the host UUID used to pick the System.
func (uc *BootMediaUseCase) endpoint(ctx context.Context, server *serverdomain.Server) (*serverdomain.BMCEndpoint, error) {
	if server.Observed.ProviderPod != "" {
		return nil, serverdomain.ErrNoBMC
	}
	endpoint, err := uc.endpoints.BMCEndpoint(ctx, server)
	if err != nil {
		return nil, err
	}
	if endpoint.HostUUID == "" {
		endpoint.HostUUID = server.Hardware.SystemUUID
	}
	return endpoint, nil
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
// Server; errors that already are one, and unexpected errors, pass through as they are.
func (uc *BootMediaUseCase) explain(server *serverdomain.Server, err error) error {
	var already *serverdomain.BootMediaError
	if errors.As(err, &already) {
		return err
	}
	for _, sentinel := range []error{
		serverdomain.ErrBootMediaNotConfigured, serverdomain.ErrNoBMC, serverdomain.ErrBMCCredentialUnavailable,
		serverdomain.ErrBMCUnreachable, serverdomain.ErrRedfishUnsupported, serverdomain.ErrBootMediaRejected,
	} {
		if errors.Is(err, sentinel) {
			detail := ""
			var d detailed
			if errors.As(err, &d) {
				detail = d.Detail()
			}
			return &serverdomain.BootMediaError{Err: sentinel, Server: server.DisplayName(), Detail: detail}
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &serverdomain.BootMediaError{Err: serverdomain.ErrBMCUnreachable, Server: server.DisplayName(), Detail: "The BMC did not answer in time."}
	}
	return err
}
