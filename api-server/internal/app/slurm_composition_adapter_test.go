package app

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	softwareapp "github.com/maple52046/swallow/internal/core/software/application"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	"github.com/maple52046/swallow/internal/core/store/storetest"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// omnistatStateRecorder captures the last fenced Omnistat State write.
type omnistatStateRecorder struct {
	platformID, workflowID string
	state                  platformdomain.OmnistatState
	calls                  int
}

func (r *omnistatStateRecorder) SetHardwareCounterNodes(context.Context, string, string, []platformdomain.HardwareCounterNode, time.Time) (bool, error) {
	return false, nil
}

func (r *omnistatStateRecorder) SetOmnistat(context.Context, string, *platformdomain.Omnistat) error {
	return nil
}

func (r *omnistatStateRecorder) UpdateOmnistatState(
	_ context.Context, platformID, workflowID string, state platformdomain.OmnistatState, _ time.Time,
) (bool, error) {
	r.platformID, r.workflowID, r.state = platformID, workflowID, state
	r.calls++
	return true, nil
}

func omnistatTestTask(status workflowdomain.TaskStatus) workflowdomain.Task {
	return workflowdomain.Task{
		ID: omnistatStepID, Kind: "ansible-playbook", Status: status,
		Completion: []workflowdomain.Completion{featureState(completionOmnistatState, "plat-1", "deploy-1")},
	}
}

// The Omnistat State follows the install-omnistat Task, fenced on the recorded deploy Workflow even
// when a rerun runs the Task under a new Workflow id; a skipped Task (an earlier Job failed) is a
// failed install.
func TestOmnistatObserverProjectsTheTaskOntoTheRecordedWorkflow(t *testing.T) {
	tests := []struct {
		status workflowdomain.TaskStatus
		want   platformdomain.OmnistatState
	}{
		{status: workflowdomain.TaskRunning, want: platformdomain.OmnistatInstalling},
		{status: workflowdomain.TaskSucceeded, want: platformdomain.OmnistatReady},
		{status: workflowdomain.TaskFailed, want: platformdomain.OmnistatFailed},
		{status: workflowdomain.TaskRequiresAttention, want: platformdomain.OmnistatFailed},
		{status: workflowdomain.TaskCanceled, want: platformdomain.OmnistatFailed},
		{status: workflowdomain.TaskSkipped, want: platformdomain.OmnistatFailed},
	}
	for _, tc := range tests {
		t.Run(string(tc.status), func(t *testing.T) {
			recorder := &omnistatStateRecorder{}
			projectWith(t, workflowProjections(nil, nil, nil, recorder, nil), completionOmnistatState, "rerun-2", omnistatTestTask(tc.status))
			if recorder.state != tc.want || recorder.platformID != "plat-1" || recorder.workflowID != "deploy-1" {
				t.Errorf("recorded (%s, %s, %s), want (plat-1, deploy-1, %s)", recorder.platformID, recorder.workflowID, recorder.state, tc.want)
			}
		})
	}
}

func TestOmnistatObserverIgnoresOtherTasksAndPending(t *testing.T) {
	recorder := &omnistatStateRecorder{}
	docker := workflowdomain.Task{ID: "install-docker-ce", Kind: "ansible-playbook", Status: workflowdomain.TaskFailed,
		Parameters: map[string]any{"softwareKind": "docker-ce"}}
	for _, task := range []workflowdomain.Task{docker, omnistatTestTask(workflowdomain.TaskPending)} {
		projectWith(t, workflowProjections(nil, nil, nil, recorder, nil), completionOmnistatState, "deploy-1", task)
	}
	if recorder.calls != 0 {
		t.Errorf("recorder called %d times, want 0", recorder.calls)
	}
}

// A Grafana run that reports no URL fails its Task rather than leave the Platform without its
// link, through the composed Grafana's completion and the one a configure-omnistat Task declares.
func TestPlatformGrafanaResultNeedsTheGrafanaURL(t *testing.T) {
	for _, completion := range []string{completionPlatformGrafanaResult, completionOmnistatResult} {
		result := workflowResults(platformDeploymentObserver{})[completion]
		if err := result.Complete(context.Background(), workflowapp.TaskResult{
			Workflow: &workflowdomain.Workflow{ID: "op-1", PlatformID: "plat-1"},
		}); err == nil {
			t.Errorf("%s without a Grafana URL must fail its Task", completion)
		}
	}
}

