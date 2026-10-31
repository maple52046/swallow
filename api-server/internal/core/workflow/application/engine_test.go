package application

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// memoryWorkflows is an in-memory WorkflowRepository for engine tests. FindByID returns a deep
// copy, so a transition only takes effect through Save, as with the store.
type memoryWorkflows struct {
	workflowdomain.WorkflowRepository
	mu     sync.Mutex
	byID   map[string]bson.Raw
	events map[string]workflowdomain.TimelineEvent
}

func newMemoryWorkflows(workflows ...*workflowdomain.Workflow) *memoryWorkflows {
	m := &memoryWorkflows{byID: map[string]bson.Raw{}, events: map[string]workflowdomain.TimelineEvent{}}
	for _, w := range workflows {
		_ = m.Save(context.Background(), w)
	}
	return m
}

func (m *memoryWorkflows) FindByID(_ context.Context, id string) (*workflowdomain.Workflow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, ok := m.byID[id]
	if !ok {
		return nil, workflowdomain.ErrWorkflowNotFound
	}
	var w workflowdomain.Workflow
	if err := bson.Unmarshal(raw, &w); err != nil {
		return nil, err
	}
	return &w, nil
}

// get reads a Workflow, failing the test on error.
func (m *memoryWorkflows) get(t *testing.T, id string) *workflowdomain.Workflow {
	t.Helper()
	w, err := m.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("FindByID(%s): %v", id, err)
	}
	return w
}

// Save stores a BSON copy, so every stored field round-trips like a document in the store.
func (m *memoryWorkflows) Save(_ context.Context, w *workflowdomain.Workflow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, err := bson.Marshal(w)
	if err != nil {
		return err
	}
	m.byID[w.ID] = raw
	return nil
}

func (m *memoryWorkflows) AppendEvent(_ context.Context, event workflowdomain.TimelineEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events[event.ID] = event
	return nil
}

// memoryOwnership is an in-memory OwnershipRepository.
type memoryOwnership struct {
	mu    sync.Mutex
	owner map[string]string
}

func newMemoryOwnership() *memoryOwnership { return &memoryOwnership{owner: map[string]string{}} }

func (o *memoryOwnership) Acquire(_ context.Context, owner string, keys []string, _ time.Time) (bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, key := range keys {
		if holder := o.owner[key]; holder != "" && holder != owner {
			return false, nil
		}
	}
	for _, key := range keys {
		o.owner[key] = owner
	}
	return true, nil
}

func (o *memoryOwnership) Release(_ context.Context, owner string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	for key, holder := range o.owner {
		if holder == owner {
			delete(o.owner, key)
		}
	}
	return nil
}

func (o *memoryOwnership) FindByOwner(_ context.Context, owner string) ([]workflowdomain.ResourceOwnership, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	var owned []workflowdomain.ResourceOwnership
	for key, holder := range o.owner {
		if holder == owner {
			owned = append(owned, workflowdomain.ResourceOwnership{ResourceKey: key, Owner: owner})
		}
	}
	sort.Slice(owned, func(i, j int) bool { return owned[i].ResourceKey < owned[j].ResourceKey })
	return owned, nil
}

func (o *memoryOwnership) IsHeld(_ context.Context, key string) (bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.owner[key] != "", nil
}

type directTransactor struct{}

func (directTransactor) InTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

type recordingProjection struct{ seen []workflowdomain.TaskStatus }

func (p *recordingProjection) Project(_ context.Context, transition TaskTransition) error {
	p.seen = append(p.seen, transition.Task.Status)
	return nil
}

type failingResult struct{}

func (failingResult) Complete(context.Context, TaskResult) error { return errors.New("no credential") }

var testCatalog = workflowdomain.PlanCatalog{
	TaskKinds: map[string]workflowdomain.TaskKind{
		"deploy":  {Runner: workflowdomain.RunnerKindProvisioner, Resume: workflowdomain.ResumeObserve},
		"run":     {Runner: workflowdomain.RunnerKindAnsible, Resume: workflowdomain.ResumeAttention},
		"recover": {Runner: workflowdomain.RunnerKindProvisioner, Resume: workflowdomain.ResumeObserve},
	},
	Completions:   map[string]workflowdomain.CompletionClass{"projected": workflowdomain.CompletionProjection, "credential": workflowdomain.CompletionResult},
	Compensations: map[string]bool{"return-deployment": true},
}

