package application

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// fakeOmnistatRecorder applies OmnistatRecorder writes to the deploy fake's in-memory Platforms, so
// tests read the record back the way the API would.
type fakeOmnistatRecorder struct {
	platforms *deployFakePlatformRepo
}

func (r *fakeOmnistatRecorder) SetOmnistat(_ context.Context, id string, omnistat *platformdomain.Omnistat) error {
	platform, ok := r.platforms.platforms[id]
	if !ok {
		return platformdomain.ErrPlatformNotFound
	}
	if omnistat == nil {
		platform.Omnistat = nil
	} else {
		copied := *omnistat
		platform.Omnistat = &copied
	}
	return nil
}

func (r *fakeOmnistatRecorder) UpdateOmnistatState(context.Context, string, string, platformdomain.OmnistatState, time.Time) (bool, error) {
	return false, nil
}

// SetHardwareCounterNodes applies the write only under the record's fence, as the store does.
func (r *fakeOmnistatRecorder) SetHardwareCounterNodes(
	_ context.Context, id, workflowID string, nodes []platformdomain.HardwareCounterNode, _ time.Time,
) (bool, error) {
	platform, ok := r.platforms.platforms[id]
	if !ok || platform.Omnistat == nil || platform.Omnistat.LastWorkflowID != workflowID {
		return false, nil
	}
	platform.Omnistat.HardwareCounters.Nodes = slices.Clone(nodes)
	return true, nil
}

func omnistatSlurmInput() DeployPlatformInput {
	input := validSlurmInput()
	input.SlurmSpec.NodeAssignments = []platformdomain.SlurmNodeAssignment{
		{ServerID: "s1", Controller: true},
		{ServerID: "s2", Compute: true},
		{ServerID: "s3", Compute: true},
	}
	input.Omnistat = &platformdomain.OmnistatSettings{Enabled: true}
	return input
}

// Enabling Omnistat records it as installing under the Workflow id the launcher must use, names
// the primary controller as the observability Server, turns on the Slurm-side overlay, and hands
// the launcher the compute nodes and the configure-omnistat variables.
func TestDeploySlurmWithOmnistatRecordsTheWorkflowBeforeLaunch(t *testing.T) {
	service, launcher, platforms := newDeployHarness(slurmServers()...)

	result, err := service.Deploy(context.Background(), omnistatSlurmInput())
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	launch := launcher.launched
	if launch.OperationID == "" {
		t.Fatal("an Omnistat deploy must preassign the Workflow id")
	}
	record := platforms.platforms[result.PlatformID].Omnistat
	if record == nil || record.State != platformdomain.OmnistatInstalling || record.LastWorkflowID != launch.OperationID ||
		record.ObservabilityServerID != "s1" || record.Version != platformdomain.OmnistatVersion {
		t.Fatalf("omnistat record = %+v, want installing under %s on s1", record, launch.OperationID)
	}
	if launch.TrustedVars[varSlurmOmnistatEnabled] != true {
		t.Errorf("%s = %v, want true", varSlurmOmnistatEnabled, launch.TrustedVars[varSlurmOmnistatEnabled])
	}
	if launch.Omnistat == nil || launch.Omnistat.ObservabilityServerID != "s1" ||
		!slices.Equal(launch.Omnistat.ComputeServerIDs, []string{"s2", "s3"}) {
		t.Fatalf("omnistat launch = %+v", launch.Omnistat)
	}
	vars := launch.Omnistat.TrustedVars
	if vars[varOmnistatObservabilityID] != "s1" || vars[varOmnistatPort] != platformdomain.OmnistatExporterPort ||
		vars[varOmnistatVersion] != platformdomain.OmnistatVersion || vars[varOmnistatSystemName] != "lab-slurm" {
		t.Errorf("omnistat vars = %v", vars)
	}
	if computes, _ := vars[varOmnistatComputeIDs].([]string); !slices.Equal(computes, []string{"s2", "s3"}) {
		t.Errorf("%s = %v, want [s2 s3]", varOmnistatComputeIDs, vars[varOmnistatComputeIDs])
	}
}

