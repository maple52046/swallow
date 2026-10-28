package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	softwareapp "github.com/maple52046/swallow/internal/software/application"
	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// Slurm feature composition (decisions 063 and 066). After its install-platform Task a Slurm
// deployment appends one Job per composed Managed Software install, each an ansible install Task
// plus the record Task that marks its Software Assignments installed:
//
//  1. the container runtime, on the compute and login nodes when the containerRuntime feature is
//     on, and on the observability Server when the monitoring feature uses the container method;
//  2. Prometheus, then Grafana, on the observability Server when a monitoring feature is on;
//
// and finally the monitoring feature's own Job: configure-omnistat (the exporter on every compute
// node) or configure-device-metrics-exporter (the exporter on every GPU compute node); a deployment
// has at most one of the two. Each Job depends on the previous one's record Task, so Prometheus
// never starts before its runtime is recorded. This is decision 038's composition rule: no nested
// software Workflow, the same playbooks and roles as a standalone install.
const (
	jobConfigureOmnistat = "configure-omnistat"
	omnistatStepID       = "install-omnistat"
	omnistatPlaybook     = "configure-omnistat"
	// omnistatStepParamOwner names the Platform whose Omnistat State the Task's outcome moves.
	omnistatStepParamOwner = "omnistatPlatformId"
	// omnistatStepParamRecord names the Workflow the Omnistat record is fenced on; like the GPU
	// Operator's, it survives a rerun that copies the Task under a new Workflow id.
	omnistatStepParamRecord = "omnistatRecordId"
	// omnistatResultGrafanaURL is the configure-omnistat result field Swallow records as the
	// Platform's Omnistat Grafana Monitoring Link.
	omnistatResultGrafanaURL = "grafanaUrl"

	jobConfigureDeviceMetricsExporter = "configure-device-metrics-exporter"
	deviceMetricsExporterStepID       = "install-device-metrics-exporter"
	deviceMetricsExporterPlaybook     = "configure-device-metrics-exporter"
	// The Device Metrics Exporter's Task carries the same owner and record parameters as Omnistat's.
	deviceMetricsExporterStepParamOwner  = "deviceMetricsExporterPlatformId"
	deviceMetricsExporterStepParamRecord = "deviceMetricsExporterRecordId"
	// The configure-device-metrics-exporter result: the Grafana URL Swallow records as the
	// Platform's Device Metrics Exporter Grafana Monitoring Link, and the GPU identity it found.
	deviceMetricsExporterResultGrafanaURL  = "grafanaUrl"
	deviceMetricsExporterResultGPUIdentity = "gpuIdentity"
)

// monitoringComposedSoftware is the order a monitoring feature's observability software is
// composed in.
var monitoringComposedSoftware = []softwaredomain.Kind{softwaredomain.KindPrometheus, softwaredomain.KindGrafana}

// slurmMonitoring is what the composition needs from the Slurm monitoring feature a deployment
// carries (Omnistat or the Device Metrics Exporter): where Prometheus and Grafana go and how they
// are installed, what Prometheus scrapes, which dashboards Grafana provisions, and the feature's own
// install Task. It is a translation of the domain launch, so the composition is written once.
type slurmMonitoring struct {
	label                 string
	observabilityServerID string
	installMethod         platformdomain.ObservabilityInstallMethod
	scrapeJob             string
	scrapeServerIDs       []string
	scrapePort            int
	dashboardBundle       string
	trustedVars           map[string]any
	// The feature's install Task and the parameters its step observer reads.
	stepID, stepName, job, playbook string
	workflowKind                    operationdomain.WorkflowKind
	ownerParam, recordParam         string
	// portVars names the playbook variables the software domain's Prometheus and Grafana ports go in.
	prometheusPortVar, grafanaPortVar string
}

