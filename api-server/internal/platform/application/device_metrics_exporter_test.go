package application

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// fakeDeviceMetricsExporterRecorder applies DeviceMetricsExporterRecorder writes to the deploy
// fake's in-memory Platforms, fenced like the Mongo repository.
type fakeDeviceMetricsExporterRecorder struct {
	platforms *deployFakePlatformRepo
}

func (r *fakeDeviceMetricsExporterRecorder) SetDeviceMetricsExporter(_ context.Context, id string, exporter *platformdomain.DeviceMetricsExporter) error {
	platform, ok := r.platforms.platforms[id]
	if !ok {
		return platformdomain.ErrPlatformNotFound
	}
	if exporter == nil {
		platform.DeviceMetricsExporter = nil
	} else {
		copied := *exporter
		platform.DeviceMetricsExporter = &copied
	}
	return nil
}

func (r *fakeDeviceMetricsExporterRecorder) UpdateDeviceMetricsExporterState(
	_ context.Context, id, workflowID string, state platformdomain.DeviceMetricsExporterState, _ time.Time,
) (bool, error) {
	platform, ok := r.platforms.platforms[id]
	if !ok || platform.DeviceMetricsExporter == nil || platform.DeviceMetricsExporter.LastWorkflowID != workflowID {
		return false, nil
	}
	platform.DeviceMetricsExporter.State = state
	return true, nil
}

func (r *fakeDeviceMetricsExporterRecorder) UpdateDeviceMetricsExporterGPUIdentity(
	_ context.Context, id, workflowID string, identity platformdomain.GPUIdentity, _ time.Time,
) (bool, error) {
	platform, ok := r.platforms.platforms[id]
	if !ok || platform.DeviceMetricsExporter == nil || platform.DeviceMetricsExporter.LastWorkflowID != workflowID {
		return false, nil
	}
	platform.DeviceMetricsExporter.GPUIdentity = identity
	return true, nil
}

// exporterSlurmServers are the Slurm fixtures with AMD compute GPUs on s3 only.
func exporterSlurmServers() []*serverdomain.Server {
	servers := slurmServers()
	for _, server := range servers {
		if server.ID == "s3" {
			withAMDGPU(server)
		}
	}
	return servers
}

func exporterSlurmInput() DeployPlatformInput {
	input := omnistatSlurmInput()
	input.Omnistat = nil
	input.DeviceMetricsExporter = &platformdomain.DeviceMetricsExporterSettings{Enabled: true}
	return input
}

func newExporterHarness(servers ...*serverdomain.Server) (*DeployService, *recordingLauncher, *deployFakePlatformRepo) {
	service, launcher, platforms := newDeployHarness(servers...)
	service.AttachOmnistatRecorder(&fakeOmnistatRecorder{platforms: platforms})
	service.AttachDeviceMetricsExporterRecorder(&fakeDeviceMetricsExporterRecorder{platforms: platforms})
	return service, launcher, platforms
}

// Enabling the exporter records it as installing under the Workflow id the launcher must use,
// installs its job hook through deploy-slurm, and hands the launcher only the GPU compute nodes,
// the exporter port, and the record id the install's completion fences the GPU identity on.
func TestDeploySlurmWithTheDeviceMetricsExporter(t *testing.T) {
	service, launcher, platforms := newExporterHarness(exporterSlurmServers()...)

	result, err := service.Deploy(context.Background(), exporterSlurmInput())
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	launch := launcher.launched
	record := platforms.platforms[result.PlatformID].DeviceMetricsExporter
	if launch.OperationID == "" || record == nil || record.State != platformdomain.DeviceMetricsExporterInstalling ||
		record.LastWorkflowID != launch.OperationID || record.ObservabilityServerID != "s1" ||
		record.Version != "1.5.3" || record.InstallMethod != platformdomain.ObservabilityInstallContainer ||
		record.GPUIdentity != platformdomain.GPUIdentityUnknown {
		t.Fatalf("exporter record = %+v, want installing under %s on s1", record, launch.OperationID)
	}
	if launch.TrustedVars[varSlurmDeviceMetricsExporterEnabled] != true {
		t.Errorf("%s = %v, want true", varSlurmDeviceMetricsExporterEnabled, launch.TrustedVars[varSlurmDeviceMetricsExporterEnabled])
	}
	if _, ok := launch.TrustedVars[varSlurmOmnistatEnabled]; ok || launch.Omnistat != nil {
		t.Error("the exporter must not turn on Omnistat")
	}
	exporter := launch.DeviceMetricsExporter
	if exporter == nil || exporter.ObservabilityServerID != "s1" || !slices.Equal(exporter.GPUComputeServerIDs, []string{"s3"}) {
		t.Fatalf("exporter launch = %+v, want the GPU compute node s3", exporter)
	}
	vars := exporter.TrustedVars
	if vars[varDeviceMetricsExporterPort] != 5001 || vars[varDeviceMetricsExporterVersion] != "1.5.3" ||
		vars[varDeviceMetricsExporterObservabilityID] != "s1" || vars[varDeviceMetricsExporterClusterName] != "lab-slurm" ||
		vars[VarDeviceMetricsExporterRecordID] != launch.OperationID {
		t.Errorf("exporter vars = %v", vars)
	}
	if computes, _ := vars[varDeviceMetricsExporterComputeIDs].([]string); !slices.Equal(computes, []string{"s3"}) {
		t.Errorf("%s = %v, want [s3]", varDeviceMetricsExporterComputeIDs, vars[varDeviceMetricsExporterComputeIDs])
	}
}

