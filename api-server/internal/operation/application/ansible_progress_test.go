package application

import (
	"context"
	"testing"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// TestRunProgressStateApply verifies the executor's live-progress accumulator: it counts each host
// result (with changed as a subset of ok), advances the cursor, and carries the most recent
// play/task forward when a later delta reports none.
func TestRunProgressStateApply(t *testing.T) {
	state := &runProgressState{}
	state.apply(operationdomain.RunProgressDelta{
		CurrentPlay: "Install", CurrentTask: "Download", LastSeq: 3,
		Events: []operationdomain.StreamedTaskEvent{
			{Seq: 1, TaskEvent: operationdomain.TaskEvent{Status: "ok", Changed: true}},
			{Seq: 2, TaskEvent: operationdomain.TaskEvent{Status: "failed"}},
			{Seq: 3, TaskEvent: operationdomain.TaskEvent{Status: "skipped"}},
		},
	})
	state.apply(operationdomain.RunProgressDelta{
		CurrentTask: "Start", LastSeq: 5,
		Events: []operationdomain.StreamedTaskEvent{
			{Seq: 4, TaskEvent: operationdomain.TaskEvent{Status: "unreachable"}},
			{Seq: 5, TaskEvent: operationdomain.TaskEvent{Status: "ok"}},
		},
	})
	// A delta with no new play/task must not clear the current task.
	state.apply(operationdomain.RunProgressDelta{LastSeq: 5})

	got := state.prog
	if state.lastSeq != 5 {
		t.Errorf("lastSeq = %d, want 5", state.lastSeq)
	}
	if got.Total != 5 {
		t.Errorf("total = %d, want 5", got.Total)
	}
	if got.OK != 2 || got.Changed != 1 || got.Failed != 1 || got.Unreachable != 1 || got.Skipped != 1 {
		t.Errorf("counts = %+v, want ok2 changed1 failed1 unreachable1 skipped1", got)
	}
	if got.CurrentPlay != "Install" || got.CurrentTask != "Start" {
		t.Errorf("current = %q/%q, want Install/Start", got.CurrentPlay, got.CurrentTask)
	}
}

// fakeEventRepo is a minimal AnsibleEventRepository returning a fixed event list.
type fakeEventRepo struct{ events []operationdomain.TaskEvent }

func (f *fakeEventRepo) AppendEvents(context.Context, []operationdomain.StreamedTaskEvent) error {
	return nil
}

func (f *fakeEventRepo) ListEvents(context.Context, string) ([]operationdomain.TaskEvent, error) {
	return f.events, nil
}

// TestEventsForRunReadsStreamAndCounts verifies EventsForRun reads the durable event stream and
// derives the per-status counts from it, so the summary and the list can never disagree.
func TestEventsForRunReadsStreamAndCounts(t *testing.T) {
	repo := &fakeEventRepo{events: []operationdomain.TaskEvent{
		{Play: "p", Task: "t1", Host: "h1", Status: "ok", Changed: true},
		{Play: "p", Task: "t2", Host: "h1", Status: "ok"},
		{Play: "p", Task: "t3", Host: "h2", Status: "failed"},
		{Play: "p", Task: "t4", Host: "h3", Status: "unreachable"},
		{Play: "p", Task: "t5", Host: "h1", Status: "skipped"},
	}}
	service := &ExecutionService{events: repo}

	item, err := service.EventsForRun(context.Background(), "run-1", "running")
	if err != nil {
		t.Fatalf("EventsForRun: %v", err)
	}
	if len(item.Events) != 5 {
		t.Fatalf("events = %d, want 5", len(item.Events))
	}
	if item.OKCount != 2 || item.ChangedCount != 1 || item.FailedCount != 1 ||
		item.UnreachableCount != 1 || item.SkippedCount != 1 {
		t.Errorf("counts = %+v, want ok2 changed1 failed1 unreachable1 skipped1", item)
	}
	if item.RunID != "run-1" || item.Status != "running" {
		t.Errorf("meta = %q/%q, want run-1/running", item.RunID, item.Status)
	}
}
