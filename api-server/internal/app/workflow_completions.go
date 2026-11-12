package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	"github.com/maple52046/swallow/modules/platform/kubernetes/kubeapi"
	"github.com/maple52046/swallow/modules/platform/slurm/slurmapi"
)

// The completion types a Task may declare (decision 072). Projection types write a read model on
// every transition of the declaring Task; result types record what a succeeded Task's playbook
// returned. Launchers declare them; the handlers below implement them.
const (
	completionServerDeployment            = "server-deployment"
	completionServerDeploymentCleared     = "server-deployment-cleared"
	completionSoftwareFailure             = "software-assignment-failure"
	completionGPUOperatorState            = "gpu-operator-state"
	completionOmnistatState               = "omnistat-state"
	completionDeviceMetricsExporterState  = "device-metrics-exporter-state"
	completionOmnistatResult              = "omnistat-result"
	completionDeviceMetricsExporterResult = "device-metrics-exporter-result"
	// completionOmnistatHardwareCounters records each compute node's hardware counters outcome the
	// configure-omnistat run reported, fenced on the Omnistat record.
	completionOmnistatHardwareCounters = "omnistat-hardware-counters"
	completionPlatformUninstall        = "platform-uninstall"
	// completionPlatformGrafanaResult records the Grafana URL a Platform's own Grafana install
	// reported as the Platform's Platform Grafana Monitoring Link.
	completionPlatformGrafanaResult = "platform-grafana-result"
)

// Completion parameter names.
const (
	completionParamPlatform = "platformId"
	completionParamRecord   = "recordId"
	completionParamMode     = "mode"
	completionParamKind     = "softwareKind"
	completionParamServers  = "serverIds"
	// completionParamDeleteRecord is the platform-uninstall completion's param: whether the user
	// asked a kept-servers uninstall to remove the Platform record.
	completionParamDeleteRecord = "deleteRecord"
)

// completionClasses registers each completion type's class in the plan catalog.
func completionClasses() map[string]workflowdomain.CompletionClass {
	projection, result := workflowdomain.CompletionProjection, workflowdomain.CompletionResult
	return map[string]workflowdomain.CompletionClass{
		completionServerDeployment: projection, completionServerDeploymentCleared: projection,
		completionSoftwareFailure: projection, completionGPUOperatorState: projection,
		completionOmnistatState: projection, completionDeviceMetricsExporterState: projection,
		kubeapi.CredentialCompletion: result, slurmapi.CredentialCompletion: result,
		completionOmnistatResult: result, completionDeviceMetricsExporterResult: result,
		completionOmnistatHardwareCounters: result,
		completionPlatformUninstall:        result, completionPlatformGrafanaResult: result,
		completionValidationResult: result, completionValidationRunState: projection,
	}
}

// featureState declares a projection keyed by the Platform and the record its state is fenced on.
func featureState(kind, platformID, recordID string) workflowdomain.Completion {
	return workflowdomain.Completion{Type: kind, Params: map[string]any{completionParamPlatform: platformID, completionParamRecord: recordID}}
}

// softwareFailure declares the failure marking of a software install's Assignments.
func softwareFailure(kind softwaredomain.Kind, serverIDs []string) workflowdomain.Completion {
	return workflowdomain.Completion{Type: completionSoftwareFailure, Params: map[string]any{
		completionParamKind: string(kind), completionParamServers: serverIDs,
	}}
}

// projectionFunc adapts a function to workflowapp.ProjectionHandler.
type projectionFunc func(ctx context.Context, transition workflowapp.TaskTransition) error

func (f projectionFunc) Project(ctx context.Context, transition workflowapp.TaskTransition) error {
	return f(ctx, transition)
}

// resultFunc adapts a function to workflowapp.ResultHandler.
type resultFunc func(ctx context.Context, result workflowapp.TaskResult) error

func (f resultFunc) Complete(ctx context.Context, result workflowapp.TaskResult) error {
	return f(ctx, result)
}

