package application

import (
	"context"
	"errors"
	"fmt"
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
	// phases are the apply phases recorded, in order; ended counts EndBootMediaApply calls.
	phases []serverdomain.BootMediaPhase
	ended  int
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

func (r *bootMediaRepo) BeginBootMediaApply(_ context.Context, id string, apply *serverdomain.BootMediaApply, staleBefore time.Time) error {
	server, ok := r.servers[id]
	if !ok {
		return serverdomain.ErrServerNotFound
	}
	if current := server.BootMediaApply; current != nil && !current.StartedAt.Before(staleBefore) {
		return serverdomain.ErrBootMediaApplying
	}
	copied := *apply
	server.BootMediaApply = &copied
	r.phases = append(r.phases, apply.Phase)
	return nil
}

func (r *bootMediaRepo) UpdateBootMediaApply(_ context.Context, id string, apply *serverdomain.BootMediaApply) error {
	if server, ok := r.servers[id]; ok && server.BootMediaApply != nil && server.BootMediaApply.StartedAt.Equal(apply.StartedAt) {
		copied := *apply
		server.BootMediaApply = &copied
		r.phases = append(r.phases, apply.Phase)
	}
	return nil
}

func (r *bootMediaRepo) EndBootMediaApply(_ context.Context, id string, startedAt time.Time) error {
	if server, ok := r.servers[id]; ok && server.BootMediaApply != nil && server.BootMediaApply.StartedAt.Equal(startedAt) {
		server.BootMediaApply = nil
	}
	r.ended++
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
	// onApply runs inside ApplyBootMedia, after the mount phases are reported.
	onApply func()
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

// ApplyBootMedia reports the phases the real controller does around a mount, so tests see the
// use case record them.
func (f *fakeRedfish) ApplyBootMedia(ctx context.Context, _ serverdomain.BMCEndpoint, isoURL string) (string, serverdomain.BootMediaState, error) {
	f.calls = append(f.calls, "apply")
	f.isoURLs = append(f.isoURLs, isoURL)
	serverdomain.ReportBootMediaPhase(ctx, serverdomain.BootMediaPhaseMounting, nil)
	settled := time.Date(2026, 10, 3, 9, 3, 0, 0, time.UTC)
	serverdomain.ReportBootMediaPhase(ctx, serverdomain.BootMediaPhaseSettling, &settled)
	if f.onApply != nil {
		f.onApply()
	}
	if f.applyErr != nil {
		return "", serverdomain.BootMediaState{}, f.applyErr
	}
	serverdomain.ReportBootMediaPhase(ctx, serverdomain.BootMediaPhaseDirecting, nil)
	serverdomain.ReportBootMediaPhase(ctx, serverdomain.BootMediaPhaseVerifying, nil)
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

// fakeISOs is the Boot ISO catalog: iso-a and iso-b chain to integration-1 (the test Servers'
// provisioner), iso-other to another one; notServed marks ISOs whose file is missing.
type fakeISOs struct{ notServed map[string]bool }

func (f fakeISOs) Resolve(_ context.Context, id string) (*serverdomain.BootISOImage, error) {
	integration := map[string]string{"iso-a": "integration-1", "iso-b": "integration-1", "iso-other": "integration-2"}[id]
	if integration == "" {
		return nil, serverdomain.ErrBootISOUnknown
	}
	image := &serverdomain.BootISOImage{ID: id, Name: id + " name", IntegrationID: integration, URL: f.URL(id)}
	if f.notServed[id] {
		return image, fmt.Errorf("%w: the file is missing", serverdomain.ErrBootMediaNotConfigured)
	}
	return image, nil
}

func (fakeISOs) URL(id string) string {
	if id == "" {
		return "http://192.0.2.1/boot-media/ipxe/swallow-ipxe.iso"
	}
	return "http://192.0.2.1/boot-media/ipxe/" + id + "/swallow-ipxe.iso"
}

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
	uc := NewBootMediaUseCase(repo, repo, bootMediaGuard{}, endpoints, redfish, fakeISOs{})
	uc.now = func() time.Time { return time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC) }
	uc.resetCheck = 0
	return bootMediaFixture{uc: uc, repo: repo, redfish: redfish, endpoints: endpoints}
}

func physicalServer(id string) *serverdomain.Server {
	return &serverdomain.Server{
		ID: id, Observed: serverdomain.Observed{Hostname: "tainan-ci"},
		Source:       serverdomain.Source{IntegrationID: "integration-1"},
		Provisioning: &serverdomain.ProvisioningStatus{State: "ready"},
	}
}

// Enabling is a preflight: probe, apply, and only then save the setting as enabled.
func TestBootMediaEnableAppliesThenSaves(t *testing.T) {
	f := newBootMediaFixture(physicalServer("srv-1"))
	change, err := f.uc.SetEnabled(context.Background(), "srv-1", true, "iso-a")
	if err != nil {
		t.Fatalf("SetEnabled(true) error = %v", err)
	}
	if want := []string{"probe", "apply"}; !equalStrings(f.redfish.calls, want) {
		t.Errorf("redfish calls = %v, want %v", f.redfish.calls, want)
	}
	if want := (fakeISOs{}).URL("iso-a"); f.redfish.isoURLs[0] != want {
		t.Errorf("applied ISO = %q, want the chosen Boot ISO's URL %q", f.redfish.isoURLs[0], want)
	}
	saved := f.repo.servers["srv-1"].BootMedia
	if saved == nil || !saved.Enabled || saved.ISOID != "iso-a" || saved.BootOverride != "Continuous" || saved.LastAppliedBy != serverdomain.BootMediaAppliedByPreflight || saved.LastAppliedAt == nil {
		t.Errorf("saved setting = %+v, want enabled with iso-a, Continuous, applied by preflight", saved)
	}
	if f.repo.servers["srv-1"].Redfish == nil {
		t.Error("the preflight probe was not stored")
	}
	if image := change.View.Image; image == nil || !image.Available || image.ID != "iso-a" || image.Name != "iso-a name" {
		t.Errorf("view image = %+v, want the available iso-a", image)
	}
}

// A failed preflight records why on the setting but never enables it.
func TestBootMediaEnableFailureRecordsReasonWithoutEnabling(t *testing.T) {
	f := newBootMediaFixture(physicalServer("srv-1"))
	f.redfish.applyErr = &detailError{sentinel: serverdomain.ErrBootMediaRejected, detail: "The BMC could not mount the ISO."}
	_, err := f.uc.SetEnabled(context.Background(), "srv-1", true, "iso-a")
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
			_, err := f.uc.SetEnabled(context.Background(), "srv-1", true, "iso-a")
			if !errors.Is(err, tc.want) {
				t.Fatalf("SetEnabled(true) error = %v, want %v", err, tc.want)
			}
			if equalStrings(f.redfish.calls, []string{"probe", "apply"}) {
				t.Error("applied Boot Media on a BMC the probe rejected")
			}
		})
	}
}

