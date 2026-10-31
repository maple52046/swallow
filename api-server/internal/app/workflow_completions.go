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
	completionKubernetesCredential        = "kubernetes-credential"
	completionSlurmCredential             = "slurm-credential"
	completionOmnistatResult              = "omnistat-result"
	completionDeviceMetricsExporterResult = "device-metrics-exporter-result"
	completionPlatformUninstall           = "platform-uninstall"
)

// Completion parameter names.
const (
	completionParamPlatform = "platformId"
	completionParamRecord   = "recordId"
	completionParamMode     = "mode"
	completionParamKind     = "softwareKind"
	completionParamServers  = "serverIds"
)

// completionClasses registers each completion type's class in the plan catalog.
func completionClasses() map[string]workflowdomain.CompletionClass {
	projection, result := workflowdomain.CompletionProjection, workflowdomain.CompletionResult
	return map[string]workflowdomain.CompletionClass{
		completionServerDeployment: projection, completionServerDeploymentCleared: projection,
		completionSoftwareFailure: projection, completionGPUOperatorState: projection,
		completionOmnistatState: projection, completionDeviceMetricsExporterState: projection,
		completionKubernetesCredential: result, completionSlurmCredential: result,
		completionOmnistatResult: result, completionDeviceMetricsExporterResult: result,
		completionPlatformUninstall: result,
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
	platforms interface {
		platformdomain.GPUOperatorRecorder
		platformdomain.OmnistatRecorder
		platformdomain.DeviceMetricsExporterRecorder
	},
) map[string]workflowapp.ProjectionHandler {
	deployments := serverDeploymentStepObserver{servers: servers}
	return map[string]workflowapp.ProjectionHandler{
		completionServerDeployment: projectionFunc(func(ctx context.Context, t workflowapp.TaskTransition) error {
			return deployments.projectDeployment(ctx, t.Workflow.ID, t.Task)
		}),
		completionServerDeploymentCleared: projectionFunc(func(ctx context.Context, t workflowapp.TaskTransition) error {
			serverID := firstTargetServer(t.Task)
			if serverID == "" || t.Task.Status != workflowdomain.TaskSucceeded {
				return nil
			}
			return servers.SetDeployment(ctx, serverID, nil)
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
			_, err := platforms.UpdateGPUOperatorState(ctx, platformID, recordID, state, time.Now().UTC())
			return err
		}),
		completionOmnistatState: projectionFunc(func(ctx context.Context, t workflowapp.TaskTransition) error {
			platformID, recordID := featureIdentity(t)
			state, ok := omnistatStateForTask(t.Task.Status)
			if platformID == "" || !ok {
				return nil
			}
			_, err := platforms.UpdateOmnistatState(ctx, platformID, recordID, state, time.Now().UTC())
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
			_, err := platforms.UpdateDeviceMetricsExporterState(ctx, platformID, recordID, state, time.Now().UTC())
			return err
		}),
	}
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
// records what a succeeded playbook returned, before the Task's outcome is recorded.
func workflowResults(observer platformDeploymentObserver) map[string]workflowapp.ResultHandler {
	return map[string]workflowapp.ResultHandler{
		completionKubernetesCredential: resultFunc(func(ctx context.Context, r workflowapp.TaskResult) error {
			credential := platformCredentialFromResult(workflowdomain.RunnerResult{Data: r.Data})
			if credential == nil {
				return fmt.Errorf("deployment returned no Kubernetes credential")
			}
			return observer.credentials.Record(ctx, r.Workflow.PlatformID, *credential)
		}),
		completionSlurmCredential: resultFunc(func(ctx context.Context, r workflowapp.TaskResult) error {
			credential := slurmCredentialFromResult(workflowdomain.RunnerResult{Data: r.Data})
			if credential == nil {
				// slurmctld and slurmd work without slurmrestd, which only powers membership reads, so
				// its absence is not a deploy failure.
				slog.Warn("slurm deployment produced no reader credential; membership will not populate until slurmrestd is available",
					"operationId", r.Workflow.ID, "platformId", r.Workflow.PlatformID)
				return nil
			}
			return observer.credentials.RecordSlurm(ctx, r.Workflow.PlatformID, *credential)
		}),
		completionOmnistatResult: resultFunc(func(ctx context.Context, r workflowapp.TaskResult) error {
			return observer.completeOmnistat(ctx, r.Workflow.PlatformID, workflowdomain.RunnerResult{Data: r.Data})
		}),
		completionDeviceMetricsExporterResult: resultFunc(func(ctx context.Context, r workflowapp.TaskResult) error {
			return observer.completeDeviceMetricsExporter(ctx, r.Workflow.PlatformID, r.Workflow.ID, taskExtraVars(r.Task),
				workflowdomain.RunnerResult{Data: r.Data})
		}),
		completionPlatformUninstall: resultFunc(func(ctx context.Context, r workflowapp.TaskResult) error {
			return observer.finishUninstall(ctx, &workflowdomain.ExecutionOperation{
				ID: r.Workflow.ID, Kind: r.Workflow.Kind, SiteID: r.Workflow.SiteID, PlatformID: r.Workflow.PlatformID,
				TargetServerIDs: r.Workflow.TargetServerIDs, ExtraVars: taskExtraVars(r.Task),
			})
		}),
	}
}

// taskExtraVars returns the playbook variables frozen on an Ansible Task, which a result handler
// reads to interpret the playbook's result (for example the record a result is fenced on).
func taskExtraVars(task workflowdomain.Task) map[string]any {
	extraVars, _ := task.Parameters["extraVars"].(map[string]any)
	return extraVars
}
