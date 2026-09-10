package app

import (
	"context"
	"errors"
	"testing"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/operation/infra/temporalworkflow"
)

// recordingFinalizer captures CompleteUninstall calls and returns a configurable error.
type recordingFinalizer struct {
	ids []string
	err error
}

func (f *recordingFinalizer) CompleteUninstall(_ context.Context, platformID string) error {
	f.ids = append(f.ids, platformID)
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