// The Platform Grafana reads the Platform metrics gateway at the public URL and provisions the
// basic-monitoring dashboards plus the monitoring feature's; the container method names the
// composed runtime and the native method none.
func TestPlatformGrafanaSpec(t *testing.T) {
	platform := &platformdomain.Platform{ID: "plat-1"}
	container := &platformdomain.MonitoringDashboardsLaunch{ServerID: "s1", InstallMethod: platformdomain.ObservabilityInstallContainer}
	native := &platformdomain.MonitoringDashboardsLaunch{ServerID: "s1", InstallMethod: platformdomain.ObservabilityInstallNative}
	tests := []struct {
		name        string
		launch      platformdomain.DeploymentLaunch
		runtime     softwaredomain.Kind
		wantMethod  string
		wantRuntime any
		wantBundles []string
	}{
		{name: "no feature", launch: platformdomain.DeploymentLaunch{Platform: platform, MonitoringDashboards: container},
			runtime: softwaredomain.KindPodman, wantMethod: "container", wantRuntime: "podman", wantBundles: []string{"node-exporter"}},
		{name: "Omnistat native", launch: platformdomain.DeploymentLaunch{Platform: platform, MonitoringDashboards: native,
			Omnistat: &platformdomain.OmnistatLaunch{ObservabilityServerID: "s1"}},
			wantMethod: "native", wantBundles: []string{"node-exporter", "omnistat"}},
		{name: "Device Metrics Exporter", launch: platformdomain.DeploymentLaunch{Platform: platform, MonitoringDashboards: container,
			DeviceMetricsExporter: &platformdomain.DeviceMetricsExporterLaunch{ObservabilityServerID: "s1"}},
			runtime: softwaredomain.KindDockerCE, wantMethod: "container", wantRuntime: "docker-ce",
			wantBundles: []string{"node-exporter", "device-metrics-exporter"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec := platformGrafanaSpec(tc.launch, tc.runtime, "http://10.170.168.20")
			if spec[softwaredomain.SpecInstallMethod] != tc.wantMethod || spec[softwaredomain.SpecContainerRuntime] != tc.wantRuntime {
				t.Errorf("method %v on %v, want %s on %v", spec[softwaredomain.SpecInstallMethod],
					spec[softwaredomain.SpecContainerRuntime], tc.wantMethod, tc.wantRuntime)
			}
			if got := spec[softwaredomain.SpecPrometheusURL]; got != "http://10.170.168.20/api/v1/platform-metrics/plat-1/prometheus" {
				t.Errorf("prometheusUrl = %v, want the Platform metrics gateway", got)
			}
			if bundles, _ := spec[softwaredomain.SpecDashboards].([]string); !slices.Equal(bundles, tc.wantBundles) {
				t.Errorf("dashboards = %v, want %v", spec[softwaredomain.SpecDashboards], tc.wantBundles)
			}
		})
	}
}

// assignmentList is an AssignmentRepository holding fixed assignments; only reads are used.
type assignmentList []*softwaredomain.Assignment

func (a assignmentList) Upsert(context.Context, *softwaredomain.Assignment) error { return nil }

func (a assignmentList) FindByServerAndKind(_ context.Context, serverID string, kind softwaredomain.Kind) (*softwaredomain.Assignment, error) {
	for _, assignment := range a {
		if assignment.ServerID == serverID && assignment.Kind == kind {
			return assignment, nil
		}
	}
	return nil, softwaredomain.ErrAssignmentNotFound
}

func (a assignmentList) List(_ context.Context, filter softwaredomain.AssignmentFilter) ([]*softwaredomain.Assignment, error) {
	var out []*softwaredomain.Assignment
	for _, assignment := range a {
		if (filter.ServerID == "" || assignment.ServerID == filter.ServerID) && (filter.Kind == "" || assignment.Kind == filter.Kind) {
			out = append(out, assignment)
		}
	}
	return out, nil
}

func (a assignmentList) SetState(context.Context, string, softwaredomain.Kind, softwaredomain.AssignmentState, string, *time.Time) error {
	return nil
}

// The containerRuntime feature names the runtime and its compute and login nodes; a Platform
// Grafana by the container method adds its Server and, without the feature, follows the runtime
// already assigned there (Docker CE when none). A Server whose runtime is installed is skipped.
func TestComposedContainerRuntime(t *testing.T) {
	podmanOnS1 := assignmentList{{ServerID: "s1", Kind: softwaredomain.KindPodman, State: softwaredomain.StateInstalled}}
	dockerOnS2 := assignmentList{{ServerID: "s2", Kind: softwaredomain.KindDockerCE, State: softwaredomain.StateInstalled}}
	container := &platformdomain.MonitoringDashboardsLaunch{ServerID: "s1", InstallMethod: platformdomain.ObservabilityInstallContainer}
	native := &platformdomain.MonitoringDashboardsLaunch{ServerID: "s1", InstallMethod: platformdomain.ObservabilityInstallNative}
	feature := &platformdomain.ContainerRuntimeLaunch{Kind: platformdomain.ContainerRuntimeDockerCE, ServerIDs: []string{"s2", "s3"}}

	tests := []struct {
		name        string
		assignments assignmentList
		launch      platformdomain.DeploymentLaunch
		wantKind    softwaredomain.Kind
		wantServers []string
	}{
		{name: "nothing composed", launch: platformdomain.DeploymentLaunch{MonitoringDashboards: native}},
		{name: "feature only", launch: platformdomain.DeploymentLaunch{ContainerRuntime: feature},
			wantKind: softwaredomain.KindDockerCE, wantServers: []string{"s2", "s3"}},
		{name: "feature skips an installed runtime", assignments: dockerOnS2, launch: platformdomain.DeploymentLaunch{ContainerRuntime: feature},
			wantKind: softwaredomain.KindDockerCE, wantServers: []string{"s3"}},
		{name: "feature with a container Grafana", launch: platformdomain.DeploymentLaunch{ContainerRuntime: feature, MonitoringDashboards: container},
			wantKind: softwaredomain.KindDockerCE, wantServers: []string{"s2", "s3", "s1"}},
		{name: "container Grafana defaults to Docker CE", launch: platformdomain.DeploymentLaunch{MonitoringDashboards: container},
			wantKind: softwaredomain.KindDockerCE, wantServers: []string{"s1"}},
		{name: "container Grafana follows the installed runtime", assignments: podmanOnS1, launch: platformdomain.DeploymentLaunch{MonitoringDashboards: container},
			wantKind: softwaredomain.KindPodman, wantServers: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			launcher := platformDeploymentLauncher{software: softwareapp.NewSoftwareService(&storetest.Transactor{}, testModules(t).software, testModules(t).accelerators, tc.assignments, nil, nil, nil)}
			kind, servers, err := launcher.composedContainerRuntime(context.Background(), tc.launch)
			if err != nil {
				t.Fatalf("composedContainerRuntime() error = %v", err)
			}
			if kind != tc.wantKind || !slices.Equal(servers, tc.wantServers) {
				t.Errorf("composedContainerRuntime() = %q %v, want %q %v", kind, servers, tc.wantKind, tc.wantServers)
			}
		})
	}
}

