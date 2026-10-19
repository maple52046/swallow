package temporalworkflow

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// awaitedProjectionDelay holds every canceled Step projection back briefly before recording it.
// Real Temporal never dispatches an activity scheduled on an already-canceled context, but the
// test environment still runs its mock body on a goroutine the workflow does not wait for. The
// delay keeps such a fire-and-forget projection from landing before the workflow completes, so
// only a projection the workflow actually awaited counts, which is what production would see.
const awaitedProjectionDelay = 200 * time.Millisecond

// cancelProjectionRecorder captures the Operation and Step projections of a workflow whose
// provider Step is canceled while its activity is running.
type cancelProjectionRecorder struct {
	mu     sync.Mutex
	states []operationdomain.WorkflowStatus
	steps  map[string]operationdomain.TaskStatus
	// returned lists the Tasks a return-canceled-deployment step ran for, and events the
	// timeline events the workflow appended for them.
	returned []StepExecutionInput
	events   []operationdomain.TimelineEvent
}

func (r *cancelProjectionRecorder) returnedSteps() []StepExecutionInput {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]StepExecutionInput(nil), r.returned...)
}

func (r *cancelProjectionRecorder) appendedEvents() []operationdomain.TimelineEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]operationdomain.TimelineEvent(nil), r.events...)
}

func (r *cancelProjectionRecorder) stepStatus(id string) operationdomain.TaskStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.steps[id]
}

func (r *cancelProjectionRecorder) lastState() operationdomain.WorkflowStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.states) == 0 {
		return ""
	}
	return r.states[len(r.states)-1]
}

// registerMidActivityCancel registers activity mocks in which ExecuteStep asks the environment to
// cancel the workflow and then keeps running, standing in for an operator cancel that arrives
// while MAAS is installing the operating system. The return-canceled-deployment step the cancel
// then issues succeeds at once and is recorded, as is every appended timeline event.
func registerMidActivityCancel(t *testing.T, env *testsuite.TestWorkflowEnvironment) *cancelProjectionRecorder {
	t.Helper()
	recorder := &cancelProjectionRecorder{steps: map[string]operationdomain.TaskStatus{}}
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	env.RegisterActivityWithOptions(func(_ context.Context, _ LeaseRequest) ([]operationdomain.ResourceLease, error) {
		return []operationdomain.ResourceLease{{ResourceKey: "server:a", Owner: "workflow", FencingToken: 1}}, nil
	}, activity.RegisterOptions{Name: ActivityAcquireLeases})
	env.RegisterActivityWithOptions(func(_ context.Context, _ LeaseRenewal) error { return nil },
		activity.RegisterOptions{Name: ActivityRenewLeases})
	env.RegisterActivityWithOptions(func(_ context.Context, _ []operationdomain.ResourceLease) error { return nil },
		activity.RegisterOptions{Name: ActivityReleaseLeases})
	env.RegisterActivityWithOptions(func(_ context.Context, input StepUpdate) error {
		if input.Step.Status == operationdomain.TaskCanceled {
			time.Sleep(awaitedProjectionDelay)
		}
		recorder.mu.Lock()
		recorder.steps[input.Step.ID] = input.Step.Status
		recorder.mu.Unlock()
		return nil
	}, activity.RegisterOptions{Name: ActivityUpdateStep})
	env.RegisterActivityWithOptions(func(_ context.Context, input StateUpdate) error {
		recorder.mu.Lock()
		recorder.states = append(recorder.states, input.Status)
		recorder.mu.Unlock()
		return nil
	}, activity.RegisterOptions{Name: ActivityUpdateState})
	env.RegisterActivityWithOptions(func(_ context.Context, event operationdomain.TimelineEvent) error {
		recorder.mu.Lock()
		recorder.events = append(recorder.events, event)
		recorder.mu.Unlock()
		return nil
	}, activity.RegisterOptions{Name: ActivityAppendEvent})
	env.RegisterActivityWithOptions(func(ctx context.Context, input StepExecutionInput) (StepExecutionResult, error) {
		if input.Step.Kind == ReturnCanceledDeploymentKind {
			recorder.mu.Lock()
			recorder.returned = append(recorder.returned, input)
			recorder.mu.Unlock()
			return StepExecutionResult{Status: operationdomain.TaskSucceeded}, nil
		}
		env.CancelWorkflow()
		select {
		case <-ctx.Done():
			return StepExecutionResult{}, ctx.Err()
		case <-release:
			return StepExecutionResult{}, context.Canceled
		}
	}, activity.RegisterOptions{Name: ActivityExecuteStep})
	return recorder
}