// The deployment's request for hardware counters is the record's intent from acceptance, with no
// nodes until the install reports them, and the launch hands it on; omitted, it is false.
func TestDeploySlurmRecordsTheHardwareCountersRequest(t *testing.T) {
	for _, requested := range []bool{true, false} {
		service, launcher, platforms := newDeployHarness(slurmServers()...)
		input := omnistatSlurmInput()
		input.Omnistat.HardwareCounters = requested
		result, err := service.Deploy(context.Background(), input)
		if err != nil {
			t.Fatalf("Deploy(hardwareCounters %v) error = %v", requested, err)
		}
		counters := platforms.platforms[result.PlatformID].Omnistat.HardwareCounters
		if counters.Requested != requested || len(counters.Nodes) != 0 {
			t.Errorf("recorded hardware counters %+v, want requested %v without nodes", counters, requested)
		}
		if launcher.launched.Omnistat.HardwareCounters != requested {
			t.Errorf("launched hardware counters %v, want %v", launcher.launched.Omnistat.HardwareCounters, requested)
		}
	}
}

// The nodes an install reports are recorded sorted by Server id under the record's fence, so a
// superseded Workflow's late result changes nothing, and they keep the deployment's request.
func TestRecordHardwareCounterNodesIsFencedOnTheRecord(t *testing.T) {
	service, launcher, platforms, servers := newClaimingDeployHarness(slurmServers()...)
	input := omnistatSlurmInput()
	input.Omnistat.HardwareCounters = true
	result, err := service.Deploy(context.Background(), input)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	platformService := mustPlatformService(testPlatformServiceDeps(platforms, servers))
	reported := []platformdomain.HardwareCounterNode{
		{ServerID: "s3", State: platformdomain.HardwareCountersUnavailable, Reason: "probe_failed"},
		{ServerID: "s2", State: platformdomain.HardwareCountersCollecting},
	}
	if err := platformService.RecordHardwareCounterNodes(context.Background(), result.PlatformID, "superseded", reported); err != nil {
		t.Fatalf("RecordHardwareCounterNodes(superseded) error = %v", err)
	}
	if nodes := platforms.platforms[result.PlatformID].Omnistat.HardwareCounters.Nodes; len(nodes) != 0 {
		t.Errorf("a superseded Workflow recorded %v, want nothing", nodes)
	}
	if err := platformService.RecordHardwareCounterNodes(context.Background(), result.PlatformID, launcher.launched.OperationID, reported); err != nil {
		t.Fatalf("RecordHardwareCounterNodes() error = %v", err)
	}
	counters := platforms.platforms[result.PlatformID].Omnistat.HardwareCounters
	want := []platformdomain.HardwareCounterNode{reported[1], reported[0]}
	if !counters.Requested || !reflect.DeepEqual(counters.Nodes, want) {
		t.Errorf("recorded %+v, want requested with %v", counters, want)
	}
	if reported[0].ServerID != "s3" {
		t.Errorf("recording sorted the caller's slice: %v", reported)
	}
}

// The install method defaults to container; the record and the launch carry the method the
// observability software is installed by, and an unknown method creates nothing.
func TestDeploySlurmOmnistatInstallMethod(t *testing.T) {
	for _, tc := range []struct {
		requested platformdomain.ObservabilityInstallMethod
		want      platformdomain.ObservabilityInstallMethod
	}{
		{requested: "", want: platformdomain.ObservabilityInstallContainer},
		{requested: platformdomain.ObservabilityInstallNative, want: platformdomain.ObservabilityInstallNative},
	} {
		t.Run(string(tc.want), func(t *testing.T) {
			service, launcher, platforms := newDeployHarness(slurmServers()...)
			input := omnistatSlurmInput()
			input.Omnistat.InstallMethod = tc.requested
			result, err := service.Deploy(context.Background(), input)
			if err != nil {
				t.Fatalf("Deploy() error = %v", err)
			}
			if got := platforms.platforms[result.PlatformID].Omnistat.InstallMethod; got != tc.want {
				t.Errorf("recorded installMethod = %q, want %q", got, tc.want)
			}
			if got := launcher.launched.Omnistat.InstallMethod; got != tc.want {
				t.Errorf("launched installMethod = %q, want %q", got, tc.want)
			}
		})
	}

	service, launcher, platforms := newDeployHarness(slurmServers()...)
	input := omnistatSlurmInput()
	input.Omnistat.InstallMethod = "snap"
	if _, err := service.Deploy(context.Background(), input); !errors.Is(err, platformdomain.ErrInvalidOmnistat) {
		t.Fatalf("Deploy(snap) error = %v, want ErrInvalidOmnistat", err)
	}
	if launcher.launched != nil || len(platforms.platforms) != 0 {
		t.Error("a refused deploy must create nothing")
	}
}