type engineFixture struct {
	engine     *Engine
	workflows  *memoryWorkflows
	ownership  *memoryOwnership
	projection *recordingProjection
}

func newEngineFixture(t *testing.T, maxParallel int, workflows ...*workflowdomain.Workflow) *engineFixture {
	t.Helper()
	f := &engineFixture{workflows: newMemoryWorkflows(workflows...), ownership: newMemoryOwnership(), projection: &recordingProjection{}}
	engine, err := NewEngine(directTransactor{}, f.workflows, f.ownership, testCatalog, CompletionHandlers{
		Projections: map[string]ProjectionHandler{"projected": f.projection},
		Results:     map[string]ResultHandler{"credential": failingResult{}},
	}, maxParallel)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	f.engine = engine
	return f
}

func pendingTask(id, kind string, runner workflowdomain.RunnerKind, deps ...string) workflowdomain.Task {
	return workflowdomain.Task{ID: id, Kind: kind, Name: id, Executor: runner, DependsOn: deps, Status: workflowdomain.TaskPending, Attempt: 1}
}

func newWorkflow(id string, tasks ...workflowdomain.Task) *workflowdomain.Workflow {
	return &workflowdomain.Workflow{
		ID: id, SchemaVersion: 4, Status: workflowdomain.WorkflowPending, Steps: tasks,
		TargetResources: []workflowdomain.ResourceReference{{Kind: "server", ID: "s1"}},
	}
}

// dispatch dispatches a Workflow and fails the test on error.
func (f *engineFixture) dispatch(t *testing.T, id string) int64 {
	t.Helper()
	generation, err := f.engine.Dispatch(context.Background(), id)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	return generation
}

func (f *engineFixture) advance(t *testing.T, id string, generation int64, running ...string) AdvanceResult {
	t.Helper()
	result, err := f.engine.Advance(context.Background(), AdvanceRequest{WorkflowID: id, Generation: generation, Running: running})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	return result
}

func startIDs(result AdvanceResult) []string {
	ids := make([]string, len(result.Start))
	for i, start := range result.Start {
		ids[i] = start.TaskID
	}
	return ids
}

func TestEngine_RunsAWorkflowToSuccessAndReleasesItsResources(t *testing.T) {
	f := newEngineFixture(t, 1, newWorkflow("w",
		pendingTask("a", "deploy", workflowdomain.RunnerKindProvisioner),
		pendingTask("b", "deploy", workflowdomain.RunnerKindProvisioner),
		pendingTask("c", "deploy", workflowdomain.RunnerKindProvisioner, "a")))
	gen := f.dispatch(t, "w")

	first := f.advance(t, "w", gen)
	if first.Action != AdvanceRun || len(first.Start) != 1 || first.Start[0].TaskID != "a" {
		t.Fatalf("first Advance = %+v, want to start only a (window of 1)", first)
	}
	if held, _ := f.ownership.IsHeld(context.Background(), "server:s1"); !held {
		t.Fatal("the Workflow did not take its resource")
	}
	if got := f.workflows.get(t, "w").Status; got != workflowdomain.WorkflowWaitingExternal {
		t.Fatalf("status = %s, want waiting_external", got)
	}
	for _, id := range []string{"a", "b", "c"} {
		if err := f.engine.Record(context.Background(), TaskRef{WorkflowID: "w", Generation: gen, TaskID: id, Attempt: 1}, Outcome{}); err != nil &&
			!errors.Is(err, workflowdomain.ErrTaskNotRunning) {
			t.Fatalf("Record(%s): %v", id, err)
		}
		f.advance(t, "w", gen)
	}
	w := f.workflows.get(t, "w")
	if w.Status != workflowdomain.WorkflowSucceeded || w.ResourcesAcquired {
		t.Fatalf("Workflow = %s (resources %v), want succeeded with resources released", w.Status, w.ResourcesAcquired)
	}
	if held, _ := f.ownership.IsHeld(context.Background(), "server:s1"); held {
		t.Error("the resource is still owned after the Workflow ended")
	}
}