// exporterStateRecorder captures the last fenced Device Metrics Exporter State write.
type exporterStateRecorder struct {
	platformID, workflowID string
	state                  platformdomain.DeviceMetricsExporterState
	calls                  int
}

func (r *exporterStateRecorder) SetDeviceMetricsExporter(context.Context, string, *platformdomain.DeviceMetricsExporter) error {
	return nil
}

func (r *exporterStateRecorder) UpdateDeviceMetricsExporterState(
	_ context.Context, platformID, workflowID string, state platformdomain.DeviceMetricsExporterState, _ time.Time,
) (bool, error) {
	r.platformID, r.workflowID, r.state = platformID, workflowID, state
	r.calls++
	return true, nil
}

func (r *exporterStateRecorder) UpdateDeviceMetricsExporterGPUIdentity(
	context.Context, string, string, platformdomain.GPUIdentity, time.Time,
) (bool, error) {
	return true, nil
}

// The Device Metrics Exporter State follows its install Task exactly like Omnistat's, fenced on the
// recorded deploy Workflow; other Tasks (including Omnistat's) and pending move nothing.
func TestDeviceMetricsExporterObserverProjectsItsTaskOnly(t *testing.T) {
	task := func(id string, status workflowdomain.TaskStatus) workflowdomain.Task {
		task := workflowdomain.Task{ID: id, Kind: "ansible-playbook", Status: status}
		if id == deviceMetricsExporterStepID {
			task.Completion = []workflowdomain.Completion{featureState(completionDeviceMetricsExporterState, "plat-1", "deploy-1")}
		}
		return task
	}
	tests := []struct {
		status workflowdomain.TaskStatus
		want   platformdomain.DeviceMetricsExporterState
	}{
		{status: workflowdomain.TaskRunning, want: platformdomain.DeviceMetricsExporterInstalling},
		{status: workflowdomain.TaskSucceeded, want: platformdomain.DeviceMetricsExporterReady},
		{status: workflowdomain.TaskFailed, want: platformdomain.DeviceMetricsExporterFailed},
		{status: workflowdomain.TaskSkipped, want: platformdomain.DeviceMetricsExporterFailed},
	}
	for _, tc := range tests {
		recorder := &exporterStateRecorder{}
		projectWith(t, workflowProjections(nil, nil, nil, nil, recorder), completionDeviceMetricsExporterState, "rerun-2",
			task(deviceMetricsExporterStepID, tc.status))
		if recorder.state != tc.want || recorder.platformID != "plat-1" || recorder.workflowID != "deploy-1" {
			t.Errorf("%s: recorded (%s, %s, %s), want (plat-1, deploy-1, %s)", tc.status,
				recorder.platformID, recorder.workflowID, recorder.state, tc.want)
		}
	}
	recorder := &exporterStateRecorder{}
	for _, other := range []workflowdomain.Task{task(omnistatStepID, workflowdomain.TaskFailed),
		task(deviceMetricsExporterStepID, workflowdomain.TaskPending)} {
		projectWith(t, workflowProjections(nil, nil, nil, nil, recorder), completionDeviceMetricsExporterState, "deploy-1", other)
	}
	if recorder.calls != 0 {
		t.Errorf("recorder called %d times for other Tasks and pending, want 0", recorder.calls)
	}
}

