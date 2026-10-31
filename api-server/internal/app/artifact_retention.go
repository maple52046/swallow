package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/maple52046/swallow/internal/adapters/hostautomation/ansible"
)

const artifactPruneInterval = 24 * time.Hour

// runEventPruner removes the streamed Ansible task events of runs whose artifacts are gone, so a
// run's live task list does not outlive its retained output (DF-10).
type runEventPruner interface {
	DeleteRuns(ctx context.Context, runIDs []string) error
}

func runArtifactRetention(ctx context.Context, root string, retention time.Duration, events runEventPruner) {
	pruneArtifacts(ctx, root, time.Now().UTC().Add(-retention), events)
	ticker := time.NewTicker(artifactPruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pruneArtifacts(ctx, root, time.Now().UTC().Add(-retention), events)
		}
	}
}

// pruneArtifacts removes the events of the runs whose artifacts are older than cutoff, then the
// artifacts. The events go first because the artifact directory is how a later pass finds the run
// again: an error stops the pass, and the next pass retries what is left.
func pruneArtifacts(ctx context.Context, root string, cutoff time.Time, events runEventPruner) {
	expired, err := ansible.ExpiredArtifacts(root, cutoff)
	if err != nil {
		slog.Error("list expired job artifacts", "root", root, "error", err)
		return
	}
	if len(expired) == 0 {
		return
	}
	if err := events.DeleteRuns(ctx, expired); err != nil {
		slog.Error("prune streamed task events", "runs", len(expired), "error", err)
		return
	}
	if err := ansible.RemoveArtifacts(root, expired); err != nil {
		slog.Error("prune job artifacts", "root", root, "error", err)
		return
	}
	slog.Info("pruned expired job artifacts", "count", len(expired))
}