func TestEngine_WaitsForAResourceAnotherWorkflowOwns(t *testing.T) {
	f := newEngineFixture(t, 4, newWorkflow("w", pendingTask("a", "deploy", workflowdomain.RunnerKindProvisioner)))
	_, _ = f.ownership.Acquire(context.Background(), "other", []string{"server:s1"}, time.Now())
	gen := f.dispatch(t, "w")

	result := f.advance(t, "w", gen)
	if result.Action != AdvanceWaitResources || len(result.Start) != 0 {
		t.Fatalf("Advance = %+v, want to wait for resources", result)
	}
	if w := f.workflows.get(t, "w"); w.Status != workflowdomain.WorkflowWaitingDependency || w.Steps[0].Status != workflowdomain.TaskPending {
		t.Fatalf("Workflow = %s, Task %s; want waiting_dependency with nothing begun", w.Status, w.Steps[0].Status)
	}
}

func TestEngine_FencesASupersededRun(t *testing.T) {
	f := newEngineFixture(t, 4, newWorkflow("w", pendingTask("a", "deploy", workflowdomain.RunnerKindProvisioner)))
	old := f.dispatch(t, "w")
	f.advance(t, "w", old)
	current := f.dispatch(t, "w")

	if result := f.advance(t, "w", old); result.Action != AdvanceFenced {
		t.Fatalf("Advance with the old generation = %s, want fenced", result.Action)
	}
	err := f.engine.Record(context.Background(), TaskRef{WorkflowID: "w", Generation: old, TaskID: "a", Attempt: 1}, Outcome{})
	if !errors.Is(err, workflowdomain.ErrDispatchFenced) {
		t.Fatalf("Record with the old generation = %v, want ErrDispatchFenced", err)
	}
	if err := f.engine.CheckGeneration(context.Background(), "w", current); err != nil {
		t.Fatalf("CheckGeneration(current) = %v", err)
	}
}

func TestEngine_RetryAppliesTheJobsGranularity(t *testing.T) {
	failed := func(id string) workflowdomain.Task {
		task := pendingTask(id, "deploy", workflowdomain.RunnerKindProvisioner)
		task.Job, task.Status = "ensure", workflowdomain.TaskFailed
		task.Error = &workflowdomain.NormalizedError{Code: "x", Message: "x", Retryable: true}
		return task
	}
	succeeded := pendingTask("p2", "deploy", workflowdomain.RunnerKindProvisioner)
	succeeded.Job, succeeded.Status = "ensure", workflowdomain.TaskSucceeded
	for _, tc := range []struct {
		retry    workflowdomain.RetryGranularity
		attempts map[string]int
	}{
		{workflowdomain.RetryJob, map[string]int{"p1": 2, "p2": 2}},
		{workflowdomain.RetryTask, map[string]int{"p1": 2, "p2": 1}},
	} {
		w := newWorkflow("w", failed("p1"), succeeded)
		w.Jobs = []workflowdomain.JobDeclaration{{Name: "ensure", Retry: tc.retry}}
		w.Commands.Retries = []workflowdomain.RetryCommand{{TaskID: "p1"}}
		f := newEngineFixture(t, 4, w)
		gen := f.dispatch(t, "w")
		f.advance(t, "w", gen)
		got := f.workflows.get(t, "w")
		for _, task := range got.Steps {
			if task.Attempt != tc.attempts[task.ID] {
				t.Errorf("%s: Task %s attempt = %d, want %d", tc.retry, task.ID, task.Attempt, tc.attempts[task.ID])
			}
		}
		if len(got.Commands.Retries) != 0 {
			t.Errorf("%s: retry commands were not cleared", tc.retry)
		}
	}
}

func TestEngine_ResumesTasksAnEarlierDispatchLeftRunning(t *testing.T) {
	f := newEngineFixture(t, 4, newWorkflow("w",
		pendingTask("deploy", "deploy", workflowdomain.RunnerKindProvisioner),
		pendingTask("playbook", "run", workflowdomain.RunnerKindAnsible)))
	first := f.dispatch(t, "w")
	f.advance(t, "w", first)
	second := f.dispatch(t, "w")

	result := f.advance(t, "w", second)
	if len(result.Start) != 1 || result.Start[0].TaskID != "deploy" || !result.Start[0].Resumed {
		t.Fatalf("Advance after a resume = %+v, want only deploy, resumed", result)
	}
	playbook := f.workflows.get(t, "w").Steps[1]
	if playbook.Status != workflowdomain.TaskRequiresAttention || playbook.Error == nil || playbook.Error.Code != "executor_outcome_unknown" {
		t.Fatalf("the Ansible Task = %s %+v, want requires_attention with executor_outcome_unknown", playbook.Status, playbook.Error)
	}
	if again := f.advance(t, "w", second, "deploy"); len(again.Start) != 0 {
		t.Errorf("a repeated Advance started %v again", startIDs(again))
	}
	if again := f.advance(t, "w", second); len(again.Start) != 1 {
		t.Errorf("an Advance that lost the running list = %v, want deploy handed back", startIDs(again))
	}
}

