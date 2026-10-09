package app

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/operation/infra/temporalworkflow"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	provisioninginfra "github.com/maple52046/swallow/internal/provisioning/infra"
	serverapp "github.com/maple52046/swallow/internal/server/application"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// bootMediaServers is a FindByID-only ServerRepository.
type bootMediaServers struct {
	serverdomain.ServerRepository
	servers map[string]*serverdomain.Server
}

func (r bootMediaServers) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	if server, ok := r.servers[id]; ok {
		return server, nil
	}
	return nil, serverdomain.ErrServerNotFound
}

// plannerISOs resolves iso-a as served and iso-missing as present without a file.
type plannerISOs struct{}

func (plannerISOs) Resolve(_ context.Context, id string) (*serverdomain.BootISOImage, error) {
	image := &serverdomain.BootISOImage{ID: id, Name: id, IntegrationID: "integration-1", URL: (plannerISOs{}).URL(id)}
	switch id {
	case "iso-a":
		return image, nil
	case "iso-missing":
		return image, serverdomain.ErrBootMediaNotConfigured
	}
	return nil, serverdomain.ErrBootISOUnknown
}

func (plannerISOs) URL(id string) string {
	return "http://192.0.2.1/boot-media/ipxe/" + id + "/swallow-ipxe.iso"
}

func provisionTask(serverID string) operationdomain.Task {
	return operationdomain.Task{
		ID: "provision-" + serverID, Kind: "provision-os", Job: "ensure-os", DependsOn: []string{"prepare"},
		Executor:   operationdomain.RunnerKindProvisioner,
		Targets:    []operationdomain.ResourceReference{{Kind: "server", ID: serverID}},
		Parameters: map[string]any{"request": "snapshot"},
	}
}

// Only Servers with Boot Media enabled get an ensure Task, placed before and depended on by their
// provision Task, in the same Job, with that Server's Boot ISO URL frozen in its parameters.
func TestBootMediaPlannerAddsEnsureTasks(t *testing.T) {
	planner := bootMediaPlanner{
		servers: bootMediaServers{servers: map[string]*serverdomain.Server{
			"on":  {ID: "on", Observed: serverdomain.Observed{Hostname: "tainan-ci"}, BootMedia: &serverdomain.BootMediaSetting{Enabled: true, ISOID: "iso-a"}},
			"off": {ID: "off", BootMedia: &serverdomain.BootMediaSetting{Enabled: false, ISOID: "iso-a"}},
			"new": {ID: "new"},
		}},
		isos: plannerISOs{},
	}
	steps := []operationdomain.Task{{ID: "prepare", Kind: "noop"}, provisionTask("on"), provisionTask("off"), provisionTask("new")}
	got, err := planner.withEnsureTasks(context.Background(), steps)
	if err != nil {
		t.Fatalf("withEnsureTasks error = %v", err)
	}
	var ids []string
	for _, step := range got {
		ids = append(ids, step.ID)
	}
	if want := []string{"prepare", "ensure-boot-media-on", "provision-on", "provision-off", "provision-new"}; !equalStringSlices(ids, want) {
		t.Fatalf("steps = %v, want %v", ids, want)
	}
	ensure, provision := got[1], got[2]
	if ensure.Kind != ensureBootMediaTaskKind || ensure.Executor != operationdomain.RunnerKindInternal || ensure.Job != "ensure-os" {
		t.Errorf("ensure task = %+v, want an internal ensure-boot-media Task in the provision Job", ensure)
	}
	if ensure.Parameters["isoUrl"] != (plannerISOs{}).URL("iso-a") {
		t.Errorf("ensure isoUrl = %v, want the Server's Boot ISO URL", ensure.Parameters["isoUrl"])
	}
	if want := []string{"prepare", "ensure-boot-media-on"}; !equalStringSlices(provision.DependsOn, want) {
		t.Errorf("provision DependsOn = %v, want %v", provision.DependsOn, want)
	}
	if provision.Parameters[bootMediaISOParameter] != (plannerISOs{}).URL("iso-a") || provision.Parameters["request"] != "snapshot" {
		t.Errorf("provision parameters = %v, want the request kept and the frozen ISO URL added", provision.Parameters)
	}
	if !equalStringSlices(steps[1].DependsOn, []string{"prepare"}) || steps[1].Parameters[bootMediaISOParameter] != nil {
		t.Error("withEnsureTasks mutated the caller's provision Task")
	}
	if !equalStringSlices(got[3].DependsOn, []string{"prepare"}) || got[3].Parameters[bootMediaISOParameter] != nil {
		t.Errorf("provision-off = %+v, want unchanged", got[3])
	}
}

