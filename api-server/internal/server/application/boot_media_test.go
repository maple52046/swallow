package application

import (
	"context"
	"errors"
	"testing"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// bootMediaRepo is an in-memory ServerRepository and BootMediaStore; only the methods Boot Media
// uses are implemented, so the embedded interface makes any other call panic.
type bootMediaRepo struct {
	serverdomain.ServerRepository
	servers      map[string]*serverdomain.Server
	settings     []serverdomain.BootMediaSetting
	capabilities []serverdomain.RedfishCapability
}

func (r *bootMediaRepo) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	server, ok := r.servers[id]
	if !ok {
		return nil, serverdomain.ErrServerNotFound
	}
	copied := *server
	return &copied, nil
}

func (r *bootMediaRepo) List(context.Context, serverdomain.ListFilter) (serverdomain.ListResult, error) {
	var result serverdomain.ListResult
	for _, server := range r.servers {
		copied := *server
		result.Servers = append(result.Servers, &copied)
	}
	result.Total = len(result.Servers)
	return result, nil
}

func (r *bootMediaRepo) SetBootMedia(_ context.Context, id string, setting *serverdomain.BootMediaSetting) error {
	server, ok := r.servers[id]
	if !ok {
		return serverdomain.ErrServerNotFound
	}
	copied := *setting
	server.BootMedia = &copied
	r.settings = append(r.settings, copied)
	return nil
}

func (r *bootMediaRepo) SetRedfishCapability(_ context.Context, id string, capability *serverdomain.RedfishCapability) error {
	server, ok := r.servers[id]
	if !ok {
		return serverdomain.ErrServerNotFound
	}
	copied := *capability
	server.Redfish = &copied
	r.capabilities = append(r.capabilities, copied)
	return nil
}

type bootMediaGuard struct{ err error }

func (g bootMediaGuard) RequireUnlocked(context.Context, []string) error { return g.err }

// fakeEndpoints hands out a fixed endpoint, or err.
type fakeEndpoints struct {
	err   error
	calls int
}

func (e *fakeEndpoints) BMCEndpoint(context.Context, *serverdomain.Server) (*serverdomain.BMCEndpoint, error) {
	e.calls++
	if e.err != nil {
		return nil, e.err
	}
	return &serverdomain.BMCEndpoint{Address: "192.0.2.10", Username: "maas", Password: "secret"}, nil
}

// fakeRedfish records calls and returns the configured outcomes.
type fakeRedfish struct {
	capability serverdomain.RedfishCapability
	state      serverdomain.BootMediaState
	// afterReset, when set, replaces state at ResetHost (a BMC that drops the mount at a reset).
	afterReset *serverdomain.BootMediaState
	applyErr   error
	mountErr   error
	clearErr   error
	mode       string
	calls      []string
	isoURLs    []string
}

func (f *fakeRedfish) Probe(context.Context, serverdomain.BMCEndpoint) (serverdomain.RedfishCapability, error) {
	f.calls = append(f.calls, "probe")
	return f.capability, nil
}

func (f *fakeRedfish) ReadBootMedia(_ context.Context, _ serverdomain.BMCEndpoint, isoURL string) (serverdomain.BootMediaState, error) {
	f.calls = append(f.calls, "read")
	f.isoURLs = append(f.isoURLs, isoURL)
	return f.state, nil
}

func (f *fakeRedfish) ApplyBootMedia(_ context.Context, _ serverdomain.BMCEndpoint, isoURL string) (string, serverdomain.BootMediaState, error) {
	f.calls = append(f.calls, "apply")
	f.isoURLs = append(f.isoURLs, isoURL)
	if f.applyErr != nil {
		return "", serverdomain.BootMediaState{}, f.applyErr
	}
	return f.mode, serverdomain.BootMediaState{MediaInserted: true, OverrideReady: true}, nil
}

func (f *fakeRedfish) ClearBootMedia(_ context.Context, _ serverdomain.BMCEndpoint, isoURL string) error {
	f.calls = append(f.calls, "clear")
	f.isoURLs = append(f.isoURLs, isoURL)
	return f.clearErr
}

func (f *fakeRedfish) MountBootMedia(_ context.Context, _ serverdomain.BMCEndpoint, isoURL string) (serverdomain.BootMediaState, error) {
	f.calls = append(f.calls, "mount")
	f.isoURLs = append(f.isoURLs, isoURL)
	if f.mountErr != nil {
		return serverdomain.BootMediaState{}, f.mountErr
	}
	f.state.MediaInserted = true
	return f.state, nil
}

