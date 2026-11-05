package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// UpdateTaskWaiting applies the store's guard: the current generation and a Task running at the
// attempt.
func (m *memoryWorkflows) UpdateTaskWaiting(ctx context.Context, id string, generation int64, taskID string, attempt int, waiting *workflowdomain.TaskWaiting) error {
	w, err := m.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if w.Dispatch.Generation != generation {
		return workflowdomain.ErrDispatchFenced
	}
	for i := range w.Steps {
		task := &w.Steps[i]
		running := task.Status == workflowdomain.TaskRunning || task.Status == workflowdomain.TaskWaitingExternal
		if task.ID == taskID && task.Attempt == attempt && running {
			task.Waiting = waiting
			return m.Save(ctx, w)
		}
	}
	return workflowdomain.ErrTaskNotRunning
}

// captureLogs sends the default logger to a buffer for the test and returns the buffer's JSON
// lines when called.
func captureLogs(t *testing.T) func() []map[string]any {
	t.Helper()
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return func() []map[string]any {
		var lines []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
			if line == "" {
				continue
			}
			var entry map[string]any
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				t.Fatalf("log line %q is not JSON: %v", line, err)
			}
			lines = append(lines, entry)
		}
		return lines
	}
}

// A Runner's waiting is stored redacted while the attempt runs, and its outcome is stored with a
// redacted, classified diagnostic whose class and endpoint stay on the timeline; the attempt's
// waiting is gone once the outcome is recorded, and the outcome is logged once with its
// correlation fields.
func TestEngine_StoresRedactedDiagnosticsAndWaitingAndLogsTheOutcome(t *testing.T) {
	logs := captureLogs(t)
	w := newWorkflow("w", pendingTask("a", "deploy", workflowdomain.RunnerKindProvisioner))
	w.RequestCorrelation = "request-7"
	f := newEngineFixture(t, 4, w)
	gen := f.dispatch(t, "w")
	f.advance(t, "w", gen)
	ref := TaskRef{WorkflowID: "w", Generation: gen, TaskID: "a", Attempt: 1}
	ctx := context.Background()

	observedAt := time.Date(2026, 11, 5, 8, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	if err := NewWaitingPublisher(f.engine, ref).Publish(ctx, workflowdomain.TaskWaiting{
		Observation: "provider at https://ops:pw@provisioner.lab/api still Deploying", ObservedAt: observedAt,
		Attempts: 3, ElapsedSeconds: 30, BudgetSeconds: 600,
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	waiting := f.workflows.get(t, "w").Steps[0].Waiting
	wantWaiting := &workflowdomain.TaskWaiting{
		Observation: "provider at https://[REDACTED]@provisioner.lab/api still Deploying", ObservedAt: observedAt.UTC(),
		Attempts: 3, ElapsedSeconds: 30, BudgetSeconds: 600,
	}
	if waiting == nil || *waiting != *wantWaiting {
		t.Fatalf("stored waiting = %+v, want %+v", waiting, wantWaiting)
	}

	err := f.engine.Record(ctx, ref, Outcome{Status: workflowdomain.TaskFailed, Error: &workflowdomain.NormalizedError{
		Code: "provider_unreachable", Message: "The provider refused token=abc123.", Retryable: true,
		Diagnostic: &workflowdomain.TaskDiagnostic{
			Endpoint:    "https://ops:pw@provisioner.lab:5240/api?op=read&token=t",
			Cause:       "GET /api: Authorization: Bearer abcdefghijkl rejected",
			Remediation: "Check the provider credentials, then retry the Task.",
		},
	}})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	task := f.workflows.get(t, "w").Steps[0]
	if task.Waiting != nil {
		t.Errorf("waiting after the outcome = %+v, want nil", task.Waiting)
	}
	if task.Error == nil || task.Error.Diagnostic == nil {
		t.Fatalf("stored error = %+v, want one with a diagnostic", task.Error)
	}
	if got, want := task.Error.Message, "The provider refused token=[REDACTED]."; got != want {
		t.Errorf("stored message = %q, want %q", got, want)
	}
	wantDiagnostic := workflowdomain.TaskDiagnostic{
		FailureClass: workflowdomain.FailureUnknown,
		Endpoint:     "https://provisioner.lab:5240/api?op=[REDACTED]&token=[REDACTED]",
		Cause:        "GET /api: Authorization: [REDACTED]",
		Remediation:  "Check the provider credentials, then retry the Task.",
	}
	if got := *task.Error.Diagnostic; got.FailureClass != wantDiagnostic.FailureClass || got.Endpoint != wantDiagnostic.Endpoint ||
		got.Cause != wantDiagnostic.Cause || got.Remediation != wantDiagnostic.Remediation {
		t.Errorf("stored diagnostic = %+v, want %+v", got, wantDiagnostic)
	}

	event, ok := f.workflows.events["w/step/a/failed/1"]
	if !ok {
		t.Fatalf("no step_status event for the failed attempt; events %v", f.workflows.events)
	}
	if event.Details["failureClass"] != "unknown" || event.Details["endpoint"] != wantDiagnostic.Endpoint ||
		event.Details["message"] != "The provider refused token=[REDACTED]." {
		t.Errorf("step_status details = %v, want the redacted message, the class, and the endpoint", event.Details)
	}

	var outcomes []map[string]any
	for _, line := range logs() {
		if line["msg"] == "workflow task outcome" {
			outcomes = append(outcomes, line)
		}
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcome log lines = %v, want exactly one", outcomes)
	}
	want := map[string]any{
		"level": "WARN", "workflowId": "w", "taskId": "a", "attempt": float64(1), "kind": "deploy",
		"runner": "maas", "status": "failed", "requestCorrelation": "request-7", "code": "provider_unreachable",
		"retryable": true, "failureClass": "unknown", "endpoint": wantDiagnostic.Endpoint,
	}
	for key, value := range want {
		if outcomes[0][key] != value {
			t.Errorf("outcome log %s = %v, want %v (line %v)", key, outcomes[0][key], value, outcomes[0])
		}
	}
}

// A successful outcome logs at Info with no failure fields.
func TestEngine_LogsASuccessAtInfo(t *testing.T) {
	logs := captureLogs(t)
	f := newEngineFixture(t, 4, newWorkflow("w", pendingTask("a", "deploy", workflowdomain.RunnerKindProvisioner)))
	gen := f.dispatch(t, "w")
	f.advance(t, "w", gen)
	if err := f.engine.Record(context.Background(), TaskRef{WorkflowID: "w", Generation: gen, TaskID: "a", Attempt: 1}, Outcome{}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	for _, line := range logs() {
		if line["msg"] != "workflow task outcome" {
			continue
		}
		if line["level"] != "INFO" || line["status"] != "succeeded" || line["code"] != nil || line["failureClass"] != nil {
			t.Errorf("success log = %v, want Info with status succeeded and no failure fields", line)
		}
		return
	}
	t.Fatal("no outcome was logged for the succeeded Task")
}

// Publication is bound to its attempt: a superseded generation is fenced, an attempt that ended
// is nothing to do, and the zero publisher discards. A retry clears what an earlier attempt left.
func TestWaitingPublisher_IsBoundToItsAttempt(t *testing.T) {
	f := newEngineFixture(t, 4, newWorkflow("w", pendingTask("a", "deploy", workflowdomain.RunnerKindProvisioner)))
	old := f.dispatch(t, "w")
	f.advance(t, "w", old)
	current := f.dispatch(t, "w")
	f.advance(t, "w", current)
	ctx := context.Background()
	waiting := workflowdomain.TaskWaiting{Observation: "still Deploying", ObservedAt: time.Now(), Attempts: 1}

	if err := NewWaitingPublisher(f.engine, TaskRef{WorkflowID: "w", Generation: old, TaskID: "a", Attempt: 1}).Publish(ctx, waiting); !errors.Is(err, workflowdomain.ErrDispatchFenced) {
		t.Errorf("Publish for a superseded generation = %v, want ErrDispatchFenced", err)
	}
	if err := NewWaitingPublisher(f.engine, TaskRef{WorkflowID: "w", Generation: current, TaskID: "a", Attempt: 2}).Publish(ctx, waiting); err != nil {
		t.Errorf("Publish for an attempt that is not running = %v, want nil", err)
	}
	if err := (WaitingPublisher{}).Publish(ctx, waiting); err != nil {
		t.Errorf("the zero WaitingPublisher = %v, want it to discard", err)
	}
	if got := f.workflows.get(t, "w").Steps[0].Waiting; got != nil {
		t.Errorf("waiting after refused publications = %+v, want nil", got)
	}

	failed := pendingTask("b", "deploy", workflowdomain.RunnerKindProvisioner)
	failed.Status = workflowdomain.TaskFailed
	failed.Error = &workflowdomain.NormalizedError{Code: "x", Message: "x", Retryable: true}
	failed.Waiting = &waiting
	w := newWorkflow("r", failed)
	w.Commands.Retries = []workflowdomain.RetryCommand{{TaskID: "b"}}
	retried := newEngineFixture(t, 4, w)
	retried.advance(t, "r", retried.dispatch(t, "r"))
	if got := retried.workflows.get(t, "r").Steps[0]; got.Attempt != 2 || got.Waiting != nil {
		t.Errorf("retried Task = attempt %d waiting %+v, want attempt 2 with no waiting", got.Attempt, got.Waiting)
	}
}
