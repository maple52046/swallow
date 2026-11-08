package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	softwareapp "github.com/maple52046/swallow/internal/core/software/application"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	dockerce "github.com/maple52046/swallow/modules/software/docker-ce"
)

// Slurm feature composition (decisions 063, 066, and 068). After its install-platform Task a Slurm
// deployment appends one Job per composed Managed Software install, each an ansible install Task
// plus the record Task that marks its Software Assignments installed:
//
//  1. the container runtime, on the compute and login nodes when the containerRuntime feature is
//     on, and on the primary controller when the Platform's Grafana uses the container method;
//  2. the Platform's Grafana on the primary controller when the Platform has its own monitoring
//     dashboards: its datasource reads the Platform metrics gateway with the Platform's monitoring
//     token, and its completion records the Platform Grafana Monitoring Link;
//  3. with the Device Metrics Exporter feature, the Managed Software device-metrics-exporter on
//     every GPU compute node, its install Task also carrying the Platform's owner and record ids
//     and the Slurm side's variables;
//
// or, with Omnistat, finally the feature's own configure-omnistat Job (the exporter on every
// compute node); a deployment has at most one of the two features. The Site metrics store scrapes
// the exporters (decision 003), so no Prometheus is composed. Each Job depends on the previous one's
// record Task, so Grafana never starts before its runtime is recorded. This is decision 038's
// composition rule: no nested software Workflow, the same playbooks and roles as a standalone
// install.
const (
	jobConfigureOmnistat = "configure-omnistat"
	omnistatStepID       = "install-omnistat"
	omnistatPlaybook     = "configure-omnistat"
	// omnistatScrapeAddressesVar lists the addresses besides the node itself that the Omnistat
	// exporter admits: the Site metrics store's scrape addresses.
	omnistatScrapeAddressesVar = "swallow_omnistat_scrape_addresses"
	// omnistatHardwareCountersVar tells configure-omnistat whether the deployment asks for the GPU
	// hardware counters; false when it does not.
	omnistatHardwareCountersVar = "swallow_omnistat_hardware_counters"

	// deviceMetricsExporterStepID is the composed device-metrics-exporter install Task:
	// appendComposedInstall names it install-<kind>, as the feature's Task was named before the
	// exporter was Managed Software (decision 068).
	deviceMetricsExporterStepID = "install-" + string(softwaredomain.KindDeviceMetricsExporter)
	// deviceMetricsExporterResultGPUIdentity is the Slurm run's deploy-device-metrics-exporter result
	// field with the GPU identity it found.
	deviceMetricsExporterResultGPUIdentity = "gpuIdentity"

	// grafanaResultURL is the result field a single-Server Grafana install (and configure-omnistat)
	// reports its Grafana URL in.
	grafanaResultURL = "grafanaUrl"
	// grafanaDatasourceTokenVar is the secret var the Grafana role hands its datasource as a bearer
	// token. It carries the Platform's monitoring token only as a sealed Task secret, never in
	// trusted vars, Task parameters, or a Software Assignment's spec.
	grafanaDatasourceTokenVar = "swallow_grafana_datasource_token"
	// platformGrafanaLinkName is the Monitoring Link swallow records for a Platform's own Grafana.
	platformGrafanaLinkName = "Platform Grafana"
)

// legacyGrafanaLinkNames are the Monitoring Links swallow recorded for a Slurm monitoring feature's
// Grafana before the Platform had its own. Recording the Platform Grafana link replaces them, and
// an uninstall removes them, since they point at the same Grafana.
var legacyGrafanaLinkNames = []string{"Omnistat Grafana", "Device Metrics Exporter Grafana"}

// ownedMonitoringLinks are every Monitoring Link name swallow writes itself; the platform service
// removes them when a Platform's uninstall completes.
var ownedMonitoringLinks = append([]string{platformGrafanaLinkName}, legacyGrafanaLinkNames...)

// platformMetricsGatewayURL is the Prometheus URL a Platform's Grafana reads: the Platform metrics
// gateway (platform-metrics-gateway.md) at the installation's public URL.
func platformMetricsGatewayURL(publicURL, platformID string) string {
	return publicURL + "/api/v1/platform-metrics/" + platformID + "/prometheus"
}

