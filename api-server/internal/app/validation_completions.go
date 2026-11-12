package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/maple52046/swallow/internal/adapters/executor/temporalworkflow"
	validationapp "github.com/maple52046/swallow/internal/core/validation/application"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// validationProjections returns the projection that folds a run's Task transitions into the run
// (validation-run-state). Every process that records Task transitions registers it.
func validationProjections(recorder *validationapp.Recorder) map[string]workflowapp.ProjectionHandler {
	return map[string]workflowapp.ProjectionHandler{
		completionValidationRunState: projectionFunc(func(ctx context.Context, t workflowapp.TaskTransition) error {
			runID, _ := t.Completion.Params[completionParamRun].(string)
			stage, _ := t.Completion.Params[completionParamStage].(string)
			if runID == "" {
				return nil
			}
			return recorder.Project(ctx, runID, validationapp.Stage(stage), t.Workflow, t.Task)
		}),
	}
}

// validationResults returns the result handler that records what a succeeded check Task's playbook
// returned (validation-result). Only the Ansible executor registers result handlers.
func validationResults(recorder *validationapp.Recorder) map[string]workflowapp.ResultHandler {
	return map[string]workflowapp.ResultHandler{
		completionValidationResult: resultFunc(func(ctx context.Context, r workflowapp.TaskResult) error {
			runID, _ := r.Completion.Params[completionParamRun].(string)
			if runID == "" {
				return nil
			}
			return recorder.RecordResults(ctx, runID, r.Workflow, r.Task, r.Data)
		}),
	}
}

// completeValidation runs a run's final Task. In report mode it only lets the run complete through
// its projection. In gate mode it reads the results its deployment's check Tasks recorded and fails,
// retryably, when a gating check did not pass, so the deployment parks the way its own checks used
// to make it park (decisions 063, 066, 081).
func (e platformWorkflowStepExecutor) completeValidation(ctx context.Context, input temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	mode, _ := input.Step.Parameters[completionParamMode].(string)
	if mode != string(validationdomain.ModeGate) {
		return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskSucceeded, Progress: 100}
	}
	runID, _ := input.Step.Parameters[completionParamRun].(string)
	if e.validations == nil || runID == "" {
		return internalStepFailed("validation_gate_unavailable", "The gate's Validation Run cannot be read.", true)
	}
	run, failures, err := e.validations.GateFailures(ctx, runID)
	if err != nil {
		return internalStepFailed("validation_gate_unavailable", "The gate's Validation Run cannot be read: "+err.Error(), true)
	}
	if len(failures) == 0 {
		return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskSucceeded, Progress: 100}
	}
	lines := make([]string, 0, len(failures))
	for _, failure := range failures {
		line := fmt.Sprintf("%s on %s %s: %s", failure.Check, failure.Target.Kind, failure.Target.ID, failure.Verdict)
		if failure.Summary != "" {
			line += " (" + failure.Summary + ")"
		}
		lines = append(lines, line)
	}
	message := fmt.Sprintf("Validation suite %s did not pass: %s.", run.Suite.ID, strings.Join(lines, "; "))
	diagnostic := workflowdomain.NewTaskDiagnostic(workflowdomain.FailurePermanent, fmt.Errorf("%s", message))
	diagnostic.Remediation = fmt.Sprintf("Read Validation Run %s for each check's evidence, fix the failing nodes, then retry this Job to run the suite again.", run.ID)
	if run.Target.PlatformID != "" {
		diagnostic.Target = &workflowdomain.ResourceReference{Kind: "platform", ID: run.Target.PlatformID}
	}
	result := internalStepFailed("validation_gate_failed", message, true)
	result.Error.Diagnostic = diagnostic
	return result
}