// The exporter itself is the composed Managed Software (decision 068), whose install Task id is the
// one the Platform's step observer and completion select; its Grafana dashboards are the
// exporter's bundle.
func TestDeviceMetricsExporterMonitoring(t *testing.T) {
	launch := platformdomain.DeploymentLaunch{DeviceMetricsExporter: &platformdomain.DeviceMetricsExporterLaunch{
		ObservabilityServerID: "s1", GPUComputeServerIDs: []string{"s3"},
		InstallMethod: platformdomain.ObservabilityInstallNative,
	}}
	monitoring := slurmMonitoringOf(launch)
	if monitoring.composedKind != softwaredomain.KindDeviceMetricsExporter || !slices.Equal(monitoring.composedServerIDs, []string{"s3"}) {
		t.Errorf("composed exporter = %s on %v, want device-metrics-exporter on the GPU compute node [s3]",
			monitoring.composedKind, monitoring.composedServerIDs)
	}
	if monitoring.dashboardBundle != "device-metrics-exporter" {
		t.Errorf("dashboard bundle = %q, want device-metrics-exporter", monitoring.dashboardBundle)
	}
	if deviceMetricsExporterStepID != "install-device-metrics-exporter" {
		t.Errorf("deviceMetricsExporterStepID = %q", deviceMetricsExporterStepID)
	}
}

// slurmTestServers is a FindByID-only ServerRepository of deployed Servers at one Site; the ids in
// gpus list AMD compute GPUs.
type slurmTestServers struct {
	serverdomain.ServerRepository
	gpus map[string]bool
}

func (r slurmTestServers) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	server := &serverdomain.Server{
		ID: id, Source: serverdomain.Source{SiteID: "site-1", IntegrationID: "maas-1"},
		Observed:     serverdomain.Observed{Hostname: id},
		Provisioning: &serverdomain.ProvisioningStatus{State: "deployed"},
	}
	if r.gpus[id] {
		server.Observed.GPUs = []serverdomain.GPU{{Vendor: "Advanced Micro Devices, Inc. [AMD/ATI]", Count: 8, Kind: serverdomain.GPUKindCompute}}
	}
	return server, nil
}

// slurmTestCatalog registers every playbook name.
type slurmTestCatalog struct{}

func (slurmTestCatalog) Resolve(name string) (string, error) { return name + ".yml", nil }
func (slurmTestCatalog) FetchesArtifacts(string) bool        { return false }

// slurmCompositionLauncher is a launcher over deployed test Servers (GPUs on gpus) that knows the
// installation's monitoring addresses.
func slurmCompositionLauncher(t *testing.T, gpus map[string]bool) platformDeploymentLauncher {
	t.Helper()
	servers := slurmTestServers{gpus: gpus}
	return platformDeploymentLauncher{
		operations: newTestExecutionService(servers, ntpTestAutomation{}, slurmTestCatalog{}, testModules(t).workflowKinds),
		software:   softwareapp.NewSoftwareService(&storetest.Transactor{}, testModules(t).software, testModules(t).accelerators, assignmentList{}, servers, nil, nil),
		monitoring: platformMonitoringAccess{publicURL: "http://10.170.168.20", scrapeAddresses: []string{"10.170.168.20", "192.168.100.1"}},
	}
}

// The Device Metrics Exporter feature composes the Managed Software device-metrics-exporter after
// the Platform's Grafana (decision 068): its install Task is the one the Platform's observer and
// completion select, carries both the software identity (for the assignments) and the Platform's
// owner and record ids, reaches only the GPU nodes it installs on, and has the Slurm side's
// variables laid over the software install's; a record Task then marks the GPU nodes installed.
func TestSlurmCompositionComposesTheDeviceMetricsExporter(t *testing.T) {
	launcher := slurmCompositionLauncher(t, map[string]bool{"s2": true, "s3": true})
	launch := platformdomain.DeploymentLaunch{
		Platform:        &platformdomain.Platform{ID: "plat-1", Name: "lab", Type: platformdomain.PlatformTypeSlurm},
		TargetServerIDs: []string{"s1", "s2", "s3"},
		RequestedBy:     "admin",
		DeviceMetricsExporter: &platformdomain.DeviceMetricsExporterLaunch{
			ObservabilityServerID: "s1", GPUComputeServerIDs: []string{"s2", "s3"},
			InstallMethod: platformdomain.ObservabilityInstallNative,
			TrustedVars: map[string]any{
				"swallow_device_metrics_exporter_job_attribution": "slurm",
				"swallow_device_metrics_exporter_compute_ids":     []string{"s2", "s3"},
			},
		},
		MonitoringDashboards: &platformdomain.MonitoringDashboardsLaunch{ServerID: "s1", InstallMethod: platformdomain.ObservabilityInstallNative},
	}
	plan, err := launcher.planSlurmComposition(context.Background(), launch, "op-1", false, installPlatformStepID)
	if err != nil {
		t.Fatalf("planSlurmComposition() error = %v", err)
	}
	if got := taskIDs(plan.steps); !slices.Equal(got, []string{
		"install-grafana", "record-grafana", deviceMetricsExporterStepID, "record-device-metrics-exporter",
	}) {
		t.Fatalf("composed Tasks = %v", got)
	}
	install, record := plan.steps[2], plan.steps[3]
	if install.Job != "configure-device-metrics-exporter" || !slices.Equal(install.DependsOn, []string{"record-grafana"}) ||
		!slices.Equal(install.Targets, serverRefs("s2", "s3")) {
		t.Errorf("install Task = Job %s DependsOn %v Targets %v", install.Job, install.DependsOn, install.Targets)
	}
	if install.Parameters["playbook"] != "deploy-device-metrics-exporter" {
		t.Errorf("install Task parameters = %v", install.Parameters)
	}
	wantCompletion := []workflowdomain.Completion{
		softwareFailure(softwaredomain.KindDeviceMetricsExporter, []string{"s2", "s3"}),
		featureState(completionDeviceMetricsExporterState, "plat-1", "op-1"),
		{Type: completionDeviceMetricsExporterResult, Params: map[string]any{completionParamRecord: "op-1"}},
	}
	if !reflect.DeepEqual(install.Completion, wantCompletion) {
		t.Errorf("install Task completion = %v, want %v", install.Completion, wantCompletion)
	}
	vars := extraVars(t, install)
	if vars["swallow_device_metrics_exporter_job_attribution"] != "slurm" || vars["swallow_software_kind"] != string(softwaredomain.KindDeviceMetricsExporter) ||
		vars["swallow_device_metrics_exporter_port"] != softwaredomain.DeviceMetricsExporterPort {
		t.Errorf("install Task extraVars = %v, want the Slurm side over the software install", vars)
	}
	if record.Kind != softwareStepKindRecord || !slices.Equal(record.DependsOn, []string{deviceMetricsExporterStepID}) ||
		!slices.Equal(record.Targets, serverRefs("s2", "s3")) {
		t.Errorf("record Task = %+v", record)
	}
	if last := plan.installs[len(plan.installs)-1]; last.Kind != softwaredomain.KindDeviceMetricsExporter ||
		!slices.Equal(last.ServerIDs, []string{"s2", "s3"}) {
		t.Errorf("last composed install = %s on %v, want the exporter on the GPU nodes", last.Kind, last.ServerIDs)
	}
}

