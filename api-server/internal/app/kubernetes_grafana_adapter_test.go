package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// kubernetesTestAutomation is a Site whose automation maps deploy-kubernetes to its playbook, as a
// Kubernetes deployment requires.
type kubernetesTestAutomation struct {
	workflowdomain.AutomationConfigurationRepository
}

func (kubernetesTestAutomation) FindBySiteID(_ context.Context, siteID string) (*workflowdomain.AutomationConfiguration, error) {
	return &workflowdomain.AutomationConfiguration{
		SiteID: siteID, Enabled: true, CredentialSource: workflowdomain.CredentialSourceDeploymentKey,
		PlaybookMappings: map[workflowdomain.WorkflowKind]string{deployKubernetesKind: deployKubernetesKind},
	}, nil
}

// kubernetesGrafanaLauncher is a launcher over deployed test Servers at a Site that can deploy
// Kubernetes, in an installation that knows its public URL.
func kubernetesGrafanaLauncher(t *testing.T) platformDeploymentLauncher {
	t.Helper()
	return platformDeploymentLauncher{
		operations: newTestExecutionService(slurmTestServers{}, kubernetesTestAutomation{}, slurmTestCatalog{}, testModules(t).workflowKinds),
		monitoring: platformMonitoringAccess{publicURL: "http://10.170.168.20"},
	}
}

// kubernetesLaunch is an existing_os Kubernetes deployment of a controller c1 and workers w1 and w2.
func kubernetesLaunch(dashboards *platformdomain.MonitoringDashboardsLaunch, gpuOperatorVars map[string]any) platformdomain.DeploymentLaunch {
	return platformdomain.DeploymentLaunch{
		Platform:        &platformdomain.Platform{ID: "plat-1", Name: "lab", Type: platformdomain.PlatformTypeKubernetes},
		OperationID:     "op-1",
		TargetServerIDs: []string{"c1", "w1", "w2"},
		TrustedVars:     map[string]any{"swallow_k0s_initial_controller_id": "c1"},
		GPUOperatorVars: gpuOperatorVars,
		RequestedBy:     "admin",
		MachinePreparation: platformdomain.MachinePreparation{
			Mode: platformdomain.MachinePreparationExistingOS,
		},
		MonitoringDashboards: dashboards,
	}
}

// preparedKubernetes plans a Kubernetes deployment with the test launcher.
func preparedKubernetes(t *testing.T, launcher platformDeploymentLauncher, launch platformdomain.DeploymentLaunch) *preparedPlatformDeployment {
	t.Helper()
	prepared, err := launcher.PrepareDeployment(context.Background(), launch)
	if err != nil {
		t.Fatalf("PrepareDeployment() error = %v", err)
	}
	return prepared.(*preparedPlatformDeployment)
}

// findTask returns the Task with id, failing the test when the plan has none.
func findTask(t *testing.T, steps []workflowdomain.Task, id string) workflowdomain.Task {
	t.Helper()
	for _, step := range steps {
		if step.ID == id {
			return step
		}
	}
	t.Fatalf("plan %v has no Task %s", taskIDs(steps), id)
	return workflowdomain.Task{}
}