// slurmMonitoringOf returns the deployment's monitoring feature, or nil when it has none. The
// deploy use case never sets both features.
func slurmMonitoringOf(launch platformdomain.DeploymentLaunch) *slurmMonitoring {
	switch {
	case launch.Omnistat != nil:
		omnistat := launch.Omnistat
		return &slurmMonitoring{
			label: "Omnistat", observabilityServerID: omnistat.ObservabilityServerID,
			installMethod: omnistat.InstallMethod, scrapeJob: platformdomain.OmnistatScrapeJob,
			scrapeServerIDs: omnistat.ComputeServerIDs, scrapePort: platformdomain.OmnistatExporterPort,
			dashboardBundle: platformdomain.OmnistatDashboardBundle, trustedVars: omnistat.TrustedVars,
			stepID: omnistatStepID, stepName: "Install Omnistat", job: jobConfigureOmnistat,
			playbook: omnistatPlaybook, workflowKind: operationdomain.WorkflowKindConfigureOmnistat,
			ownerParam: omnistatStepParamOwner, recordParam: omnistatStepParamRecord,
			prometheusPortVar: "swallow_omnistat_prometheus_port", grafanaPortVar: "swallow_omnistat_grafana_port",
		}
	case launch.DeviceMetricsExporter != nil:
		exporter := launch.DeviceMetricsExporter
		return &slurmMonitoring{
			label: "the Device Metrics Exporter", observabilityServerID: exporter.ObservabilityServerID,
			installMethod: exporter.InstallMethod, scrapeJob: platformdomain.DeviceMetricsExporterScrapeJob,
			scrapeServerIDs: exporter.GPUComputeServerIDs, scrapePort: platformdomain.DeviceMetricsExporterPort,
			dashboardBundle: platformdomain.DeviceMetricsExporterDashboardBundle, trustedVars: exporter.TrustedVars,
			stepID: deviceMetricsExporterStepID, stepName: "Install the Device Metrics Exporter",
			job: jobConfigureDeviceMetricsExporter, playbook: deviceMetricsExporterPlaybook,
			workflowKind: operationdomain.WorkflowKindConfigureDeviceMetricsExporter,
			ownerParam:   deviceMetricsExporterStepParamOwner, recordParam: deviceMetricsExporterStepParamRecord,
			prometheusPortVar: "swallow_device_metrics_exporter_prometheus_port",
			grafanaPortVar:    "swallow_device_metrics_exporter_grafana_port",
		}
	default:
		return nil
	}
}

// composedSoftwarePlaybook resolves a composed kind's configure Workflow kind and playbook from the
// same table the standalone software launcher uses, so a composed install runs the same playbook.
func composedSoftwarePlaybook(kind softwaredomain.Kind) (softwarePlaybook, error) {
	mapping, ok := softwareInstallPlaybooks[kind]
	if !ok {
		return softwarePlaybook{}, fmt.Errorf("%w: %s", softwaredomain.ErrUnknownKind, kind)
	}
	return mapping, nil
}

// composedPlan is the composed part of a Slurm deployment: the Tasks to append after
// install-platform and the software installs whose pending assignments are written once the
// Workflow exists.
type composedPlan struct {
	steps    []operationdomain.Task
	installs []softwareapp.ComposedInstall
}