// Without a monitoring feature the Platform still gets its Grafana on the primary controller, after
// the runtime its container method needs: the install Task records the Platform Grafana link, reads
// the gateway, and is the one Task that receives the monitoring token, which no plan field carries.
// No Prometheus is composed.
func TestSlurmCompositionComposesThePlatformGrafana(t *testing.T) {
	launcher := slurmCompositionLauncher(t, nil)
	launch := platformdomain.DeploymentLaunch{
		Platform:             &platformdomain.Platform{ID: "plat-1", Name: "lab", Type: platformdomain.PlatformTypeSlurm},
		TargetServerIDs:      []string{"s1", "s2"},
		RequestedBy:          "admin",
		MonitoringDashboards: &platformdomain.MonitoringDashboardsLaunch{ServerID: "s1", InstallMethod: platformdomain.ObservabilityInstallContainer},
	}
	plan, err := launcher.planSlurmComposition(context.Background(), launch, "op-1", false, installPlatformStepID)
	if err != nil {
		t.Fatalf("planSlurmComposition() error = %v", err)
	}
	if got := taskIDs(plan.steps); !slices.Equal(got, []string{"install-docker-ce", "record-docker-ce", "install-grafana", "record-grafana"}) {
		t.Fatalf("composed Tasks = %v", got)
	}
	grafana := plan.steps[2]
	if !slices.Equal(grafana.Targets, serverRefs("s1")) || !slices.Equal(grafana.DependsOn, []string{"record-docker-ce"}) {
		t.Errorf("Grafana Task targets %v depends on %v", grafana.Targets, grafana.DependsOn)
	}
	if !slices.ContainsFunc(grafana.Completion, func(c workflowdomain.Completion) bool { return c.Type == completionPlatformGrafanaResult }) {
		t.Errorf("Grafana Task completion = %v, want %s", grafana.Completion, completionPlatformGrafanaResult)
	}
	vars := extraVars(t, grafana)
	if vars["swallow_grafana_prometheus_url"] != "http://10.170.168.20/api/v1/platform-metrics/plat-1/prometheus" {
		t.Errorf("swallow_grafana_prometheus_url = %v, want the gateway", vars["swallow_grafana_prometheus_url"])
	}
	if dashboards, _ := vars["swallow_grafana_dashboards"].([]string); !slices.Equal(dashboards, []string{"node-exporter"}) {
		t.Errorf("swallow_grafana_dashboards = %v, want [node-exporter]", vars["swallow_grafana_dashboards"])
	}
	if _, leaked := vars[grafanaDatasourceTokenVar]; leaked {
		t.Error("the monitoring token must not be a trusted variable")
	}
	if !slices.Equal(plan.tokenSteps, []string{"install-grafana"}) {
		t.Errorf("token steps = %v, want [install-grafana]", plan.tokenSteps)
	}
	for _, install := range plan.installs {
		if install.Kind == softwaredomain.KindPrometheus {
			t.Error("a Slurm deployment must not compose Prometheus")
		}
	}

	launch.MonitoringDashboards = nil
	if plan, err = launcher.planSlurmComposition(context.Background(), launch, "op-1", false, installPlatformStepID); err != nil || len(plan.steps) != 0 {
		t.Errorf("without dashboards: steps %v, err %v; want none", taskIDs(plan.steps), err)
	}
}

