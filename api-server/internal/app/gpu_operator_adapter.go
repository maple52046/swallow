package app

import (
	"context"
	"fmt"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// GPU Operator composition (decision 062). The same ansible Task installs, changes, or removes the
// Swallow-managed AMD GPU Operator, either as the last Job of a deploy-kubernetes Workflow or as
// the configure-gpu-operator Workflow that changes it later. The playbook name is hardcoded (like
// Slurm and software), so no site playbookMappings entry is needed.
const (
	jobConfigureGPUOperator   = "configure-gpu-operator"
	gpuOperatorStepID         = "install-gpu-operator"
	gpuOperatorPlaybook       = "configure-amd-gpu-operator"
	gpuOperatorStepParamMode  = "gpuOperatorMode"
	gpuOperatorStepParamOwner = "gpuOperatorPlatformId"
	// gpuOperatorStepParamRecord names the Workflow the Platform's GPU Operator record is fenced
	// on. It is a parameter rather than the running Workflow's id because a rerun (decision 060)
	// copies the Task under a new Workflow id and must still report the outcome of that intent.
	gpuOperatorStepParamRecord = "gpuOperatorRecordId"
)

// gpuOperatorTask builds the ansible Task that applies GPU Operator settings. mode is the
// trusted swallow_amd_gpu_operator_state ("present" or "absent"), stamped so the observer knows
// which states the Task's outcome maps to.
func gpuOperatorTask(
	prepared *workflowapp.PreparedAnsibleStep,
	platformID, recordID string,
	dependsOn []string,
	name string,
) workflowdomain.Task {
	mode, _ := prepared.ExtraVars["swallow_amd_gpu_operator_state"].(string)
	return workflowdomain.Task{
		ID: gpuOperatorStepID, Kind: "ansible-playbook", Name: name,
		Job:      jobConfigureGPUOperator,
		Executor: workflowdomain.RunnerKindAnsible, DependsOn: dependsOn,
		Targets: prepared.Targets,
		Parameters: map[string]any{
			"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars,
			gpuOperatorStepParamMode: mode, gpuOperatorStepParamOwner: platformID,
			gpuOperatorStepParamRecord: recordID,
		},
	}
}

// prepareGPUOperatorStep resolves the playbook, targets, and extra vars of the GPU Operator Task
// with the operation layer's catalog, credential, policy, and lock checks. provisionFirst lets a
// deploy's preceding provision-os Tasks satisfy the deployed-state requirement.
func prepareGPUOperatorStep(
	ctx context.Context,
	operations *workflowapp.ExecutionService,
	operationID, platformID, intent string,
	targets []string,
	trustedVars map[string]any,
	requestedBy string,
	provisionFirst bool,
) (*workflowapp.PreparedAnsibleStep, error) {
	return operations.PrepareAnsibleStep(ctx, workflowapp.CreateExecutionInput{
		Kind: string(workflowdomain.WorkflowKindConfigureGPUOperator), PlaybookName: gpuOperatorPlaybook,
		Intent: intent, TargetServerIDs: targets, PlatformID: platformID,
		TrustedVars: trustedVars, RequestedBy: requestedBy,
	}, operationID, provisionFirst)
}

// gpuOperatorLauncher implements platformdomain.GPUOperatorLauncher: prepare-hosts (SSH
// readiness) followed by the configure-gpu-operator Job, over the Platform's deployment targets.
// It requires durable orchestration; there is no legacy single-step path.
type gpuOperatorLauncher struct {
	operations     *workflowapp.ExecutionService
	orchestrations *workflowapp.WorkflowService
}

// PrepareGPUOperator plans the Workflow under the preassigned id without writing it. Lock and
// automation errors from the operation layer are returned unchanged so the API maps them to
// 409 / 503.
func (l gpuOperatorLauncher) PrepareGPUOperator(ctx context.Context, launch platformdomain.GPUOperatorLaunch) (platformdomain.PreparedWorkflow, error) {
	if l.orchestrations == nil {
		return nil, fmt.Errorf("durable GPU Operator changes are unavailable")
	}
	summary := "Configure AMD GPU Operator on " + launch.Platform.Name
	stepName := "Configure AMD GPU Operator"
	if launch.Uninstall {
		summary = "Uninstall AMD GPU Operator from " + launch.Platform.Name
		stepName = "Uninstall AMD GPU Operator"
	}
	prepared, err := prepareGPUOperatorStep(ctx, l.operations, launch.WorkflowID, launch.Platform.ID, summary,
		launch.TargetServerIDs, launch.TrustedVars, launch.RequestedBy, false)
	if err != nil {
		return nil, err
	}
	steps := []workflowdomain.Task{
		{
			ID: softwareWaitStepID, Kind: "wait-for-ssh", Name: "Verify SSH readiness",
			Job:      jobPrepareHosts,
			Executor: workflowdomain.RunnerKindInternal, Targets: prepared.Targets,
		},
		gpuOperatorTask(prepared, launch.Platform.ID, launch.WorkflowID, []string{softwareWaitStepID}, stepName),
	}
	return &preparedPlatformDeployment{workflow: workflowapp.CreateWorkflowInput{
		ID: launch.WorkflowID, Kind: workflowdomain.WorkflowKindConfigureGPUOperator,
		IntentSummary:  summary,
		IntentSnapshot: map[string]any{"platformName": launch.Platform.Name, "extraVars": prepared.ExtraVars},
		Definition:     "platform-gpu-operator", DefinitionVersion: 1,
		SiteID: prepared.SiteID, PlatformID: launch.Platform.ID,
		TargetServerIDs: launch.TargetServerIDs,
		TargetResources: []workflowdomain.ResourceReference{{Kind: "platform", ID: launch.Platform.ID}},
		Steps:           steps, RequestedBy: launch.RequestedBy, RequestCorrelation: launch.RequestCorrelation,
	}}, nil
}

// PersistGPUOperator writes a plan from PrepareGPUOperator; the caller runs it inside the
// transaction that also writes the GPU Operator record. A busy target is detected here and
// returned unchanged.
func (l gpuOperatorLauncher) PersistGPUOperator(ctx context.Context, prepared platformdomain.PreparedWorkflow) (string, error) {
	plan, ok := prepared.(*preparedPlatformDeployment)
	if !ok || plan == nil {
		return "", fmt.Errorf("persist GPU Operator change: plan %T was not prepared by this launcher", prepared)
	}
	created, err := l.orchestrations.Create(ctx, plan.workflow)
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

// gpuOperatorStepObserver projects the GPU Operator Task onto the Platform's GPU Operator State
// (implements temporalworkflow.StepProjectionObserver). It ignores every other Task. The write is
// fenced on the record id stamped on the Task, so a superseded Workflow — or a Task of a deleted
// Platform — changes nothing.
type gpuOperatorStepObserver struct {
	recorder platformdomain.GPUOperatorRecorder
}

// ObserveStep maps the Task status to a GPU Operator State: running is installing or
// uninstalling, success is ready or absent, and failure, requires_attention, cancel, or skip is
// failed.
func (o gpuOperatorStepObserver) ObserveStep(ctx context.Context, operationID string, step workflowdomain.Task) error {
	if step.ID != gpuOperatorStepID || step.Kind != "ansible-playbook" || step.Parameters == nil {
		return nil
	}
	platformID, _ := step.Parameters[gpuOperatorStepParamOwner].(string)
	mode, _ := step.Parameters[gpuOperatorStepParamMode].(string)
	if platformID == "" || (mode != "present" && mode != "absent") {
		return nil
	}
	recordID, _ := step.Parameters[gpuOperatorStepParamRecord].(string)
	if recordID == "" {
		recordID = operationID
	}
	state, ok := gpuOperatorStateForTask(step.Status, mode == "absent")
	if !ok {
		return nil
	}
	_, err := o.recorder.UpdateGPUOperatorState(ctx, platformID, recordID, state, time.Now().UTC())
	return err
}

// gpuOperatorStateForTask returns the state a Task status implies, or false for a status that
// does not move it (pending leaves the state recorded at acceptance).
func gpuOperatorStateForTask(status workflowdomain.TaskStatus, uninstall bool) (platformdomain.GPUOperatorState, bool) {
	switch status {
	case workflowdomain.TaskRunning, workflowdomain.TaskWaitingExternal:
		if uninstall {
			return platformdomain.GPUOperatorUninstalling, true
		}
		return platformdomain.GPUOperatorInstalling, true
	case workflowdomain.TaskSucceeded:
		if uninstall {
			return platformdomain.GPUOperatorAbsent, true
		}
		return platformdomain.GPUOperatorReady, true
	case workflowdomain.TaskFailed, workflowdomain.TaskRequiresAttention, workflowdomain.TaskCanceled,
		// A skipped Task applied nothing, so the requested settings are not in effect.
		workflowdomain.TaskSkipped:
		return platformdomain.GPUOperatorFailed, true
	default:
		return "", false
	}
}
