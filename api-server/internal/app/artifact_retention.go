package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/maple52046/swallow/internal/adapters/hostautomation/ansible"
)

const artifactPruneInterval = 24 * time.Hour

func runArtifactRetention(ctx context.Context, root string, retention time.Duration) {
	prune := func() {
		removed, err := ansible.PruneArtifacts(root, time.Now().UTC().Add(-retention))
		if err != nil {
			slog.Error("prune job artifacts", "root", root, "error", err)
			return
		}
		if removed > 0 {
			slog.Info("pruned expired job artifacts", "count", removed)
		}
	}

	prune()
	ticker := time.NewTicker(artifactPruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			prune()
		}
	}
}