// Without a usable Boot ISO of the Server's own provisioner, or on a locked Server, nothing
// touches the BMC.
func TestBootMediaEnableGates(t *testing.T) {
	isoCases := []struct {
		name  string
		isoID string
		isos  fakeISOs
		want  error
	}{
		{name: "no Boot ISO chosen", isoID: "", want: serverdomain.ErrBootISORequired},
		{name: "unknown Boot ISO", isoID: "iso-gone", want: serverdomain.ErrBootISOUnknown},
		{name: "another provisioner's Boot ISO", isoID: "iso-other", want: serverdomain.ErrBootISOWrongIntegration},
		{name: "Boot ISO not served", isoID: "iso-a", isos: fakeISOs{notServed: map[string]bool{"iso-a": true}}, want: serverdomain.ErrBootMediaNotConfigured},
	}
	for _, tc := range isoCases {
		t.Run(tc.name, func(t *testing.T) {
			f := newBootMediaFixture(physicalServer("srv-1"))
			f.uc.isos = tc.isos
			if _, err := f.uc.SetEnabled(context.Background(), "srv-1", true, tc.isoID); !errors.Is(err, tc.want) {
				t.Fatalf("SetEnabled(true, %q) error = %v, want %v", tc.isoID, err, tc.want)
			}
			if len(f.redfish.calls) != 0 || f.endpoints.calls != 0 {
				t.Errorf("BMC was contacted: redfish %v, endpoint reads %d", f.redfish.calls, f.endpoints.calls)
			}
		})
	}
	t.Run("locked server", func(t *testing.T) {
		f := newBootMediaFixture(physicalServer("srv-1"))
		f.uc.guard = bootMediaGuard{err: serverdomain.ErrServerLocked}
		if _, err := f.uc.SetEnabled(context.Background(), "srv-1", true, "iso-a"); !errors.Is(err, serverdomain.ErrServerLocked) {
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
		if _, err := f.uc.SetEnabled(context.Background(), "vm-1", true, "iso-a"); !errors.Is(err, serverdomain.ErrNoBMC) {
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
	change, err := f.uc.SetEnabled(context.Background(), "srv-1", false, "")
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

// The preflight is recorded while it runs — probing, ejecting on a switch, then the controller's
// phases — and the record is removed when it ends, whether it succeeded or failed.
func TestBootMediaEnableRecordsProgress(t *testing.T) {
	applied := time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC)
	server := physicalServer("srv-1")
	server.BootMedia = &serverdomain.BootMediaSetting{Enabled: true, ISOID: "iso-a", LastAppliedAt: &applied}
	f := newBootMediaFixture(server)
	var seenDuringApply *serverdomain.BootMediaApply
	f.redfish.onApply = func() {
		if apply := f.repo.servers["srv-1"].BootMediaApply; apply != nil {
			copied := *apply
			seenDuringApply = &copied
		}
	}
	if _, err := f.uc.SetEnabled(context.Background(), "srv-1", true, "iso-b"); err != nil {
		t.Fatalf("SetEnabled error = %v", err)
	}
	want := []serverdomain.BootMediaPhase{
		serverdomain.BootMediaPhaseProbing, serverdomain.BootMediaPhaseEjecting, serverdomain.BootMediaPhaseMounting,
		serverdomain.BootMediaPhaseSettling, serverdomain.BootMediaPhaseDirecting, serverdomain.BootMediaPhaseVerifying,
	}
	if !equalPhases(f.repo.phases, want) {
		t.Errorf("recorded phases = %v, want %v", f.repo.phases, want)
	}
	if seenDuringApply == nil || seenDuringApply.ISOID != "iso-b" || !seenDuringApply.StartedAt.Equal(f.uc.now()) {
		t.Errorf("apply during the mount = %+v, want iso-b started now", seenDuringApply)
	}
	if f.repo.servers["srv-1"].BootMediaApply != nil || f.repo.ended != 1 {
		t.Errorf("apply after the preflight = %+v (ended %d), want removed once", f.repo.servers["srv-1"].BootMediaApply, f.repo.ended)
	}

	f.redfish.applyErr = &serverdomain.BootMediaError{Err: serverdomain.ErrBootMediaRejected, Server: "tainan-ci"}
	if _, err := f.uc.SetEnabled(context.Background(), "srv-1", true, "iso-b"); err == nil {
		t.Fatal("SetEnabled succeeded with a refusing BMC")
	}
	if f.repo.servers["srv-1"].BootMediaApply != nil || f.repo.ended != 2 {
		t.Error("a failed preflight left its apply recorded")
	}
}

// A settle phase carries its end, so a client can count it down.
func TestBootMediaViewReportsRunningApply(t *testing.T) {
	ends := time.Date(2026, 10, 3, 9, 2, 0, 0, time.UTC)
	server := physicalServer("srv-1")
	server.BootMediaApply = &serverdomain.BootMediaApply{
		ISOID: "iso-a", Phase: serverdomain.BootMediaPhaseSettling,
		StartedAt: time.Date(2026, 10, 3, 8, 58, 0, 0, time.UTC), PhaseStartedAt: time.Date(2026, 10, 3, 8, 59, 0, 0, time.UTC), PhaseEndsAt: &ends,
	}
	f := newBootMediaFixture(server)
	view, err := f.uc.Get(context.Background(), "srv-1", false)
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	item := ToBootMediaItem(view)
	if item.Apply == nil || item.Apply.Phase != "settling" || item.Apply.PhaseEndsAt == nil || *item.Apply.PhaseEndsAt != "2026-10-03T09:02:00Z" {
		t.Errorf("apply = %+v, want the settle phase with its end", item.Apply)
	}

	// Older than any preflight can run: its API process stopped, so it is not reported.
	server.BootMediaApply.StartedAt = f.uc.now().Add(-serverdomain.BootMediaApplyStaleAfter)
	if view, _ := f.uc.Get(context.Background(), "srv-1", false); view.Apply != nil {
		t.Errorf("stale apply reported: %+v", view.Apply)
	}
}

// While a preflight runs, a second enable and a disable are refused without touching the BMC; an
// abandoned record does not block a new preflight.
func TestBootMediaRefusesWritesWhileApplying(t *testing.T) {
	server := physicalServer("srv-1")
	server.BootMediaApply = &serverdomain.BootMediaApply{ISOID: "iso-a", Phase: serverdomain.BootMediaPhaseSettling, StartedAt: time.Date(2026, 10, 3, 8, 58, 0, 0, time.UTC)}
	f := newBootMediaFixture(server)
	if _, err := f.uc.SetEnabled(context.Background(), "srv-1", true, "iso-a"); !errors.Is(err, serverdomain.ErrBootMediaApplying) {
		t.Errorf("second enable error = %v, want ErrBootMediaApplying", err)
	}
	if _, err := f.uc.SetEnabled(context.Background(), "srv-1", false, ""); !errors.Is(err, serverdomain.ErrBootMediaApplying) {
		t.Errorf("disable error = %v, want ErrBootMediaApplying", err)
	}
	if len(f.redfish.calls) != 0 || len(f.repo.settings) != 0 {
		t.Errorf("BMC calls %v, settings %v; want none while applying", f.redfish.calls, f.repo.settings)
	}

	f.repo.servers["srv-1"].BootMediaApply.StartedAt = f.uc.now().Add(-serverdomain.BootMediaApplyStaleAfter - time.Second)
	if _, err := f.uc.SetEnabled(context.Background(), "srv-1", true, "iso-a"); err != nil {
		t.Errorf("enable over an abandoned apply error = %v", err)
	}
}

func equalPhases(a, b []serverdomain.BootMediaPhase) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Enabling another Boot ISO on an enabled Server ejects the previous one before mounting the new
// one, so a BMC whose virtual CDs are taken still accepts it, and saves the new choice.
func TestBootMediaSwitchesBootISO(t *testing.T) {
	applied := time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC)
	server := physicalServer("srv-1")
	server.BootMedia = &serverdomain.BootMediaSetting{Enabled: true, ISOID: "iso-a", LastAppliedAt: &applied}
	f := newBootMediaFixture(server)
	if _, err := f.uc.SetEnabled(context.Background(), "srv-1", true, "iso-b"); err != nil {
		t.Fatalf("SetEnabled(true, iso-b) error = %v", err)
	}
	isos := fakeISOs{}
	if want := []string{"probe", "clear", "apply"}; !equalStrings(f.redfish.calls, want) {
		t.Errorf("redfish calls = %v, want %v", f.redfish.calls, want)
	}
	if want := []string{isos.URL("iso-a"), isos.URL("iso-b")}; !equalStrings(f.redfish.isoURLs, want) {
		t.Errorf("ISO URLs = %v, want eject %v", f.redfish.isoURLs, want)
	}
	if saved := f.repo.servers["srv-1"].BootMedia; saved == nil || !saved.Enabled || saved.ISOID != "iso-b" {
		t.Errorf("saved setting = %+v, want enabled with iso-b", saved)
	}
}

// A setting enabled before Boot ISOs names none: it reads as enabled without an image, and
// disabling it ejects the retired installation ISO it had mounted.
func TestBootMediaLegacySettingWithoutBootISO(t *testing.T) {
	applied := time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC)
	server := physicalServer("srv-1")
	server.BootMedia = &serverdomain.BootMediaSetting{Enabled: true, LastAppliedAt: &applied}
	f := newBootMediaFixture(server)
	view, err := f.uc.Get(context.Background(), "srv-1", true)
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if view.Image != nil || view.Live != nil {
		t.Errorf("view = image %+v, live %+v; want neither for a setting without a Boot ISO", view.Image, view.Live)
	}
	if _, err := f.uc.SetEnabled(context.Background(), "srv-1", false, ""); err != nil {
		t.Fatalf("SetEnabled(false) error = %v", err)
	}
	if want := []string{(fakeISOs{}).URL("")}; !equalStrings(f.redfish.isoURLs, want) {
		t.Errorf("ejected %v, want the retired installation ISO %v", f.redfish.isoURLs, want)
	}
}

// A setting whose Boot ISO was deleted is still reported, unavailable, so the operator sees it.
func TestBootMediaViewReportsMissingBootISO(t *testing.T) {
	server := physicalServer("srv-1")
	server.BootMedia = &serverdomain.BootMediaSetting{Enabled: true, ISOID: "iso-gone"}
	f := newBootMediaFixture(server)
	view, err := f.uc.Get(context.Background(), "srv-1", false)
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if view.Image == nil || view.Image.ID != "iso-gone" || view.Image.Available || view.Image.Reason == "" {
		t.Errorf("view image = %+v, want iso-gone unavailable with a reason", view.Image)
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