// fakeBootRecoverer records RecoverDeploymentBoot calls and reports a change when changed is set.
type fakeBootRecoverer struct {
	changed bool
	calls   []bool
	isoURL  string
}

func (f *fakeBootRecoverer) RecoverDeploymentBoot(_ context.Context, _, isoURL string, notBooted bool) (bool, error) {
	f.calls = append(f.calls, notBooted)
	f.isoURL = isoURL
	return f.changed, nil
}

func watchFixture(t *testing.T, recoverer *fakeBootRecoverer) *bootMediaWatch {
	t.Helper()
	executor := providerStepExecutor{bootMedia: recoverer, bootMediaMediaWait: time.Minute, bootMediaBootWait: 5 * time.Minute}
	step := temporalworkflow.StepExecutionInput{Step: provisionTask("srv")}
	step.Step.Parameters = map[string]any{bootMediaISOParameter: "http://frozen/x.iso"}
	return executor.newBootMediaWatch(step)
}

var watchStart = time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)

// progressAt is a deployment attempt that started four minutes before the first Deploying
// reading (the ensure Task's mount and settle) and whose newest provider event is eventType.
func progressAt(eventType string) deploymentProgress {
	start := watchStart.Add(-4 * time.Minute)
	return deploymentProgress{notBefore: start, since: watchStart, shown: providerEvent{eventType: eventType, at: watchStart}}
}

// Both checks are timed from the first Deploying reading, not from the attempt's start; the media
// check runs once and, even when it remounted, the boot check still runs once while the provider
// has recorded nothing since the deployment start.
func TestBootMediaWatchChecksMediaThenBoot(t *testing.T) {
	recoverer := &fakeBootRecoverer{changed: true}
	watch := watchFixture(t, recoverer)
	deploying := progressAt("Deploying")
	for _, at := range []time.Duration{0, 59 * time.Second, time.Minute, 2 * time.Minute, 4*time.Minute + 59*time.Second} {
		if watch.observe(context.Background(), "srv", deploying, watchStart.Add(at)) {
			t.Fatalf("observe at %s reported a restart", at)
		}
	}
	if len(recoverer.calls) != 1 || recoverer.calls[0] {
		t.Fatalf("calls before the boot check = %v, want the media check only", recoverer.calls)
	}
	if !watch.observe(context.Background(), "srv", deploying, watchStart.Add(5*time.Minute)) {
		t.Error("boot check did not report its restart")
	}
	if watch.observe(context.Background(), "srv", deploying, watchStart.Add(20*time.Minute)) {
		t.Error("the watch restarted the host twice")
	}
	if len(recoverer.calls) != 2 || !recoverer.calls[1] || recoverer.isoURL != "http://frozen/x.iso" {
		t.Errorf("calls = %v with %q, want one not-booted recovery with the frozen URL", recoverer.calls, recoverer.isoURL)
	}
}

// The boot check never restarts a host that reached the provisioner, nor one whose provider
// reports no events; and a Task without a frozen ISO URL is never watched.
func TestBootMediaWatchLeavesProgressingAndUnknownBootsAlone(t *testing.T) {
	for name, progress := range map[string]deploymentProgress{
		"network-booted": progressAt("Performing PXE boot"),
		"no events":      {notBefore: watchStart, since: watchStart},
	} {
		recoverer := &fakeBootRecoverer{}
		watch := watchFixture(t, recoverer)
		for _, at := range []time.Duration{0, time.Minute, 5 * time.Minute, 20 * time.Minute} {
			watch.observe(context.Background(), "srv", progress, watchStart.Add(at))
		}
		if len(recoverer.calls) != 1 || recoverer.calls[0] {
			t.Errorf("%s: calls = %v, want the media check only", name, recoverer.calls)
		}
	}

	recoverer := &fakeBootRecoverer{}
	inert := (providerStepExecutor{bootMedia: recoverer}).newBootMediaWatch(temporalworkflow.StepExecutionInput{Step: provisionTask("srv")})
	inert.observe(context.Background(), "srv", progressAt("Deploying"), watchStart.Add(time.Hour))
	if len(recoverer.calls) != 0 {
		t.Errorf("calls = %v, want none without a frozen ISO URL", recoverer.calls)
	}
}