func (f *fakeRedfish) ResetHost(context.Context, serverdomain.BMCEndpoint) error {
	f.calls = append(f.calls, "reset")
	if f.afterReset != nil {
		f.state = *f.afterReset
	}
	return nil
}

// fakeImage is the installation ISO; err makes it unavailable.
type fakeImage struct{ err error }

func (fakeImage) URL() string        { return "http://192.0.2.1/boot-media/ipxe/swallow-ipxe.iso" }
func (i fakeImage) Available() error { return i.err }

type bootMediaFixture struct {
	uc        *BootMediaUseCase
	repo      *bootMediaRepo
	redfish   *fakeRedfish
	endpoints *fakeEndpoints
}

func newBootMediaFixture(servers ...*serverdomain.Server) bootMediaFixture {
	repo := &bootMediaRepo{servers: map[string]*serverdomain.Server{}}
	for _, server := range servers {
		repo.servers[server.ID] = server
	}
	redfish := &fakeRedfish{capability: serverdomain.RedfishCapability{Support: serverdomain.RedfishSupported}, mode: "Continuous"}
	endpoints := &fakeEndpoints{}
	uc := NewBootMediaUseCase(repo, repo, bootMediaGuard{}, endpoints, redfish, fakeImage{})
	uc.now = func() time.Time { return time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC) }
	uc.resetCheck = 0
	return bootMediaFixture{uc: uc, repo: repo, redfish: redfish, endpoints: endpoints}
}

func physicalServer(id string) *serverdomain.Server {
	return &serverdomain.Server{
		ID: id, Observed: serverdomain.Observed{Hostname: "tainan-ci"},
		Provisioning: &serverdomain.ProvisioningStatus{State: "ready"},
	}
}

// Enabling is a preflight: probe, apply, and only then save the setting as enabled.
func TestBootMediaEnableAppliesThenSaves(t *testing.T) {
	f := newBootMediaFixture(physicalServer("srv-1"))
	change, err := f.uc.SetEnabled(context.Background(), "srv-1", true)
	if err != nil {
		t.Fatalf("SetEnabled(true) error = %v", err)
	}
	if want := []string{"probe", "apply"}; !equalStrings(f.redfish.calls, want) {
		t.Errorf("redfish calls = %v, want %v", f.redfish.calls, want)
	}
	if want := (fakeImage{}).URL(); f.redfish.isoURLs[0] != want {
		t.Errorf("applied ISO = %q, want the installation URL", f.redfish.isoURLs[0])
	}
	saved := f.repo.servers["srv-1"].BootMedia
	if saved == nil || !saved.Enabled || saved.BootOverride != "Continuous" || saved.LastAppliedBy != serverdomain.BootMediaAppliedByPreflight || saved.LastAppliedAt == nil {
		t.Errorf("saved setting = %+v, want enabled, Continuous, applied by preflight", saved)
	}
	if f.repo.servers["srv-1"].Redfish == nil {
		t.Error("the preflight probe was not stored")
	}
	if !change.View.ImageAvailable {
		t.Error("view does not report the installation ISO as available")
	}
}

// A failed preflight records why on the setting but never enables it.
func TestBootMediaEnableFailureRecordsReasonWithoutEnabling(t *testing.T) {
	f := newBootMediaFixture(physicalServer("srv-1"))
	f.redfish.applyErr = &detailError{sentinel: serverdomain.ErrBootMediaRejected, detail: "The BMC could not mount the ISO."}
	_, err := f.uc.SetEnabled(context.Background(), "srv-1", true)
	var bootErr *serverdomain.BootMediaError
	if !errors.As(err, &bootErr) || !errors.Is(err, serverdomain.ErrBootMediaRejected) {
		t.Fatalf("SetEnabled(true) error = %v, want a BootMediaError wrapping ErrBootMediaRejected", err)
	}
	if bootErr.Detail != "The BMC could not mount the ISO." {
		t.Errorf("error detail = %q, want the BMC's explanation", bootErr.Detail)
	}
	saved := f.repo.servers["srv-1"].BootMedia
	if saved == nil || saved.Enabled || saved.LastError == "" || saved.LastErrorAt == nil {
		t.Errorf("saved setting = %+v, want disabled with the failure recorded", saved)
	}
}