// platformMonitoringAccess is how the Servers a deployment configures and the Site metrics store
// reach each other (config PublicURL and MonitoringScrapeAddresses). A deployment that needs an
// empty one is refused with platformdomain.ErrMonitoringEndpointMissing when it is planned.
type platformMonitoringAccess struct {
	// publicURL is where the deployed Servers reach this API, without a trailing slash.
	publicURL string
	// scrapeAddresses are the addresses the Site metrics store scrapes from, as the Servers see them.
	scrapeAddresses []string
}

// slurmMonitoring is what the composition needs from the Slurm monitoring feature a deployment
// carries (Omnistat or the Device Metrics Exporter): the dashboards the Platform's Grafana adds for
// it and the feature's own install Task. It is a translation of the domain launch, so the
// composition is written once.
type slurmMonitoring struct {
	label           string
	dashboardBundle string
	trustedVars     map[string]any
	// composedKind, when set, is the Managed Software the feature installs on composedServerIDs
	// (the Device Metrics Exporter); otherwise the feature has its own install Task below.
	composedKind      softwaredomain.Kind
	composedServerIDs []string
	// The feature's own install Task (Omnistat).
	stepID, stepName, job, playbook string
	workflowKind                    workflowdomain.WorkflowKind
	// stateCompletion is the feature's State projection; resultCompletion, when set, records what
	// its playbook returned.
	stateCompletion, resultCompletion string
	// grafanaPortVar names the playbook variable the software domain's Grafana port goes in, for the
	// URL omnistat-query reads and the run reports.
	grafanaPortVar string
}

// completions declares the feature Task's State projection and result recording, fenced on the
// Platform and the record of this Workflow; like the GPU Operator's, the record survives a rerun
// that copies the Task under a new Workflow id.
func (m *slurmMonitoring) completions(platformID, recordID string) []workflowdomain.Completion {
	completions := []workflowdomain.Completion{featureState(m.stateCompletion, platformID, recordID)}
	if m.resultCompletion != "" {
		completions = append(completions, workflowdomain.Completion{Type: m.resultCompletion, Params: map[string]any{completionParamRecord: recordID}})
	}
	return completions
}

// slurmMonitoringOf returns the deployment's monitoring feature, or nil when it has none. The
// deploy use case never sets both features.
func slurmMonitoringOf(launch platformdomain.DeploymentLaunch) *slurmMonitoring {
	switch {
	case launch.Omnistat != nil:
		omnistat := launch.Omnistat
		trustedVars := maps.Clone(omnistat.TrustedVars)
		if trustedVars == nil {
			trustedVars = map[string]any{}
		}
		trustedVars[omnistatHardwareCountersVar] = omnistat.HardwareCounters
		return &slurmMonitoring{
			label: "Omnistat", dashboardBundle: softwaredomain.DashboardBundleOmnistat, trustedVars: trustedVars,
			stepID: omnistatStepID, stepName: "Install Omnistat", job: jobConfigureOmnistat,
			playbook: omnistatPlaybook, workflowKind: workflowdomain.WorkflowKindConfigureOmnistat,
			stateCompletion: completionOmnistatState, resultCompletion: completionOmnistatHardwareCounters,
			grafanaPortVar: "swallow_omnistat_grafana_port",
		}
	case launch.DeviceMetricsExporter != nil:
		exporter := launch.DeviceMetricsExporter
		return &slurmMonitoring{
			label: "the Device Metrics Exporter", dashboardBundle: softwaredomain.DashboardBundleDeviceMetricsExporter,
			trustedVars:  exporter.TrustedVars,
			composedKind: softwaredomain.KindDeviceMetricsExporter, composedServerIDs: exporter.GPUComputeServerIDs,
			stateCompletion: completionDeviceMetricsExporterState, resultCompletion: completionDeviceMetricsExporterResult,
		}
	default:
		return nil
	}
}

// composedPlan is the composed part of a deployment: the Tasks to append after the Platform's own
// install, the software installs whose pending assignments are written once the Workflow exists (a
// Slurm deployment's), and the install Tasks that receive the Platform's monitoring token as a
// secret (either type's Platform Grafana).
type composedPlan struct {
	steps    []workflowdomain.Task
	installs []softwareapp.ComposedInstall
	// tokenSteps receive the Platform's monitoring token as grafanaDatasourceTokenVar. The token is
	// issued when the Workflow is persisted, in the accepting transaction, so planning writes nothing.
	tokenSteps []string
}