// The exporter cannot apply on Kubernetes (the GPU Operator runs it there), together with Omnistat,
// without a GPU compute node, by an unknown install method, or in a process that cannot record it;
// each refusal creates nothing.
func TestDeployRefusesADeviceMetricsExporterItCannotApply(t *testing.T) {
	cases := map[string]struct {
		servers []*serverdomain.Server
		input   func() DeployPlatformInput
		attach  bool
	}{
		"Kubernetes Platform": {servers: exporterSlurmServers(), attach: true, input: func() DeployPlatformInput {
			return DeployPlatformInput{SiteID: "site-1", Name: "lab-k0s", Type: platformdomain.PlatformTypeKubernetes,
				DeviceMetricsExporter: &platformdomain.DeviceMetricsExporterSettings{Enabled: true}}
		}},
		"with Omnistat": {servers: exporterSlurmServers(), attach: true, input: func() DeployPlatformInput {
			input := exporterSlurmInput()
			input.Omnistat = &platformdomain.OmnistatSettings{Enabled: true}
			return input
		}},
		"no GPU compute node": {servers: slurmServers(), attach: true, input: exporterSlurmInput},
		"unknown install method": {servers: exporterSlurmServers(), attach: true, input: func() DeployPlatformInput {
			input := exporterSlurmInput()
			input.DeviceMetricsExporter.InstallMethod = "snap"
			return input
		}},
		"no recorder": {servers: exporterSlurmServers(), input: exporterSlurmInput},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			service, launcher, platforms := newDeployHarness(tc.servers...)
			if tc.attach {
				service.AttachOmnistatRecorder(&fakeOmnistatRecorder{platforms: platforms})
				service.AttachDeviceMetricsExporterRecorder(&fakeDeviceMetricsExporterRecorder{platforms: platforms})
			}
			if _, err := service.Deploy(context.Background(), tc.input()); !errors.Is(err, platformdomain.ErrInvalidDeviceMetricsExporter) {
				t.Fatalf("Deploy() error = %v, want ErrInvalidDeviceMetricsExporter", err)
			}
			if launcher.launched != nil || len(platforms.platforms) != 0 {
				t.Error("a refused deploy must create nothing")
			}
		})
	}
}

// Without the exporter there is no record, no Jobs, and no job hook variable; a disabled object
// is the same as an omitted one.
func TestDeploySlurmWithoutTheDeviceMetricsExporter(t *testing.T) {
	service, launcher, platforms := newExporterHarness(exporterSlurmServers()...)
	input := exporterSlurmInput()
	input.DeviceMetricsExporter.Enabled = false
	result, err := service.Deploy(context.Background(), input)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if launcher.launched.DeviceMetricsExporter != nil || platforms.platforms[result.PlatformID].DeviceMetricsExporter != nil {
		t.Error("a disabled exporter must add no Jobs and no record")
	}
	if _, ok := launcher.launched.TrustedVars[varSlurmDeviceMetricsExporterEnabled]; ok {
		t.Errorf("%s must be absent without the exporter", varSlurmDeviceMetricsExporterEnabled)
	}
}

// The native OCI container and host MPI features are recorded on the Platform and always reach the
// playbook, false when off, so a redeploy without them converges.
func TestDeploySlurmOCIContainersAndPMIx(t *testing.T) {
	service, launcher, platforms := newDeployHarness(slurmServers()...)
	if _, err := service.Deploy(context.Background(), validSlurmInput()); err != nil {
		t.Fatalf("Deploy(off) error = %v", err)
	}
	if launcher.launched.TrustedVars[varSlurmOCIContainers] != false || launcher.launched.TrustedVars[varSlurmPMIx] != false {
		t.Errorf("off: %s = %v, %s = %v, want false", varSlurmOCIContainers, launcher.launched.TrustedVars[varSlurmOCIContainers],
			varSlurmPMIx, launcher.launched.TrustedVars[varSlurmPMIx])
	}

	service, launcher, platforms = newDeployHarness(slurmServers()...)
	input := validSlurmInput()
	input.SlurmSpec.OCIContainers, input.SlurmSpec.PMIx = true, true
	result, err := service.Deploy(context.Background(), input)
	if err != nil {
		t.Fatalf("Deploy(on) error = %v", err)
	}
	platform := platforms.platforms[result.PlatformID]
	if launcher.launched.TrustedVars[varSlurmOCIContainers] != true || launcher.launched.TrustedVars[varSlurmPMIx] != true ||
		!platform.OCIContainers || !platform.PMIx {
		t.Errorf("on: vars %v/%v, recorded %v/%v", launcher.launched.TrustedVars[varSlurmOCIContainers],
			launcher.launched.TrustedVars[varSlurmPMIx], platform.OCIContainers, platform.PMIx)
	}
}

