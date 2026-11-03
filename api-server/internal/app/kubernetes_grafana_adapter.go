package app

import (
	"context"
	"fmt"
	"slices"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// Kubernetes Platform Grafana (monitoring.md §6.2). A Kubernetes deployment whose Platform has its
// own monitoring dashboards ends with one more Job: the configure-kubernetes-grafana playbook
// installs the pinned Grafana chart in the cluster from the initial controller, after the cluster is
// validated and after the GPU Operator when one is composed. Its datasource is the Platform metrics
// gateway, read with the Platform's monitoring token, which PersistDeployment issues and seals into
// this one Task; its completion records the Platform Grafana Monitoring Link from the run's result.
// The release lives in the cluster, so a cluster uninstall removes it and nothing records it
// besides the link.
const (
	jobConfigurePlatformGrafana = "configure-platform-grafana"
	kubernetesGrafanaStepID     = "install-platform-grafana"
	kubernetesGrafanaPlaybook   = "configure-kubernetes-grafana"
)

// Trusted variables of the configure-kubernetes-grafana playbook. The token is the secret var
// grafanaDatasourceTokenVar, never one of these.
const (
	varK0sGrafanaControllerID = "swallow_k0s_grafana_controller_id"
	// varK0sGrafanaNodeIDs are the workload members, which serve the node port; the playbook links
	// the first one's address.
	varK0sGrafanaNodeIDs    = "swallow_k0s_grafana_node_ids"
	varK0sGrafanaDatasource = "swallow_k0s_grafana_datasource_url"
	varK0sGrafanaDashboards = "swallow_k0s_grafana_dashboards"
)

// planKubernetesGrafana builds the Platform Grafana Job of a Kubernetes deployment, its Task
// depending on after; a deployment without monitoring dashboards gets none. An installation without
// SWALLOW_API_PUBLIC_URL is refused with ErrMonitoringEndpointMissing when the deployment is
// planned, before anything is created. The dashboards are the basic-monitoring bundle plus the
// Device Metrics Exporter's when the GPU Operator runs it. The Task targets every deployment target
// so the playbook can open the node port on each workload node.
func (l platformDeploymentLauncher) planKubernetesGrafana(
	ctx context.Context,
	launch platformdomain.DeploymentLaunch,
	operationID string,
	provisionFirst bool,
	after []string,
) (composedPlan, error) {
	dashboards := launch.MonitoringDashboards
	if dashboards == nil {
		return composedPlan{}, nil
	}
	if l.monitoring.publicURL == "" {
		return composedPlan{}, fmt.Errorf("%w: the Platform's Grafana reads swallow's metrics gateway, so set SWALLOW_API_PUBLIC_URL "+
			"to the URL the Servers reach swallow at (swallowctl sets it from SWALLOW_ADDRESS), or turn monitoringDashboards off",
			platformdomain.ErrMonitoringEndpointMissing)
	}
	endpoints := dashboards.EndpointServerIDs
	if len(endpoints) == 0 {
		endpoints = []string{dashboards.ServerID}
	}
	bundles := []string{softwaredomain.DashboardBundleNodeExporter}
	if dashboards.GPUMetrics {
		bundles = append(bundles, softwaredomain.DashboardBundleDeviceMetricsExporter)
	}
	prepared, err := l.operations.PrepareAnsibleStep(ctx, workflowapp.CreateExecutionInput{
		Kind: deployKubernetesKind, PlaybookName: kubernetesGrafanaPlaybook,
		Intent:          "Install the Platform Grafana on " + launch.Platform.Name,
		TargetServerIDs: launch.TargetServerIDs, PlatformID: launch.Platform.ID,
		TrustedVars: map[string]any{
			varK0sGrafanaControllerID: dashboards.ServerID,
			varK0sGrafanaNodeIDs:      slices.Clone(endpoints),
			varK0sGrafanaDatasource:   platformMetricsGatewayURL(l.monitoring.publicURL, launch.Platform.ID),
			varK0sGrafanaDashboards:   bundles,
		},
		RequestedBy: launch.RequestedBy,
	}, operationID, provisionFirst)
	if err != nil {
		return composedPlan{}, err
	}
	return composedPlan{
		steps: []workflowdomain.Task{{
			ID: kubernetesGrafanaStepID, Kind: "ansible-playbook", Name: "Install the Platform Grafana",
			Job:      jobConfigurePlatformGrafana,
			Executor: workflowdomain.RunnerKindHostAutomation, DependsOn: slices.Clone(after),
			Targets:    prepared.Targets,
			Parameters: map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars},
			Completion: []workflowdomain.Completion{{Type: completionPlatformGrafanaResult}},
		}},
		tokenSteps: []string{kubernetesGrafanaStepID},
	}, nil
}