// The container runtime feature installs on every compute and login node, never on a
// controller-only one, and the Platform records the choice.
func TestDeploySlurmContainerRuntimeFeature(t *testing.T) {
	service, launcher, platforms := newDeployHarness(append(slurmServers(),
		deployedServer("s4", "lab-slurm-4", "site-1", "192.168.100.5"))...)
	input := validSlurmInput()
	input.SlurmSpec.NodeAssignments = []platformdomain.SlurmNodeAssignment{
		{ServerID: "s1", Controller: true},
		{ServerID: "s2", Compute: true},
		{ServerID: "s3", Compute: true},
		{ServerID: "s4", Login: true},
	}
	input.ContainerRuntime = " podman "
	result, err := service.Deploy(context.Background(), input)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	runtime := launcher.launched.ContainerRuntime
	if runtime == nil || runtime.Kind != platformdomain.ContainerRuntimePodman ||
		!slices.Equal(runtime.ServerIDs, []string{"s2", "s3", "s4"}) {
		t.Fatalf("container runtime launch = %+v, want podman on [s2 s3 s4]", runtime)
	}
	if got := platforms.platforms[result.PlatformID].ContainerRuntime; got != platformdomain.ContainerRuntimePodman {
		t.Errorf("recorded containerRuntime = %q, want podman", got)
	}

	service, launcher, _ = newDeployHarness(slurmServers()...)
	if _, err := service.Deploy(context.Background(), validSlurmInput()); err != nil {
		t.Fatalf("Deploy(no runtime) error = %v", err)
	}
	if launcher.launched.ContainerRuntime != nil {
		t.Errorf("container runtime launch = %+v, want none", launcher.launched.ContainerRuntime)
	}
}

// GPU driver options are recorded on the Platform and always reach the playbook, empty when none
// were requested, so a redeploy without them clears what an earlier run applied.
func TestDeploySlurmGPUDriverOptions(t *testing.T) {
	service, launcher, platforms := newDeployHarness(slurmServers()...)
	input := validSlurmInput()
	input.GPUDriver = &platformdomain.GPUDriverSettings{ModuleOptions: map[string]string{"pcie_p2p": "0"}}
	result, err := service.Deploy(context.Background(), input)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if got := platforms.platforms[result.PlatformID].GPUDriver.ModuleOptions["pcie_p2p"]; got != "0" {
		t.Errorf("recorded pcie_p2p = %q, want 0", got)
	}
	if options, _ := launcher.launched.TrustedVars[varGPUDriverModuleOptions].(map[string]string); options["pcie_p2p"] != "0" {
		t.Errorf("%s = %v, want pcie_p2p=0", varGPUDriverModuleOptions, launcher.launched.TrustedVars[varGPUDriverModuleOptions])
	}

	service, launcher, _ = newDeployHarness(slurmServers()...)
	if _, err := service.Deploy(context.Background(), validSlurmInput()); err != nil {
		t.Fatalf("Deploy(no options) error = %v", err)
	}
	if options, ok := launcher.launched.TrustedVars[varGPUDriverModuleOptions].(map[string]string); !ok || options == nil || len(options) != 0 {
		t.Errorf("%s = %#v, want an empty map", varGPUDriverModuleOptions, launcher.launched.TrustedVars[varGPUDriverModuleOptions])
	}

	service, launcher, platforms = newDeployHarness(slurmServers()...)
	input = validSlurmInput()
	input.GPUDriver = &platformdomain.GPUDriverSettings{ModuleOptions: map[string]string{"pcie_p2p": "0\ninstall x"}}
	if _, err := service.Deploy(context.Background(), input); !errors.Is(err, platformdomain.ErrInvalidGPUDriver) {
		t.Fatalf("Deploy(bad option) error = %v, want ErrInvalidGPUDriver", err)
	}
	if launcher.launched != nil || len(platforms.platforms) != 0 {
		t.Error("a refused deploy must create nothing")
	}
}