// planSlurmComposition validates and builds the container runtime, Platform Grafana, and monitoring
// feature Jobs. provisionFirst lets the deployment's provision-os Tasks satisfy the deployed-state
// requirement of every composed ansible Task. A composed install the software policy refuses (for
// example Podman where Docker CE is installed, the two being mutually exclusive) is reported as
// ErrComposedSoftwareConflict, and an installation without the monitoring addresses a Job needs as
// ErrMonitoringEndpointMissing, before anything is created.
func (l platformDeploymentLauncher) planSlurmComposition(
	ctx context.Context,
	launch platformdomain.DeploymentLaunch,
	operationID string,
	provisionFirst bool,
) (composedPlan, error) {
	plan := composedPlan{}
	monitoring := slurmMonitoringOf(launch)
	dashboards := launch.MonitoringDashboards
	if launch.ContainerRuntime == nil && monitoring == nil && dashboards == nil {
		return plan, nil
	}
	if l.software == nil {
		switch {
		case launch.Omnistat != nil:
			return plan, fmt.Errorf("%w: software composition is unavailable", platformdomain.ErrInvalidOmnistat)
		case launch.DeviceMetricsExporter != nil:
			return plan, fmt.Errorf("%w: software composition is unavailable", platformdomain.ErrInvalidDeviceMetricsExporter)
		case launch.ContainerRuntime != nil:
			return plan, fmt.Errorf("%w: software composition is unavailable", platformdomain.ErrInvalidContainerRuntime)
		}
		return plan, fmt.Errorf("%w: software composition is unavailable", platformdomain.ErrInvalidDeployment)
	}
	if dashboards != nil && l.monitoring.publicURL == "" {
		return plan, fmt.Errorf("%w: the Platform's Grafana reads swallow's metrics gateway, so set SWALLOW_API_PUBLIC_URL "+
			"to the URL the Servers reach swallow at (swallowctl sets it from SWALLOW_ADDRESS), or turn monitoringDashboards off",
			platformdomain.ErrMonitoringEndpointMissing)
	}
	if launch.Omnistat != nil && len(l.monitoring.scrapeAddresses) == 0 {
		return plan, fmt.Errorf("%w: the Omnistat exporter admits only the Site's Prometheus, so set "+
			"SWALLOW_API_MONITORING_SCRAPE_ADDRESSES to the addresses it scrapes from (swallowctl sets SWALLOW_ADDRESS)",
			platformdomain.ErrMonitoringEndpointMissing)
	}
	previous := installPlatformStepID

	runtime, runtimeTargets, err := l.composedContainerRuntime(ctx, launch)
	if err != nil {
		return plan, err
	}
	if len(runtimeTargets) > 0 {
		previous, err = l.appendComposedInstall(ctx, &plan, launch, operationID, provisionFirst, previous,
			runtime, runtimeTargets, containerRuntimeSpec(runtime),
			"Install "+l.software.Label(runtime)+" on "+launch.Platform.Name, composedInstallExtras{})
		if err != nil {
			return plan, err
		}
	}

	if dashboards != nil {
		previous, err = l.appendComposedInstall(ctx, &plan, launch, operationID, provisionFirst, previous,
			softwaredomain.KindGrafana, []string{dashboards.ServerID},
			platformGrafanaSpec(launch, runtime, l.monitoring.publicURL),
			"Install the Platform Grafana on "+launch.Platform.Name,
			composedInstallExtras{
				platformToken: true,
				completions:   []workflowdomain.Completion{{Type: completionPlatformGrafanaResult}},
			})
		if err != nil {
			return plan, err
		}
	}

	if monitoring == nil {
		return plan, nil
	}
	trustedVars := maps.Clone(monitoring.trustedVars)
	if trustedVars == nil {
		trustedVars = map[string]any{}
	}
	if monitoring.grafanaPortVar != "" {
		// The software domain owns the Grafana port; the URL the feature reports must use the same one.
		trustedVars[monitoring.grafanaPortVar] = softwaredomain.GrafanaPort
	}
	if monitoring.composedKind != "" {
		// The software install with the Slurm side laid over it.
		_, err = l.appendComposedInstall(ctx, &plan, launch, operationID, provisionFirst, previous,
			monitoring.composedKind, monitoring.composedServerIDs, map[string]any{},
			"Install "+l.software.Label(monitoring.composedKind)+" on "+launch.Platform.Name,
			composedInstallExtras{trustedVars: trustedVars, completions: monitoring.completions(launch.Platform.ID, operationID)})
		return plan, err
	}
	trustedVars[omnistatScrapeAddressesVar] = slices.Clone(l.monitoring.scrapeAddresses)
	prepared, err := l.operations.PrepareAnsibleStep(ctx, workflowapp.CreateExecutionInput{
		Kind: string(monitoring.workflowKind), PlaybookName: monitoring.playbook,
		Intent:          "Install " + monitoring.label + " on " + launch.Platform.Name,
		TargetServerIDs: launch.TargetServerIDs, PlatformID: launch.Platform.ID,
		TrustedVars: trustedVars, RequestedBy: launch.RequestedBy,
	}, operationID, provisionFirst)
	if err != nil {
		return plan, err
	}
	plan.steps = append(plan.steps, workflowdomain.Task{
		ID: monitoring.stepID, Kind: "ansible-playbook", Name: monitoring.stepName,
		Job:      monitoring.job,
		Executor: workflowdomain.RunnerKindHostAutomation, DependsOn: []string{previous},
		Targets:    prepared.Targets,
		Parameters: map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars},
		Completion: monitoring.completions(launch.Platform.ID, operationID),
	})
	return plan, nil
}

