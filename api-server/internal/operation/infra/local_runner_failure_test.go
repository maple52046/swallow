package infra

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeJobEvent writes one ansible-runner job-event file into a run's job_events directory,
// mirroring the on-disk layout summarizeRunFailure reads (ordinal-prefixed JSON files).
func writeJobEvent(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSummarizeRunFailure(t *testing.T) {
	root := t.TempDir()
	runID := "run-1"
	events := filepath.Join(root, runID, "job_events")
	if err := os.MkdirAll(events, 0o750); err != nil {
		t.Fatal(err)
	}
	// A successful task must be ignored; only failed/unreachable events explain a failure.
	writeJobEvent(t, events, "3-a.json",
		`{"event":"runner_on_ok","event_data":{"task":"Install packages","host":"host-a","res":{"changed":true}}}`)
	// The real scontrol-ping failure: the actionable detail is in stdout, not stderr, and
	// the module message is the generic "non-zero return code".
	writeJobEvent(t, events, "12-b.json",
		`{"event":"runner_on_failed","event_data":{"task":"Wait for the controller to answer scontrol ping","host":"host-controller","res":{"msg":"non-zero return code","rc":1,"stderr":"","stdout":"Slurmctld(primary) at lab-control-1 is DOWN"}}}`)
	// An unreachable host is a distinct failure class and must be labelled as such.
	writeJobEvent(t, events, "5-c.json",
		`{"event":"runner_on_unreachable","event_data":{"task":"Gathering Facts","host":"host-down","res":{"msg":"Failed to connect to the host via ssh: timed out"}}}`)

	runner := &LocalRunner{artifactRoot: root}
	summary := runner.summarizeRunFailure(runID)

	if summary == "" {
		t.Fatalf("summarizeRunFailure(%q) = empty, want failure detail", runID)
	}
	if strings.Contains(summary, "Install packages") {
		t.Errorf("summary includes a successful task:\n%s", summary)
	}
	// Ordering follows the event ordinal, so the unreachable event (5) precedes failed (12).
	if idxUnreachable, idxFailed := strings.Index(summary, "host-down"), strings.Index(summary, "host-controller"); idxUnreachable > idxFailed {
		t.Errorf("events not ordered by ordinal:\n%s", summary)
	}
	for _, want := range []string{
		`task "Wait for the controller to answer scontrol ping" failed`,
		"host-controller",
		"rc=1",
		"Slurmctld(primary) at lab-control-1 is DOWN",
		"host-down",
		"unreachable",
		"Failed to connect to the host via ssh",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q:\n%s", want, summary)
		}
	}
}

func TestSummarizeRunFailureKeepsLatestPerHost(t *testing.T) {
	root := t.TempDir()
	runID := "run-rescue"
	events := filepath.Join(root, runID, "job_events")
	if err := os.MkdirAll(events, 0o750); err != nil {
		t.Fatal(err)
	}
	// A block/rescue emits the original task failure and then the rescue's diagnostic
	// failure for the same host. The summary must collapse these to a single line and keep
	// the later, more specific diagnostic rather than repeating the host.
	writeJobEvent(t, events, "40-orig.json",
		`{"event":"runner_on_failed","event_data":{"task":"Wait for the controller to answer scontrol ping","host":"host-x","res":{"msg":"non-zero return code","rc":1}}}`)
	writeJobEvent(t, events, "44-rescue.json",
		`{"event":"runner_on_failed","event_data":{"task":"Fail with the slurmctld startup diagnostics","host":"host-x","res":{"msg":"slurmctld journal tail: heartbeat open attempt failed from /slurmdata/heartbeat"}}}`)

	summary := (&LocalRunner{artifactRoot: root}).summarizeRunFailure(runID)

	if lines := strings.Count(summary, "\n") + 1; lines != 1 {
		t.Errorf("summary has %d lines, want 1 (one per host):\n%s", lines, summary)
	}
	if !strings.Contains(summary, "heartbeat open attempt failed from /slurmdata/heartbeat") {
		t.Errorf("summary dropped the later diagnostic:\n%s", summary)
	}
	if strings.Contains(summary, "non-zero return code") {
		t.Errorf("summary kept the superseded first failure:\n%s", summary)
	}
}

func TestSummarizeRunFailureNoEvents(t *testing.T) {
	root := t.TempDir()
	runner := &LocalRunner{artifactRoot: root}
	// A run whose job_events directory never materialized (an early ansible-runner error)
	// must yield no summary so the caller keeps the raw exit error instead of an empty cause.
	if got := runner.summarizeRunFailure("missing-run"); got != "" {
		t.Errorf("summarizeRunFailure(missing) = %q, want empty", got)
	}
}

func TestSummarizeRunFailureBoundsHosts(t *testing.T) {
	root := t.TempDir()
	runID := "run-many"
	events := filepath.Join(root, runID, "job_events")
	if err := os.MkdirAll(events, 0o750); err != nil {
		t.Fatal(err)
	}
	// More failing hosts than the cap: the summary must stay bounded and never grow without
	// limit, because the full record is the retained stdout artifact.
	for i := 0; i < maxFailureHosts+4; i++ {
		writeJobEvent(t, events, fmt.Sprintf("%02d-e.json", i),
			fmt.Sprintf(`{"event":"runner_on_failed","event_data":{"task":"t","host":"h%d","res":{"msg":"boom"}}}`, i))
	}
	summary := (&LocalRunner{artifactRoot: root}).summarizeRunFailure(runID)
	if lines := strings.Count(summary, "\n") + 1; lines > maxFailureHosts {
		t.Errorf("summary has %d lines, want <= %d:\n%s", lines, maxFailureHosts, summary)
	}
}