// An unsupported or unreachable BMC fails before anything is applied.
func TestBootMediaEnableRefusesUnsupportedBMC(t *testing.T) {
	cases := []struct {
		name    string
		support serverdomain.RedfishSupport
		want    error
	}{
		{"no virtual media", serverdomain.RedfishUnsupported, serverdomain.ErrRedfishUnsupported},
		{"no redfish service", serverdomain.RedfishUnreachable, serverdomain.ErrBMCUnreachable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newBootMediaFixture(physicalServer("srv-1"))
			f.redfish.capability = serverdomain.RedfishCapability{Support: tc.support, Reason: "why"}
			_, err := f.uc.SetEnabled(context.Background(), "srv-1", true)
			if !errors.Is(err, tc.want) {
				t.Fatalf("SetEnabled(true) error = %v, want %v", err, tc.want)
			}
			if equalStrings(f.redfish.calls, []string{"probe", "apply"}) {
				t.Error("applied Boot Media on a BMC the probe rejected")
			}
		})
	}
}

// Without the installation ISO, or on a locked Server, nothing touches the BMC.
func TestBootMediaEnableGates(t *testing.T) {
	t.Run("no installation ISO", func(t *testing.T) {
		f := newBootMediaFixture(physicalServer("srv-1"))
		f.uc.image = fakeImage{err: serverdomain.ErrBootMediaNotConfigured}
		if _, err := f.uc.SetEnabled(context.Background(), "srv-1", true); !errors.Is(err, serverdomain.ErrBootMediaNotConfigured) {
			t.Fatalf("SetEnabled(true) error = %v, want ErrBootMediaNotConfigured", err)
		}
		if len(f.redfish.calls) != 0 || f.endpoints.calls != 0 {
			t.Errorf("BMC was contacted: redfish %v, endpoint reads %d", f.redfish.calls, f.endpoints.calls)
		}
	})
	t.Run("locked server", func(t *testing.T) {
		f := newBootMediaFixture(physicalServer("srv-1"))
		f.uc.guard = bootMediaGuard{err: serverdomain.ErrServerLocked}
		if _, err := f.uc.SetEnabled(context.Background(), "srv-1", true); !errors.Is(err, serverdomain.ErrServerLocked) {
			t.Fatalf("SetEnabled(true) error = %v, want ErrServerLocked", err)
		}
		if len(f.redfish.calls) != 0 {
			t.Errorf("BMC was contacted on a locked Server: %v", f.redfish.calls)
		}
	})
	t.Run("virtual machine", func(t *testing.T) {
		vm := physicalServer("vm-1")
		vm.Observed.ProviderPod = "lab-host"
		f := newBootMediaFixture(vm)
		if _, err := f.uc.SetEnabled(context.Background(), "vm-1", true); !errors.Is(err, serverdomain.ErrNoBMC) {
			t.Fatalf("SetEnabled(true) error = %v, want ErrNoBMC", err)
		}
		if f.endpoints.calls != 0 {
			t.Error("asked the provisioner for the BMC of a virtual machine")
		}
	})
}

// Disabling always saves the operator's intent; resetting the BMC is best effort.
func TestBootMediaDisableSavesEvenWhenBMCFails(t *testing.T) {
	server := physicalServer("srv-1")
	server.BootMedia = &serverdomain.BootMediaSetting{Enabled: true, BootOverride: "Once"}
	f := newBootMediaFixture(server)
	f.redfish.clearErr = &detailError{sentinel: serverdomain.ErrBMCUnreachable, detail: "BMC busy"}
	change, err := f.uc.SetEnabled(context.Background(), "srv-1", false)
	if err != nil {
		t.Fatalf("SetEnabled(false) error = %v", err)
	}
	if saved := f.repo.servers["srv-1"].BootMedia; saved == nil || saved.Enabled || saved.BootOverride != "Once" {
		t.Errorf("saved setting = %+v, want disabled with history kept", saved)
	}
	if change.Reverted || change.RevertError == "" {
		t.Errorf("change = %+v, want not reverted with the reason", change)
	}
}

