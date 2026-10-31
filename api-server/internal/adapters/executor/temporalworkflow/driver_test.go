package temporalworkflow

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"go.mongodb.org/mongo-driver/bson"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// driverStore is an in-memory store for driver tests: Workflows round-trip through BSON like
// stored documents, and ownership is a map.
type driverStore struct {
	workflowdomain.WorkflowRepository
	mu     sync.Mutex
	docs   map[string]bson.Raw
	owners map[string]string
	events []workflowdomain.TimelineEvent
}

func newDriverStore(workflows ...*workflowdomain.Workflow) *driverStore {
	s := &driverStore{docs: map[string]bson.Raw{}, owners: map[string]string{}}
	for _, w := range workflows {
		_ = s.Save(context.Background(), w)
	}
	return s
}

func (s *driverStore) FindByID(_ context.Context, id string) (*workflowdomain.Workflow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, ok := s.docs[id]
	if !ok {
		return nil, workflowdomain.ErrWorkflowNotFound
	}
	var w workflowdomain.Workflow
	return &w, bson.Unmarshal(raw, &w)
}

func (s *driverStore) Save(_ context.Context, w *workflowdomain.Workflow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := bson.Marshal(w)
	s.docs[w.ID] = raw
	return err
}

func (s *driverStore) AppendEvent(_ context.Context, event workflowdomain.TimelineEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

func (s *driverStore) Acquire(_ context.Context, owner string, keys []string, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range keys {
		if holder := s.owners[key]; holder != "" && holder != owner {
			return false, nil
		}
	}
	for _, key := range keys {
		s.owners[key] = owner
	}
	return true, nil
}

func (s *driverStore) Release(_ context.Context, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, holder := range s.owners {
		if holder == owner {
			delete(s.owners, key)
		}
	}
	return nil
}

func (s *driverStore) FindByOwner(context.Context, string) ([]workflowdomain.ResourceOwnership, error) {
	return nil, nil
}

func (s *driverStore) IsHeld(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.owners[key] != "", nil
}

func (s *driverStore) workflow(t *testing.T, id string) *workflowdomain.Workflow {
	t.Helper()
	w, err := s.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	return w
}

type immediateTransactor struct{}

func (immediateTransactor) InTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// scriptedRunner returns the next result for a Task, or blocks until canceled when told to.
type scriptedRunner struct {
	mu      sync.Mutex
	results map[string][]StepExecutionResult
	block   map[string]bool
	ran     []string
}

func (r *scriptedRunner) Execute(ctx context.Context, input StepExecutionInput) StepExecutionResult {
	r.mu.Lock()
	r.ran = append(r.ran, input.Step.ID)
	block := r.block[input.Step.ID]
	var result StepExecutionResult
	if queue := r.results[input.Step.ID]; len(queue) > 0 {
		result, r.results[input.Step.ID] = queue[0], queue[1:]
	}
	r.mu.Unlock()
	if block {
		<-ctx.Done()
		return StepExecutionResult{Status: workflowdomain.TaskCanceled}
	}
	if result.Status == "" {
		result.Status = workflowdomain.TaskSucceeded
	}
	return result
}

type recordingCompensation struct{ compensated []string }

func (c *recordingCompensation) Compensate(_ context.Context, input StepExecutionInput) (string, string) {
	c.compensated = append(c.compensated, input.Step.ID)
	return "compensated", "Compensated."
}

var driverCatalog = workflowdomain.PlanCatalog{
	TaskKinds: map[string]workflowdomain.TaskKind{
		"deploy": {Runner: workflowdomain.RunnerKindProvisioner, Resume: workflowdomain.ResumeObserve},
	},
	Compensations: map[string]bool{"return": true},
}

type driverFixture struct {
	store        *driverStore
	runner       *scriptedRunner
	compensation *recordingCompensation
	env          *testsuite.TestWorkflowEnvironment
	engine       *workflowapp.Engine
}

func newDriverFixture(t *testing.T, w *workflowdomain.Workflow) *driverFixture {
	t.Helper()
	f := &driverFixture{
		store:        newDriverStore(w),
		runner:       &scriptedRunner{results: map[string][]StepExecutionResult{}, block: map[string]bool{}},
		compensation: &recordingCompensation{},
	}
	engine, err := workflowapp.NewEngine(immediateTransactor{}, f.store, f.store, driverCatalog, workflowapp.CompletionHandlers{}, 4)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	f.engine = engine
	var suite testsuite.WorkflowTestSuite
	f.env = suite.NewTestWorkflowEnvironment()
	f.env.RegisterWorkflowWithOptions(DriverWorkflow, workflow.RegisterOptions{Name: DriverWorkflowName})
	driver := NewDriverActivities(engine)
	tasks := NewTaskActivities(engine,
		map[workflowdomain.RunnerKind]StepLifecycleExecutor{workflowdomain.RunnerKindProvisioner: f.runner},
		map[string]CompensationExecutor{"return": f.compensation})
	tasks.fenceInterval = time.Hour
	for name, fn := range map[string]any{
		ActivityAdvance: driver.Advance, ActivityRecordFailure: driver.RecordFailure,
		ActivityBeginCancel: driver.BeginCancel, ActivityFinishCanceled: driver.FinishCanceled,
		ActivityExecuteTask: tasks.ExecuteTask, ActivityCompensate: tasks.Compensate,
	} {
		f.env.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: name})
	}
	return f
}