// A successful install records its Grafana link and, fenced on the record's Workflow, the GPU
// identity it found; the uninstall completion marks the exporter absent and removes only its link.
func TestPlatformServiceRecordsAndRetiresTheDeviceMetricsExporter(t *testing.T) {
	platforms := &deployFakePlatformRepo{platforms: map[string]*platformdomain.Platform{}}
	recorder := &fakeDeviceMetricsExporterRecorder{platforms: platforms}
	platforms.platforms["p1"] = &platformdomain.Platform{
		ID: "p1", Type: platformdomain.PlatformTypeSlurm,
		MonitoringLinks: []platformdomain.MonitoringLink{{Name: "Runbook", URL: "https://wiki/runbook"}},
		DeviceMetricsExporter: &platformdomain.DeviceMetricsExporter{
			Enabled: true, State: platformdomain.DeviceMetricsExporterInstalling, LastWorkflowID: "deploy-1",
		},
	}
	service := NewPlatformService(platforms, &deployFakeSiteRepo{}, &deployFakeServerRepo{servers: map[string]*serverdomain.Server{}}, nil, nil)
	service.AttachDeviceMetricsExporterRecorder(recorder)
	ctx := context.Background()

	if err := service.RecordDeviceMetricsExporterResult(ctx, "p1", "deploy-1", "http://10.0.0.1:3000",
		platformdomain.GPUIdentityCollapsed); err != nil {
		t.Fatalf("RecordDeviceMetricsExporterResult() error = %v", err)
	}
	platform := platforms.platforms["p1"]
	if platform.DeviceMetricsExporter.GPUIdentity != platformdomain.GPUIdentityCollapsed {
		t.Errorf("gpuIdentity = %q, want collapsed", platform.DeviceMetricsExporter.GPUIdentity)
	}
	wantLinks := []platformdomain.MonitoringLink{
		{Name: "Runbook", URL: "https://wiki/runbook"},
		{Name: platformdomain.DeviceMetricsExporterGrafanaLinkName, URL: "http://10.0.0.1:3000"},
	}
	if !slices.Equal(platform.MonitoringLinks, wantLinks) {
		t.Errorf("links = %v, want %v", platform.MonitoringLinks, wantLinks)
	}
	if err := service.RecordDeviceMetricsExporterResult(ctx, "p1", "superseded", "http://10.0.0.9:3000",
		platformdomain.GPUIdentityDistinct); err != nil {
		t.Fatalf("RecordDeviceMetricsExporterResult(superseded) error = %v", err)
	}
	if platform := platforms.platforms["p1"]; platform.DeviceMetricsExporter.GPUIdentity != platformdomain.GPUIdentityCollapsed {
		t.Errorf("a superseded Workflow's identity was recorded: %q", platform.DeviceMetricsExporter.GPUIdentity)
	}

	if err := service.CompleteUninstall(ctx, "p1", false); err != nil {
		t.Fatalf("CompleteUninstall() error = %v", err)
	}
	platform = platforms.platforms["p1"]
	if platform.DeviceMetricsExporter.State != platformdomain.DeviceMetricsExporterAbsent || platform.DeviceMetricsExporter.Managed() {
		t.Errorf("exporter after uninstall = %+v, want absent", platform.DeviceMetricsExporter)
	}
	if !slices.Equal(platform.MonitoringLinks, wantLinks[:1]) {
		t.Errorf("links after uninstall = %v, want only the operator's %v", platform.MonitoringLinks, wantLinks[:1])
	}
}

func TestParseGPUIdentityNeverReportsAnUnconfirmedIdentityAsDistinct(t *testing.T) {
	for value, want := range map[string]platformdomain.GPUIdentity{
		"distinct": platformdomain.GPUIdentityDistinct, "collapsed": platformdomain.GPUIdentityCollapsed,
		"": platformdomain.GPUIdentityCollapsed, "DISTINCT": platformdomain.GPUIdentityCollapsed,
	} {
		if got := platformdomain.ParseGPUIdentity(value); got != want {
			t.Errorf("ParseGPUIdentity(%q) = %q, want %q", value, got, want)
		}
	}
}