// The ensure step re-applies even when the BMC already looks ready, records it, and skips a
// Server whose Boot Media was disabled after the Workflow was created.
func TestBootMediaEnsure(t *testing.T) {
	enabled := physicalServer("srv-1")
	enabled.BootMedia = &serverdomain.BootMediaSetting{Enabled: true}
	disabled := physicalServer("srv-2")
	disabled.BootMedia = &serverdomain.BootMediaSetting{Enabled: false}
	f := newBootMediaFixture(enabled, disabled)
	f.redfish.state = serverdomain.BootMediaState{MediaInserted: true, OverrideReady: true}

	outcome, err := f.uc.Ensure(context.Background(), "srv-1", "http://frozen/boot-media/ipxe/swallow-ipxe.iso")
	if err != nil {
		t.Fatalf("Ensure(enabled) error = %v", err)
	}
	if !outcome.WasReady || outcome.Skipped || outcome.Mode != "Continuous" {
		t.Errorf("outcome = %+v, want was-ready, applied Continuous", outcome)
	}
	if want := []string{"read", "apply"}; !equalStrings(f.redfish.calls, want) {
		t.Errorf("redfish calls = %v, want %v (always re-apply)", f.redfish.calls, want)
	}
	if f.redfish.isoURLs[1] != "http://frozen/boot-media/ipxe/swallow-ipxe.iso" {
		t.Errorf("applied ISO = %q, want the URL frozen in the Task", f.redfish.isoURLs[1])
	}
	if saved := f.repo.servers["srv-1"].BootMedia; saved.LastAppliedBy != serverdomain.BootMediaAppliedByEnsure || saved.LastAppliedAt == nil {
		t.Errorf("saved setting = %+v, want applied by ensure", saved)
	}

	f.redfish.calls = nil
	outcome, err = f.uc.Ensure(context.Background(), "srv-2", "http://frozen/x.iso")
	if err != nil || !outcome.Skipped || len(f.redfish.calls) != 0 {
		t.Errorf("Ensure(disabled) = %+v, %v, calls %v; want skipped without BMC calls", outcome, err, f.redfish.calls)
	}
}

// A failed ensure keeps the setting enabled and records the failure.
func TestBootMediaEnsureFailureIsRecorded(t *testing.T) {
	server := physicalServer("srv-1")
	server.BootMedia = &serverdomain.BootMediaSetting{Enabled: true}
	f := newBootMediaFixture(server)
	f.redfish.applyErr = &detailError{sentinel: serverdomain.ErrBMCUnreachable, detail: "timeout"}
	if _, err := f.uc.Ensure(context.Background(), "srv-1", "http://frozen/x.iso"); !errors.Is(err, serverdomain.ErrBMCUnreachable) {
		t.Fatalf("Ensure error = %v, want ErrBMCUnreachable", err)
	}
	if saved := f.repo.servers["srv-1"].BootMedia; !saved.Enabled || saved.LastError == "" {
		t.Errorf("saved setting = %+v, want still enabled with the failure recorded", saved)
	}
}

// Shortly after the power-on (host in POST) a dropped ISO is mounted again without a restart, and
// a mounted one is left alone. A Server without Boot Media gets no BMC call.
func TestBootMediaRecoverDeploymentBootDuringPOST(t *testing.T) {
	enabled := physicalServer("srv-1")
	enabled.BootMedia = &serverdomain.BootMediaSetting{Enabled: true, BootOverride: "Continuous"}
	disabled := physicalServer("srv-2")
	f := newBootMediaFixture(enabled, disabled)
	const iso = "http://frozen/boot-media/ipxe/swallow-ipxe.iso"

	f.redfish.state = serverdomain.BootMediaState{MediaInserted: true, OverrideReady: true}
	if changed, err := f.uc.RecoverDeploymentBoot(context.Background(), "srv-1", iso, false); err != nil || changed {
		t.Fatalf("RecoverDeploymentBoot(mounted) = %v, %v; want false, nil", changed, err)
	}
	if want := []string{"read"}; !equalStrings(f.redfish.calls, want) {
		t.Errorf("redfish calls = %v, want %v (no mutation while mounted)", f.redfish.calls, want)
	}

	f.redfish.calls = nil
	f.redfish.state = serverdomain.BootMediaState{OverrideReady: true}
	if changed, err := f.uc.RecoverDeploymentBoot(context.Background(), "srv-1", iso, false); err != nil || !changed {
		t.Fatalf("RecoverDeploymentBoot(dropped) = %v, %v; want true, nil", changed, err)
	}
	if want := []string{"read", "mount"}; !equalStrings(f.redfish.calls, want) {
		t.Errorf("redfish calls = %v, want %v (mount at once, no restart)", f.redfish.calls, want)
	}
	if saved := f.repo.servers["srv-1"].BootMedia; saved.LastAppliedBy != serverdomain.BootMediaAppliedByEnsure || saved.LastAppliedAt == nil || saved.BootOverride != "Continuous" {
		t.Errorf("saved setting = %+v, want applied by ensure with the boot override kept", saved)
	}

	f.redfish.calls = nil
	if changed, err := f.uc.RecoverDeploymentBoot(context.Background(), "srv-2", iso, true); err != nil || changed || len(f.redfish.calls) != 0 {
		t.Errorf("RecoverDeploymentBoot(no Boot Media) = %v, %v, calls %v; want untouched", changed, err, f.redfish.calls)
	}
}

