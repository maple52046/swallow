package application

import (
	"context"
	"testing"

	"github.com/maple52046/swallow/internal/core/store/storetest"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
	"github.com/maple52046/swallow/internal/core/validation/domain/validationtest"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

func newRecorderFixture(t *testing.T, run validationdomain.Run) (*Recorder, *validationtest.Runs) {
	t.Helper()
	runs := validationtest.NewRuns()
	if err := runs.Create(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	recorder, err := NewRecorder(&storetest.Transactor{}, runs)
	if err != nil {
		t.Fatal(err)
	}
	return recorder, runs
}

func reportRun() validationdomain.Run {
	return validationdomain.Run{ID: "r", WorkflowID: "w", Mode: validationdomain.ModeReport, Status: validationdomain.StatusPending,
		Checks: []validationdomain.Check{{ID: "time-sync"}},
		Target: validationdomain.Target{Kind: validationdomain.TargetServers, ServerIDs: []string{"s-1", "s-2"}}}
}

// A report run starts with its first check Task, records the results its playbook returned, and
// completes when its final Task ends, filling what no Task judged with the check Task's error.
func TestRecorderReportRun(t *testing.T) {
	recorder, runs := newRecorderFixture(t, reportRun())
	ctx := context.Background()
	workflow := &workflowdomain.Workflow{ID: "w"}
	check := workflowdomain.Task{ID: "check", Name: "Check", Status: workflowdomain.TaskRunning, Attempt: 1}
	if err := recorder.Project(ctx, "r", StageCheck, workflow, check); err != nil {
		t.Fatal(err)
	}
	if run, _ := runs.Get(ctx, "r"); run.Status != validationdomain.StatusRunning {
		t.Fatalf("after the check started: %s, want running", run.Status)
	}
	if err := recorder.RecordResults(ctx, "r", workflow, check, map[string]any{"checks": []any{
		map[string]any{"check": "time-sync", "target": map[string]any{"kind": "server", "id": "s-1"}, "verdict": "passed",
			"measurements": []any{map[string]any{"name": "offset", "value": 0.002, "unit": "s"},
				map[string]any{"name": "cpus", "value": " 64 "}, map[string]any{"name": "bad", "value": "many"}}},
		map[string]any{"check": "nonsense"},
	}}); err != nil {
		t.Fatal(err)
	}
	workflow.Steps = []workflowdomain.Task{{ID: "other", Name: "Probe s-2", Status: workflowdomain.TaskFailed,
		Error: &workflowdomain.NormalizedError{Message: "s-2 unreachable"}}}
	complete := workflowdomain.Task{ID: "complete", Status: workflowdomain.TaskSucceeded}
	if err := recorder.Project(ctx, "r", StageComplete, workflow, complete); err != nil {
		t.Fatal(err)
	}
	run, _ := runs.Get(ctx, "r")
	if run.Status != validationdomain.StatusCompleted || run.Verdict != validationdomain.VerdictError || len(run.Results) != 2 {
		t.Fatalf("completed run = %+v", run)
	}
	if measurements := run.Results[0].Measurements; len(measurements) != 2 || measurements[0].Value != 0.002 ||
		measurements[1].Value != 64 || run.Results[0].TaskID != "check" {
		t.Errorf("recorded result = %+v", run.Results[0])
	}
}

// A Workflow that does not execute the run writes nothing; a canceled final Task cancels the run.
func TestRecorderFencesAndCancels(t *testing.T) {
	recorder, runs := newRecorderFixture(t, reportRun())
	ctx := context.Background()
	if err := recorder.Project(ctx, "r", StageCheck, &workflowdomain.Workflow{ID: "other"},
		workflowdomain.Task{Status: workflowdomain.TaskRunning}); err != nil {
		t.Fatal(err)
	}
	if run, _ := runs.Get(ctx, "r"); run.Status != validationdomain.StatusPending {
		t.Errorf("an unrelated Workflow moved the run to %s", run.Status)
	}
	canceled := &workflowdomain.Workflow{ID: "w", Commands: workflowdomain.Commands{Cancel: &workflowdomain.CancelCommand{Reason: "Preempted by Workflow d."}}}
	if err := recorder.Project(ctx, "r", StageComplete, canceled, workflowdomain.Task{Status: workflowdomain.TaskCanceled}); err != nil {
		t.Fatal(err)
	}
	if run, _ := runs.Get(ctx, "r"); run.Status != validationdomain.StatusCanceled || run.StatusReason != "Preempted by Workflow d." {
		t.Errorf("canceled run = %+v", run)
	}
}

// A gate run reports its failing gating checks, and is reopened by its deployment's retry or rerun.
func TestRecorderGate(t *testing.T) {
	gate := validationdomain.Run{ID: "g", WorkflowID: "deploy", Mode: validationdomain.ModeGate, Status: validationdomain.StatusRunning,
		Checks: []validationdomain.Check{{ID: "gpu-gres", Gating: true}, {ID: "srun"}},
		Target: validationdomain.Target{Kind: validationdomain.TargetPlatform, PlatformID: "p", MemberIDs: []string{"c-1"}}}
	recorder, runs := newRecorderFixture(t, gate)
	ctx := context.Background()
	workflow := &workflowdomain.Workflow{ID: "deploy"}
	if err := recorder.RecordResults(ctx, "g", workflow, workflowdomain.Task{ID: "acceptance"}, map[string]any{"checks": []any{
		map[string]any{"check": "gpu-gres", "target": map[string]any{"kind": "server", "id": "c-1"}, "verdict": "failed", "summary": "7 of 8 GPUs"},
		map[string]any{"check": "srun", "target": map[string]any{"kind": "platform", "id": "p"}, "verdict": "failed"},
	}}); err != nil {
		t.Fatal(err)
	}
	_, failures, err := recorder.GateFailures(ctx, "g")
	if err != nil || len(failures) != 1 || failures[0].Check != "gpu-gres" {
		t.Fatalf("GateFailures() = %+v, %v, want only the gating gpu-gres", failures, err)
	}
	if err := recorder.Project(ctx, "g", StageComplete, workflow, workflowdomain.Task{ID: "complete", Status: workflowdomain.TaskFailed}); err != nil {
		t.Fatal(err)
	}
	if run, _ := runs.Get(ctx, "g"); run.Status != validationdomain.StatusCompleted || run.Verdict != validationdomain.VerdictFailed {
		t.Fatalf("gate after the failed gate = %+v", run)
	}
	// The user retries the Job: the check starts again and the run reopens.
	if err := recorder.Project(ctx, "g", StageCheck, workflow, workflowdomain.Task{Status: workflowdomain.TaskRunning, Attempt: 2}); err != nil {
		t.Fatal(err)
	}
	if run, _ := runs.Get(ctx, "g"); run.Status != validationdomain.StatusRunning || len(run.Results) != 0 {
		t.Errorf("gate after a retry = %+v, want running with its results cleared", run)
	}
	// A rerun of the deployment takes the run over.
	rerun := &workflowdomain.Workflow{ID: "deploy-2", RetryOfOperationID: "deploy"}
	if err := recorder.Project(ctx, "g", StageCheck, rerun, workflowdomain.Task{Status: workflowdomain.TaskRunning}); err != nil {
		t.Fatal(err)
	}
	if run, _ := runs.Get(ctx, "g"); run.WorkflowID != "deploy-2" {
		t.Errorf("gate after a rerun belongs to %s, want deploy-2", run.WorkflowID)
	}
}
