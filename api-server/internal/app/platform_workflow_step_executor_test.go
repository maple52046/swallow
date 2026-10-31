package app

import (
	"context"
	"errors"
	"testing"

	"github.com/maple52046/swallow/internal/adapters/executor/temporalworkflow"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// recordingFinalizer captures CompleteUninstall calls and returns a configurable error.
type recordingFinalizer struct {
	ids           []string
	deleteRecords []bool
	err           error
}

func (f *recordingFinalizer) CompleteUninstall(_ context.Context, platformID string, deleteRecord bool) error {
	f.ids = append(f.ids, platformID)
	f.deleteRecords = append(f.deleteRecords, deleteRecord)
	return f.err
}

func completeUninstallInput() temporalworkflow.StepExecutionInput {
	return temporalworkflow.StepExecutionInput{
		PlatformID: "platform-a",
		Step:       operationdomain.Task{ID: completeUninstallStepID, Kind: "complete-uninstall", Name: "Finalize platform uninstall"},
	}
}

func TestCompleteUninstallStepInvokesFinalizer(t *testing.T) {
	finalizer := &recordingFinalizer{}
	executor := platformWorkflowStepExecutor{finalizer: finalizer}

	result := executor.Execute(context.Background(), completeUninstallInput())

	if result.Status != operationdomain.TaskSucceeded {
		t.Fatalf("status = %v, want succeeded", result.Status)
	}
	if len(finalizer.ids) != 1 || finalizer.ids[0] != "platform-a" {
		t.Fatalf("CompleteUninstall calls = %v, want [platform-a]", finalizer.ids)
	}
	// A finalize Task without the parameter (accepted before deleteRecord existed) keeps the record.
	if finalizer.deleteRecords[0] {
		t.Error("CompleteUninstall deleteRecord = true, want false without the Task parameter")
	}
}

func TestCompleteUninstallStepForwardsDeleteRecord(t *testing.T) {
	finalizer := &recordingFinalizer{}
	executor := platformWorkflowStepExecutor{finalizer: finalizer}
	input := completeUninstallInput()
	input.Step.Parameters = map[string]any{deleteRecordParameter: true}

	result := executor.Execute(context.Background(), input)

	if result.Status != operationdomain.TaskSucceeded {
		t.Fatalf("status = %v, want succeeded", result.Status)
	}
	if len(finalizer.deleteRecords) != 1 || !finalizer.deleteRecords[0] {
		t.Fatalf("CompleteUninstall deleteRecord calls = %v, want [true]", finalizer.deleteRecords)
	}
}

func TestCompleteUninstallStepFailsWithoutFinalizer(t *testing.T) {
	executor := platformWorkflowStepExecutor{}

	result := executor.Execute(context.Background(), completeUninstallInput())

	if result.Status != operationdomain.TaskFailed || result.Error == nil {
		t.Fatalf("result = %+v, want a failed result", result)
	}
}

func TestCompleteUninstallStepFailsWhenFinalizerErrors(t *testing.T) {
	finalizer := &recordingFinalizer{err: errors.New("integration delete failed")}
	executor := platformWorkflowStepExecutor{finalizer: finalizer}

	result := executor.Execute(context.Background(), completeUninstallInput())

	if result.Status != operationdomain.TaskFailed || result.Error == nil {
		t.Fatalf("result = %+v, want a failed result", result)
	}
	// The cleanup is idempotent, so a transient failure should be retryable.
	if !result.Error.Retryable {
		t.Errorf("error retryable = false, want true for a transient finalize failure")
	}
}
