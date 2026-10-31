package temporalworkflow

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// executeStepActivityOptions retains the one-attempt history for old workflows and gives new
// histories heartbeat-aware retries bounded by one seven-day schedule-to-close window.
func executeStepActivityOptions(retryWorkerLoss bool) workflow.ActivityOptions {
	options := workflow.ActivityOptions{
		StartToCloseTimeout: 7 * 24 * time.Hour,
		HeartbeatTimeout:    30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	}
	if retryWorkerLoss {
		options.ScheduleToCloseTimeout = 7 * 24 * time.Hour
		options.RetryPolicy = &temporal.RetryPolicy{
			InitialInterval: time.Second, BackoffCoefficient: 2,
			MaximumInterval: 30 * time.Second, MaximumAttempts: 0,
		}
	}
	return options
}
