package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/maple52046/swallow/internal/core/store"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// Stage says which Task of a run a transition belongs to.
type Stage string

// The stages.
const (
	// StageCheck is a Task that judges checks (a template Task).
	StageCheck Stage = "check"
	// StageComplete is the run's final complete-validation Task.
	StageComplete Stage = "complete"
)

// Recorder folds what a run's Workflow does into the run: the results its check Tasks return, its
// status and verdict as its Tasks transition, and the gate a gate run's deployment passes through.
// It is the only writer of a run's results, status, and verdict; every write is fenced on the
// Workflow that executes the run, so a superseded or unrelated Workflow writes nothing.
type Recorder struct {
	transactor store.Transactor
	runs       validationdomain.RunRepository
	now        func() time.Time
}

// NewRecorder builds the Recorder.
func NewRecorder(transactor store.Transactor, runs validationdomain.RunRepository) (*Recorder, error) {
	if transactor == nil || runs == nil {
		return nil, errors.New("validation recorder: every dependency is required")
	}
	return &Recorder{transactor: transactor, runs: runs, now: time.Now}, nil
}

// RecordResults stores the results a succeeded check Task returned in its result file
// (validation.md §4.2) on run runID. It runs in the Ansible executor before the Task's outcome is
// recorded, in a transaction of its own so concurrent check Tasks of one run do not lose each
// other's results. Results the run does not declare are logged and dropped; a run that is gone or
// belongs to another Workflow is left alone. An error makes the Task retryable (completion_failed).
func (r *Recorder) RecordResults(ctx context.Context, runID string, workflow *workflowdomain.Workflow, task workflowdomain.Task, data map[string]any) error {
	results := parseResults(data, task, r.now().UTC())
	return r.transactor.InTransaction(ctx, func(ctx context.Context) error {
		run, ok, err := r.fencedRun(ctx, runID, workflow)
		if err != nil || !ok {
			return err
		}
		if rejected := run.RecordResults(results); len(rejected) > 0 {
			slog.Warn("validation results refused", "runId", runID, "workflowId", workflow.ID, "taskId", task.ID, "refused", rejected)
		}
		return r.runs.Save(ctx, run)
	})
}

// Project folds one Task transition into run runID's status, inside the transition's transaction:
// a check Task that starts moves a pending run to running (and reopens a gate run its deployment
// retries or reruns); the complete Task's end completes the run and derives its verdict, or cancels
// it when the Task was canceled.
func (r *Recorder) Project(ctx context.Context, runID string, stage Stage, workflow *workflowdomain.Workflow, task workflowdomain.Task) error {
	run, err := r.runs.Get(ctx, runID)
	if errors.Is(err, validationdomain.ErrRunNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	now := r.now().UTC()
	adopted := run.WorkflowID != workflow.ID
	if adopted && !(run.Mode == validationdomain.ModeGate && workflow.RetryOfOperationID == run.WorkflowID) {
		return nil
	}
	switch stage {
	case StageCheck:
		if task.Status != workflowdomain.TaskRunning {
			return nil
		}
		if adopted || (run.Mode == validationdomain.ModeGate && !run.Status.Active()) {
			run.Reopen(workflow.ID, now)
		} else {
			run.Start(now)
		}
	case StageComplete:
		switch task.Status {
		case workflowdomain.TaskSucceeded, workflowdomain.TaskFailed, workflowdomain.TaskSkipped:
			if adopted {
				run.Reopen(workflow.ID, now)
			}
			run.Complete(checkTaskFailure(workflow, task.ID), now)
		case workflowdomain.TaskCanceled:
			run.Cancel(cancelReason(workflow), now)
		default:
			return nil
		}
	default:
		return nil
	}
	return r.runs.Save(ctx, run)
}

// GateFailures returns run runID's gating checks that did not pass, read while the deployment's
// complete Task runs: none means the deployment may go on.
func (r *Recorder) GateFailures(ctx context.Context, runID string) (validationdomain.Run, []validationdomain.Result, error) {
	run, err := r.runs.Get(ctx, runID)
	if err != nil {
		return validationdomain.Run{}, nil, err
	}
	return run, run.GateFailures(), nil
}

// fencedRun reads the run when the Workflow is the one that executes it, or the rerun of a gate
// run's deployment, which then takes the run over.
func (r *Recorder) fencedRun(ctx context.Context, runID string, workflow *workflowdomain.Workflow) (validationdomain.Run, bool, error) {
	run, err := r.runs.Get(ctx, runID)
	if errors.Is(err, validationdomain.ErrRunNotFound) {
		return validationdomain.Run{}, false, nil
	}
	if err != nil {
		return validationdomain.Run{}, false, err
	}
	if run.WorkflowID == workflow.ID {
		return run, true, nil
	}
	if run.Mode == validationdomain.ModeGate && workflow.RetryOfOperationID == run.WorkflowID {
		run.Reopen(workflow.ID, r.now().UTC())
		return run, true, nil
	}
	return validationdomain.Run{}, false, nil
}

// checkTaskFailure is the error of the first check Task that failed without judging its checks, for
// the error results the completed run fills in; empty when none failed.
func checkTaskFailure(workflow *workflowdomain.Workflow, completeTaskID string) string {
	for _, task := range workflow.Steps {
		if task.ID == completeTaskID || task.Error == nil {
			continue
		}
		if task.Status == workflowdomain.TaskFailed || task.Status == workflowdomain.TaskRequiresAttention {
			return fmt.Sprintf("%s: %s", task.Name, task.Error.Message)
		}
	}
	return ""
}

func cancelReason(workflow *workflowdomain.Workflow) string {
	if workflow.Commands.Cancel != nil && strings.TrimSpace(workflow.Commands.Cancel.Reason) != "" {
		return workflow.Commands.Cancel.Reason
	}
	if workflow.StatusReason != "" {
		return workflow.StatusReason
	}
	return "The run's Workflow was canceled."
}

// parseResults reads the result file's checks (validation.md §4.2) leniently: a malformed entry is
// skipped, and the domain refuses what does not belong to the run.
func parseResults(data map[string]any, task workflowdomain.Task, now time.Time) []validationdomain.Result {
	raw, _ := data["checks"].([]any)
	results := make([]validationdomain.Result, 0, len(raw))
	for _, entry := range raw {
		object, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		target, _ := object["target"].(map[string]any)
		result := validationdomain.Result{
			Check:      stringOf(object["check"]),
			Target:     validationdomain.ResultTarget{Kind: stringOf(target["kind"]), ID: stringOf(target["id"])},
			Verdict:    validationdomain.Verdict(stringOf(object["verdict"])),
			Summary:    stringOf(object["summary"]),
			Evidence:   stringOf(object["evidence"]),
			ObservedAt: now, TaskID: task.ID, Attempt: task.Attempt,
		}
		measurements, _ := object["measurements"].([]any)
		for _, item := range measurements {
			measurement, ok := item.(map[string]any)
			if !ok {
				continue
			}
			value, ok := numberOf(measurement["value"])
			if !ok {
				continue
			}
			result.Measurements = append(result.Measurements, validationdomain.Measurement{
				Name: stringOf(measurement["name"]), Value: value,
				Unit: stringOf(measurement["unit"]), Expected: stringOf(measurement["expected"]),
			})
		}
		results = append(results, result)
	}
	return results
}

func stringOf(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func numberOf(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case string:
		// A playbook's templated value can reach the result file as a numeric string.
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0)
	}
	return 0, false
}