// The GRES type is derived unless the deploy names one; the name reaches the playbook, is recorded
// on the Platform, and must be a plain identifier.
func TestDeploySlurmGPUGresType(t *testing.T) {
	service, launcher, platforms := newDeployHarness(slurmServers()...)
	if _, err := service.Deploy(context.Background(), validSlurmInput()); err != nil {
		t.Fatalf("Deploy(derived) error = %v", err)
	}
	if got, ok := launcher.launched.TrustedVars[varSlurmGPUGresType].(string); !ok || got != "" {
		t.Errorf("%s = %#v, want an empty (derived) type", varSlurmGPUGresType, launcher.launched.TrustedVars[varSlurmGPUGresType])
	}

	service, launcher, platforms = newDeployHarness(slurmServers()...)
	input := validSlurmInput()
	input.SlurmSpec.GPUGresType = " mi308x_vf "
	result, err := service.Deploy(context.Background(), input)
	if err != nil {
		t.Fatalf("Deploy(named) error = %v", err)
	}
	if launcher.launched.TrustedVars[varSlurmGPUGresType] != "mi308x_vf" || platforms.platforms[result.PlatformID].GPUGresType != "mi308x_vf" {
		t.Errorf("named type: var = %v, recorded = %q", launcher.launched.TrustedVars[varSlurmGPUGresType], platforms.platforms[result.PlatformID].GPUGresType)
	}

	for _, name := range []string{"gpu:8", "mi308x vf", "8gpu", "a,b"} {
		service, launcher, platforms = newDeployHarness(slurmServers()...)
		input := validSlurmInput()
		input.SlurmSpec.GPUGresType = name
		if _, err := service.Deploy(context.Background(), input); !errors.Is(err, platformdomain.ErrInvalidDeployment) {
			t.Errorf("Deploy(%q) error = %v, want ErrInvalidDeployment", name, err)
		}
		if launcher.launched != nil || len(platforms.platforms) != 0 {
			t.Errorf("Deploy(%q) created something", name)
		}
	}
}

// The GPU Operator's host driver step loads amdgpu too, so its vars carry the Platform's options.
func TestGPUOperatorVarsCarryDriverOptions(t *testing.T) {
	vars := buildGPUOperatorVars(platformdomain.GPUOperatorSettings{Enabled: true}, "c1",
		platformdomain.GPUDriverSettings{ModuleOptions: map[string]string{"pcie_p2p": "0"}})
	if options, _ := vars[varGPUDriverModuleOptions].(map[string]string); options["pcie_p2p"] != "0" {
		t.Errorf("%s = %v, want pcie_p2p=0", varGPUDriverModuleOptions, vars[varGPUDriverModuleOptions])
	}
}

func TestDeployRefusesContainerRuntimeItCannotApply(t *testing.T) {
	service, launcher, platforms := newDeployHarness(slurmServers()...)
	input := validSlurmInput()
	input.ContainerRuntime = "containerd"
	if _, err := service.Deploy(context.Background(), input); !errors.Is(err, platformdomain.ErrInvalidContainerRuntime) {
		t.Fatalf("Deploy(containerd) error = %v, want ErrInvalidContainerRuntime", err)
	}
	k0s := DeployPlatformInput{SiteID: "site-1", Name: "lab-k0s", Type: platformdomain.PlatformTypeKubernetes, ContainerRuntime: "docker-ce"}
	if _, err := service.Deploy(context.Background(), k0s); !errors.Is(err, platformdomain.ErrInvalidContainerRuntime) {
		t.Fatalf("Deploy(Kubernetes with runtime) error = %v, want ErrInvalidContainerRuntime", err)
	}
	if launcher.launched != nil || len(platforms.platforms) != 0 {
		t.Error("a refused deploy must create nothing")
	}
}

func TestDeploySlurmWithoutOmnistatLeavesItOut(t *testing.T) {
	service, launcher, platforms := newDeployHarness(slurmServers()...)
	input := omnistatSlurmInput()
	input.Omnistat = &platformdomain.OmnistatSettings{Enabled: false}

	result, err := service.Deploy(context.Background(), input)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if launcher.launched.Omnistat != nil || platforms.platforms[result.PlatformID].Omnistat != nil {
		t.Error("a disabled Omnistat must add no Jobs and no record")
	}
	if _, ok := launcher.launched.TrustedVars[varSlurmOmnistatEnabled]; ok {
		t.Errorf("%s must be absent without Omnistat", varSlurmOmnistatEnabled)
	}
}

