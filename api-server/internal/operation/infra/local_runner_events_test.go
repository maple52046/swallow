package infra

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEventsSinceStreamsHostResultsAndCurrentTask verifies the incremental reader used for live
// progress: it returns only host-result events past the cursor, advances the cursor to the highest
// ordinal seen, and reports the most recently started play/task from play/task-start events.
func TestEventsSinceStreamsHostResultsAndCurrentTask(t *testing.T) {
	root := t.TempDir()
	runID := "run-e"
	events := filepath.Join(root, runID, "job_events")
	if err := os.MkdirAll(events, 0o750); err != nil {
		t.Fatal(err)
	}
	writeJobEvent(t, events, "1-play.json", `{"event":"playbook_on_play_start","event_data":{"play":"Install k0s"}}`)
	writeJobEvent(t, events, "2-task.json", `{"event":"playbook_on_task_start","event_data":{"play":"Install k0s","task":"Download k0s"}}`)
	writeJobEvent(t, events, "3-ok.json", `{"event":"runner_on_ok","event_data":{"play":"Install k0s","task":"Download k0s","host":"srv-1","res":{"changed":true}}}`)
	writeJobEvent(t, events, "4-task.json", `{"event":"playbook_on_task_start","event_data":{"play":"Install k0s","task":"Start k0s"}}`)
	writeJobEvent(t, events, "5-fail.json", `{"event":"runner_on_failed","event_data":{"play":"Install k0s","task":"Start k0s","host":"srv-2","res":{}}}`)

	runner := &LocalRunner{artifactRoot: root}

	delta, err := runner.EventsSince(runID, 0)
	if err != nil {
		t.Fatalf("EventsSince: %v", err)
	}
	if delta.LastSeq != 5 {
		t.Errorf("LastSeq = %d, want 5", delta.LastSeq)
	}
	if delta.CurrentPlay != "Install k0s" || delta.CurrentTask != "Start k0s" {
		t.Errorf("current = %q/%q, want Install k0s/Start k0s", delta.CurrentPlay, delta.CurrentTask)
	}
	// Only host results are streamed; play/task-start markers are not events.
	if len(delta.Events) != 2 {
		t.Fatalf("events = %d, want 2 (%+v)", len(delta.Events), delta.Events)
	}
	if delta.Events[0].Seq != 3 || delta.Events[0].Status != "ok" || !delta.Events[0].Changed {
		t.Errorf("first event = %+v, want seq 3 ok changed", delta.Events[0])
	}
	if delta.Events[1].Seq != 5 || delta.Events[1].Status != "failed" {
		t.Errorf("second event = %+v, want seq 5 failed", delta.Events[1])
	}

	// Incremental: past the cursor only the later event is returned, but the current task is still
	// reported from the task-start seen in this window.
	next, err := runner.EventsSince(runID, 3)
	if err != nil {
		t.Fatalf("EventsSince(after 3): %v", err)
	}
	if len(next.Events) != 1 || next.Events[0].Seq != 5 {
		t.Fatalf("incremental events = %+v, want only seq 5", next.Events)
	}
	if next.CurrentTask != "Start k0s" {
		t.Errorf("incremental current task = %q, want Start k0s", next.CurrentTask)
	}
}

// TestEventsSinceMissingDirectory verifies a run that has not written any event yet returns an
// empty delta at the same cursor rather than an error, so streaming before the first event is safe.
func TestEventsSinceMissingDirectory(t *testing.T) {
	runner := &LocalRunner{artifactRoot: t.TempDir()}
	delta, err := runner.EventsSince("never-ran", 7)
	if err != nil {
		t.Fatalf("EventsSince(missing): %v", err)
	}
	if delta.LastSeq != 7 || len(delta.Events) != 0 {
		t.Errorf("delta = %+v, want cursor 7 and no events", delta)
	}
}
