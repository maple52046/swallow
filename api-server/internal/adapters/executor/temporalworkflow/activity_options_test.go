package temporalworkflow

import (
	"context"
	"testing"
	"time"
)

func TestExecuteStepActivityOptionsEnableBoundedHeartbeatRetry(t *testing.T) {
	legacy := executeStepActivityOptions(false)
	if legacy.RetryPolicy == nil || legacy.RetryPolicy.MaximumAttempts != 1 || legacy.ScheduleToCloseTimeout != 0 {
		t.Fatalf("legacy options = %+v, want one attempt and no new schedule timeout", legacy)
	}

	restartSafe := executeStepActivityOptions(true)
	if restartSafe.ScheduleToCloseTimeout != 7*24*time.Hour || restartSafe.HeartbeatTimeout != 30*time.Second {
		t.Fatalf("restart-safe timeouts = %+v", restartSafe)
	}
	if restartSafe.RetryPolicy == nil || restartSafe.RetryPolicy.MaximumAttempts != 0 || restartSafe.RetryPolicy.MaximumInterval != 30*time.Second {
		t.Fatalf("restart-safe retry = %+v", restartSafe.RetryPolicy)
	}
}

func TestActivityWorkerStoppingCauseIsDistinctFromWorkflowCancellation(t *testing.T) {
	workerContext, stopWorker := context.WithCancelCause(context.Background())
	stopWorker(ErrActivityWorkerStopping)
	if !IsActivityWorkerStopping(workerContext) {
		t.Fatal("worker stop cause was not recognized")
	}

	workflowContext, cancelWorkflow := context.WithCancelCause(context.Background())
	cancelWorkflow(context.Canceled)
	if IsActivityWorkerStopping(workflowContext) {
		t.Fatal("workflow cancellation was misclassified as worker stop")
	}
}