func (f *driverFixture) run(t *testing.T, id string) {
	t.Helper()
	f.runWith(t, id, RunnerRoute{TaskQueue: "default-test-taskqueue"})
}

func (f *driverFixture) runWith(t *testing.T, id string, route RunnerRoute) {
	t.Helper()
	generation, err := f.engine.Dispatch(context.Background(), id)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	f.env.ExecuteWorkflow(DriverWorkflowName, DriverInput{WorkflowID: id, Generation: generation, Routes: map[string]RunnerRoute{
		string(workflowdomain.RunnerKindProvisioner): route,
	}})
	if !f.env.IsWorkflowCompleted() {
		t.Fatal("the run did not complete")
	}
	if err := f.env.GetWorkflowError(); err != nil {
		t.Fatalf("the run failed: %v", err)
	}
}

func driverTask(id string, deps ...string) workflowdomain.Task {
	return workflowdomain.Task{ID: id, Kind: "deploy", Name: id, Executor: workflowdomain.RunnerKindProvisioner,
		DependsOn: deps, Status: workflowdomain.TaskPending, Attempt: 1}
}

func driverWorkflow(id string, tasks ...workflowdomain.Task) *workflowdomain.Workflow {
	return &workflowdomain.Workflow{
		ID: id, Status: workflowdomain.WorkflowPending, Steps: tasks,
		Dispatch:        workflowdomain.Dispatch{Driver: workflowdomain.DriverVersion},
		TargetResources: []workflowdomain.ResourceReference{{Kind: "server", ID: "s1"}},
	}
}

func TestDriverRunsAWorkflowToSuccess(t *testing.T) {
	f := newDriverFixture(t, driverWorkflow("w", driverTask("a"), driverTask("b", "a")))
	f.run(t, "w")
	w := f.store.workflow(t, "w")
	if w.Status != workflowdomain.WorkflowSucceeded {
		t.Fatalf("Workflow = %s, want succeeded", w.Status)
	}
	if len(f.runner.ran) != 2 || f.runner.ran[0] != "a" || f.runner.ran[1] != "b" {
		t.Fatalf("ran %v, want a then b", f.runner.ran)
	}
	if held, _ := f.store.IsHeld(context.Background(), "server:s1"); held {
		t.Error("the resource is still owned after the run")
	}
}

func TestDriverCancelStopsTheRunnerCompensatesAndCancels(t *testing.T) {
	deploy := driverTask("deploy")
	deploy.OnCancel = &workflowdomain.CancelBehavior{Compensation: "return"}
	f := newDriverFixture(t, driverWorkflow("w", deploy, driverTask("after", "deploy")))
	f.runner.block["deploy"] = true
	f.env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, _ converter.EncodedValues) {
		if info.ActivityType.Name == ActivityExecuteTask {
			f.env.CancelWorkflow()
		}
	})
	f.run(t, "w")
	w := f.store.workflow(t, "w")
	if w.Status != workflowdomain.WorkflowCanceled {
		t.Fatalf("Workflow = %s, want canceled", w.Status)
	}
	for _, task := range w.Steps {
		if task.Status != workflowdomain.TaskCanceled {
			t.Errorf("Task %s = %s, want canceled", task.ID, task.Status)
		}
	}
	if len(f.compensation.compensated) != 1 || f.compensation.compensated[0] != "deploy" {
		t.Fatalf("compensated %v, want the interrupted deploy", f.compensation.compensated)
	}
}