// Omnistat's exporter admits the node itself and the Site Prometheus's scrape addresses; its Task
// records only its State, because the Platform Grafana's own install records the link.
func TestSlurmCompositionComposesOmnistat(t *testing.T) {
	launcher := slurmCompositionLauncher(t, nil)
	launch := platformdomain.DeploymentLaunch{
		Platform:        &platformdomain.Platform{ID: "plat-1", Name: "lab", Type: platformdomain.PlatformTypeSlurm},
		TargetServerIDs: []string{"s1", "s2"},
		RequestedBy:     "admin",
		Omnistat: &platformdomain.OmnistatLaunch{
			ObservabilityServerID: "s1", ComputeServerIDs: []string{"s2"}, InstallMethod: platformdomain.ObservabilityInstallNative,
			HardwareCounters: true, TrustedVars: map[string]any{"swallow_omnistat_compute_ids": []string{"s2"}},
		},
		MonitoringDashboards: &platformdomain.MonitoringDashboardsLaunch{ServerID: "s1", InstallMethod: platformdomain.ObservabilityInstallNative},
	}
	plan, err := launcher.planSlurmComposition(context.Background(), launch, "op-1", false, installPlatformStepID)
	if err != nil {
		t.Fatalf("planSlurmComposition() error = %v", err)
	}
	if got := taskIDs(plan.steps); !slices.Equal(got, []string{"install-grafana", "record-grafana", omnistatStepID}) {
		t.Fatalf("composed Tasks = %v", got)
	}
	omnistat := plan.steps[2]
	want := []workflowdomain.Completion{
		featureState(completionOmnistatState, "plat-1", "op-1"),
		{Type: completionOmnistatHardwareCounters, Params: map[string]any{completionParamRecord: "op-1"}},
	}
	if !reflect.DeepEqual(omnistat.Completion, want) {
		t.Errorf("Omnistat Task completion = %v, want %v", omnistat.Completion, want)
	}
	vars := extraVars(t, omnistat)
	if vars[omnistatHardwareCountersVar] != true {
		t.Errorf("%s = %v, want the deployment's request (true)", omnistatHardwareCountersVar, vars[omnistatHardwareCountersVar])
	}
	if _, shared := launch.Omnistat.TrustedVars[omnistatHardwareCountersVar]; shared {
		t.Errorf("composing the Task wrote %s into the launch's own variables", omnistatHardwareCountersVar)
	}
	if addresses, _ := vars[omnistatScrapeAddressesVar].([]string); !slices.Equal(addresses, []string{"10.170.168.20", "192.168.100.1"}) {
		t.Errorf("%s = %v", omnistatScrapeAddressesVar, vars[omnistatScrapeAddressesVar])
	}
	if vars["swallow_omnistat_grafana_port"] != softwaredomain.GrafanaPort {
		t.Errorf("swallow_omnistat_grafana_port = %v", vars["swallow_omnistat_grafana_port"])
	}
	if !slices.Equal(omnistat.Targets, serverRefs("s1", "s2")) {
		t.Errorf("Omnistat Task targets = %v, want every target", omnistat.Targets)
	}
}

