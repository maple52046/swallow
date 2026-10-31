package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	"github.com/maple52046/swallow/internal/core/store"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// WorkflowExecutor is the Workflow executor port the dispatcher drives (decision 072). A Workflow
// has at most one run at a time; the adapter keeps executor types on its side.
type WorkflowExecutor interface {
	// Start runs the driver for a Workflow at generation, first terminating any run the Workflow
	// still has, and returns the new run's id.
	Start(ctx context.Context, workflowID string, generation int64) (string, error)
	// Running reports whether the Workflow has a running run.
	Running(ctx context.Context, workflowID string) (bool, error)
	// Wake signals the running run to apply new commands. It returns
	// workflowdomain.ErrWorkflowExecutionGone when the Workflow has no running run.
	Wake(ctx context.Context, workflowID string) error
	// Cancel asks the running run to cancel. It returns workflowdomain.ErrWorkflowExecutionGone
	// when the Workflow has no running run.
	Cancel(ctx context.Context, workflowID string) error
	// OpenRuns lists the runs the executor holds open, of any driver version.
	OpenRuns(ctx context.Context) ([]OpenRun, error)
	// Terminate ends a run without running any of its code.
	Terminate(ctx context.Context, run OpenRun, reason string) error
}

// OpenRun is one run the executor holds open. Current is false for a run of an earlier driver.
type OpenRun struct {
	WorkflowID string
	RunID      string
	Current    bool
}

// DispatcherName is the controller name, and so its singleton lease "controller:workflow-dispatch".
const DispatcherName = "workflow-dispatch"

// orphansKey is the resync key that sweeps executor runs the store does not account for.
const orphansKey = "orphans"

// Dispatcher is the workflow-dispatch convergence controller (decision 072). It makes the executor
// match the store: a Workflow that was never dispatched, or whose run is gone, gets a run; stored
// commands reach the run; runs the store does not account for are terminated. It runs in the
// worker on the controller runtime, so one instance is active and the change feed of `workflows`
// wakes it from any process.
//
// A change-triggered Reconcile compares stored fields only (generation, run, command sequence),
// because live progress changes a Workflow every few seconds; asking the executor whether a run
// is still alive happens on resync.
type Dispatcher struct {
	workflows workflowdomain.WorkflowRepository
	engine    *Engine
	executor  WorkflowExecutor
	resync    time.Duration
}

var (
	_ convergence.Controller   = (*Dispatcher)(nil)
	_ convergence.ResyncPeriod = (*Dispatcher)(nil)
)

// NewDispatcher builds the controller; resync is its own resync interval.
func NewDispatcher(workflows workflowdomain.WorkflowRepository, engine *Engine, executor WorkflowExecutor, resync time.Duration) *Dispatcher {
	return &Dispatcher{workflows: workflows, engine: engine, executor: executor, resync: resync}
}

// Name implements convergence.Controller.
func (d *Dispatcher) Name() string { return DispatcherName }

// Collections implements convergence.Controller.
func (d *Dispatcher) Collections() []string { return []string{"workflows"} }

// ResyncInterval implements convergence.ResyncPeriod.
func (d *Dispatcher) ResyncInterval() time.Duration { return d.resync }

// KeysForChange implements convergence.Controller: a changed Workflow is its own key.
func (d *Dispatcher) KeysForChange(_ context.Context, change store.Change) ([]string, error) {
	if change.Operation == store.ChangeDelete {
		return nil, nil
	}
	return []string{change.Key}, nil
}

// Keys implements convergence.Controller: every unfinished Workflow, then the orphan sweep.
func (d *Dispatcher) Keys(ctx context.Context) ([]string, error) {
	ids, err := d.workflows.ListUnfinishedIDs(ctx)
	if err != nil {
		return nil, err
	}
	return append(ids, orphansKey), nil
}

