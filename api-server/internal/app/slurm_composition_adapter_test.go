package app

import (
	"context"
	"slices"
	"testing"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	softwareapp "github.com/maple52046/swallow/internal/software/application"
	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
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

func omnistatTestTask(status operationdomain.TaskStatus) operationdomain.Task {
	return operationdomain.Task{
		ID: omnistatStepID, Kind: "ansible-playbook", Status: status,
		Parameters: map[string]any{omnistatStepParamOwner: "plat-1", omnistatStepParamRecord: "deploy-1"},
	}
}

// The Omnistat State follows the install-omnistat Task, fenced on the recorded deploy Workflow even
// when a rerun runs the Task under a new Workflow id; a skipped Task (an earlier Job failed) is a
// failed install.
func TestOmnistatObserverProjectsTheTaskOntoTheRecordedWorkflow(t *testing.T) {
	tests := []struct {
		status operationdomain.TaskStatus
		want   platformdomain.OmnistatState
	}{
		{status: operationdomain.TaskRunning, want: platformdomain.OmnistatInstalling},
		{status: operationdomain.TaskSucceeded, want: platformdomain.OmnistatReady},
		{status: operationdomain.TaskFailed, want: platformdomain.OmnistatFailed},
		{status: operationdomain.TaskRequiresAttention, want: platformdomain.OmnistatFailed},
		{status: operationdomain.TaskCanceled, want: platformdomain.OmnistatFailed},
		{status: operationdomain.TaskSkipped, want: platformdomain.OmnistatFailed},
	}
	for _, tc := range tests {
		t.Run(string(tc.status), func(t *testing.T) {
			recorder := &omnistatStateRecorder{}
			observer := omnistatStepObserver{recorder: recorder}
			if err := observer.ObserveStep(context.Background(), "rerun-2", omnistatTestTask(tc.status)); err != nil {
				t.Fatalf("ObserveStep: %v", err)
			}
			if recorder.state != tc.want || recorder.platformID != "plat-1" || recorder.workflowID != "deploy-1" {
				t.Errorf("recorded (%s, %s, %s), want (plat-1, deploy-1, %s)", recorder.platformID, recorder.workflowID, recorder.state, tc.want)
			}
		})
	}
}

func TestOmnistatObserverIgnoresOtherTasksAndPending(t *testing.T) {
	recorder := &omnistatStateRecorder{}
	observer := omnistatStepObserver{recorder: recorder}
	docker := operationdomain.Task{ID: "install-docker-ce", Kind: "ansible-playbook", Status: operationdomain.TaskFailed,
		Parameters: map[string]any{"softwareKind": "docker-ce"}}
	for _, task := range []operationdomain.Task{docker, omnistatTestTask(operationdomain.TaskPending)} {
		if err := observer.ObserveStep(context.Background(), "deploy-1", task); err != nil {
			t.Fatalf("ObserveStep(%s): %v", task.ID, err)
		}
	}
	if recorder.calls != 0 {
		t.Errorf("recorder called %d times, want 0", recorder.calls)
	}
}

// Inside a configure-slurm Workflow every ansible execution carries the Workflow's kind. The
// composed software Tasks must not run the Slurm credential completion, and the Omnistat Task
// must fail without a Grafana URL rather than leave the Platform without its link.
func TestPlatformDeploymentObserverRoutesSlurmTasksByID(t *testing.T) {
	observer := platformDeploymentObserver{}
	for _, stepID := range []string{"install-docker-ce", "install-prometheus", "install-grafana"} {
		execution := &operationdomain.AnsibleExecution{
			OperationID: "op-1", Kind: operationdomain.WorkflowKindConfigureSlurm, PlatformID: "plat-1", StepID: stepID,
		}
		if err := observer.AnsibleStepSucceeded(context.Background(), execution, operationdomain.RunnerResult{}); err != nil {
			t.Errorf("AnsibleStepSucceeded(%s) = %v, want nil", stepID, err)
		}
	}
	execution := &operationdomain.AnsibleExecution{
		OperationID: "op-1", Kind: operationdomain.WorkflowKindConfigureSlurm, PlatformID: "plat-1", StepID: omnistatStepID,
	}
	if err := observer.AnsibleStepSucceeded(context.Background(), execution, operationdomain.RunnerResult{}); err == nil {
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
			launcher := platformDeploymentLauncher{software: softwareapp.NewSoftwareService(tc.assignments, nil, nil)}
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
	task := func(id string, status operationdomain.TaskStatus) operationdomain.Task {
		return operationdomain.Task{ID: id, Kind: "ansible-playbook", Status: status, Parameters: map[string]any{
			deviceMetricsExporterStepParamOwner: "plat-1", deviceMetricsExporterStepParamRecord: "deploy-1",
			omnistatStepParamOwner: "plat-1", omnistatStepParamRecord: "deploy-1",
		}}
	}
	tests := []struct {
		status operationdomain.TaskStatus
		want   platformdomain.DeviceMetricsExporterState
	}{
		{status: operationdomain.TaskRunning, want: platformdomain.DeviceMetricsExporterInstalling},
		{status: operationdomain.TaskSucceeded, want: platformdomain.DeviceMetricsExporterReady},
		{status: operationdomain.TaskFailed, want: platformdomain.DeviceMetricsExporterFailed},
		{status: operationdomain.TaskSkipped, want: platformdomain.DeviceMetricsExporterFailed},
	}
	for _, tc := range tests {
		recorder := &exporterStateRecorder{}
		observer := deviceMetricsExporterStepObserver{recorder: recorder}
		if err := observer.ObserveStep(context.Background(), "rerun-2", task(deviceMetricsExporterStepID, tc.status)); err != nil {
			t.Fatalf("ObserveStep: %v", err)
		}
		if recorder.state != tc.want || recorder.platformID != "plat-1" || recorder.workflowID != "deploy-1" {
			t.Errorf("%s: recorded (%s, %s, %s), want (plat-1, deploy-1, %s)", tc.status,
				recorder.platformID, recorder.workflowID, recorder.state, tc.want)
		}
	}
	recorder := &exporterStateRecorder{}
	observer := deviceMetricsExporterStepObserver{recorder: recorder}
	for _, other := range []operationdomain.Task{task(omnistatStepID, operationdomain.TaskFailed),
		task(deviceMetricsExporterStepID, operationdomain.TaskPending)} {
		if err := observer.ObserveStep(context.Background(), "deploy-1", other); err != nil {
			t.Fatalf("ObserveStep(%s): %v", other.ID, err)
		}
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
	if monitoring.stepID != "install-device-metrics-exporter" || monitoring.workflowKind != operationdomain.WorkflowKindConfigureDeviceMetricsExporter {
		t.Errorf("monitoring Task = %s/%s", monitoring.stepID, monitoring.workflowKind)
	}
}

// The exporter's container method composes the observability Server's runtime like Omnistat's.
func TestComposedContainerRuntimeForTheDeviceMetricsExporter(t *testing.T) {
	launcher := platformDeploymentLauncher{software: softwareapp.NewSoftwareService(assignmentList{}, nil, nil)}
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
	observer := platformDeploymentObserver{}
	execution := &operationdomain.AnsibleExecution{
		OperationID: "op-1", Kind: operationdomain.WorkflowKindConfigureSlurm, PlatformID: "plat-1",
		StepID: deviceMetricsExporterStepID,
	}
	if err := observer.AnsibleStepSucceeded(context.Background(), execution, operationdomain.RunnerResult{}); err == nil {
		t.Error("a Device Metrics Exporter run without a Grafana URL must fail its Task")
	}
}
