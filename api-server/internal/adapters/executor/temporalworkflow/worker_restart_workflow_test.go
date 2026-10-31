package temporalworkflow

import (
	"context"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// A worker shutdown is infrastructure turnover, not an operator-visible Task failure. The
// activity retry must resume the same Job/Task attempt and let its provider idempotency key
// reconnect to the existing external execution without entering requires_attention.
func TestOperationWorkflowV1RetriesExecuteActivityAfterWorkerShutdown(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(JobWorkflowV1, workflow.RegisterOptions{Name: JobWorkflowName})
	attempts := 0
	states := []operationdomain.WorkflowStatus{}

	env.RegisterActivityWithOptions(func(_ context.Context, _ LeaseRequest) ([]operationdomain.ResourceLease, error) {
		return []operationdomain.ResourceLease{{ResourceKey: "server:a", Owner: "workflow", FencingToken: 1}}, nil
	}, activity.RegisterOptions{Name: ActivityAcquireLeases})
	env.RegisterActivityWithOptions(func(_ context.Context, _ LeaseRenewal) error { return nil },
		activity.RegisterOptions{Name: ActivityRenewLeases})
	env.RegisterActivityWithOptions(func(_ context.Context, _ []operationdomain.ResourceLease) error { return nil },
		activity.RegisterOptions{Name: ActivityReleaseLeases})
	env.RegisterActivityWithOptions(func(_ context.Context, _ StepUpdate) error { return nil },
		activity.RegisterOptions{Name: ActivityUpdateStep})
	env.RegisterActivityWithOptions(func(_ context.Context, input StateUpdate) error {
		states = append(states, input.Status)
		return nil
	}, activity.RegisterOptions{Name: ActivityUpdateState})
	env.RegisterActivityWithOptions(func(_ context.Context, _ StepExecutionInput) (StepExecutionResult, error) {
		attempts++
		if attempts == 1 {
			return StepExecutionResult{}, temporal.NewApplicationError("worker is stopping", "worker_shutdown")
		}
		return StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}, nil
	}, activity.RegisterOptions{Name: ActivityExecuteStep})

	env.ExecuteWorkflow(OperationWorkflowV1, WorkflowInput{
		OperationID: "operation-worker-restart", Kind: operationdomain.WorkflowKindConfigureSlurm,
		SiteID: "site-a", Definition: "platform-deployment", DefinitionVersion: 1,
		LeaseDuration: time.Minute, ResourceKeys: []string{"server:a"},
		Steps: []operationdomain.Task{{
			ID: "provision", Job: "ensure-os", Kind: "provision-os", Name: "Ensure OS",
			Executor: operationdomain.RunnerKindProvisioner, Status: operationdomain.TaskPending, Attempt: 1,
		}},
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed after activity worker turnover: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("execute activity attempts = %d, want 2", attempts)
	}
	for _, state := range states {
		if state == operationdomain.WorkflowRequiresAttention || state == operationdomain.WorkflowCanceled || state == operationdomain.WorkflowCanceling {
			t.Fatalf("worker turnover leaked into operator-visible state: %v", states)
		}
	}
	if len(states) == 0 || states[len(states)-1] != operationdomain.WorkflowSucceeded {
		t.Fatalf("final states = %v, want succeeded", states)
	}
}