// Reconcile implements convergence.Controller.
func (d *Dispatcher) Reconcile(ctx context.Context, request convergence.Request) error {
	if request.Key == orphansKey {
		return d.sweepOrphans(ctx)
	}
	w, err := d.workflows.FindByID(ctx, request.Key)
	if errors.Is(err, workflowdomain.ErrWorkflowNotV3) || errors.Is(err, workflowdomain.ErrWorkflowNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if w.Status.Terminal() {
		return nil
	}
	switch {
	case w.Dispatch.Driver != workflowdomain.DriverVersion || w.Dispatch.Generation == 0:
		return d.dispatch(ctx, w.ID)
	case w.Temporal.RunID == "":
		return d.start(ctx, w.ID, w.Dispatch.Generation)
	case w.Commands.Sequence > w.Dispatch.DeliveredCommand:
		return d.deliver(ctx, w)
	case request.Trigger == convergence.TriggerResync:
		running, err := d.executor.Running(ctx, w.ID)
		if err != nil {
			return err
		}
		if !running {
			return d.dispatch(ctx, w.ID)
		}
	}
	return nil
}

// dispatch starts a new generation and its run.
func (d *Dispatcher) dispatch(ctx context.Context, id string) error {
	generation, err := d.engine.Dispatch(ctx, id)
	if errors.Is(err, workflowdomain.ErrWorkflowControlConflict) {
		return nil
	}
	if err != nil {
		return err
	}
	return d.start(ctx, id, generation)
}

// start runs the driver for generation and records the run. A failure is returned so the runtime
// retries; the missing run reference makes the next Reconcile start it again.
func (d *Dispatcher) start(ctx context.Context, id string, generation int64) error {
	runID, err := d.executor.Start(ctx, id, generation)
	if err != nil {
		return err
	}
	return d.workflows.RecordRun(ctx, id, generation, runID)
}

// deliver hands the stored commands to the run: a cancel cancels it, anything else wakes it. A
// run that is gone is replaced, and the new run applies every stored command.
func (d *Dispatcher) deliver(ctx context.Context, w *workflowdomain.Workflow) error {
	var err error
	if w.Commands.Cancel != nil {
		err = d.executor.Cancel(ctx, w.ID)
	} else {
		err = d.executor.Wake(ctx, w.ID)
	}
	if errors.Is(err, workflowdomain.ErrWorkflowExecutionGone) {
		return d.dispatch(ctx, w.ID)
	}
	if err != nil {
		return err
	}
	return d.workflows.MarkCommandsDelivered(ctx, w.ID, w.Commands.Sequence)
}

// sweepOrphans terminates runs whose Workflow is unknown or finished, and earlier-driver runs of a
// Workflow the current driver now runs.
func (d *Dispatcher) sweepOrphans(ctx context.Context) error {
	runs, err := d.executor.OpenRuns(ctx)
	if err != nil {
		return err
	}
	var failed error
	for _, run := range runs {
		w, err := d.workflows.FindByID(ctx, workflowIDOf(run.WorkflowID))
		var reason string
		switch {
		case errors.Is(err, workflowdomain.ErrWorkflowNotV3) || errors.Is(err, workflowdomain.ErrWorkflowNotFound):
			reason = "The store has no Workflow for this run."
		case err != nil:
			failed = errors.Join(failed, err)
			continue
		case w.Status.Terminal():
			reason = "The Workflow already ended."
		case !run.Current && w.Dispatch.Driver == workflowdomain.DriverVersion:
			reason = "The Workflow runs on the current driver."
		default:
			continue
		}
		if err := d.executor.Terminate(ctx, run, reason); err != nil && !errors.Is(err, workflowdomain.ErrWorkflowExecutionGone) {
			failed = errors.Join(failed, err)
		}
	}
	return failed
}

// workflowIDOf maps an executor run identity to its Workflow id: `swallow-operation/<id>`, or
// `swallow-operation/<id>/job/...` for a Job child of an earlier driver.
func workflowIDOf(executionID string) string {
	id := strings.TrimPrefix(executionID, "swallow-operation/")
	if i := strings.Index(id, "/"); i >= 0 {
		id = id[:i]
	}
	return id
}