// composedInstallExtras is what a deployment adds to a composed software install beyond the
// software's own variables. The zero value adds nothing.
type composedInstallExtras struct {
	// trustedVars are laid over the software install's own trusted variables.
	trustedVars map[string]any
	// completions are added to the install Task's declared completions.
	completions []workflowdomain.Completion
	// platformToken gives the install Task the Platform's monitoring token as the sealed secret var
	// grafanaDatasourceTokenVar (composedPlan.tokenSteps).
	platformToken bool
}

// appendComposedInstall plans one composed software install on serverIDs and appends its Job (the
// ansible install Task after previous, then its record Task). It returns the record Task's id, the
// dependency of the next Job.
func (l platformDeploymentLauncher) appendComposedInstall(
	ctx context.Context,
	plan *composedPlan,
	launch platformdomain.DeploymentLaunch,
	operationID string,
	provisionFirst bool,
	previous string,
	kind softwaredomain.Kind,
	serverIDs []string,
	spec map[string]any,
	intent string,
	extras composedInstallExtras,
) (string, error) {
	targets := make([]softwareapp.InstallTarget, len(serverIDs))
	for i, id := range serverIDs {
		targets[i] = softwareapp.InstallTarget{ServerID: id}
	}
	install, err := l.software.PlanComposedInstall(ctx, softwareapp.InstallInput{
		Kind: kind, Targets: targets, Spec: spec, RequestedBy: launch.RequestedBy,
	})
	if err != nil {
		return "", fmt.Errorf("%w: %s: %v", platformdomain.ErrComposedSoftwareConflict, l.software.Label(kind), err)
	}
	// The same playbook the standalone software launcher runs for the kind.
	mapping := installPlaybook(kind)
	trustedVars := install.TrustedVars
	if len(extras.trustedVars) > 0 {
		trustedVars = maps.Clone(install.TrustedVars)
		if trustedVars == nil {
			trustedVars = map[string]any{}
		}
		maps.Copy(trustedVars, extras.trustedVars)
	}
	prepared, err := l.operations.PrepareAnsibleStep(ctx, workflowapp.CreateExecutionInput{
		Kind: string(mapping.workflowKind), PlaybookName: mapping.playbook,
		Intent: intent, TargetServerIDs: install.ServerIDs, PlatformID: launch.Platform.ID,
		TrustedVars: trustedVars, RequestedBy: launch.RequestedBy,
	}, operationID, provisionFirst)
	if err != nil {
		return "", err
	}
	installID := "install-" + string(kind)
	recordID := "record-" + string(kind)
	job := softwareConfigureJob(kind)
	plan.steps = append(plan.steps,
		workflowdomain.Task{
			ID: installID, Kind: "ansible-playbook", Name: "Install " + l.software.Label(kind),
			Job:      job,
			Executor: workflowdomain.RunnerKindHostAutomation, DependsOn: []string{previous},
			Targets:    prepared.Targets,
			Parameters: map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars},
			// The failure marking covers a failed install whose record Task never runs.
			Completion: append([]workflowdomain.Completion{softwareFailure(kind, install.ServerIDs)}, extras.completions...),
		},
		workflowdomain.Task{
			ID: recordID, Kind: softwareStepKindRecord, Name: "Record " + l.software.Label(kind) + " assignment",
			Job:        job,
			Executor:   workflowdomain.RunnerKindInternal,
			DependsOn:  []string{installID},
			Targets:    serverResourceRefs(install.ServerIDs),
			Parameters: map[string]any{"softwareKind": string(kind), "serverIds": install.ServerIDs},
			Completion: []workflowdomain.Completion{softwareFailure(kind, install.ServerIDs)},
		},
	)
	if extras.platformToken {
		plan.tokenSteps = append(plan.tokenSteps, installID)
	}
	plan.installs = append(plan.installs, install)
	return recordID, nil
}