// workflowProjections returns the projection handlers every process that records Task transitions
// registers. Each runs inside the transition's transaction.
func workflowProjections(
	servers serverdomain.ServerRepository,
	assignments softwaredomain.AssignmentRepository,
	gpuOperator platformdomain.GPUOperatorRecorder,
	omnistat platformdomain.OmnistatRecorder,
	exporter platformdomain.DeviceMetricsExporterRecorder,
) map[string]workflowapp.ProjectionHandler {
	deployments := serverDeploymentStepObserver{servers: servers}
	return map[string]workflowapp.ProjectionHandler{
		completionServerDeployment: projectionFunc(func(ctx context.Context, t workflowapp.TaskTransition) error {
			return deployments.projectDeployment(ctx, t.Workflow.ID, t.Task)
		}),
		// A succeeded Task that declares it (release-os, recover-server) took the Server's OS away:
		// its deployment record and the Managed Software that lived on that OS are cleared.
		completionServerDeploymentCleared: projectionFunc(func(ctx context.Context, t workflowapp.TaskTransition) error {
			serverID := firstTargetServer(t.Task)
			if serverID == "" || t.Task.Status != workflowdomain.TaskSucceeded {
				return nil
			}
			if err := servers.SetDeployment(ctx, serverID, nil); err != nil {
				return err
			}
			return clearSoftwareAssignments(ctx, assignments, serverID)
		}),
		completionSoftwareFailure: projectionFunc(func(ctx context.Context, t workflowapp.TaskTransition) error {
			kind, _ := t.Completion.Params[completionParamKind].(string)
			if kind == "" || !softwareStepTerminalFailure(t.Task.Status) {
				return nil
			}
			for _, serverID := range coerceStringSlice(t.Completion.Params[completionParamServers]) {
				err := assignments.SetState(ctx, serverID, softwaredomain.Kind(kind), softwaredomain.StateFailed, "", nil)
				// The pending record is written just after the Workflow is accepted, so a very early
				// failure can race ahead of it; the failure stays visible on the Workflow.
				if err != nil && !errors.Is(err, softwaredomain.ErrAssignmentNotFound) {
					return err
				}
			}
			return nil
		}),
		completionGPUOperatorState: projectionFunc(func(ctx context.Context, t workflowapp.TaskTransition) error {
			platformID, recordID := featureIdentity(t)
			mode, _ := t.Completion.Params[completionParamMode].(string)
			if platformID == "" || (mode != "present" && mode != "absent") {
				return nil
			}
			state, ok := gpuOperatorStateForTask(t.Task.Status, mode == "absent")
			if !ok {
				return nil
			}
			_, err := gpuOperator.UpdateGPUOperatorState(ctx, platformID, recordID, state, time.Now().UTC())
			return err
		}),
		completionOmnistatState: projectionFunc(func(ctx context.Context, t workflowapp.TaskTransition) error {
			platformID, recordID := featureIdentity(t)
			state, ok := omnistatStateForTask(t.Task.Status)
			if platformID == "" || !ok {
				return nil
			}
			_, err := omnistat.UpdateOmnistatState(ctx, platformID, recordID, state, time.Now().UTC())
			return err
		}),
		completionDeviceMetricsExporterState: projectionFunc(func(ctx context.Context, t workflowapp.TaskTransition) error {
			platformID, recordID := featureIdentity(t)
			var state platformdomain.DeviceMetricsExporterState
			switch installTaskPhaseOf(t.Task.Status) {
			case installPhaseRunning:
				state = platformdomain.DeviceMetricsExporterInstalling
			case installPhaseSucceeded:
				state = platformdomain.DeviceMetricsExporterReady
			case installPhaseFailed:
				state = platformdomain.DeviceMetricsExporterFailed
			default:
				return nil
			}
			if platformID == "" {
				return nil
			}
			_, err := exporter.UpdateDeviceMetricsExporterState(ctx, platformID, recordID, state, time.Now().UTC())
			return err
		}),
	}
}

// clearSoftwareAssignments marks every live Software Assignment of a Server whose OS is gone
// absent, keeping each record's last Workflow: the software went with the OS, and an assignment
// must not outlive it (decision 038).
func clearSoftwareAssignments(ctx context.Context, assignments softwaredomain.AssignmentRepository, serverID string) error {
	records, err := assignments.List(ctx, softwaredomain.AssignmentFilter{ServerID: serverID})
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.State == softwaredomain.StateAbsent {
			continue
		}
		err := assignments.SetState(ctx, serverID, record.Kind, softwaredomain.StateAbsent, "", nil)
		if err != nil && !errors.Is(err, softwaredomain.ErrAssignmentNotFound) {
			return err
		}
	}
	return nil
}