func TestDriverParksAndAppliesAStoredRetryWhenWoken(t *testing.T) {
	f := newDriverFixture(t, driverWorkflow("w", driverTask("a")))
	f.runner.results["a"] = []StepExecutionResult{{Status: workflowdomain.TaskFailed,
		Error: &workflowdomain.NormalizedError{Code: "x", Message: "boom", Retryable: true}}}
	f.env.RegisterDelayedCallback(func() {
		w := f.store.workflow(t, "w")
		if w.Status != workflowdomain.WorkflowRequiresAttention {
			t.Errorf("Workflow before the retry = %s, want requires_attention", w.Status)
		}
		w.Commands.Retries = []workflowdomain.RetryCommand{{TaskID: "a"}}
		w.Commands.Sequence++
		_ = f.store.Save(context.Background(), w)
		f.env.SignalWorkflow(WakeSignal, nil)
	}, time.Hour)
	f.run(t, "w")
	w := f.store.workflow(t, "w")
	if w.Status != workflowdomain.WorkflowSucceeded || w.Steps[0].Attempt != 2 {
		t.Fatalf("Workflow = %s, Task attempt %d; want succeeded at attempt 2", w.Status, w.Steps[0].Attempt)
	}
}

func TestDriverOfASupersededGenerationEndsWithoutSideEffects(t *testing.T) {
	f := newDriverFixture(t, driverWorkflow("w", driverTask("a")))
	generation, _ := f.engine.Dispatch(context.Background(), "w")
	if _, err := f.engine.Dispatch(context.Background(), "w"); err != nil {
		t.Fatalf("second Dispatch: %v", err)
	}
	f.env.ExecuteWorkflow(DriverWorkflowName, DriverInput{WorkflowID: "w", Generation: generation})
	if err := f.env.GetWorkflowError(); err != nil {
		t.Fatalf("the superseded run failed: %v", err)
	}
	if len(f.runner.ran) != 0 || f.store.workflow(t, "w").Steps[0].Status != workflowdomain.TaskPending {
		t.Fatalf("the superseded run acted: ran %v", f.runner.ran)
	}
}

// A single-attempt executor that stops (its heartbeat times out) leaves the Task's outcome
// unknown: the Task is handed to the operator instead of being run again.
func TestDriverRecordsAnUnknownOutcomeWhenASingleAttemptExecutorStops(t *testing.T) {
	f := newDriverFixture(t, driverWorkflow("w", driverTask("a")))
	f.env.OnActivity(ActivityExecuteTask, mock.Anything, mock.Anything).
		Return(temporal.NewTimeoutError(enumspb.TIMEOUT_TYPE_HEARTBEAT, nil))
	f.env.RegisterDelayedCallback(func() {
		w := f.store.workflow(t, "w")
		task := w.Steps[0]
		if task.Status != workflowdomain.TaskRequiresAttention || task.Error == nil || task.Error.Code != "executor_outcome_unknown" {
			t.Errorf("Task = %s %+v, want requires_attention with executor_outcome_unknown", task.Status, task.Error)
		}
		if w.Status != workflowdomain.WorkflowRequiresAttention {
			t.Errorf("Workflow = %s, want it parked", w.Status)
		}
		f.env.CancelWorkflow()
	}, time.Hour)
	f.runWith(t, "w", RunnerRoute{TaskQueue: "default-test-taskqueue", SingleAttempt: true})
}

// A canceled activity waits for its Runner to stop before it ends, so the run's cancel path does
// not compensate or release resources while the interrupted work (an ansible-runner process tree)
// still runs. The Temporal testsuite resolves a canceled activity at once, so stopRunner is tested
// directly.
func TestStopRunnerWaitsForTheRunnerToStop(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan StepExecutionResult, 1)
	go func() {
		<-ctx.Done()
		time.Sleep(200 * time.Millisecond)
		done <- StepExecutionResult{Status: workflowdomain.TaskCanceled}
	}()
	started := time.Now()
	stopRunner(ctx, cancel, ErrActivityWorkerStopping, done)
	if took := time.Since(started); took < 200*time.Millisecond {
		t.Errorf("stopRunner returned after %s, before the Runner stopped", took)
	}
	if !errors.Is(context.Cause(ctx), ErrActivityWorkerStopping) {
		t.Errorf("Runner context cause = %v, want the stop cause", context.Cause(ctx))
	}
}
