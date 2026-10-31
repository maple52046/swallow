package app

import (
	"context"
	"reflect"
	"slices"
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

// Inside a configure-slurm Workflow every ansible execution carries the Workflow's kind. The
// composed software Tasks must not run the Slurm credential completion, and the Omnistat Task
// must fail without a Grafana URL rather than leave the Platform without its link.
func TestOmnistatResultNeedsTheGrafanaURL(t *testing.T) {
	result := workflowResults(platformDeploymentObserver{})[completionOmnistatResult]
	if err := result.Complete(context.Background(), workflowapp.TaskResult{
		Workflow: &workflowdomain.Workflow{ID: "op-1", PlatformID: "plat-1"},
	}); err == nil {
		t.Error("an Omnistat run without a Grafana URL must fail its Task")
	}
}

// Prometheus scrapes the compute nodes by Server id on the exporter port, so the addresses are
// resolved when the Task runs; Grafana provisions the Omnistat dashboards. Both carry Omnistat's
// install method, and the container method names the composed runtime.
func TestOmnistatSoftwareSpecs(t *testing.T) {
	omnistat := &platformdomain.OmnistatLaunch{
		ObservabilityServerID: "s1", ComputeServerIDs: []string{"s2", "s3"},
		InstallMethod: platformdomain.ObservabilityInstallContainer,
	}

	prometheus := monitoringSoftwareSpec(softwaredomain.KindPrometheus,
		slurmMonitoringOf(platformdomain.DeploymentLaunch{Omnistat: omnistat}), softwaredomain.KindPodman)
	if prometheus[softwaredomain.SpecScrapeJob] != platformdomain.OmnistatScrapeJob ||
		prometheus[softwaredomain.SpecScrapePort] != platformdomain.OmnistatExporterPort ||
		prometheus[softwaredomain.SpecInstallMethod] != "container" || prometheus[softwaredomain.SpecContainerRuntime] != "podman" ||
		prometheus[softwaredomain.SpecListen] != "loopback" {
		t.Errorf("prometheus spec = %v", prometheus)
	}
	if ids, _ := prometheus[softwaredomain.SpecScrapeServerIDs].([]string); !slices.Equal(ids, []string{"s2", "s3"}) {
		t.Errorf("scrapeServerIds = %v, want [s2 s3]", prometheus[softwaredomain.SpecScrapeServerIDs])
	}

	omnistat.InstallMethod = platformdomain.ObservabilityInstallNative
	grafana := monitoringSoftwareSpec(softwaredomain.KindGrafana, slurmMonitoringOf(platformdomain.DeploymentLaunch{Omnistat: omnistat}), "")
	if bundles, _ := grafana[softwaredomain.SpecDashboards].([]string); !slices.Equal(bundles, []string{"omnistat"}) {
		t.Errorf("grafana dashboards = %v, want [omnistat]", grafana[softwaredomain.SpecDashboards])
	}
	if _, present := grafana[softwaredomain.SpecContainerRuntime]; present || grafana[softwaredomain.SpecInstallMethod] != "native" {
		t.Errorf("grafana spec = %v, want native without a runtime", grafana)
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

func (a assignmentList) ListAll(context.Context) ([]*softwaredomain.Assignment, error) { return a, nil }

// The containerRuntime feature names the runtime and its compute and login nodes; Omnistat's
// container method adds the observability Server and, without the feature, follows the runtime
// already assigned there (Docker CE when none). A Server whose runtime is installed is skipped.
func TestComposedContainerRuntime(t *testing.T) {
	podmanOnS1 := assignmentList{{ServerID: "s1", Kind: softwaredomain.KindPodman, State: softwaredomain.StateInstalled}}
	dockerOnS2 := assignmentList{{ServerID: "s2", Kind: softwaredomain.KindDockerCE, State: softwaredomain.StateInstalled}}
	container := &platformdomain.OmnistatLaunch{ObservabilityServerID: "s1", InstallMethod: platformdomain.ObservabilityInstallContainer}
	native := &platformdomain.OmnistatLaunch{ObservabilityServerID: "s1", InstallMethod: platformdomain.ObservabilityInstallNative}
	feature := &platformdomain.ContainerRuntimeLaunch{Kind: platformdomain.ContainerRuntimeDockerCE, ServerIDs: []string{"s2", "s3"}}

	tests := []struct {
		name        string
		assignments assignmentList
		launch      platformdomain.DeploymentLaunch
		wantKind    softwaredomain.Kind
		wantServers []string
	}{
		{name: "nothing composed", launch: platformdomain.DeploymentLaunch{Omnistat: native}},
		{name: "feature only", launch: platformdomain.DeploymentLaunch{ContainerRuntime: feature},
			wantKind: softwaredomain.KindDockerCE, wantServers: []string{"s2", "s3"}},
		{name: "feature skips an installed runtime", assignments: dockerOnS2, launch: platformdomain.DeploymentLaunch{ContainerRuntime: feature},
			wantKind: softwaredomain.KindDockerCE, wantServers: []string{"s3"}},
		{name: "feature with Omnistat container", launch: platformdomain.DeploymentLaunch{ContainerRuntime: feature, Omnistat: container},
			wantKind: softwaredomain.KindDockerCE, wantServers: []string{"s2", "s3", "s1"}},
		{name: "Omnistat container defaults to Docker CE", launch: platformdomain.DeploymentLaunch{Omnistat: container},
			wantKind: softwaredomain.KindDockerCE, wantServers: []string{"s1"}},
		{name: "Omnistat container follows the installed runtime", assignments: podmanOnS1, launch: platformdomain.DeploymentLaunch{Omnistat: container},
			wantKind: softwaredomain.KindPodman, wantServers: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			launcher := platformDeploymentLauncher{software: softwareapp.NewSoftwareService(&storetest.Transactor{}, tc.assignments, nil, nil)}
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

// The exporter's Prometheus scrapes the GPU compute nodes on port 5001 (never the RDC exporter's
// 5000) under its own job, and Grafana provisions the exporter's dashboards.
func TestDeviceMetricsExporterSoftwareSpecs(t *testing.T) {
	launch := platformdomain.DeploymentLaunch{DeviceMetricsExporter: &platformdomain.DeviceMetricsExporterLaunch{
		ObservabilityServerID: "s1", GPUComputeServerIDs: []string{"s3"},
		InstallMethod: platformdomain.ObservabilityInstallNative,
	}}
	monitoring := slurmMonitoringOf(launch)
	prometheus := monitoringSoftwareSpec(softwaredomain.KindPrometheus, monitoring, "")
	if prometheus[softwaredomain.SpecScrapeJob] != "device-metrics-exporter" || prometheus[softwaredomain.SpecScrapePort] != 5001 ||
		prometheus[softwaredomain.SpecInstallMethod] != "native" || prometheus[softwaredomain.SpecListen] != "loopback" {
		t.Errorf("prometheus spec = %v", prometheus)
	}
	if ids, _ := prometheus[softwaredomain.SpecScrapeServerIDs].([]string); !slices.Equal(ids, []string{"s3"}) {
		t.Errorf("scrapeServerIds = %v, want the GPU compute node [s3]", prometheus[softwaredomain.SpecScrapeServerIDs])
	}
	grafana := monitoringSoftwareSpec(softwaredomain.KindGrafana, monitoring, "")
	if bundles, _ := grafana[softwaredomain.SpecDashboards].([]string); !slices.Equal(bundles, []string{"device-metrics-exporter"}) {
		t.Errorf("grafana dashboards = %v, want [device-metrics-exporter]", grafana[softwaredomain.SpecDashboards])
	}
	// The exporter itself is the composed Managed Software (decision 068), whose install Task id is
	// the one the Platform's step observer and completion select.
	if monitoring.composedKind != softwaredomain.KindDeviceMetricsExporter || !slices.Equal(monitoring.composedServerIDs, []string{"s3"}) {
		t.Errorf("composed exporter = %s on %v, want device-metrics-exporter on the GPU compute node [s3]",
			monitoring.composedKind, monitoring.composedServerIDs)
	}
	if deviceMetricsExporterStepID != "install-device-metrics-exporter" {
		t.Errorf("deviceMetricsExporterStepID = %q", deviceMetricsExporterStepID)
	}
}

// The exporter's container method composes the observability Server's runtime like Omnistat's.
func TestComposedContainerRuntimeForTheDeviceMetricsExporter(t *testing.T) {
	launcher := platformDeploymentLauncher{software: softwareapp.NewSoftwareService(&storetest.Transactor{}, assignmentList{}, nil, nil)}
	launch := platformdomain.DeploymentLaunch{DeviceMetricsExporter: &platformdomain.DeviceMetricsExporterLaunch{
		ObservabilityServerID: "s1", InstallMethod: platformdomain.ObservabilityInstallContainer,
	}}
	kind, servers, err := launcher.composedContainerRuntime(context.Background(), launch)
	if err != nil || kind != softwaredomain.KindDockerCE || !slices.Equal(servers, []string{"s1"}) {
		t.Errorf("composedContainerRuntime() = %q %v %v, want docker-ce [s1]", kind, servers, err)
	}
}

// The exporter's Task must report a Grafana URL; without one it fails instead of leaving the
// Platform without its link.
func TestDeviceMetricsExporterCompletionNeedsTheGrafanaURL(t *testing.T) {
	result := workflowResults(platformDeploymentObserver{})[completionDeviceMetricsExporterResult]
	if err := result.Complete(context.Background(), workflowapp.TaskResult{
		Workflow: &workflowdomain.Workflow{ID: "op-1", PlatformID: "plat-1"},
	}); err == nil {
		t.Error("a Device Metrics Exporter run without a Grafana URL must fail its Task")
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

// The Device Metrics Exporter feature composes the Managed Software device-metrics-exporter after
// Prometheus and Grafana (decision 068): its install Task is the one the Platform's observer and
// completion select, carries both the software identity (for the assignments) and the Platform's
// owner and record ids, reaches every target for the scrape check, and has the Slurm side's
// variables laid over the software install's; a record Task then marks the GPU nodes installed.
func TestSlurmCompositionComposesTheDeviceMetricsExporter(t *testing.T) {
	servers := slurmTestServers{gpus: map[string]bool{"s2": true, "s3": true}}
	launcher := platformDeploymentLauncher{
		operations: workflowapp.NewExecutionService(nil, servers, ntpTestAutomation{}, slurmTestCatalog{}, nil, nil, nil),
		software:   softwareapp.NewSoftwareService(&storetest.Transactor{}, assignmentList{}, servers, nil),
	}
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
				"swallow_device_metrics_exporter_record_id":       "op-1",
			},
		},
	}
	plan, err := launcher.planSlurmComposition(context.Background(), launch, "op-1", false)
	if err != nil {
		t.Fatalf("planSlurmComposition() error = %v", err)
	}
	if got := taskIDs(plan.steps); !slices.Equal(got, []string{
		"install-prometheus", "record-prometheus", "install-grafana", "record-grafana",
		deviceMetricsExporterStepID, "record-device-metrics-exporter",
	}) {
		t.Fatalf("composed Tasks = %v", got)
	}
	install, record := plan.steps[4], plan.steps[5]
	if install.Job != "configure-device-metrics-exporter" || !slices.Equal(install.DependsOn, []string{"record-grafana"}) ||
		!slices.Equal(install.Targets, serverRefs("s1", "s2", "s3")) {
		t.Errorf("install Task = Job %s DependsOn %v Targets %v", install.Job, install.DependsOn, install.Targets)
	}
	if install.Parameters["playbook"] != "deploy-device-metrics-exporter" {
		t.Errorf("install Task parameters = %v", install.Parameters)
	}
	wantCompletion := []workflowdomain.Completion{
		softwareFailure(softwaredomain.KindDeviceMetricsExporter, []string{"s2", "s3"}),
		featureState(completionDeviceMetricsExporterState, "plat-1", "op-1"),
		{Type: completionDeviceMetricsExporterResult},
	}
	if !reflect.DeepEqual(install.Completion, wantCompletion) {
		t.Errorf("install Task completion = %v, want %v", install.Completion, wantCompletion)
	}
	vars := extraVars(t, install)
	if vars["swallow_device_metrics_exporter_job_attribution"] != "slurm" || vars["swallow_software_kind"] != string(softwaredomain.KindDeviceMetricsExporter) ||
		vars["swallow_device_metrics_exporter_port"] != softwaredomain.DeviceMetricsExporterPort ||
		vars["swallow_device_metrics_exporter_grafana_port"] != softwaredomain.GrafanaPort ||
		vars["swallow_device_metrics_exporter_record_id"] != "op-1" {
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

// Both Device Metrics Exporters are the same software: the platform domain's constants (the Slurm
// feature) and the software domain's (the Managed Software kind) must agree.
func TestDeviceMetricsExporterConstantsAgree(t *testing.T) {
	if platformdomain.DeviceMetricsExporterVersion != softwaredomain.DeviceMetricsExporterVersion ||
		platformdomain.DeviceMetricsExporterPort != softwaredomain.DeviceMetricsExporterPort {
		t.Errorf("platform %s:%d, software %s:%d", platformdomain.DeviceMetricsExporterVersion, platformdomain.DeviceMetricsExporterPort,
			softwaredomain.DeviceMetricsExporterVersion, softwaredomain.DeviceMetricsExporterPort)
	}
}