// A hardware counters change on a deployed Slurm Platform is its own configure-omnistat Workflow:
// SSH readiness, then the Omnistat Task a deployment composes, with the same variables and
// completions fenced on this Workflow. It composes no Grafana, runtime, or install of the Platform,
// and only a Slurm Platform with Omnistat has one.
func TestAHardwareCountersChangeRunsOnlyOmnistat(t *testing.T) {
	launcher := slurmCompositionLauncher(t, nil)
	launch := platformdomain.DeploymentLaunch{
		Platform:        &platformdomain.Platform{ID: "plat-1", Name: "lab", Type: platformdomain.PlatformTypeSlurm},
		OperationID:     "op-2",
		TargetServerIDs: []string{"s1", "s2"},
		RequestedBy:     "platform-controller",
		Realization:     platformdomain.RealizeHardwareCounters,
		ContainerRuntime: &platformdomain.ContainerRuntimeLaunch{
			Kind: platformdomain.ContainerRuntimeDockerCE, ServerIDs: []string{"s2"},
		},
		Omnistat: &platformdomain.OmnistatLaunch{
			ObservabilityServerID: "s1", ComputeServerIDs: []string{"s2"}, InstallMethod: platformdomain.ObservabilityInstallNative,
			HardwareCounters: true, TrustedVars: map[string]any{"swallow_omnistat_compute_ids": []string{"s2"}},
		},
		MonitoringDashboards: &platformdomain.MonitoringDashboardsLaunch{ServerID: "s1", InstallMethod: platformdomain.ObservabilityInstallNative},
	}
	prepared, err := launcher.PrepareDeployment(context.Background(), launch)
	if err != nil {
		t.Fatalf("PrepareDeployment() error = %v", err)
	}
	plan := prepared.(*preparedPlatformDeployment)
	workflow := plan.workflow
	if workflow.ID != "op-2" || workflow.Kind != workflowdomain.WorkflowKindConfigureOmnistat ||
		workflow.IntentSummary != "Enable Omnistat hardware counters on lab" || workflow.PlatformID != "plat-1" {
		t.Errorf("workflow = id %s kind %s summary %q platform %s", workflow.ID, workflow.Kind, workflow.IntentSummary, workflow.PlatformID)
	}
	if got := taskIDs(workflow.Steps); !slices.Equal(got, []string{softwareWaitStepID, omnistatStepID}) {
		t.Fatalf("Tasks = %v, want SSH readiness then Omnistat only", got)
	}
	if len(plan.composed.installs) != 0 || len(plan.composed.tokenSteps) != 0 {
		t.Errorf("composed installs %v token steps %v, want none", plan.composed.installs, plan.composed.tokenSteps)
	}
	omnistat := workflow.Steps[1]
	if omnistat.Job != jobConfigureOmnistat || !slices.Equal(omnistat.DependsOn, []string{softwareWaitStepID}) ||
		!slices.Equal(omnistat.Targets, serverRefs("s1", "s2")) {
		t.Errorf("Omnistat Task = job %s depends on %v targets %v", omnistat.Job, omnistat.DependsOn, omnistat.Targets)
	}
	want := []workflowdomain.Completion{
		featureState(completionOmnistatState, "plat-1", "op-2"),
		{Type: completionOmnistatHardwareCounters, Params: map[string]any{completionParamRecord: "op-2"}},
	}
	if !reflect.DeepEqual(omnistat.Completion, want) {
		t.Errorf("Omnistat Task completion = %v, want %v", omnistat.Completion, want)
	}
	vars := extraVars(t, omnistat)
	if vars[omnistatHardwareCountersVar] != true || vars["swallow_omnistat_grafana_port"] != softwaredomain.GrafanaPort {
		t.Errorf("extraVars = %v, want the counters on and the Grafana port", vars)
	}
	if addresses, _ := vars[omnistatScrapeAddressesVar].([]string); !slices.Equal(addresses, []string{"10.170.168.20", "192.168.100.1"}) {
		t.Errorf("%s = %v", omnistatScrapeAddressesVar, vars[omnistatScrapeAddressesVar])
	}

	launch.Omnistat.HardwareCounters = false
	prepared, err = launcher.PrepareDeployment(context.Background(), launch)
	if err != nil {
		t.Fatalf("PrepareDeployment(off) error = %v", err)
	}
	off := prepared.(*preparedPlatformDeployment).workflow
	if off.IntentSummary != "Disable Omnistat hardware counters on lab" || extraVars(t, off.Steps[1])[omnistatHardwareCountersVar] != false {
		t.Errorf("turning the counters off = %q with %v", off.IntentSummary, extraVars(t, off.Steps[1])[omnistatHardwareCountersVar])
	}

	withoutOmnistat := launch
	withoutOmnistat.Omnistat = nil
	if _, err := launcher.PrepareDeployment(context.Background(), withoutOmnistat); !errors.Is(err, platformdomain.ErrInvalidOmnistat) {
		t.Errorf("without Omnistat: err = %v, want ErrInvalidOmnistat", err)
	}
	kubernetes := launch
	kubernetes.Platform = &platformdomain.Platform{ID: "plat-2", Name: "k0s", Type: platformdomain.PlatformTypeKubernetes}
	if _, err := launcher.PrepareDeployment(context.Background(), kubernetes); !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Errorf("Kubernetes Platform: err = %v, want ErrInvalidDeployment", err)
	}
	unknown := launch
	unknown.Realization = platformdomain.RealizeGPUOperator
	if _, err := launcher.PrepareDeployment(context.Background(), unknown); err == nil {
		t.Error("a launch for another realization must be refused")
	}
}

// A configure-omnistat run's hardwareCounters.nodes are read as the Platform records them; a node
// the playbook reported in a shape swallow does not know (an unknown reason, a reason while
// collecting, no serverId) is left out instead of failing the Task, and a run that asked for no
// counters reports no nodes. The completion is registered as a result.
func TestOmnistatHardwareCountersResultReadsTheNodes(t *testing.T) {
	if completionClasses()[completionOmnistatHardwareCounters] != workflowdomain.CompletionResult {
		t.Fatalf("%s is not a result completion", completionOmnistatHardwareCounters)
	}
	if _, ok := workflowResults(platformDeploymentObserver{})[completionOmnistatHardwareCounters]; !ok {
		t.Fatalf("no result handler for %s", completionOmnistatHardwareCounters)
	}
	nodes := hardwareCounterNodesOf("plat-1", "deploy-1", map[string]any{
		"grafanaUrl": "http://10.0.0.1:3000",
		"hardwareCounters": map[string]any{"nodes": []any{
			map[string]any{"serverId": "s3", "state": "unavailable", "reason": "rocm_unsupported"},
			map[string]any{"serverId": "s2", "state": "collecting", "reason": ""},
			map[string]any{"serverId": "s4", "state": "unavailable", "reason": "cosmic_rays"},
			map[string]any{"serverId": "s5", "state": "collecting", "reason": "probe_failed"},
			map[string]any{"state": "collecting", "reason": ""},
		}},
	})
	want := []platformdomain.HardwareCounterNode{
		{ServerID: "s3", State: platformdomain.HardwareCountersUnavailable, Reason: "rocm_unsupported"},
		{ServerID: "s2", State: platformdomain.HardwareCountersCollecting},
	}
	if !reflect.DeepEqual(nodes, want) {
		t.Errorf("hardwareCounterNodesOf() = %v, want %v", nodes, want)
	}
	if nodes := hardwareCounterNodesOf("plat-1", "deploy-1", map[string]any{"grafanaUrl": "http://10.0.0.1:3000"}); len(nodes) != 0 {
		t.Errorf("a run without counters reported %v, want no nodes", nodes)
	}
}