// composedContainerRuntime returns the container runtime the deployment composes and the Servers
// that still need it. The containerRuntime feature names the runtime and puts it on the compute
// and login nodes; a Platform Grafana installed by the container method adds its Server (the
// primary controller) and, without the feature, runs on the runtime already assigned there (Docker
// CE when none is). A Server whose runtime is already installed is left alone, so a user's own
// runtime settings (an enabled Docker Engine API, for example) are never reverted. The kind is ""
// when no runtime is needed.
func (l platformDeploymentLauncher) composedContainerRuntime(
	ctx context.Context,
	launch platformdomain.DeploymentLaunch,
) (softwaredomain.Kind, []string, error) {
	var kind softwaredomain.Kind
	var serverIDs []string
	if launch.ContainerRuntime != nil {
		kind = softwaredomain.Kind(launch.ContainerRuntime.Kind)
		serverIDs = append(serverIDs, launch.ContainerRuntime.ServerIDs...)
	}
	if dashboards := launch.MonitoringDashboards; dashboards != nil && dashboards.InstallMethod != platformdomain.ObservabilityInstallNative {
		if kind == "" {
			assigned, err := l.assignedContainerRuntime(ctx, dashboards.ServerID)
			if err != nil {
				return "", nil, err
			}
			kind = assigned
		}
		if !slices.Contains(serverIDs, dashboards.ServerID) {
			serverIDs = append(serverIDs, dashboards.ServerID)
		}
	}
	if kind == "" {
		return "", nil, nil
	}
	needed := make([]string, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		existing, err := l.software.ListAssignments(ctx, softwaredomain.AssignmentFilter{ServerID: serverID, Kind: kind})
		if err != nil {
			return "", nil, err
		}
		if len(existing) > 0 && existing[0].State == softwaredomain.StateInstalled {
			continue
		}
		needed = append(needed, serverID)
	}
	return kind, needed, nil
}

// assignedContainerRuntime returns the container runtime with a live Software Assignment on the
// Server, or Docker CE when it has none. The runtimes are mutually exclusive, so at most one is
// live.
func (l platformDeploymentLauncher) assignedContainerRuntime(ctx context.Context, serverID string) (softwaredomain.Kind, error) {
	for _, kind := range []softwaredomain.Kind{softwaredomain.KindDockerCE, softwaredomain.KindPodman} {
		existing, err := l.software.ListAssignments(ctx, softwaredomain.AssignmentFilter{ServerID: serverID, Kind: kind})
		if err != nil {
			return "", err
		}
		if len(existing) > 0 && existing[0].State != softwaredomain.StateAbsent {
			return kind, nil
		}
	}
	return softwaredomain.KindDockerCE, nil
}

// containerRuntimeSpec is a composed runtime's spec. Docker CE leaves the unauthenticated Engine
// API off: neither Slurm workloads nor the Platform's Grafana need it.
func containerRuntimeSpec(kind softwaredomain.Kind) map[string]any {
	if kind == softwaredomain.KindDockerCE {
		return map[string]any{dockerce.SpecEnableAPI: false}
	}
	return map[string]any{}
}