// planSlurmComposition validates and builds the container runtime and monitoring feature Jobs. provisionFirst
// lets the deployment's provision-os Tasks satisfy the deployed-state requirement of every composed
// ansible Task. A composed install the software policy refuses (for example Podman where Docker CE
// is installed, the two being mutually exclusive) is reported as ErrComposedSoftwareConflict before
// anything is created.
func (l platformDeploymentLauncher) planSlurmComposition(
	ctx context.Context,
	launch platformdomain.DeploymentLaunch,
	operationID string,
	provisionFirst bool,
) (composedPlan, error) {
	plan := composedPlan{}
	monitoring := slurmMonitoringOf(launch)
	if launch.ContainerRuntime == nil && monitoring == nil {
		return plan, nil
	}
	if l.software == nil {
		switch {
		case launch.Omnistat != nil:
			return plan, fmt.Errorf("%w: software composition is unavailable", platformdomain.ErrInvalidOmnistat)
		case launch.DeviceMetricsExporter != nil:
			return plan, fmt.Errorf("%w: software composition is unavailable", platformdomain.ErrInvalidDeviceMetricsExporter)
		}
		return plan, fmt.Errorf("%w: software composition is unavailable", platformdomain.ErrInvalidContainerRuntime)
	}
	previous := installPlatformStepID

	runtime, runtimeTargets, err := l.composedContainerRuntime(ctx, launch)
	if err != nil {
		return plan, err
	}
	if len(runtimeTargets) > 0 {
		previous, err = l.appendComposedInstall(ctx, &plan, launch, operationID, provisionFirst, previous,
			runtime, runtimeTargets, containerRuntimeSpec(runtime),
			"Install "+softwareLabel(runtime)+" on "+launch.Platform.Name)
		if err != nil {
			return plan, err
		}
	}

	if monitoring == nil {
		return plan, nil
	}
	observability := []string{monitoring.observabilityServerID}
	for _, kind := range monitoringComposedSoftware {
		previous, err = l.appendComposedInstall(ctx, &plan, launch, operationID, provisionFirst, previous,
			kind, observability, monitoringSoftwareSpec(kind, monitoring, runtime),
			"Install "+softwareLabel(kind)+" for "+monitoring.label+" on "+launch.Platform.Name)
		if err != nil {
			return plan, err
		}
	}

	trustedVars := make(map[string]any, len(monitoring.trustedVars)+2)
	for key, value := range monitoring.trustedVars {
		trustedVars[key] = value
	}
	// The software domain owns the Prometheus and Grafana ports; the exporter allowlist and the
	// result URLs must use the same ones.
	trustedVars[monitoring.prometheusPortVar] = softwaredomain.PrometheusPort
	trustedVars[monitoring.grafanaPortVar] = softwaredomain.GrafanaPort
	prepared, err := l.operations.PrepareAnsibleStep(ctx, operationapp.CreateExecutionInput{
		Kind: string(monitoring.workflowKind), PlaybookName: monitoring.playbook,
		Intent:          "Install " + monitoring.label + " on " + launch.Platform.Name,
		TargetServerIDs: launch.TargetServerIDs, PlatformID: launch.Platform.ID,
		TrustedVars: trustedVars, RequestedBy: launch.RequestedBy,
	}, operationID, provisionFirst)
	if err != nil {
		return plan, err
	}
	plan.steps = append(plan.steps, operationdomain.Task{
		ID: monitoring.stepID, Kind: "ansible-playbook", Name: monitoring.stepName,
		Job:      monitoring.job,
		Executor: operationdomain.RunnerKindAnsible, DependsOn: []string{previous},
		Targets: prepared.Targets,
		Parameters: map[string]any{
			"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars,
			monitoring.ownerParam: launch.Platform.ID, monitoring.recordParam: operationID,
		},
	})
	return plan, nil
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
) (string, error) {
	targets := make([]softwareapp.InstallTarget, len(serverIDs))
	for i, id := range serverIDs {
		targets[i] = softwareapp.InstallTarget{ServerID: id}
	}
	install, err := l.software.PlanComposedInstall(ctx, softwareapp.InstallInput{
		Kind: kind, Targets: targets, Spec: spec, RequestedBy: launch.RequestedBy,
	})
	if err != nil {
		return "", fmt.Errorf("%w: %s: %v", platformdomain.ErrComposedSoftwareConflict, softwareLabel(kind), err)
	}
	mapping, err := composedSoftwarePlaybook(kind)
	if err != nil {
		return "", err
	}
	prepared, err := l.operations.PrepareAnsibleStep(ctx, operationapp.CreateExecutionInput{
		Kind: string(mapping.workflowKind), PlaybookName: mapping.playbook,
		Intent: intent, TargetServerIDs: install.ServerIDs, PlatformID: launch.Platform.ID,
		TrustedVars: install.TrustedVars, RequestedBy: launch.RequestedBy,
	}, operationID, provisionFirst)
	if err != nil {
		return "", err
	}
	installID := "install-" + string(kind)
	recordID := "record-" + string(kind)
	job := "configure-" + string(kind)
	plan.steps = append(plan.steps,
		operationdomain.Task{
			ID: installID, Kind: "ansible-playbook", Name: "Install " + softwareLabel(kind),
			Job:      job,
			Executor: operationdomain.RunnerKindAnsible, DependsOn: []string{previous},
			Targets: prepared.Targets,
			// The software identity lets the software step observer mark the assignments failed
			// when this Task fails and the record Task never runs.
			Parameters: map[string]any{
				"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars,
				"softwareKind": string(kind), "serverIds": install.ServerIDs, "softwareMode": "install",
			},
		},
		operationdomain.Task{
			ID: recordID, Kind: softwareStepKindRecord, Name: "Record " + softwareLabel(kind) + " assignment",
			Job:        job,
			Executor:   operationdomain.RunnerKindInternal,
			DependsOn:  []string{installID},
			Targets:    serverResourceRefs(install.ServerIDs),
			Parameters: map[string]any{"softwareKind": string(kind), "serverIds": install.ServerIDs},
		},
	)
	plan.installs = append(plan.installs, install)
	return recordID, nil
}

