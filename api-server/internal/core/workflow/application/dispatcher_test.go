package application

import (
	"context"
	"testing"

	"github.com/maple52046/swallow/internal/core/convergence"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// fakeExecutor records what the dispatcher asks of the executor.
type fakeExecutor struct {
	running    map[string]bool
	open       []OpenRun
	starts     []int64
	calls      []string
	terminated []string
}

func (e *fakeExecutor) Start(_ context.Context, id string, generation int64) (string, error) {
	e.starts = append(e.starts, generation)
	e.running[id] = true
	return "run-" + id, nil
}

func (e *fakeExecutor) Running(_ context.Context, id string) (bool, error) { return e.running[id], nil }

func (e *fakeExecutor) Wake(_ context.Context, id string) error {
	e.calls = append(e.calls, "wake")
	if !e.running[id] {
		return workflowdomain.ErrWorkflowExecutionGone
	}
	return nil
}

func (e *fakeExecutor) Cancel(_ context.Context, id string) error {
	e.calls = append(e.calls, "cancel")
	if !e.running[id] {
		return workflowdomain.ErrWorkflowExecutionGone
	}
	return nil
}

func (e *fakeExecutor) OpenRuns(context.Context) ([]OpenRun, error) { return e.open, nil }

func (e *fakeExecutor) Terminate(_ context.Context, run OpenRun, _ string) error {
	e.terminated = append(e.terminated, run.WorkflowID)
	return nil
}

func newDispatcherFixture(t *testing.T, workflows ...*workflowdomain.Workflow) (*Dispatcher, *engineFixture, *fakeExecutor) {
	t.Helper()
	f := newEngineFixture(t, 4, workflows...)
	executor := &fakeExecutor{running: map[string]bool{}}
	return NewDispatcher(f.workflows, f.engine, executor, 0, false), f, executor
}

func currentWorkflow(id string) *workflowdomain.Workflow {
	w := newWorkflow(id, pendingTask("a", "deploy", workflowdomain.RunnerKindProvisioner))
	w.Dispatch.Driver = workflowdomain.DriverVersion
	return w
}

func reconcile(t *testing.T, d *Dispatcher, key string, trigger convergence.Trigger) {
	t.Helper()
	if err := d.Reconcile(context.Background(), convergence.Request{Key: key, Trigger: trigger}); err != nil {
		t.Fatalf("Reconcile(%s): %v", key, err)
	}
}

func TestDispatcher_DispatchesANewWorkflowOnce(t *testing.T) {
	d, f, executor := newDispatcherFixture(t, currentWorkflow("w"))
	reconcile(t, d, "w", convergence.TriggerChange)
	reconcile(t, d, "w", convergence.TriggerChange)

	if len(executor.starts) != 1 || executor.starts[0] != 1 {
		t.Fatalf("starts = %v, want one start at generation 1", executor.starts)
	}
	if w := f.workflows.get(t, "w"); w.Temporal.RunID != "run-w" || w.StartState != "started" {
		t.Fatalf("Workflow run %q, startState %q; want the run recorded and started", w.Temporal.RunID, w.StartState)
	}
}

func TestDispatcher_LeavesAnEarlierDriversWorkflowToIt(t *testing.T) {
	d, _, executor := newDispatcherFixture(t, newWorkflow("legacy", pendingTask("a", "deploy", workflowdomain.RunnerKindProvisioner)))
	reconcile(t, d, "legacy", convergence.TriggerResync)
	if len(executor.starts) != 0 {
		t.Fatalf("starts = %v, want none for a Workflow of an earlier driver", executor.starts)
	}
}

func TestDispatcher_DeliversCommandsAndReplacesAGoneRun(t *testing.T) {
	d, f, executor := newDispatcherFixture(t, currentWorkflow("w"))
	reconcile(t, d, "w", convergence.TriggerChange)
	_ = f.workflows.RequestRetry(context.Background(), "w", workflowdomain.RetryCommand{TaskID: "a"})
	reconcile(t, d, "w", convergence.TriggerChange)
	if w := f.workflows.get(t, "w"); w.Dispatch.DeliveredCommand != 1 || executor.calls[0] != "wake" {
		t.Fatalf("after a retry: calls %v, delivered %d; want a wake and the command delivered", executor.calls, w.Dispatch.DeliveredCommand)
	}

	executor.running["w"] = false
	_ = f.workflows.RequestCancel(context.Background(), "w", workflowdomain.CancelCommand{Reason: "x"})
	reconcile(t, d, "w", convergence.TriggerChange)
	w := f.workflows.get(t, "w")
	if len(executor.starts) != 2 || w.Dispatch.Generation != 2 || w.Dispatch.DeliveredCommand != w.Commands.Sequence {
		t.Fatalf("a cancel with the run gone: starts %v, generation %d; want a new dispatch that owns the command", executor.starts, w.Dispatch.Generation)
	}
}

func TestDispatcher_ResumesAWorkflowWhoseRunIsGoneOnResync(t *testing.T) {
	d, f, executor := newDispatcherFixture(t, currentWorkflow("w"))
	reconcile(t, d, "w", convergence.TriggerChange)
	executor.running["w"] = false
	reconcile(t, d, "w", convergence.TriggerChange)
	if len(executor.starts) != 1 {
		t.Fatalf("a change trigger asked the executor: starts %v", executor.starts)
	}
	reconcile(t, d, "w", convergence.TriggerResync)
	if len(executor.starts) != 2 || f.workflows.get(t, "w").Dispatch.Generation != 2 {
		t.Fatalf("resync starts = %v, want a resume at generation 2", executor.starts)
	}
}

func TestDispatcher_RestartsARunThatWasNeverRecorded(t *testing.T) {
	w := currentWorkflow("w")
	w.Dispatch.Generation = 3
	d, f, executor := newDispatcherFixture(t, w)
	reconcile(t, d, "w", convergence.TriggerChange)
	if len(executor.starts) != 1 || executor.starts[0] != 3 || f.workflows.get(t, "w").Dispatch.Generation != 3 {
		t.Fatalf("starts = %v, want the dispatched generation 3 started without a new dispatch", executor.starts)
	}
}

func TestDispatcher_TerminatesOrphanRuns(t *testing.T) {
	done := currentWorkflow("done")
	done.Status = workflowdomain.WorkflowSucceeded
	d, _, executor := newDispatcherFixture(t, currentWorkflow("current"), done,
		newWorkflow("legacy", pendingTask("a", "deploy", workflowdomain.RunnerKindProvisioner)))
	executor.open = []OpenRun{
		{WorkflowID: "swallow-operation/unknown", Current: true},
		{WorkflowID: "swallow-operation/done", Current: true},
		{WorkflowID: "swallow-operation/current/job/ensure/1", Current: false},
		{WorkflowID: "swallow-operation/current", Current: true},
		{WorkflowID: "swallow-operation/legacy", Current: false},
	}
	reconcile(t, d, orphansKey, convergence.TriggerResync)
	want := []string{"swallow-operation/unknown", "swallow-operation/done", "swallow-operation/current/job/ensure/1"}
	if len(executor.terminated) != len(want) {
		t.Fatalf("terminated %v, want %v", executor.terminated, want)
	}
	for i := range want {
		if executor.terminated[i] != want[i] {
			t.Fatalf("terminated %v, want %v", executor.terminated, want)
		}
	}
}