// platformGrafanaSpec builds the Platform Grafana's spec: its install method, on the composed
// runtime when it is the container method; the Platform metrics gateway as its datasource, so it
// reads only the Platform's members; and the basic-monitoring dashboards plus the monitoring
// feature's. The spec holds no secret: the gateway token reaches the Task only as a sealed secret.
func platformGrafanaSpec(launch platformdomain.DeploymentLaunch, runtime softwaredomain.Kind, publicURL string) map[string]any {
	spec := map[string]any{softwaredomain.SpecInstallMethod: string(softwaredomain.InstallMethodNative)}
	if launch.MonitoringDashboards.InstallMethod != platformdomain.ObservabilityInstallNative {
		spec[softwaredomain.SpecInstallMethod] = string(softwaredomain.InstallMethodContainer)
		spec[softwaredomain.SpecContainerRuntime] = string(runtime)
	}
	spec[softwaredomain.SpecPrometheusURL] = platformMetricsGatewayURL(publicURL, launch.Platform.ID)
	bundles := []string{softwaredomain.DashboardBundleNodeExporter}
	if monitoring := slurmMonitoringOf(launch); monitoring != nil {
		bundles = append(bundles, monitoring.dashboardBundle)
	}
	spec[softwaredomain.SpecDashboards] = bundles
	return spec
}

// withPlatformToken returns workflow with token added, as grafanaDatasourceTokenVar, to the sealed
// secrets of every step in steps. It copies the secret maps rather than changing the plan's, so a
// transaction that runs the persist again starts from the plan as prepared.
func withPlatformToken(workflow workflowapp.CreateWorkflowInput, steps []string, token string) workflowapp.CreateWorkflowInput {
	secrets := make(map[string]map[string]any, len(workflow.StepSecretValues)+len(steps))
	for step, values := range workflow.StepSecretValues {
		secrets[step] = maps.Clone(values)
	}
	for _, step := range steps {
		if secrets[step] == nil {
			secrets[step] = map[string]any{}
		}
		secrets[step][grafanaDatasourceTokenVar] = token
	}
	workflow.StepSecretValues = secrets
	return workflow
}

// recordComposedInstalls writes the composed installs' pending Software Assignments once the
// Workflow exists. A write failure is logged by the caller rather than undoing an accepted
// Workflow: the record Tasks tolerate a missing assignment, so the cost is a missing row, not a
// failed deployment.
func (l platformDeploymentLauncher) recordComposedInstalls(ctx context.Context, plan composedPlan, workflowID string) error {
	var errs []error
	for _, install := range plan.installs {
		if err := l.software.RecordComposedInstall(ctx, install, workflowID); err != nil {
			errs = append(errs, fmt.Errorf("record %s assignment: %w", install.Kind, err))
		}
	}
	return errors.Join(errs...)
}

// omnistatStateForTask returns the state a Task status implies, or false for a status that does
// not move it (pending leaves the state recorded at acceptance).
func omnistatStateForTask(status workflowdomain.TaskStatus) (platformdomain.OmnistatState, bool) {
	switch installTaskPhaseOf(status) {
	case installPhaseRunning:
		return platformdomain.OmnistatInstalling, true
	case installPhaseSucceeded:
		return platformdomain.OmnistatReady, true
	case installPhaseFailed:
		return platformdomain.OmnistatFailed, true
	default:
		return "", false
	}
}

// installTaskPhase is what a monitoring feature's install Task status means for the feature's state:
// running and waiting are still installing, success is ready, and failure, requires_attention,
// cancel, or skip (an earlier Job failed) is a failed install. Pending moves nothing.
type installTaskPhase int

const (
	installPhaseNone installTaskPhase = iota
	installPhaseRunning
	installPhaseSucceeded
	installPhaseFailed
)

func installTaskPhaseOf(status workflowdomain.TaskStatus) installTaskPhase {
	switch status {
	case workflowdomain.TaskRunning, workflowdomain.TaskWaitingExternal:
		return installPhaseRunning
	case workflowdomain.TaskSucceeded:
		return installPhaseSucceeded
	case workflowdomain.TaskFailed, workflowdomain.TaskRequiresAttention, workflowdomain.TaskCanceled,
		workflowdomain.TaskSkipped:
		return installPhaseFailed
	default:
		return installPhaseNone
	}
}
