package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// recordingEventPruner records the runs whose events retention deletes; err makes the delete fail.
type recordingEventPruner struct {
	deleted []string
	err     error
}

func (p *recordingEventPruner) DeleteRuns(_ context.Context, runIDs []string) error {
	if p.err != nil {
		return p.err
	}
	p.deleted = append(p.deleted, runIDs...)
	return nil
}

// DF-10: retention removes a run's streamed task events with its artifacts, and keeps the
// artifacts when the events could not be removed, so the next pass finds the run again.
func TestArtifactRetentionRemovesTheEventsOfPrunedRuns(t *testing.T) {
	seed := func(t *testing.T) string {
		root := t.TempDir()
		for _, run := range []string{"old-run", "fresh-run"} {
			if err := os.Mkdir(filepath.Join(root, run), 0o750); err != nil {
				t.Fatal(err)
			}
		}
		old := time.Now().Add(-48 * time.Hour)
		if err := os.Chtimes(filepath.Join(root, "old-run"), old, old); err != nil {
			t.Fatal(err)
		}
		return root
	}
	cutoff := time.Now().Add(-24 * time.Hour)

	root := seed(t)
	events := &recordingEventPruner{}
	pruneArtifacts(context.Background(), root, cutoff, events)
	if !reflect.DeepEqual(events.deleted, []string{"old-run"}) {
		t.Errorf("deleted events of %v, want [old-run]", events.deleted)
	}
	if _, err := os.Stat(filepath.Join(root, "old-run")); !os.IsNotExist(err) {
		t.Errorf("old-run artifacts remain: %v", err)
	}

	root = seed(t)
	pruneArtifacts(context.Background(), root, cutoff, &recordingEventPruner{err: errors.New("mongo down")})
	if _, err := os.Stat(filepath.Join(root, "old-run")); err != nil {
		t.Errorf("old-run artifacts were removed although its events were kept: %v", err)
	}
}