func TestEngine_CancelCompensatesAndCancelsEveryUnfinishedTask(t *testing.T) {
	deploy := pendingTask("deploy", "deploy", workflowdomain.RunnerKindProvisioner)
	deploy.OnCancel = &workflowdomain.CancelBehavior{Compensation: "return-deployment"}
	parked := pendingTask("parked", "run", workflowdomain.RunnerKindAnsible)
	parked.Status = workflowdomain.TaskRequiresAttention
	parked.Error = &workflowdomain.NormalizedError{Code: "x", Message: "x", Retryable: true}
	done := pendingTask("done", "deploy", workflowdomain.RunnerKindProvisioner)
	done.Status = workflowdomain.TaskSucceeded
	recover := pendingTask("recover", "recover", workflowdomain.RunnerKindProvisioner, "deploy")
	recover.OnCancel = &workflowdomain.CancelBehavior{Run: true}
	f := newEngineFixture(t, 4, newWorkflow("w", deploy, parked, done, recover))
	gen := f.dispatch(t, "w")
	f.advance(t, "w", gen)
	if err := f.workflowsRequestCancel("w", "Superseded by a rerun."); err != nil {
		t.Fatal(err)
	}

	if result := f.advance(t, "w", gen, "deploy"); result.Action != AdvanceCancel {
		t.Fatalf("Advance after a cancel request = %s, want cancel", result.Action)
	}
	compensations, err := f.engine.BeginCancel(context.Background(), "w", gen)
	if err != nil {
		t.Fatalf("BeginCancel: %v", err)
	}
	if len(compensations) != 2 || compensations[0].Kind != "return-deployment" || !compensations[1].Run {
		t.Fatalf("compensations = %+v, want the interrupted deploy's and the recover Task", compensations)
	}
	if err := f.engine.FinishCanceled(context.Background(), "w", gen); err != nil {
		t.Fatalf("FinishCanceled: %v", err)
	}
	w := f.workflows.get(t, "w")
	if w.Status != workflowdomain.WorkflowCanceled || w.StatusReason != "Superseded by a rerun." {
		t.Fatalf("Workflow = %s %q, want canceled with the command's reason", w.Status, w.StatusReason)
	}
	want := map[string]workflowdomain.TaskStatus{
		"deploy": workflowdomain.TaskCanceled, "parked": workflowdomain.TaskCanceled,
		"done": workflowdomain.TaskSucceeded, "recover": workflowdomain.TaskCanceled,
	}
	for _, task := range w.Steps {
		if task.Status != want[task.ID] {
			t.Errorf("Task %s = %s, want %s", task.ID, task.Status, want[task.ID])
		}
	}
	if held, _ := f.ownership.IsHeld(context.Background(), "server:s1"); held {
		t.Error("the canceled Workflow still owns its resource")
	}
}

// workflowsRequestCancel stores a cancel command the way the repository does.
func (f *engineFixture) workflowsRequestCancel(id, reason string) error {
	w, err := f.workflows.FindByID(context.Background(), id)
	if err != nil {
		return err
	}
	w.Commands.Cancel = &workflowdomain.CancelCommand{Reason: reason}
	w.Commands.Sequence++
	return f.workflows.Save(context.Background(), w)
}

