package app

import (
	"context"
	"encoding/json"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// defaultSelfHostedMountOptions are the NFS options a self-hosted Slurm workload filesystem gets
// when the request names none, so a recorded value equal to them was not requested.
const defaultSelfHostedMountOptions = "rw,_netdev,hard,timeo=600,retrans=2,vers=3"

// platformDeploymentRecords reads back the deploy request a Platform's latest deploy Workflow
// carried out, from the trusted variables and the machine preparation it recorded in its intent
// (deploymentWorkflowInput) and the Platform's own record. It inverts the deploy use case's variable
// builders for the fields a Platform spec keeps; like the lifecycle reader it is where the
// recorded variables are read.
type platformDeploymentRecords struct {
	orchestrations workflowdomain.WorkflowRepository
}

var _ platformdomain.DeploymentRecordReader = platformDeploymentRecords{}

// LatestDeploymentRecord rebuilds the deploy request of the Platform's most recently requested
// deploy Workflow.
func (r platformDeploymentRecords) LatestDeploymentRecord(ctx context.Context, platform *platformdomain.Platform) (platformdomain.DeploymentRecord, bool, error) {
	workflows, _, err := r.orchestrations.List(ctx, workflowdomain.WorkflowFilter{PlatformIDs: []string{platform.ID}})
	if err != nil {
		return platformdomain.DeploymentRecord{}, false, err
	}
	var latest *workflowdomain.Workflow
	for _, workflow := range workflows {
		isDeploy := workflow.Kind == workflowdomain.WorkflowKindDeployKubernetes || workflow.Kind == workflowdomain.WorkflowKindConfigureSlurm
		if isDeploy && (latest == nil || latest.RequestedAt.Before(workflow.RequestedAt)) {
			latest = workflow
		}
	}
	if latest == nil {
		return platformdomain.DeploymentRecord{}, false, nil
	}
	vars, _ := latest.Intent["extraVars"].(map[string]any)
	var roles []platformdomain.RoleAssignment
	if intent := deploymentIntentKubernetes(latest); intent != nil {
		roles = intent.RoleAssignments
	}
	record := platformdomain.DeploymentRecord{Request: adoptionRequest(platform, roles, vars)}
	if preparation, ok := latest.Intent["machinePreparation"].(map[string]any); ok {
		if raw, err := json.Marshal(preparation); err == nil {
			_ = json.Unmarshal(raw, &record.MachinePreparation)
		}
	}
	return record, true, nil
}

// adoptionRequest rebuilds the type-specific deploy request fields from the recorded variables, the
// Kubernetes role assignments, and the Platform's feature records.
func adoptionRequest(platform *platformdomain.Platform, roles []platformdomain.RoleAssignment, vars map[string]any) map[string]any {
	body := map[string]any{}
	if platform.GPUStackOwner != "" {
		body["gpuStackOwner"] = string(platform.GPUStackOwner)
	}
	if len(platform.GPUDriver.ModuleOptions) > 0 {
		options := map[string]any{}
		for key, value := range platform.GPUDriver.ModuleOptions {
			options[key] = value
		}
		body["gpuDriver"] = map[string]any{"moduleOptions": options}
	}
	switch platform.Type {
	case platformdomain.PlatformTypeKubernetes:
		assignments := []any{}
		for _, assignment := range roles {
			assignments = append(assignments, map[string]any{
				"serverId": assignment.ServerID, "role": string(assignment.Role), "runWorkloads": assignment.RunWorkloads,
			})
		}
		body["roleAssignments"] = assignments
		copyVar(body, "k0sVersion", vars, "swallow_k0s_version")
		copyVar(body, "podCidr", vars, "swallow_k0s_pod_cidr")
		copyVar(body, "serviceCidr", vars, "swallow_k0s_service_cidr")
		copyVar(body, "apiVip", vars, "swallow_k0s_api_vip")
		if vip, _ := vars["swallow_k0s_api_vip"].(string); vip != "" {
			copyVar(body, "apiVipPrefix", vars, "swallow_k0s_api_vip_prefix")
		}
		if operator := platform.GPUOperator; operator.Managed() {
			body["gpuOperator"] = gpuOperatorBody(operator.Settings)
		}
	case platformdomain.PlatformTypeSlurm:
		body["slurm"] = adoptedSlurm(platform, vars)
		if platform.ContainerRuntime != "" {
			body["containerRuntime"] = map[string]any{"kind": string(platform.ContainerRuntime)}
		}
		if omnistat := platform.Omnistat; omnistat != nil && omnistat.Enabled {
			body["omnistat"] = map[string]any{"enabled": true, "installMethod": string(omnistat.InstallMethod)}
		}
		if exporter := platform.DeviceMetricsExporter; exporter != nil && exporter.Enabled {
			body["deviceMetricsExporter"] = map[string]any{"enabled": true, "installMethod": string(exporter.InstallMethod)}
		}
	}
	return body
}

// adoptedSlurm rebuilds the slurm object of a deploy request. Node assignments list the
// controllers, then the compute nodes, then the login hosts, each in the recorded order, so the
// primary controller and the primary login host stay first.
func adoptedSlurm(platform *platformdomain.Platform, vars map[string]any) map[string]any {
	var order []string
	flags := map[string]map[string]any{}
	add := func(ids []string, flag string) {
		for _, id := range ids {
			if flags[id] == nil {
				flags[id] = map[string]any{"serverId": id}
				order = append(order, id)
			}
			flags[id][flag] = true
		}
	}
	add(decodeStringSlice(vars["swallow_slurm_controller_ids"]), "controller")
	add(decodeStringSlice(vars["swallow_slurm_compute_ids"]), "compute")
	add(decodeStringSlice(vars["swallow_slurm_login_ids"]), "login")
	assignments := make([]any, 0, len(order))
	for _, id := range order {
		assignments = append(assignments, flags[id])
	}
	slurm := map[string]any{
		"nodeAssignments": assignments, "ociContainers": platform.OCIContainers, "pmix": platform.PMIx,
	}
	copyVar(slurm, "clusterName", vars, "swallow_slurm_cluster_name")
	copyVar(slurm, "apiVersion", vars, "swallow_slurm_api_version")
	copyVar(slurm, "stateSaveLocation", vars, "swallow_slurm_state_save_location")
	if platform.GPUGresType != "" {
		slurm["gpuGresType"] = platform.GPUGresType
	}
	if enabled, _ := vars["swallow_slurm_workload_enabled"].(bool); enabled {
		mode, _ := vars["swallow_slurm_workload_mode"].(string)
		storage := map[string]any{"mode": mode, "type": "nfs"}
		copyVar(storage, "mountPath", vars, "swallow_slurm_workload_mount_path")
		nfs := map[string]any{}
		options, _ := vars["swallow_slurm_workload_mount_options"].(string)
		if mode == string(platformdomain.SlurmWorkloadStorageSelfHosted) && options == defaultSelfHostedMountOptions {
			options = ""
		}
		if options != "" {
			nfs["mountOptions"] = options
		}
		copyVar(nfs, "url", vars, "swallow_slurm_workload_source")
		if len(nfs) > 0 {
			storage["nfs"] = nfs
		}
		slurm["workloadStorage"] = storage
	}
	return slurm
}

// gpuOperatorBody renders recorded GPU Operator settings as the request's gpuOperator object.
func gpuOperatorBody(settings platformdomain.GPUOperatorSettings) map[string]any {
	body := map[string]any{
		"enabled": settings.Enabled, "nodeLabeller": settings.NodeLabeller, "metricsExporter": settings.MetricsExporter,
	}
	for key, value := range map[string]string{
		"version": settings.Version, "allocationMode": string(settings.AllocationMode),
		"draDriverVersion": settings.DRADriverVersion, "resourceNaming": string(settings.ResourceNaming),
	} {
		if value != "" {
			body[key] = value
		}
	}
	return body
}

// copyVar copies a recorded variable that is set into the request under key.
func copyVar(body map[string]any, key string, vars map[string]any, name string) {
	switch value := vars[name].(type) {
	case nil:
	case string:
		if value != "" {
			body[key] = value
		}
	default:
		body[key] = value
	}
}