// An enabled Server without a served Boot ISO (none chosen, deleted, file missing) still gets its
// ensure Task, with an empty URL, so the deployment stops with boot_media_not_configured instead
// of booting a host that cannot reach the provisioner.
func TestBootMediaPlannerFreezesNoURLWithoutServedBootISO(t *testing.T) {
	for _, isoID := range []string{"", "iso-gone", "iso-missing"} {
		planner := bootMediaPlanner{
			servers: bootMediaServers{servers: map[string]*serverdomain.Server{
				"on": {ID: "on", BootMedia: &serverdomain.BootMediaSetting{Enabled: true, ISOID: isoID}},
			}},
			isos: plannerISOs{},
		}
		got, err := planner.withEnsureTasks(context.Background(), []operationdomain.Task{provisionTask("on")})
		if err != nil {
			t.Fatalf("withEnsureTasks(%q) error = %v", isoID, err)
		}
		if len(got) != 2 || got[0].Kind != ensureBootMediaTaskKind || got[0].Parameters["isoUrl"] != "" {
			t.Errorf("withEnsureTasks(%q) = %+v, want an ensure Task with an empty isoUrl", isoID, got)
		}
	}
}

func TestBootMediaPlannerZeroValueIsNoOp(t *testing.T) {
	steps := []operationdomain.Task{provisionTask("a")}
	got, err := (bootMediaPlanner{}).withEnsureTasks(context.Background(), steps)
	if err != nil || len(got) != 1 {
		t.Fatalf("zero planner = %v, %v; want the steps unchanged", got, err)
	}
}

// fakeEnsurer records the Ensure call.
type fakeEnsurer struct {
	err              error
	serverID, isoURL string
}

func (f *fakeEnsurer) Ensure(_ context.Context, serverID, isoURL string) (serverapp.EnsureOutcome, error) {
	f.serverID, f.isoURL = serverID, isoURL
	return serverapp.EnsureOutcome{}, f.err
}

func ensureInput(isoURL string) temporalworkflow.StepExecutionInput {
	return temporalworkflow.StepExecutionInput{Step: operationdomain.Task{
		ID: "ensure-boot-media-srv", Kind: ensureBootMediaTaskKind,
		Targets:    []operationdomain.ResourceReference{{Kind: "server", ID: "srv"}},
		Parameters: map[string]any{"isoUrl": isoURL},
	}}
}

func TestEnsureBootMediaStep(t *testing.T) {
	ensurer := &fakeEnsurer{}
	executor := platformWorkflowStepExecutor{bootMedia: ensurer}
	result := executor.Execute(context.Background(), ensureInput("http://frozen/x.iso"))
	if result.Status != operationdomain.TaskSucceeded || ensurer.serverID != "srv" || ensurer.isoURL != "http://frozen/x.iso" {
		t.Fatalf("result = %+v, ensure(%q, %q); want success for the Task's Server and URL", result, ensurer.serverID, ensurer.isoURL)
	}

	ensurer.err = &serverdomain.BootMediaError{Err: serverdomain.ErrBMCUnreachable, Server: "tainan-ci"}
	result = executor.Execute(context.Background(), ensureInput("http://frozen/x.iso"))
	if result.Status != operationdomain.TaskFailed || result.Error == nil || !result.Error.Retryable {
		t.Errorf("result = %+v, want a retryable failure while the BMC is unreachable", result)
	}

	result = executor.Execute(context.Background(), ensureInput(""))
	if result.Status != operationdomain.TaskFailed || result.Error.Code != "boot_media_not_configured" || result.Error.Retryable {
		t.Errorf("result = %+v, want a non-retryable failure without an ISO URL", result)
	}
}