// installedSoftwareRecords implements provisioningapp.InstalledSoftware over the Software
// Assignment repository, so reconcile clears the records of software whose OS is gone.
type installedSoftwareRecords struct {
	assignments softwaredomain.AssignmentRepository
}

func (r installedSoftwareRecords) ClearForServer(ctx context.Context, serverID string) error {
	return clearSoftwareAssignments(ctx, r.assignments, serverID)
}

// featureIdentity reads the Platform and fence record of a feature-state projection; the record
// defaults to the Workflow, which is what the launcher fences a fresh install on.
func featureIdentity(t workflowapp.TaskTransition) (string, string) {
	platformID, _ := t.Completion.Params[completionParamPlatform].(string)
	recordID, _ := t.Completion.Params[completionParamRecord].(string)
	if recordID == "" {
		recordID = t.Workflow.ID
	}
	return platformID, recordID
}

// workflowResults returns the result handlers of the process that runs the Ansible Runner: each
// records what a succeeded playbook returned, before the Task's outcome is recorded. Each Platform
// module's credential reader handles the credential completion its deploy Task declares.
func workflowResults(observer platformDeploymentObserver, credentials ...platformdomain.CredentialReader) map[string]workflowapp.ResultHandler {
	handlers := map[string]workflowapp.ResultHandler{
		completionPlatformGrafanaResult: resultFunc(func(ctx context.Context, r workflowapp.TaskResult) error {
			return observer.recordPlatformGrafana(ctx, r.Workflow.PlatformID, workflowdomain.RunnerResult{Data: r.Data})
		}),
		// A configure-omnistat Task planned while its Grafana had no completion of its own declares
		// this; its run reports the same Grafana.
		completionOmnistatResult: resultFunc(func(ctx context.Context, r workflowapp.TaskResult) error {
			return observer.recordPlatformGrafana(ctx, r.Workflow.PlatformID, workflowdomain.RunnerResult{Data: r.Data})
		}),
		completionDeviceMetricsExporterResult: resultFunc(func(ctx context.Context, r workflowapp.TaskResult) error {
			recordID, _ := r.Completion.Params[completionParamRecord].(string)
			return observer.completeDeviceMetricsExporter(ctx, r.Workflow.PlatformID, r.Workflow.ID, recordID,
				workflowdomain.RunnerResult{Data: r.Data})
		}),
		completionOmnistatHardwareCounters: resultFunc(func(ctx context.Context, r workflowapp.TaskResult) error {
			recordID, _ := r.Completion.Params[completionParamRecord].(string)
			return observer.recordHardwareCounters(ctx, r.Workflow.PlatformID, r.Workflow.ID, recordID,
				workflowdomain.RunnerResult{Data: r.Data})
		}),
		completionPlatformUninstall: resultFunc(func(ctx context.Context, r workflowapp.TaskResult) error {
			return observer.finishUninstall(ctx, r.Workflow, uninstallChoiceOf(r.Completion))
		}),
	}
	for _, reader := range credentials {
		handlers[reader.Completion()] = credentialResult(observer, reader)
	}
	return handlers
}

// credentialResult records the read credential a succeeded deploy Task returned. A deployment that
// returned none fails when the module requires one; otherwise it is logged, because the Platform
// works without swallow reading it.
func credentialResult(observer platformDeploymentObserver, reader platformdomain.CredentialReader) workflowapp.ResultHandler {
	return resultFunc(func(ctx context.Context, r workflowapp.TaskResult) error {
		credential, ok := reader.Credential(r.Data)
		if ok {
			return observer.credentials.Record(ctx, r.Workflow.PlatformID, credential)
		}
		if reader.Required() {
			return fmt.Errorf("deployment returned no %s credential", reader.PlatformType())
		}
		slog.Warn("platform deployment produced no reader credential; membership will not populate until its API is available",
			"operationId", r.Workflow.ID, "platformId", r.Workflow.PlatformID, "platformType", reader.PlatformType())
		return nil
	})
}