// An installation without the address a Job needs refuses the deployment when it is planned and
// names the setting.
func TestSlurmCompositionNeedsTheMonitoringAddresses(t *testing.T) {
	platform := &platformdomain.Platform{ID: "plat-1", Name: "lab", Type: platformdomain.PlatformTypeSlurm}
	dashboards := &platformdomain.MonitoringDashboardsLaunch{ServerID: "s1", InstallMethod: platformdomain.ObservabilityInstallNative}
	omnistat := &platformdomain.OmnistatLaunch{ObservabilityServerID: "s1", ComputeServerIDs: []string{"s2"}}
	tests := []struct {
		name    string
		access  platformMonitoringAccess
		launch  platformdomain.DeploymentLaunch
		setting string
	}{
		{name: "Grafana without the public URL", access: platformMonitoringAccess{scrapeAddresses: []string{"10.0.0.1"}},
			launch: platformdomain.DeploymentLaunch{Platform: platform, MonitoringDashboards: dashboards}, setting: "SWALLOW_API_PUBLIC_URL"},
		{name: "Omnistat without scrape addresses", access: platformMonitoringAccess{publicURL: "http://10.0.0.1"},
			launch: platformdomain.DeploymentLaunch{Platform: platform, Omnistat: omnistat}, setting: "SWALLOW_API_MONITORING_SCRAPE_ADDRESSES"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			launcher := slurmCompositionLauncher(t, nil)
			launcher.monitoring = tc.access
			_, err := launcher.planSlurmComposition(context.Background(), tc.launch, "op-1", false, installPlatformStepID)
			if !errors.Is(err, platformdomain.ErrMonitoringEndpointMissing) || !strings.Contains(err.Error(), tc.setting) {
				t.Errorf("planSlurmComposition() error = %v, want ErrMonitoringEndpointMissing naming %s", err, tc.setting)
			}
		})
	}
}

// failingTokenIssuer records the Platform it was asked for and refuses.
type failingTokenIssuer struct{ platformIDs *[]string }

func (f failingTokenIssuer) Issue(_ context.Context, platformID string) (string, error) {
	*f.platformIDs = append(*f.platformIDs, platformID)
	return "", errors.New("token store unavailable")
}

// Persisting a deployment that installs the Platform's Grafana issues the Platform's token first; a
// failure fails the persist, and the token lands only in the named Tasks' sealed secrets, without
// changing the prepared plan.
func TestPersistDeploymentIssuesThePlatformToken(t *testing.T) {
	var asked []string
	launcher := platformDeploymentLauncher{monitoringTokens: failingTokenIssuer{platformIDs: &asked}}
	prepared := &preparedPlatformDeployment{
		workflow: workflowapp.CreateWorkflowInput{ID: "op-1", PlatformID: "plat-1"},
		composed: composedPlan{tokenSteps: []string{"install-grafana"}},
	}
	if _, err := launcher.PersistDeployment(context.Background(), prepared); err == nil || !slices.Equal(asked, []string{"plat-1"}) {
		t.Errorf("PersistDeployment() error = %v, issued for %v; want the issuer's failure for plat-1", err, asked)
	}

	plan := workflowapp.CreateWorkflowInput{StepSecretValues: map[string]map[string]any{"install-platform": {"swallow_k0s_vrrp_auth_pass": "x"}}}
	sealed := withPlatformToken(plan, []string{"install-grafana"}, "swpm_token")
	if sealed.StepSecretValues["install-grafana"][grafanaDatasourceTokenVar] != "swpm_token" ||
		sealed.StepSecretValues["install-platform"]["swallow_k0s_vrrp_auth_pass"] != "x" {
		t.Errorf("sealed secrets = %v", sealed.StepSecretValues)
	}
	if _, changed := plan.StepSecretValues["install-grafana"]; changed {
		t.Error("withPlatformToken changed the prepared plan")
	}
}

// Both Device Metrics Exporters are the same software: the platform domain's constants (the Slurm
// feature) and the software domain's (the Managed Software kind) must agree.
func TestDeviceMetricsExporterConstantsAgree(t *testing.T) {
	if platformdomain.DeviceMetricsExporterVersion != softwaredomain.DeviceMetricsExporterVersion ||
		platformdomain.DeviceMetricsExporterPort != softwaredomain.DeviceMetricsExporterPort {
		t.Errorf("platform %s:%d, software %s:%d", platformdomain.DeviceMetricsExporterVersion, platformdomain.DeviceMetricsExporterPort,
			softwaredomain.DeviceMetricsExporterVersion, softwaredomain.DeviceMetricsExporterPort)
	}
}