// fakeBMCProvider is a provisioner with the PowerConfiguration capability.
type fakeBMCProvider struct {
	provisioningdomain.OSProvisioningProvider
	config *provisioningdomain.PowerConfiguration
	err    error
}

func (p fakeBMCProvider) Capabilities() provisioningdomain.ProviderCapabilities {
	return provisioningdomain.ProviderCapabilities{PowerConfiguration: true}
}

func (p fakeBMCProvider) PowerConfiguration(context.Context, string) (*provisioningdomain.PowerConfiguration, error) {
	return p.config, p.err
}

type fakeBMCProviders struct {
	provider provisioningdomain.OSProvisioningProvider
}

func (f fakeBMCProviders) For(context.Context, string) (provisioningdomain.OSProvisioningProvider, error) {
	return f.provider, nil
}

// The endpoint source asks the power adapter of the Power Configuration whether there is a BMC:
// only a bmc-family driver of a machine outside a VM host yields one (decision 054).
func TestBMCEndpointSourceMapsProvisionerAnswers(t *testing.T) {
	server := &serverdomain.Server{ID: "srv", Hardware: serverdomain.Hardware{SystemUUID: "uuid-1"}}
	source := newBMCEndpointSource(fakeBMCProviders{provider: fakeBMCProvider{
		config: &provisioningdomain.PowerConfiguration{Driver: "ipmi", Address: "10.0.0.5", Username: "maas", Password: "pw", NodeID: "Self"},
	}})
	endpoint, err := source.BMCEndpoint(context.Background(), server)
	if err != nil {
		t.Fatalf("BMCEndpoint error = %v", err)
	}
	if endpoint.Address != "10.0.0.5" || endpoint.Password != "pw" || endpoint.SystemHint != "Self" ||
		endpoint.HostUUID != "uuid-1" || endpoint.PowerType != "ipmi" {
		t.Errorf("endpoint = %+v", endpoint)
	}

	cases := []struct {
		name   string
		config *provisioningdomain.PowerConfiguration
		err    error
		want   error
	}{
		{"virsh driver", &provisioningdomain.PowerConfiguration{Driver: "virsh", Address: "qemu+ssh://h/system", PowerID: "vm"}, nil, serverdomain.ErrNoBMC},
		{"VM-host member", &provisioningdomain.PowerConfiguration{Driver: "ipmi", Address: "10.0.0.5", ManagedBy: "kvm-3"}, nil, serverdomain.ErrNoBMC},
		{"no driver", &provisioningdomain.PowerConfiguration{}, nil, serverdomain.ErrNoBMC},
		{"unknown driver", &provisioningdomain.PowerConfiguration{Driver: "lxd", Address: "https://h"}, nil, serverdomain.ErrNoBMC},
		{"no BMC address", &provisioningdomain.PowerConfiguration{Driver: "redfish"}, nil, serverdomain.ErrNoBMC},
		{"machine gone", nil, provisioningdomain.ErrMachineNotFound, serverdomain.ErrNoBMC},
		{"not an admin", nil, &provisioningdomain.ProviderError{Kind: provisioningdomain.ProviderErrorAuth}, serverdomain.ErrBMCCredentialUnavailable},
		{"provisioner down", nil, &provisioningdomain.ProviderError{Kind: provisioningdomain.ProviderErrorUnavailable}, serverdomain.ErrBMCConnectionUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := newBMCEndpointSource(fakeBMCProviders{provider: fakeBMCProvider{config: tc.config, err: tc.err}})
			if _, err := source.BMCEndpoint(context.Background(), server); !errors.Is(err, tc.want) {
				t.Errorf("BMCEndpoint error = %v, want %v", err, tc.want)
			}
		})
	}
}