// A host that never network-booted is re-applied and restarted even with the ISO mounted; a mount
// the restart dropped is mounted again while the host POSTs.
func TestBootMediaRecoverDeploymentBootNotBooted(t *testing.T) {
	server := physicalServer("srv-1")
	server.BootMedia = &serverdomain.BootMediaSetting{Enabled: true}
	f := newBootMediaFixture(server)
	const iso = "http://frozen/boot-media/ipxe/swallow-ipxe.iso"

	f.redfish.state = serverdomain.BootMediaState{MediaInserted: true, OverrideReady: true}
	if changed, err := f.uc.RecoverDeploymentBoot(context.Background(), "srv-1", iso, true); err != nil || !changed {
		t.Fatalf("RecoverDeploymentBoot(not booted) = %v, %v; want true, nil", changed, err)
	}
	if want := []string{"apply", "reset", "read"}; !equalStrings(f.redfish.calls, want) {
		t.Errorf("redfish calls = %v, want %v", f.redfish.calls, want)
	}

	f.redfish.calls = nil
	f.redfish.afterReset = &serverdomain.BootMediaState{OverrideReady: true}
	if changed, err := f.uc.RecoverDeploymentBoot(context.Background(), "srv-1", iso, true); err != nil || !changed {
		t.Fatalf("RecoverDeploymentBoot(dropped at reset) = %v, %v; want true, nil", changed, err)
	}
	if want := []string{"apply", "reset", "read", "mount"}; !equalStrings(f.redfish.calls, want) {
		t.Errorf("redfish calls = %v, want %v", f.redfish.calls, want)
	}
}

// A failed re-apply is recorded and returned, and the host is not restarted onto a missing ISO.
func TestBootMediaRecoverDeploymentBootFailure(t *testing.T) {
	server := physicalServer("srv-1")
	server.BootMedia = &serverdomain.BootMediaSetting{Enabled: true}
	f := newBootMediaFixture(server)
	f.redfish.applyErr = &detailError{sentinel: serverdomain.ErrBootMediaRejected, detail: "mount dropped"}
	if _, err := f.uc.RecoverDeploymentBoot(context.Background(), "srv-1", "http://frozen/x.iso", true); !errors.Is(err, serverdomain.ErrBootMediaRejected) {
		t.Fatalf("RecoverDeploymentBoot error = %v, want ErrBootMediaRejected", err)
	}
	if want := []string{"apply"}; !equalStrings(f.redfish.calls, want) {
		t.Errorf("redfish calls = %v, want %v (no restart)", f.redfish.calls, want)
	}
	if saved := f.repo.servers["srv-1"].BootMedia; saved.LastError == "" {
		t.Errorf("saved setting = %+v, want the failure recorded", saved)
	}
}

// The sweep probes only Servers never probed or probed longer ago than the window, and records a
// virtual machine as having no BMC.
func TestBootMediaProbeStale(t *testing.T) {
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	fresh := physicalServer("fresh")
	fresh.Redfish = &serverdomain.RedfishCapability{Support: serverdomain.RedfishSupported, ProbedAt: now.Add(-time.Hour)}
	old := physicalServer("old")
	old.Redfish = &serverdomain.RedfishCapability{Support: serverdomain.RedfishSupported, ProbedAt: now.Add(-48 * time.Hour)}
	never := physicalServer("never")
	vm := physicalServer("vm")
	vm.Observed.ProviderPod = "lab-host"
	f := newBootMediaFixture(fresh, old, never, vm)

	probed, err := f.uc.ProbeStale(context.Background(), 24*time.Hour, 2)
	if err != nil {
		t.Fatalf("ProbeStale error = %v", err)
	}
	if probed != 3 {
		t.Errorf("ProbeStale probed %d Servers, want 3 (old, never, vm)", probed)
	}
	if got := f.repo.servers["fresh"].Redfish.ProbedAt; !got.Equal(now.Add(-time.Hour)) {
		t.Error("re-probed a Server whose capability is still current")
	}
	if got := f.repo.servers["vm"].Redfish; got == nil || got.Support != serverdomain.RedfishNoBMC {
		t.Errorf("vm capability = %+v, want no_bmc", got)
	}
}

// detailError mimics the Redfish controller's errors: a sentinel plus the BMC's own words.
type detailError struct {
	sentinel error
	detail   string
}

func (e *detailError) Error() string  { return e.detail }
func (e *detailError) Unwrap() error  { return e.sentinel }
func (e *detailError) Detail() string { return e.detail }
