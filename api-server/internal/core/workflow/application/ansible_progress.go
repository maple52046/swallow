package application

import (
	"context"
	"time"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// InventorySource produces the same serverId-keyed inventory projection as the public
// dynamic-inventory endpoint, so an Ansible Task runs against swallow's own identifiers.
type InventorySource interface {
	Inventory(ctx context.Context, siteID string) (map[string]any, error)
}

// streamInterval bounds how often the executor tails a run's events into the durable event store
// and refreshes its progress. It is fast enough to feel live under the dashboard's polling and slow
// enough to keep the per-run read/write load negligible next to the ansible-runner subprocess.
const streamInterval = 2 * time.Second

// runProgressState accumulates a run's live progress across streaming ticks. lastSeq is the event
// ordinal cursor; prog carries the cumulative counts and current play/task. It advances only after
// new events are durably appended, so a failed append is retried on the next tick without losing or
// double-counting events.
type runProgressState struct {
	lastSeq int
	prog    workflowdomain.TaskLive
}

// apply folds a delta into the accumulator: it advances the cursor, carries forward the newest
// play/task, and counts each host result. A changed result also counts as ok, matching Ansible's
// recap where "changed" is a subset of "ok".
func (s *runProgressState) apply(delta workflowdomain.RunProgressDelta) {
	if delta.CurrentPlay != "" {
		s.prog.CurrentPlay = delta.CurrentPlay
	}
	if delta.CurrentTask != "" {
		s.prog.CurrentTask = delta.CurrentTask
	}
	for _, event := range delta.Events {
		s.prog.Total++
		switch event.Status {
		case "ok":
			s.prog.OK++
		case "failed":
			s.prog.Failed++
		case "unreachable":
			s.prog.Unreachable++
		case "skipped":
			s.prog.Skipped++
		}
		if event.Changed {
			s.prog.Changed++
		}
	}
	if delta.LastSeq > s.lastSeq {
		s.lastSeq = delta.LastSeq
	}
}