// The ISO route serves a Boot ISO by id with HEAD and byte ranges, which BMC HTTP virtual media
// requires, and serves nothing for an unknown or malformed id — never a path outside the Boot
// Media directory.
func TestServeBootISO(t *testing.T) {
	dir := t.TempDir()
	const id = "6b3f0c1e-4f7a-4f53-9d2a-2a7f1d0c9e11"
	if err := os.MkdirAll(filepath.Join(dir, id), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id, provisioninginfra.BootISOFileName), []byte("0123456789abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("not an ISO"), 0o600); err != nil {
		t.Fatal(err)
	}
	files := provisioninginfra.NewGenfsimgBuilder(dir, t.TempDir(), "http://192.0.2.1/")
	app := fiber.New()
	app.Get(bootISORoute, serveBootISO(files))
	path := "/boot-media/ipxe/" + id + "/swallow-ipxe.iso"
	if got := files.URL(id); got != "http://192.0.2.1"+path {
		t.Fatalf("URL = %q, want the route under the base URL", got)
	}

	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Range", "bytes=4-7")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusPartialContent || string(body) != "4567" || resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Errorf("range GET = %d %q (Accept-Ranges %q), want 206 \"4567\"", resp.StatusCode, body, resp.Header.Get("Accept-Ranges"))
	}

	resp, err = app.Test(httptest.NewRequest("HEAD", path, nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK || resp.Header.Get("Content-Length") != "16" {
		t.Errorf("HEAD = %d length %q, want 200 and the file size", resp.StatusCode, resp.Header.Get("Content-Length"))
	}

	for _, bad := range []string{
		"/boot-media/ipxe/7c4e2d1f-0000-4000-8000-000000000000/swallow-ipxe.iso",
		"/boot-media/ipxe/..%2Fsecret.txt/swallow-ipxe.iso",
		"/boot-media/ipxe/not-an-id/swallow-ipxe.iso",
	} {
		resp, err := app.Test(httptest.NewRequest("GET", bad, nil))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", bad, resp.StatusCode)
		}
	}
}

// The catalog resolves a Boot ISO only when it is served, reports the retired installation URL
// for a setting that names none, and maps a missing record to ErrBootISOUnknown.
func TestBootISOCatalog(t *testing.T) {
	dir := t.TempDir()
	const id = "6b3f0c1e-4f7a-4f53-9d2a-2a7f1d0c9e11"
	repo := &catalogRepo{iso: &provisioningdomain.BootISO{ID: id, Name: "tainan-rack", IntegrationID: "integration-1"}}
	catalog := newBootISOCatalog(repo, provisioninginfra.NewGenfsimgBuilder(dir, t.TempDir(), "http://192.0.2.1"), "http://192.0.2.1")

	if image, err := catalog.Resolve(context.Background(), id); !errors.Is(err, serverdomain.ErrBootMediaNotConfigured) || image == nil || image.Name != "tainan-rack" {
		t.Errorf("Resolve without a file = %+v, %v; want the image and ErrBootMediaNotConfigured", image, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, id), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id, provisioninginfra.BootISOFileName), []byte("iso"), 0o600); err != nil {
		t.Fatal(err)
	}
	image, err := catalog.Resolve(context.Background(), id)
	if err != nil || image.IntegrationID != "integration-1" || image.URL != "http://192.0.2.1/boot-media/ipxe/"+id+"/swallow-ipxe.iso" {
		t.Errorf("Resolve = %+v, %v; want the served image", image, err)
	}
	if _, err := catalog.Resolve(context.Background(), "7c4e2d1f-0000-4000-8000-000000000000"); !errors.Is(err, serverdomain.ErrBootISOUnknown) {
		t.Errorf("Resolve(unknown) error = %v, want ErrBootISOUnknown", err)
	}
	if got := catalog.URL(""); got != "http://192.0.2.1/boot-media/ipxe/swallow-ipxe.iso" {
		t.Errorf("URL(\"\") = %q, want the retired installation ISO URL", got)
	}
	if got := newBootISOCatalog(repo, provisioninginfra.NewGenfsimgBuilder(dir, "", ""), "").URL(id); got != "" {
		t.Errorf("URL without a base URL = %q, want empty", got)
	}
}

// catalogRepo is a one-record BootISORepository.
type catalogRepo struct {
	provisioningdomain.BootISORepository
	iso *provisioningdomain.BootISO
}

func (r *catalogRepo) FindByID(_ context.Context, id string) (*provisioningdomain.BootISO, error) {
	if r.iso != nil && r.iso.ID == id {
		return r.iso, nil
	}
	return nil, provisioningdomain.ErrBootISONotFound
}

func equalStringSlices(a, b []string) bool {
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
