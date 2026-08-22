package app

import (
	"context"
	"log"
	"time"

	operationapp "github.com/AFDEAPAC/swallow/internal/operation/application"
)

// runOperationPoller re-reads unfinished operations from their automation controller
// until ctx is cancelled.
//
// This is the correctness backstop for status mirroring. Controller webhooks make
// status feel live but are lossy — one lost while gdcm restarts is gone — so the poller
// is what guarantees an operation eventually reflects reality.
func runOperationPoller(ctx context.Context, operations *operationapp.OperationService, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("operation poller stopped")
			return
		case <-ticker.C:
			if _, err := operations.RefreshActive(ctx); err != nil {
				log.Printf("operation poller: cannot list active operations: %v", err)
			}
		}
	}
}
