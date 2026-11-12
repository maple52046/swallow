package app

import (
	"context"
	"strings"
	"testing"

	"github.com/maple52046/swallow/internal/adapters/executor/temporalworkflow"
	"github.com/maple52046/swallow/internal/core/store/storetest"
	validationapp "github.com/maple52046/swallow/internal/core/validation/application"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
	"github.com/maple52046/swallow/internal/core/validation/domain/validationtest"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// complete-validation lets a report run end, passes a gate whose gating checks passed, and fails a
// gate whose gating check did not pass with a retryable validation_gate_failed that names the check,
// so the deployment parks at the suite's Job (validation.md §5).
func TestCompleteValidationGatesTheDeployment(t *testing.T) {
	ctx := context.Background()
	runs := validationtest.NewRuns()
	recorder, err := validationapp.NewRecorder(&storetest.Transactor{}, runs)
	if err != nil {
		t.Fatal(err)
	}
	executor := platformWorkflowStepExecutor{validations: recorder}
	complete := func(runID string, mode validationdomain.Mode) temporalworkflow.StepExecutionResult {
		return executor.Execute(ctx, temporalworkflow.StepExecutionInput{OperationID: "deploy", Step: workflowdomain.Task{
			ID: gateCompleteTaskID, Kind: completeValidationTaskKind,
			Parameters: map[string]any{completionParamRun: runID, completionParamMode: string(mode)},
		}})
	}
	gate := func(id string, gpuVerdict validationdomain.Verdict) {
		run := validationdomain.Run{ID: id, WorkflowID: "deploy", Mode: validationdomain.ModeGate, Status: validationdomain.StatusRunning,
			Suite:  validationdomain.RunSuite{ID: slurmAcceptanceSuite},
			Checks: []validationdomain.Check{{ID: "gpu-gres", Gating: true}, {ID: "shared-node"}},
			Target: validationdomain.Target{Kind: validationdomain.TargetPlatform, PlatformID: "p", MemberIDs: []string{"c-1"}}}
		run.RecordResults([]validationdomain.Result{
			{Check: "gpu-gres", Target: validationdomain.ResultTarget{Kind: validationdomain.ResultTargetServer, ID: "c-1"}, Verdict: gpuVerdict, Summary: "7 of 8 GPUs"},
			{Check: "shared-node", Target: validationdomain.ResultTarget{Kind: validationdomain.ResultTargetPlatform, ID: "p"}, Verdict: validationdomain.VerdictFailed},
		})
		if err := runs.Create(ctx, run); err != nil {
			t.Fatal(err)
		}
	}

	if result := complete("report", validationdomain.ModeReport); result.Status != workflowdomain.TaskSucceeded {
		t.Errorf("report run: %+v, want succeeded", result)
	}
	gate("passing", validationdomain.VerdictPassed)
	if result := complete("passing", validationdomain.ModeGate); result.Status != workflowdomain.TaskSucceeded {
		t.Errorf("gate whose only failure does not gate: %+v, want succeeded", result)
	}
	gate("failing", validationdomain.VerdictFailed)
	result := complete("failing", validationdomain.ModeGate)
	if result.Status != workflowdomain.TaskFailed || result.Error == nil || result.Error.Code != "validation_gate_failed" || !result.Error.Retryable ||
		!strings.Contains(result.Error.Message, "gpu-gres on server c-1: failed (7 of 8 GPUs)") || strings.Contains(result.Error.Message, "shared-node") {
		t.Errorf("gate with a failed gating check: %+v, want retryable validation_gate_failed naming gpu-gres only", result.Error)
	}
}