// The Platform Grafana is the deployment's last Job, after the GPU Operator: it runs the module's
// playbook over every target, names the initial controller and the workload nodes, reads the
// gateway with the device-metrics-exporter dashboards the operator's exporter needs, records the
// Platform Grafana link, and is the one Task that receives the monitoring token, which no trusted
// variable carries.
func TestKubernetesDeploymentComposesThePlatformGrafanaLast(t *testing.T) {
	launcher := kubernetesGrafanaLauncher(t)
	dashboards := &platformdomain.MonitoringDashboardsLaunch{ServerID: "c1", EndpointServerIDs: []string{"w1", "w2"}, GPUMetrics: true}
	plan := preparedKubernetes(t, launcher, kubernetesLaunch(dashboards, map[string]any{"swallow_amd_gpu_operator_state": "present"}))

	steps := plan.workflow.Steps
	if ids := taskIDs(steps); ids[len(ids)-1] != kubernetesGrafanaStepID {
		t.Fatalf("Tasks = %v, want %s last", ids, kubernetesGrafanaStepID)
	}
	grafana := findTask(t, steps, kubernetesGrafanaStepID)
	if grafana.Job != jobConfigurePlatformGrafana || !slices.Equal(grafana.DependsOn, []string{gpuOperatorStepID}) ||
		!slices.Equal(grafana.Targets, serverRefs("c1", "w1", "w2")) {
		t.Errorf("Grafana Task = Job %s DependsOn %v Targets %v", grafana.Job, grafana.DependsOn, grafana.Targets)
	}
	if grafana.Parameters["playbook"] != kubernetesGrafanaPlaybook {
		t.Errorf("Grafana Task playbook = %v, want %s", grafana.Parameters["playbook"], kubernetesGrafanaPlaybook)
	}
	if !slices.ContainsFunc(grafana.Completion, func(c workflowdomain.Completion) bool { return c.Type == completionPlatformGrafanaResult }) {
		t.Errorf("Grafana Task completion = %v, want %s", grafana.Completion, completionPlatformGrafanaResult)
	}
	vars := extraVars(t, grafana)
	if vars[varK0sGrafanaControllerID] != "c1" {
		t.Errorf("%s = %v, want c1", varK0sGrafanaControllerID, vars[varK0sGrafanaControllerID])
	}
	if nodes, _ := vars[varK0sGrafanaNodeIDs].([]string); !slices.Equal(nodes, []string{"w1", "w2"}) {
		t.Errorf("%s = %v, want the workload nodes", varK0sGrafanaNodeIDs, vars[varK0sGrafanaNodeIDs])
	}
	if vars[varK0sGrafanaDatasource] != "http://10.170.168.20/api/v1/platform-metrics/plat-1/prometheus" {
		t.Errorf("%s = %v, want the gateway", varK0sGrafanaDatasource, vars[varK0sGrafanaDatasource])
	}
	if bundles, _ := vars[varK0sGrafanaDashboards].([]string); !slices.Equal(bundles, []string{
		softwaredomain.DashboardBundleNodeExporter, softwaredomain.DashboardBundleDeviceMetricsExporter,
	}) {
		t.Errorf("%s = %v", varK0sGrafanaDashboards, vars[varK0sGrafanaDashboards])
	}
	if _, leaked := vars[grafanaDatasourceTokenVar]; leaked {
		t.Error("the monitoring token must not be a trusted variable")
	}
	if !slices.Equal(plan.composed.tokenSteps, []string{kubernetesGrafanaStepID}) || len(plan.composed.installs) != 0 {
		t.Errorf("composed = token steps %v, installs %v; want only the Grafana Task's token", plan.composed.tokenSteps, plan.composed.installs)
	}
	if port := extraVars(t, findTask(t, steps, gpuOperatorStepID))[gpuOperatorMetricsPortVar]; port != platformdomain.DeviceMetricsExporterPort {
		t.Errorf("GPU Operator %s = %v, want %d", gpuOperatorMetricsPortVar, port, platformdomain.DeviceMetricsExporterPort)
	}
}

// Without the GPU Operator the Grafana follows the health gate with the basic-monitoring dashboards
// only, and links the controller when no workload node is named; without dashboards nothing is
// composed and no token is issued.
func TestKubernetesPlatformGrafanaFollowsTheClusterWithoutAGPUOperator(t *testing.T) {
	launcher := kubernetesGrafanaLauncher(t)
	plan := preparedKubernetes(t, launcher, kubernetesLaunch(&platformdomain.MonitoringDashboardsLaunch{ServerID: "c1"}, nil))
	grafana := findTask(t, plan.workflow.Steps, kubernetesGrafanaStepID)
	if !slices.Equal(grafana.DependsOn, []string{"validate-platform"}) {
		t.Errorf("Grafana Task DependsOn %v, want the health gate", grafana.DependsOn)
	}
	vars := extraVars(t, grafana)
	if bundles, _ := vars[varK0sGrafanaDashboards].([]string); !slices.Equal(bundles, []string{softwaredomain.DashboardBundleNodeExporter}) {
		t.Errorf("%s = %v, want the basic-monitoring bundle only", varK0sGrafanaDashboards, vars[varK0sGrafanaDashboards])
	}
	if nodes, _ := vars[varK0sGrafanaNodeIDs].([]string); !slices.Equal(nodes, []string{"c1"}) {
		t.Errorf("%s = %v, want the controller", varK0sGrafanaNodeIDs, vars[varK0sGrafanaNodeIDs])
	}

	plan = preparedKubernetes(t, launcher, kubernetesLaunch(nil, nil))
	if slices.Contains(taskIDs(plan.workflow.Steps), kubernetesGrafanaStepID) || len(plan.composed.tokenSteps) != 0 {
		t.Errorf("without dashboards: Tasks %v, token steps %v; want neither", taskIDs(plan.workflow.Steps), plan.composed.tokenSteps)
	}
}

// An installation without its public URL refuses the deployment when it is planned and names the
// setting, before anything is created.
func TestKubernetesPlatformGrafanaNeedsThePublicURL(t *testing.T) {
	launcher := kubernetesGrafanaLauncher(t)
	launcher.monitoring = platformMonitoringAccess{}
	_, err := launcher.PrepareDeployment(context.Background(),
		kubernetesLaunch(&platformdomain.MonitoringDashboardsLaunch{ServerID: "c1", EndpointServerIDs: []string{"w1"}}, nil))
	if !errors.Is(err, platformdomain.ErrMonitoringEndpointMissing) || !strings.Contains(err.Error(), "SWALLOW_API_PUBLIC_URL") {
		t.Errorf("PrepareDeployment() error = %v, want ErrMonitoringEndpointMissing naming SWALLOW_API_PUBLIC_URL", err)
	}
}