func TestEngine_ProjectionsRunOnEveryTransitionOfADeclaringTask(t *testing.T) {
	task := pendingTask("a", "deploy", workflowdomain.RunnerKindProvisioner)
	task.Completion = []workflowdomain.Completion{{Type: "projected"}}
	f := newEngineFixture(t, 4, newWorkflow("w", task, pendingTask("b", "deploy", workflowdomain.RunnerKindProvisioner)))
	gen := f.dispatch(t, "w")
	f.advance(t, "w", gen)
	if err := f.engine.Record(context.Background(), TaskRef{WorkflowID: "w", Generation: gen, TaskID: "a", Attempt: 1}, Outcome{}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if len(f.projection.seen) != 2 || f.projection.seen[0] != workflowdomain.TaskRunning || f.projection.seen[1] != workflowdomain.TaskSucceeded {
		t.Fatalf("projected statuses = %v, want [running succeeded] for the declaring Task only", f.projection.seen)
	}
}

func TestEngine_AFailingResultCompletionFailsTheTaskRetryably(t *testing.T) {
	task := pendingTask("a", "run", workflowdomain.RunnerKindAnsible)
	task.Completion = []workflowdomain.Completion{{Type: "credential"}}
	f := newEngineFixture(t, 4, newWorkflow("w", task))
	gen := f.dispatch(t, "w")
	f.advance(t, "w", gen)
	if err := f.engine.Complete(context.Background(), TaskRef{WorkflowID: "w", Generation: gen, TaskID: "a", Attempt: 1}, Outcome{}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	got := f.workflows.get(t, "w").Steps[0]
	if got.Status != workflowdomain.TaskFailed || got.Error == nil || got.Error.Code != "completion_failed" || !got.Error.Retryable {
		t.Fatalf("Task = %s %+v, want a retryable completion_failed", got.Status, got.Error)
	}
}

func TestEngine_ARecordedOutcomeIsNotRewritten(t *testing.T) {
	f := newEngineFixture(t, 4, newWorkflow("w", pendingTask("a", "deploy", workflowdomain.RunnerKindProvisioner)))
	gen := f.dispatch(t, "w")
	f.advance(t, "w", gen)
	ref := TaskRef{WorkflowID: "w", Generation: gen, TaskID: "a", Attempt: 1}
	if err := f.engine.Record(context.Background(), ref, Outcome{}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	err := f.engine.Record(context.Background(), ref, Outcome{Status: workflowdomain.TaskCanceled})
	if !errors.Is(err, workflowdomain.ErrTaskNotRunning) {
		t.Fatalf("a second Record = %v, want ErrTaskNotRunning", err)
	}
	if got := f.workflows.get(t, "w").Steps[0].Status; got != workflowdomain.TaskSucceeded {
		t.Fatalf("Task = %s, want it to stay succeeded", got)
	}
}

func TestCompletionHandlers_RequireFindsMissingHandlers(t *testing.T) {
	if err := (CompletionHandlers{}).Require(testCatalog, workflowdomain.CompletionProjection, workflowdomain.CompletionResult); err == nil {
		t.Fatal("Require with no handlers = nil, want the missing types")
	}
	if _, err := NewEngine(directTransactor{}, newMemoryWorkflows(), newMemoryOwnership(), testCatalog, CompletionHandlers{}, 4); err == nil {
		t.Fatal("NewEngine without projection handlers = nil error, want a refusal to start")
	}
}

func (m *memoryWorkflows) ListUnfinishedIDs(context.Context) ([]string, error) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.byID))
	for id := range m.byID {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	sort.Strings(ids)
	var unfinished []string
	for _, id := range ids {
		if w, _ := m.FindByID(context.Background(), id); !w.Status.Terminal() {
			unfinished = append(unfinished, id)
		}
	}
	return unfinished, nil
}

func (m *memoryWorkflows) update(id string, change func(*workflowdomain.Workflow)) error {
	w, err := m.FindByID(context.Background(), id)
	if err != nil {
		return err
	}
	change(w)
	return m.Save(context.Background(), w)
}

func (m *memoryWorkflows) RecordRun(_ context.Context, id string, generation int64, runID string) error {
	return m.update(id, func(w *workflowdomain.Workflow) {
		if w.Dispatch.Generation == generation {
			w.Temporal.RunID = runID
		}
	})
}

func (m *memoryWorkflows) MarkCommandsDelivered(_ context.Context, id string, sequence int64) error {
	return m.update(id, func(w *workflowdomain.Workflow) {
		if w.Dispatch.DeliveredCommand < sequence {
			w.Dispatch.DeliveredCommand = sequence
		}
	})
}

func (m *memoryWorkflows) RequestCancel(_ context.Context, id string, command workflowdomain.CancelCommand) error {
	return m.update(id, func(w *workflowdomain.Workflow) {
		if w.Commands.Cancel == nil {
			w.Commands.Cancel = &command
			w.Commands.Sequence++
		}
	})
}

func (m *memoryWorkflows) RequestRetry(_ context.Context, id string, command workflowdomain.RetryCommand) error {
	return m.update(id, func(w *workflowdomain.Workflow) {
		w.Commands.Retries = append(w.Commands.Retries, command)
		w.Commands.Sequence++
	})
}