func TestDeployRefusesOmnistatItCannotApply(t *testing.T) {
	t.Run("Kubernetes Platform", func(t *testing.T) {
		service, launcher, platforms := newDeployHarness(slurmServers()...)
		input := DeployPlatformInput{
			SiteID: "site-1", Name: "lab-k0s", Type: platformdomain.PlatformTypeKubernetes,
			Omnistat: &platformdomain.OmnistatSettings{Enabled: true},
		}
		if _, err := service.Deploy(context.Background(), input); !errors.Is(err, platformdomain.ErrInvalidOmnistat) {
			t.Fatalf("Deploy() error = %v, want ErrInvalidOmnistat", err)
		}
		if launcher.launched != nil || len(platforms.platforms) != 0 {
			t.Error("a refused deploy must create nothing")
		}
	})
}

func TestMonitoringLinkUpsertAndRemove(t *testing.T) {
	links := []platformdomain.MonitoringLink{{Name: "Runbook", URL: "https://wiki/runbook"}}
	links = platformdomain.UpsertMonitoringLink(links, platformdomain.MonitoringLink{Name: testOwnedLinkName, URL: "http://10.0.0.1:3000"})
	links = platformdomain.UpsertMonitoringLink(links, platformdomain.MonitoringLink{Name: testOwnedLinkName, URL: "http://10.0.0.2:3000"})
	want := []platformdomain.MonitoringLink{
		{Name: "Runbook", URL: "https://wiki/runbook"},
		{Name: testOwnedLinkName, URL: "http://10.0.0.2:3000"},
	}
	if !slices.Equal(links, want) {
		t.Errorf("upserted links = %v, want %v", links, want)
	}
	if got := platformdomain.RemoveMonitoringLinks(links, testOwnedLinkName, testLegacyLinkName); !slices.Equal(got, want[:1]) {
		t.Errorf("removed links = %v, want %v", got, want[:1])
	}
}

