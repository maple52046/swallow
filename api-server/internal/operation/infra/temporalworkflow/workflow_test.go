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
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// normalizeActivityError must keep raw activity error text out of workflow history and
// must report a non-retryable application error (for example a lost lease) as terminal so
// the workflow never offers a retry that is guaranteed to fail again.
func TestNormalizeActivityErrorMapsRetryabilityAndRedactsText(t *testing.T) {
	fenced := temporal.NewNonRetryableApplicationError(
		"ssh to 10.0.0.5 failed: secret at /run/creds/id_rsa", "lease_fenced", nil)
	code, message, retryable := normalizeActivityError(fenced)
	if code != "lease_fenced" || retryable {
		t.Errorf("lease_fenced must be non-retryable: code=%q retryable=%v", code, retryable)
	}
	if strings.Contains(message, "10.0.0.5") || strings.Contains(message, "/run/creds") {
		t.Errorf("raw error detail must not reach history: %q", message)
	}

	code, _, retryable = normalizeActivityError(errors.New("boom: /etc/shadow"))
	if code != "activity_failure" || !retryable {
		t.Errorf("generic infra failure should be retryable activity_failure: code=%q retryable=%v", code, retryable)
	}
}

func TestOperationWorkflowV1RunsDependenciesInOrder(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var mu sync.Mutex
	executed := []string{}
	states := []operationdomain.WorkflowStatus{}

	registerWorkflowActivityMocks(env, func(input StepExecutionInput) StepExecutionResult {
		mu.Lock()
		executed = append(executed, input.Step.ID)
		mu.Unlock()
		return StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
	}, func(input StateUpdate) {
		states = append(states, input.Status)
	})

	env.ExecuteWorkflow(OperationWorkflowV1, WorkflowInput{
		OperationID: "operation-a", Kind: operationdomain.WorkflowKindDeployKubernetes,
		SiteID: "site-a", Definition: "test", DefinitionVersion: 1,
		LeaseDuration: time.Minute, MaxParallelism: 1,
		ResourceKeys: []string{"server:a"},
		Steps: []operationdomain.Task{
			{ID: "prepare", Kind: "noop", Name: "Prepare", Executor: operationdomain.RunnerKindInternal, Status: operationdomain.TaskPending, Attempt: 1},
			{ID: "install", Kind: "noop", Name: "Install", Executor: operationdomain.RunnerKindInternal, Status: operationdomain.TaskPending, Attempt: 1, DependsOn: []string{"prepare"}},
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
	if len(states) == 0 || states[len(states)-1] != operationdomain.WorkflowSucceeded {
		t.Fatalf("last operation state = %v, want succeeded", states)
	}
}

// When Tasks are grouped into Jobs (ADR 017), the Operation must run each Job as a child
// workflow, honoring cross-Job dependency order, and still finish succeeded.
func TestOperationWorkflowV1RunsJobsAsChildWorkflows(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(JobWorkflowV1, workflow.RegisterOptions{Name: JobWorkflowName})
	var mu sync.Mutex
	executed := []string{}
	states := []operationdomain.WorkflowStatus{}
	registerWorkflowActivityMocks(env, func(input StepExecutionInput) StepExecutionResult {
		mu.Lock()
		executed = append(executed, input.Step.ID)
		mu.Unlock()
		return StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
	}, func(input StateUpdate) {
		states = append(states, input.Status)
	})

	env.ExecuteWorkflow(OperationWorkflowV1, WorkflowInput{
		OperationID: "operation-jobs", Kind: operationdomain.WorkflowKindDeployKubernetes,
		SiteID: "site-a", Definition: "platform-deployment", DefinitionVersion: 1,
		LeaseDuration: time.Minute, MaxParallelism: 2, ResourceKeys: []string{"server:a"},
		Steps: []operationdomain.Task{
			{ID: "provision", Job: "ensure-os", Kind: "provision-os", Name: "Ensure OS", Executor: operationdomain.RunnerKindProvisioner, Status: operationdomain.TaskPending, Attempt: 1},
			{ID: "install", Job: "configure-k0s", Kind: "ansible-playbook", Name: "Install k0s", Executor: operationdomain.RunnerKindAnsible, Status: operationdomain.TaskPending, Attempt: 1, DependsOn: []string{"provision"}},
		},
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("jobbed workflow failed: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(executed) != 2 || executed[0] != "provision" || executed[1] != "install" {
		t.Fatalf("execution order = %v, want [provision install] across child Jobs", executed)
	}
	if len(states) == 0 || states[len(states)-1] != operationdomain.WorkflowSucceeded {
		t.Fatalf("last operation state = %v, want succeeded", states)
	}
}

// A retryable failure inside a Job must actually re-run on operator retry. The retried Job
// re-runs its Tasks under a bumped attempt so the executor cannot return the prior attempt's
// cached outcome. Regression test: the job path previously replayed the cached failure
// forever because the Task attempt (and thus the executor idempotency key) never changed.
func TestOperationWorkflowV1RetriesFailedJobWithBumpedAttempt(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(JobWorkflowV1, workflow.RegisterOptions{Name: JobWorkflowName})
	var mu sync.Mutex
	installAttempts := []int{}
	states := []operationdomain.WorkflowStatus{}
	registerWorkflowActivityMocks(env, func(input StepExecutionInput) StepExecutionResult {
		if input.Step.ID == "install" {
			mu.Lock()
			installAttempts = append(installAttempts, input.Step.Attempt)
			first := len(installAttempts) == 1
			mu.Unlock()
			if first {
				return StepExecutionResult{Status: operationdomain.TaskFailed, Error: &operationdomain.NormalizedError{
					Code: "transient", Message: "host key not yet learned", Retryable: true,
				}}
			}
		}
		return StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
	}, func(input StateUpdate) {
		mu.Lock()
		states = append(states, input.Status)
		mu.Unlock()
	})
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(RetryStepSignal, RetryStepCommand{StepID: "install"})
	}, 2*time.Second)

	env.ExecuteWorkflow(OperationWorkflowV1, WorkflowInput{
		OperationID: "operation-job-retry", Kind: operationdomain.WorkflowKindDeployKubernetes,
		SiteID: "site-a", Definition: "platform-deployment", DefinitionVersion: 1,
		LeaseDuration: time.Minute, MaxParallelism: 2, ResourceKeys: []string{"server:a"},
		Steps: []operationdomain.Task{
			{ID: "provision", Job: "ensure-os", Kind: "provision-os", Name: "Ensure OS", Executor: operationdomain.RunnerKindProvisioner, Status: operationdomain.TaskPending, Attempt: 1},
			{ID: "install", Job: "configure-k0s", Kind: "ansible-playbook", Name: "Install k0s", Executor: operationdomain.RunnerKindAnsible, Status: operationdomain.TaskPending, Attempt: 1, DependsOn: []string{"provision"}},
		},
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("jobbed retry workflow failed: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(installAttempts) != 2 {
		t.Fatalf("install executed %d times, want 2 (initial + retry): %v", len(installAttempts), installAttempts)
	}
	if installAttempts[0] != 1 || installAttempts[1] != 2 {
		t.Fatalf("install attempts = %v, want [1 2]: retry must bump the attempt / idempotency key", installAttempts)
	}
	seenAttention := false
	for _, state := range states {
		seenAttention = seenAttention || state == operationdomain.WorkflowRequiresAttention
	}
	if !seenAttention || states[len(states)-1] != operationdomain.WorkflowSucceeded {
		t.Fatalf("states = %v, want requires_attention then succeeded", states)
	}
}

func TestOperationWorkflowV1RetriesFailedStepInSameOperation(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	attempts := 0
	states := []operationdomain.WorkflowStatus{}
	registerWorkflowActivityMocks(env, func(input StepExecutionInput) StepExecutionResult {
		attempts++
		if attempts == 1 {
			return StepExecutionResult{Status: operationdomain.TaskFailed, Error: &operationdomain.NormalizedError{
				Code: "transient", Message: "try again", Retryable: true,
			}}
		}
		if input.Step.Attempt != 2 {
			t.Fatalf("retry attempt = %d, want 2", input.Step.Attempt)
		}
		return StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
	}, func(input StateUpdate) {
		states = append(states, input.Status)
	})
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(RetryStepSignal, RetryStepCommand{StepID: "step-a"})
	}, time.Second)

	env.ExecuteWorkflow(OperationWorkflowV1, WorkflowInput{
		OperationID: "operation-a", Kind: operationdomain.WorkflowKindCustom,
		SiteID: "site-a", Definition: "test", DefinitionVersion: 1,
		LeaseDuration: time.Minute, ResourceKeys: []string{"server:a"},
		Steps: []operationdomain.Task{{
			ID: "step-a", Kind: "noop", Name: "Step A", Executor: operationdomain.RunnerKindInternal, Status: operationdomain.TaskPending, Attempt: 1,
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
		seenAttention = seenAttention || state == operationdomain.WorkflowRequiresAttention
	}
	if !seenAttention || states[len(states)-1] != operationdomain.WorkflowSucceeded {
		t.Fatalf("states = %v, want requires_attention then succeeded", states)
	}
}
func TestOperationWorkflowV1CancellationMarksPendingSteps(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	states := []operationdomain.WorkflowStatus{}
	stepUpdates := []operationdomain.Task{}
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
		OperationID: "operation-cancel", Kind: operationdomain.WorkflowKindCustom,
		SiteID: "site-a", Definition: "test", DefinitionVersion: 1,
		LeaseDuration: time.Minute, ResourceKeys: []string{"server:a"},
		Steps: []operationdomain.Task{{
			ID: "step-a", Kind: "noop", Name: "Step A", Executor: operationdomain.RunnerKindInternal,
			Status: operationdomain.TaskPending, Attempt: 1,
		}},
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("canceled workflow returned an error: %v", err)
	}
	if len(states) < 3 || states[len(states)-2] != operationdomain.WorkflowCanceling ||
		states[len(states)-1] != operationdomain.WorkflowCanceled {
		t.Fatalf("states = %v, want canceling then canceled", states)
	}
	if len(stepUpdates) != 1 || stepUpdates[0].Status != operationdomain.TaskCanceled {
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
