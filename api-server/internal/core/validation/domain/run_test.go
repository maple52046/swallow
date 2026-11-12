package domain

import (
	"strings"
	"testing"
	"time"
)

func serversRun() Run {
	return Run{
		ID: "r", Status: StatusPending, Mode: ModeReport,
		Checks: []Check{{ID: "time-sync"}, {ID: "memory", Gating: true}},
		Target: Target{Kind: TargetServers, ServerIDs: []string{"s-1", "s-2"}},
	}
}

func server(id string) ResultTarget { return ResultTarget{Kind: ResultTargetServer, ID: id} }

// A Task's results are kept for declared checks on the run's targets, bounded and redacted;
// anything else is refused, and a later result replaces an earlier one for the same check and target.
func TestRunRecordResults(t *testing.T) {
	run := serversRun()
	rejected := run.RecordResults([]Result{
		{Check: "time-sync", Target: server("s-1"), Verdict: VerdictFailed, Summary: "offset 3s"},
		{Check: "time-sync", Target: server("s-1"), Verdict: VerdictPassed, Evidence: strings.Repeat("x", 9000) + " password=hunter2"},
		{Check: "undeclared", Target: server("s-1"), Verdict: VerdictPassed},
		{Check: "memory", Target: server("s-9"), Verdict: VerdictPassed},
		{Check: "memory", Target: server("s-2"), Verdict: "maybe"},
	})
	if len(rejected) != 3 {
		t.Errorf("RecordResults() rejected %v, want 3", rejected)
	}
	if len(run.Results) != 1 || run.Results[0].Verdict != VerdictPassed {
		t.Fatalf("Results = %+v, want the later time-sync result only", run.Results)
	}
	evidence := run.Results[0].Evidence
	if len(evidence) > maxEvidenceBytes || strings.Contains(evidence, "hunter2") {
		t.Errorf("evidence is %d bytes with the secret %v, want it bounded and redacted", len(evidence), strings.Contains(evidence, "hunter2"))
	}
}

// Completing fills every check without a result with an error and derives the verdict; a canceled
// run keeps its results and has none.
func TestRunCompleteAndVerdict(t *testing.T) {
	now := time.Date(2026, 11, 12, 2, 0, 0, 0, time.UTC)
	run := serversRun()
	run.RecordResults([]Result{
		{Check: "time-sync", Target: server("s-1"), Verdict: VerdictPassed},
		{Check: "time-sync", Target: server("s-2"), Verdict: VerdictSkipped},
		{Check: "memory", Target: server("s-1"), Verdict: VerdictPassed},
	})
	run.Complete("host s-2 unreachable", now)
	if run.Status != StatusCompleted || run.Verdict != VerdictError || len(run.Results) != 4 {
		t.Fatalf("Complete() = %s %s with %d results, want completed error with 4", run.Status, run.Verdict, len(run.Results))
	}
	if counts := run.Counts(); counts != (Counts{Passed: 2, Error: 1, Skipped: 1}) {
		t.Errorf("Counts() = %+v", counts)
	}
	if got := run.Results[3]; got.Check != "memory" || got.Target != server("s-2") || !strings.Contains(got.Summary, "unreachable") {
		t.Errorf("filled result = %+v, want memory on s-2 explaining the Task's failure", got)
	}
	if failures := run.GateFailures(); len(failures) != 1 || failures[0].Target != server("s-2") {
		t.Errorf("GateFailures() = %+v, want the memory error on s-2", failures)
	}

	canceled := serversRun()
	canceled.RecordResults([]Result{{Check: "memory", Target: server("s-1"), Verdict: VerdictFailed}})
	canceled.Cancel("Preempted by Workflow w", now)
	if canceled.Status != StatusCanceled || canceled.Verdict != "" || len(canceled.Results) != 1 {
		t.Errorf("Cancel() = %+v, want canceled without a verdict, keeping its result", canceled)
	}
	if rejected := canceled.RecordResults([]Result{{Check: "time-sync", Target: server("s-1"), Verdict: VerdictPassed}}); len(rejected) == 0 {
		t.Error("RecordResults() after the run ended accepted a result")
	}
}

func TestDeriveVerdict(t *testing.T) {
	tests := []struct {
		verdicts []Verdict
		want     Verdict
	}{
		{nil, VerdictPassed},
		{[]Verdict{VerdictSkipped}, VerdictPassed},
		{[]Verdict{VerdictPassed, VerdictError}, VerdictError},
		{[]Verdict{VerdictError, VerdictFailed, VerdictPassed}, VerdictFailed},
	}
	for _, tc := range tests {
		results := make([]Result, 0, len(tc.verdicts))
		for _, verdict := range tc.verdicts {
			results = append(results, Result{Verdict: verdict})
		}
		if got := DeriveVerdict(results); got != tc.want {
			t.Errorf("DeriveVerdict(%v) = %s, want %s", tc.verdicts, got, tc.want)
		}
	}
}

// A platform run judges the Platform and its members; a check no Task judged is an error on the
// Platform.
func TestPlatformRunTargets(t *testing.T) {
	run := Run{Status: StatusRunning, Checks: []Check{{ID: "gpu-gres", Gating: true}, {ID: "srun"}},
		Target: Target{Kind: TargetPlatform, PlatformID: "p", ServerIDs: []string{"login"}, MemberIDs: []string{"login", "c-1"}}}
	if rejected := run.RecordResults([]Result{
		{Check: "gpu-gres", Target: server("c-1"), Verdict: VerdictPassed},
		{Check: "srun", Target: ResultTarget{Kind: ResultTargetPlatform, ID: "other"}, Verdict: VerdictPassed},
	}); len(rejected) != 1 {
		t.Errorf("RecordResults() rejected %v, want only the other Platform", rejected)
	}
	run.Complete("", time.Now())
	if len(run.Results) != 2 || run.Results[1].Target != (ResultTarget{Kind: ResultTargetPlatform, ID: "p"}) || run.Verdict != VerdictError {
		t.Errorf("Complete() = %+v, want srun as an error on the Platform", run.Results)
	}
}