// The Platform's own dashboards are on unless the deploy turns them off; they go on the primary
// controller and take the monitoring feature's install method, or the native method without one.
func TestDeploySlurmMonitoringDashboards(t *testing.T) {
	off := false
	for _, tc := range []struct {
		name  string
		input func() DeployPlatformInput
		want  *platformdomain.MonitoringDashboardsLaunch
	}{
		{name: "no feature", input: validSlurmInput,
			want: &platformdomain.MonitoringDashboardsLaunch{ServerID: "s1", InstallMethod: platformdomain.ObservabilityInstallNative}},
		{name: "Omnistat native", input: func() DeployPlatformInput {
			input := omnistatSlurmInput()
			input.Omnistat.InstallMethod = platformdomain.ObservabilityInstallNative
			return input
		}, want: &platformdomain.MonitoringDashboardsLaunch{ServerID: "s1", InstallMethod: platformdomain.ObservabilityInstallNative}},
		{name: "turned off", input: func() DeployPlatformInput {
			input := omnistatSlurmInput()
			input.MonitoringDashboards = &off
			return input
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, launcher, _ := newDeployHarness(slurmServers()...)
			if _, err := service.Deploy(context.Background(), tc.input()); err != nil {
				t.Fatalf("Deploy() error = %v", err)
			}
			got := launcher.launched.MonitoringDashboards
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("monitoring dashboards = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// slurmSpecModule is a Slurm module stub for realizing a spec: the deploy request it renders is
// read by the test's request reader (readSlurmInput), not decoded from the body.
type slurmSpecModule struct{}

func (slurmSpecModule) PlatformType() platformdomain.PlatformType {
	return platformdomain.PlatformTypeSlurm
}

func (slurmSpecModule) DeployRequest(platformdomain.PlatformSpec) map[string]any {
	return map[string]any{}
}

func (slurmSpecModule) SpecFromDeployRequest(map[string]any) (platformdomain.PlatformSpec, error) {
	return platformdomain.PlatformSpec{}, nil
}

func (slurmSpecModule) Changes(*platformdomain.PlatformSpec, platformdomain.PlatformSpec) (platformdomain.PlatformChanges, error) {
	return platformdomain.PlatformChanges{}, nil
}

func (slurmSpecModule) ReportsMembership(platformdomain.PlatformMember) bool { return true }

func (slurmSpecModule) AccessServers(platformdomain.PlatformSpec) []string { return nil }

// readSlurmInput reads every spec as the Omnistat Slurm deployment with the given settings.
func readSlurmInput(omnistat platformdomain.OmnistatSettings) DeployRequestReader {
	return func([]byte) (DeployPlatformInput, error) {
		input := omnistatSlurmInput()
		input.Omnistat = &omnistat
		return input, nil
	}
}

// A hardware counters change is planned as a deploy of the spec onto the stored Platform, so the
// Omnistat record is written as a deploy writes it: the new request, installing, no nodes, and fenced
// on the change's Workflow, which the launcher is told to limit to Omnistat. No Platform is created.
func TestAHardwareCountersChangeRecordsTheRequestUnderItsWorkflow(t *testing.T) {
	service, launcher, platforms := newDeployHarness(slurmServers()...)
	launcher.launchIDs = true
	result, err := service.Deploy(context.Background(), omnistatSlurmInput())
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	platform := platforms.platforms[result.PlatformID]
	platform.Omnistat.HardwareCounters.Nodes = []platformdomain.HardwareCounterNode{{ServerID: "s2", State: platformdomain.HardwareCountersUnavailable, Reason: "probe_failed"}}
	platform.Spec = &platformdomain.PlatformSpec{Desired: platformdomain.PlatformPresent}

	for _, requested := range []bool{true, false} {
		service.readRequest = readSlurmInput(platformdomain.OmnistatSettings{Enabled: true, HardwareCounters: requested})
		prepared, err := PrepareSpecDeployment(context.Background(), service, slurmSpecModule{}, platform, "admin",
			platformdomain.RealizeHardwareCounters)
		if err != nil {
			t.Fatalf("PrepareSpecDeployment(hardwareCounters %v) error = %v", requested, err)
		}
		workflowID, err := service.Persist(context.Background(), prepared)
		if err != nil {
			t.Fatalf("Persist(hardwareCounters %v) error = %v", requested, err)
		}
		launch := launcher.launched
		if launch.Realization != platformdomain.RealizeHardwareCounters || launch.Omnistat == nil ||
			launch.Omnistat.HardwareCounters != requested || launch.OperationID != workflowID {
			t.Fatalf("launch = realization %q omnistat %+v id %s, want the hardware counters change (%v) under %s",
				launch.Realization, launch.Omnistat, launch.OperationID, requested, workflowID)
		}
		record := platforms.platforms[result.PlatformID].Omnistat
		if record.HardwareCounters.Requested != requested || len(record.HardwareCounters.Nodes) != 0 ||
			record.State != platformdomain.OmnistatInstalling || record.LastWorkflowID != workflowID {
			t.Errorf("record = %+v, want requested %v, installing, no nodes, under %s", record, requested, workflowID)
		}
	}
	if len(platforms.platforms) != 1 {
		t.Errorf("platforms = %d, want the change to reuse the Platform", len(platforms.platforms))
	}
}

// Only a deployed Slurm Platform whose spec keeps Omnistat on has a hardware counters change.
func TestAHardwareCountersChangeNeedsADeployedOmnistat(t *testing.T) {
	service, launcher, platforms := newDeployHarness(slurmServers()...)
	result, err := service.Deploy(context.Background(), omnistatSlurmInput())
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	platform := platforms.platforms[result.PlatformID]
	platform.Spec = &platformdomain.PlatformSpec{Desired: platformdomain.PlatformPresent}
	launches := launcher.launches

	service.readRequest = readSlurmInput(platformdomain.OmnistatSettings{Enabled: false})
	if _, err := PrepareSpecDeployment(context.Background(), service, slurmSpecModule{}, platform, "admin",
		platformdomain.RealizeHardwareCounters); !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Errorf("Omnistat off: err = %v, want ErrInvalidDeployment", err)
	}
	notStored := omnistatSlurmInput()
	notStored.realization = platformdomain.RealizeHardwareCounters
	if _, err := service.Prepare(context.Background(), notStored); !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Errorf("no stored Platform: err = %v, want ErrInvalidDeployment", err)
	}
	kubernetes := DeployPlatformInput{SiteID: "site-1", Name: "lab-k0s", Type: platformdomain.PlatformTypeKubernetes,
		PlatformID: result.PlatformID, realization: platformdomain.RealizeHardwareCounters}
	if _, err := service.Prepare(context.Background(), kubernetes); !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Errorf("Kubernetes Platform: err = %v, want ErrInvalidDeployment", err)
	}
	if launcher.launches != launches {
		t.Error("a refused hardware counters change must launch nothing")
	}
}
