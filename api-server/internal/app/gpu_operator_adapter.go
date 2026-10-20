package app

import (
	"context"
	"fmt"
	"time"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
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
	prepared *operationapp.PreparedAnsibleStep,
	platformID, recordID string,
	dependsOn []string,
	name string,
) operationdomain.Task {
	mode, _ := prepared.ExtraVars["swallow_amd_gpu_operator_state"].(string)
	return operationdomain.Task{
		ID: gpuOperatorStepID, Kind: "ansible-playbook", Name: name,
		Job:      jobConfigureGPUOperator,
		Executor: operationdomain.RunnerKindAnsible, DependsOn: dependsOn,
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
	operations *operationapp.ExecutionService,
	operationID, platformID, intent string,
	targets []string,
	trustedVars map[string]any,
	requestedBy string,
	provisionFirst bool,
) (*operationapp.PreparedAnsibleStep, error) {
	return operations.PrepareAnsibleStep(ctx, operationapp.CreateExecutionInput{
		Kind: string(operationdomain.WorkflowKindConfigureGPUOperator), PlaybookName: gpuOperatorPlaybook,
		Intent: intent, TargetServerIDs: targets, PlatformID: platformID,
		TrustedVars: trustedVars, RequestedBy: requestedBy,
	}, operationID, provisionFirst)
}

// gpuOperatorLauncher implements platformdomain.GPUOperatorLauncher: prepare-hosts (SSH
// readiness) followed by the configure-gpu-operator Job, over the Platform's deployment targets.
// It requires durable orchestration; there is no legacy single-step path.
type gpuOperatorLauncher struct {
	operations     *operationapp.ExecutionService
	orchestrations *operationapp.WorkflowService
}

// LaunchGPUOperator creates the Workflow under the preassigned id. Busy, lock, and automation
// errors from the operation layer are returned unchanged so the API maps them to 409 / 503.
func (l gpuOperatorLauncher) LaunchGPUOperator(ctx context.Context, launch platformdomain.GPUOperatorLaunch) (string, error) {
	if l.orchestrations == nil {
		return "", fmt.Errorf("durable GPU Operator changes are unavailable")
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
		return "", err
	}
	steps := []operationdomain.Task{
		{
			ID: softwareWaitStepID, Kind: "wait-for-ssh", Name: "Verify SSH readiness",
			Job:      jobPrepareHosts,
			Executor: operationdomain.RunnerKindInternal, Targets: prepared.Targets,
		},
		gpuOperatorTask(prepared, launch.Platform.ID, launch.WorkflowID, []string{softwareWaitStepID}, stepName),
	}
	created, err := l.orchestrations.Create(ctx, operationapp.CreateWorkflowInput{
		ID: launch.WorkflowID, Kind: operationdomain.WorkflowKindConfigureGPUOperator,
		IntentSummary:  summary,
		IntentSnapshot: map[string]any{"platformName": launch.Platform.Name, "extraVars": prepared.ExtraVars},
		Definition:     "platform-gpu-operator", DefinitionVersion: 1,
		SiteID: prepared.SiteID, PlatformID: launch.Platform.ID,
		TargetServerIDs: launch.TargetServerIDs,
		TargetResources: []operationdomain.ResourceReference{{Kind: "platform", ID: launch.Platform.ID}},
		Steps:           steps, RequestedBy: launch.RequestedBy, RequestCorrelation: launch.RequestCorrelation,
	})
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
func (o gpuOperatorStepObserver) ObserveStep(ctx context.Context, operationID string, step operationdomain.Task) error {
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
func gpuOperatorStateForTask(status operationdomain.TaskStatus, uninstall bool) (platformdomain.GPUOperatorState, bool) {
	switch status {
	case operationdomain.TaskRunning, operationdomain.TaskWaitingExternal:
		if uninstall {
			return platformdomain.GPUOperatorUninstalling, true
		}
		return platformdomain.GPUOperatorInstalling, true
	case operationdomain.TaskSucceeded:
		if uninstall {
			return platformdomain.GPUOperatorAbsent, true
		}
		return platformdomain.GPUOperatorReady, true
	case operationdomain.TaskFailed, operationdomain.TaskRequiresAttention, operationdomain.TaskCanceled,
		// A skipped Task applied nothing, so the requested settings are not in effect.
		operationdomain.TaskSkipped:
		return platformdomain.GPUOperatorFailed, true
	default:
		return "", false
	}
}