// TestOperationWorkflowV1CancelDuringActivityProjectsCanceledStep reproduces the stuck-deploying
// defect on the flat path that deploy-os takes: an operator cancel while provision-os is running
// must still project that Step as canceled, because the Server Deployment result is written from
// that projection. Before the fix the Step was marked canceled only in memory, its projection was
// issued on the canceled workflow context (never dispatched), and finishCanceled skipped it.
func TestOperationWorkflowV1CancelDuringActivityProjectsCanceledStep(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	recorder := registerMidActivityCancel(t, env)

	env.ExecuteWorkflow(OperationWorkflowV1, WorkflowInput{
		OperationID: "operation-deploy-os", Kind: operationdomain.WorkflowKindDeployOS,
		SiteID: "site-a", Definition: "os-deployment", DefinitionVersion: 1,
		LeaseDuration: time.Minute, MaxParallelism: 1, ResourceKeys: []string{"server:a"},
		Steps: []operationdomain.Task{{
			ID: "provision-a", Kind: "provision-os", Name: "Provision a",
			Executor: operationdomain.RunnerKindProvisioner, Status: operationdomain.TaskPending, Attempt: 1,
			Targets: []operationdomain.ResourceReference{{Kind: "server", ID: "a"}},
		}},
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("canceled workflow returned an error: %v", err)
	}
	if got := recorder.lastState(); got != operationdomain.WorkflowCanceled {
		t.Errorf("final Operation state = %q, want %q", got, operationdomain.WorkflowCanceled)
	}
	if got := recorder.stepStatus("provision-a"); got != operationdomain.TaskCanceled {
		t.Errorf("provision-a projected status = %q, want %q", got, operationdomain.TaskCanceled)
	}
	assertInterruptedDeploymentReturned(t, recorder, "operation-deploy-os")
}

// assertInterruptedDeploymentReturned checks decision 060 after a cancel interrupted provision-a:
// one return-canceled-deployment step ran for its Server under a Task ID outside the Operation's
// Task list (so the canceled provision-os projection is untouched), and its outcome reached the
// timeline on provision-a.
func assertInterruptedDeploymentReturned(t *testing.T, recorder *cancelProjectionRecorder, operationID string) {
	t.Helper()
	returned := recorder.returnedSteps()
	if len(returned) != 1 {
		t.Fatalf("return-canceled-deployment steps = %d, want 1", len(returned))
	}
	step := returned[0].Step
	if step.ID == "provision-a" || len(step.Targets) != 1 || step.Targets[0].ID != "a" || len(returned[0].Leases) == 0 {
		t.Errorf("return step = %+v (leases %d), want a separate ID targeting server a under the Operation's leases", step, len(returned[0].Leases))
	}
	if got := recorder.stepStatus(step.ID); got != "" {
		t.Errorf("return step projected status %q, want no Task projection", got)
	}
	events := recorder.appendedEvents()
	if len(events) != 1 || events[0].StepID != "provision-a" || events[0].OperationID != operationID ||
		events[0].Type != "canceled_deployment_returned_to_ready" {
		t.Errorf("timeline events = %+v, want one returned_to_ready event on provision-a", events)
	}
}

// TestOperationWorkflowV1CancelRepairsWorkflowStartedBeforeFix covers a deploy that was already
// running when the fix shipped: its history pinned operation-workflow to version 2, so the
// mid-activity projection still goes out on the canceled context, and only finishCanceled (which
// first records its own version when the cancel arrives) can project the canceled Step.
func TestOperationWorkflowV1CancelRepairsWorkflowStartedBeforeFix(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.OnGetVersion("operation-workflow", workflow.DefaultVersion, operationWorkflowVersion).Return(workflow.Version(2))
	recorder := registerMidActivityCancel(t, env)

	env.ExecuteWorkflow(OperationWorkflowV1, WorkflowInput{
		OperationID: "operation-in-flight", Kind: operationdomain.WorkflowKindDeployOS,
		SiteID: "site-a", Definition: "os-deployment", DefinitionVersion: 1,
		LeaseDuration: time.Minute, MaxParallelism: 1, ResourceKeys: []string{"server:a"},
		Steps: []operationdomain.Task{{
			ID: "provision-a", Kind: "provision-os", Name: "Provision a",
			Executor: operationdomain.RunnerKindProvisioner, Status: operationdomain.TaskPending, Attempt: 1,
			Targets: []operationdomain.ResourceReference{{Kind: "server", ID: "a"}},
		}},
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("canceled workflow returned an error: %v", err)
	}
	if got := recorder.stepStatus("provision-a"); got != operationdomain.TaskCanceled {
		t.Errorf("provision-a projected status = %q, want %q", got, operationdomain.TaskCanceled)
	}
}

// TestJobWorkflowV1CancelDuringActivityProjectsCanceledStep covers the same cancel on the Job
// path a platform deploy takes. The parent re-projects from its start snapshot, so this guards
// that the canceled Task still reaches the projection when the operator cancels mid-activity.
func TestJobWorkflowV1CancelDuringActivityProjectsCanceledStep(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(JobWorkflowV1, workflow.RegisterOptions{Name: JobWorkflowName})
	recorder := registerMidActivityCancel(t, env)

	env.ExecuteWorkflow(OperationWorkflowV1, WorkflowInput{
		OperationID: "operation-platform", Kind: operationdomain.WorkflowKindDeployKubernetes,
		SiteID: "site-a", Definition: "platform-deployment", DefinitionVersion: 1,
		LeaseDuration: time.Minute, MaxParallelism: 1, ResourceKeys: []string{"server:a"},
		Steps: []operationdomain.Task{{
			ID: "provision-a", Job: "ensure-os", Kind: "provision-os", Name: "Ensure OS on a",
			Executor: operationdomain.RunnerKindProvisioner, Status: operationdomain.TaskPending, Attempt: 1,
			Targets: []operationdomain.ResourceReference{{Kind: "server", ID: "a"}},
		}},
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("canceled jobbed workflow returned an error: %v", err)
	}
	if got := recorder.lastState(); got != operationdomain.WorkflowCanceled {
		t.Errorf("final Operation state = %q, want %q", got, operationdomain.WorkflowCanceled)
	}
	if got := recorder.stepStatus("provision-a"); got != operationdomain.TaskCanceled {
		t.Errorf("provision-a projected status = %q, want %q", got, operationdomain.TaskCanceled)
	}
	// The parent must wait for the child's cancellation, or the child returning the deployment
	// would be terminated when the parent closes and releases the leases.
	assertInterruptedDeploymentReturned(t, recorder, "operation-platform")
}
