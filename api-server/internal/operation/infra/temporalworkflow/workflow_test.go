package temporalworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

func TestOperationWorkflowV1RunsDependenciesInOrder(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var mu sync.Mutex
	executed := []string{}
	states := []operationdomain.OrchestrationStatus{}

	registerWorkflowActivityMocks(env, func(input StepExecutionInput) StepExecutionResult {
		mu.Lock()
		executed = append(executed, input.Step.ID)
		mu.Unlock()
		return StepExecutionResult{Status: operationdomain.StepSucceeded, Progress: 100}
	}, func(input StateUpdate) {
		states = append(states, input.Status)
	})

	env.ExecuteWorkflow(OperationWorkflowV1, WorkflowInput{
		OperationID: "operation-a", Kind: operationdomain.OperationKindDeployKubernetes,
		SiteID: "site-a", Definition: "test", DefinitionVersion: 1,
		LeaseDuration: time.Minute, MaxParallelism: 1,
		ResourceKeys: []string{"server:a"},
		Steps: []operationdomain.OperationStep{
			{ID: "prepare", Kind: "noop", Name: "Prepare", Executor: operationdomain.StepExecutorInternal, Status: operationdomain.StepPending, Attempt: 1},
			{ID: "install", Kind: "noop", Name: "Install", Executor: operationdomain.StepExecutorInternal, Status: operationdomain.StepPending, Attempt: 1, DependsOn: []string{"prepare"}},
		},
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(executed) != 2 || executed[0] != "prepare" || executed[1] != "install" {
		t.Fatalf("execution order = %v, want [prepare install]", executed)
	}
	if len(states) == 0 || states[len(states)-1] != operationdomain.OrchestrationSucceeded {
		t.Fatalf("last operation state = %v, want succeeded", states)
	}
}

func TestOperationWorkflowV1RetriesFailedStepInSameOperation(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	attempts := 0
	states := []operationdomain.OrchestrationStatus{}
	registerWorkflowActivityMocks(env, func(input StepExecutionInput) StepExecutionResult {
		attempts++
		if attempts == 1 {
			return StepExecutionResult{Status: operationdomain.StepFailed, Error: &operationdomain.NormalizedError{
				Code: "transient", Message: "try again", Retryable: true,
			}}
		}
		if input.Step.Attempt != 2 {
			t.Fatalf("retry attempt = %d, want 2", input.Step.Attempt)
		}
		return StepExecutionResult{Status: operationdomain.StepSucceeded, Progress: 100}
	}, func(input StateUpdate) {
		states = append(states, input.Status)
	})
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(RetryStepSignal, RetryStepCommand{StepID: "step-a"})
	}, time.Second)

	env.ExecuteWorkflow(OperationWorkflowV1, WorkflowInput{
		OperationID: "operation-a", Kind: operationdomain.OperationKindCustom,
		SiteID: "site-a", Definition: "test", DefinitionVersion: 1,
		LeaseDuration: time.Minute, ResourceKeys: []string{"server:a"},
		Steps: []operationdomain.OperationStep{{
			ID: "step-a", Kind: "noop", Name: "Step A", Executor: operationdomain.StepExecutorInternal, Status: operationdomain.StepPending, Attempt: 1,
		}},
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("execute attempts = %d, want 2", attempts)
	}
	seenAttention := false
	for _, state := range states {
		seenAttention = seenAttention || state == operationdomain.OrchestrationRequiresAttention
	}
	if !seenAttention || states[len(states)-1] != operationdomain.OrchestrationSucceeded {
		t.Fatalf("states = %v, want requires_attention then succeeded", states)
	}
}
func TestOperationWorkflowV1CancellationMarksPendingSteps(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	states := []operationdomain.OrchestrationStatus{}
	stepUpdates := []operationdomain.OperationStep{}
	env.RegisterActivityWithOptions(func(_ context.Context, _ LeaseRequest) ([]operationdomain.ResourceLease, error) {
		return nil, errors.New("resources busy")
	}, activity.RegisterOptions{Name: ActivityAcquireLeases})
	env.RegisterActivityWithOptions(func(_ context.Context, _ []operationdomain.ResourceLease) error { return nil },
		activity.RegisterOptions{Name: ActivityReleaseLeases})
	env.RegisterActivityWithOptions(func(_ context.Context, input StepUpdate) error {
		stepUpdates = append(stepUpdates, input.Step)
		return nil
	}, activity.RegisterOptions{Name: ActivityUpdateStep})
	env.RegisterActivityWithOptions(func(_ context.Context, input StateUpdate) error {
		states = append(states, input.Status)
		return nil
	}, activity.RegisterOptions{Name: ActivityUpdateState})
	env.RegisterDelayedCallback(env.CancelWorkflow, time.Second)

	env.ExecuteWorkflow(OperationWorkflowV1, WorkflowInput{
		OperationID: "operation-cancel", Kind: operationdomain.OperationKindCustom,
		SiteID: "site-a", Definition: "test", DefinitionVersion: 1,
		LeaseDuration: time.Minute, ResourceKeys: []string{"server:a"},
		Steps: []operationdomain.OperationStep{{
			ID: "step-a", Kind: "noop", Name: "Step A", Executor: operationdomain.StepExecutorInternal,
			Status: operationdomain.StepPending, Attempt: 1,
		}},
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("canceled workflow returned an error: %v", err)
	}
	if len(states) < 3 || states[len(states)-2] != operationdomain.OrchestrationCanceling ||
		states[len(states)-1] != operationdomain.OrchestrationCanceled {
		t.Fatalf("states = %v, want canceling then canceled", states)
	}
	if len(stepUpdates) != 1 || stepUpdates[0].Status != operationdomain.StepCanceled {
		t.Fatalf("Step updates = %#v, want one canceled pending Step", stepUpdates)
	}
}

func registerWorkflowActivityMocks(
	env *testsuite.TestWorkflowEnvironment,
	execute func(StepExecutionInput) StepExecutionResult,
	updateState func(StateUpdate),
) {
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
		updateState(input)
		return nil
	}, activity.RegisterOptions{Name: ActivityUpdateState})
	env.RegisterActivityWithOptions(func(_ context.Context, input StepExecutionInput) (StepExecutionResult, error) {
		return execute(input), nil
	}, activity.RegisterOptions{Name: ActivityExecuteStep})
}

func TestAnsibleStepArtifactsExposeMetadataWithoutPaths(t *testing.T) {
	root := t.TempDir()
	runID := "run-1"
	runDir := filepath.Join(root, runID)
	if err := os.MkdirAll(filepath.Join(runDir, "job_events"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "stdout"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "job_events", "1.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := &AnsibleStepExecutor{artifactRoot: root}
	artifacts := executor.artifacts(runID)
	if len(artifacts) != 2 {
		t.Fatalf("artifacts = %#v, want stdout and job_events", artifacts)
	}
	if artifacts[0].ID != "stdout" || artifacts[0].SizeBytes != 5 || artifacts[0].RelativeKey != filepath.Join(runID, "stdout") {
		t.Fatalf("stdout metadata = %#v", artifacts[0])
	}
	if artifacts[1].ID != "job_events" || artifacts[1].SizeBytes != 2 {
		t.Fatalf("event metadata = %#v", artifacts[1])
	}
	encoded, err := json.Marshal(artifacts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), root) || strings.Contains(string(encoded), "relativeKey") {
		t.Fatalf("artifact JSON exposed a server-side path: %s", encoded)
	}
}