// composedContainerRuntime returns the container runtime the deployment composes and the Servers
// that still need it. The containerRuntime feature names the runtime and puts it on the compute
// and login nodes; a monitoring feature's container method adds the observability Server and,
// without the feature, runs on the runtime already assigned there (Docker CE when none is). A Server whose
// runtime is already installed is left alone, so an operator's own runtime settings (an enabled
// Docker Engine API, for example) are never reverted. The kind is "" when no runtime is needed.
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
	if monitoring := slurmMonitoringOf(launch); monitoring != nil && monitoring.installMethod != platformdomain.ObservabilityInstallNative {
		if kind == "" {
			assigned, err := l.assignedContainerRuntime(ctx, monitoring.observabilityServerID)
			if err != nil {
				return "", nil, err
			}
			kind = assigned
		}
		if !slices.Contains(serverIDs, monitoring.observabilityServerID) {
			serverIDs = append(serverIDs, monitoring.observabilityServerID)
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
// API off: neither Slurm workloads nor Omnistat need it.
func containerRuntimeSpec(kind softwaredomain.Kind) map[string]any {
	if kind == softwaredomain.KindDockerCE {
		return map[string]any{softwaredomain.SpecEnableAPI: false}
	}
	return map[string]any{}
}

// monitoringSoftwareSpec builds Prometheus's and Grafana's specs for a monitoring feature: its
// install method, on the composed runtime when it is the container method. Prometheus scrapes every
// exporter node by Server id (resolved to its current address on each run, so a provision_os deploy
// whose addresses are not known yet still converges); Grafana reads that Prometheus and provisions
// the feature's dashboards.
func monitoringSoftwareSpec(kind softwaredomain.Kind, monitoring *slurmMonitoring, runtime softwaredomain.Kind) map[string]any {
	spec := map[string]any{softwaredomain.SpecInstallMethod: string(softwaredomain.InstallMethodNative)}
	if monitoring.installMethod != platformdomain.ObservabilityInstallNative {
		spec[softwaredomain.SpecInstallMethod] = string(softwaredomain.InstallMethodContainer)
		spec[softwaredomain.SpecContainerRuntime] = string(runtime)
	}
	switch kind {
	case softwaredomain.KindPrometheus:
		// Only Grafana on the same Server reads the feature's Prometheus, and its API has no
		// authentication, so it never listens beyond the loopback address.
		spec[softwaredomain.SpecListen] = string(softwaredomain.PrometheusListenLoopback)
		spec[softwaredomain.SpecScrapeJob] = monitoring.scrapeJob
		spec[softwaredomain.SpecScrapeServerIDs] = monitoring.scrapeServerIDs
		spec[softwaredomain.SpecScrapePort] = monitoring.scrapePort
	case softwaredomain.KindGrafana:
		spec[softwaredomain.SpecDashboards] = []string{monitoring.dashboardBundle}
	}
	return spec
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

// omnistatStepObserver projects the install-omnistat Task onto the Platform's Omnistat State
// (implements temporalworkflow.StepProjectionObserver); every other Task is ignored. The write is
// fenced on the record id stamped on the Task, so a superseded Workflow — or a Task of a deleted
// Platform — changes nothing.
type omnistatStepObserver struct {
	recorder platformdomain.OmnistatRecorder
}

// ObserveStep maps the Task status: running is installing, success is ready, and failure,
// requires_attention, cancel, or skip (an earlier Job failed) is failed.
func (o omnistatStepObserver) ObserveStep(ctx context.Context, operationID string, step operationdomain.Task) error {
	if step.ID != omnistatStepID || step.Kind != "ansible-playbook" || step.Parameters == nil {
		return nil
	}
	platformID, _ := step.Parameters[omnistatStepParamOwner].(string)
	if platformID == "" {
		return nil
	}
	recordID, _ := step.Parameters[omnistatStepParamRecord].(string)
	if recordID == "" {
		recordID = operationID
	}
	state, ok := omnistatStateForTask(step.Status)
	if !ok {
		return nil
	}
	_, err := o.recorder.UpdateOmnistatState(ctx, platformID, recordID, state, time.Now().UTC())
	return err
}

// omnistatStateForTask returns the state a Task status implies, or false for a status that does
// not move it (pending leaves the state recorded at acceptance).
func omnistatStateForTask(status operationdomain.TaskStatus) (platformdomain.OmnistatState, bool) {
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

func installTaskPhaseOf(status operationdomain.TaskStatus) installTaskPhase {
	switch status {
	case operationdomain.TaskRunning, operationdomain.TaskWaitingExternal:
		return installPhaseRunning
	case operationdomain.TaskSucceeded:
		return installPhaseSucceeded
	case operationdomain.TaskFailed, operationdomain.TaskRequiresAttention, operationdomain.TaskCanceled,
		operationdomain.TaskSkipped:
		return installPhaseFailed
	default:
		return installPhaseNone
	}
}

// deviceMetricsExporterStepObserver projects the install-device-metrics-exporter Task onto the
// Platform's Device Metrics Exporter State (implements temporalworkflow.StepProjectionObserver);
// every other Task is ignored. Like Omnistat's, the write is fenced on the record id stamped on the
// Task, so a superseded Workflow — or a Task of a deleted Platform — changes nothing.
type deviceMetricsExporterStepObserver struct {
	recorder platformdomain.DeviceMetricsExporterRecorder
}

// ObserveStep maps the Task status through installTaskPhaseOf.
func (o deviceMetricsExporterStepObserver) ObserveStep(ctx context.Context, operationID string, step operationdomain.Task) error {
	if step.ID != deviceMetricsExporterStepID || step.Kind != "ansible-playbook" || step.Parameters == nil {
		return nil
	}
	platformID, _ := step.Parameters[deviceMetricsExporterStepParamOwner].(string)
	if platformID == "" {
		return nil
	}
	recordID, _ := step.Parameters[deviceMetricsExporterStepParamRecord].(string)
	if recordID == "" {
		recordID = operationID
	}
	var state platformdomain.DeviceMetricsExporterState
	switch installTaskPhaseOf(step.Status) {
	case installPhaseRunning:
		state = platformdomain.DeviceMetricsExporterInstalling
	case installPhaseSucceeded:
		state = platformdomain.DeviceMetricsExporterReady
	case installPhaseFailed:
		state = platformdomain.DeviceMetricsExporterFailed
	default:
		return nil
	}
	_, err := o.recorder.UpdateDeviceMetricsExporterState(ctx, platformID, recordID, state, time.Now().UTC())
	return err
}
