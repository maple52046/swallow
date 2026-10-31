package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/mongo"

	storemongo "github.com/maple52046/swallow/internal/adapters/store/mongodb"
	convergencestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/convergence"
	"github.com/maple52046/swallow/internal/core/convergence"
)

// controllerRuntime assembles the desired-state controllers the worker process runs
// (desired-state-model.md §7.6) and returns a function that runs them until ctx ends. Controllers
// never run in the API process: the runtime gives each one a singleton store lease, so even several
// worker processes keep one active instance per controller.
//
// The holder name is unique per process start, so a restarted worker competes for the leases as a
// new holder and waits at most one lease TTL for its predecessor's leases to expire.
func controllerRuntime(db *mongo.Database, resync convergence.RuntimeOptions, controllers ...convergence.Controller) func(context.Context) {
	host, _ := os.Hostname()
	resync.Holder = fmt.Sprintf("%s/%d/%s", host, os.Getpid(), uuid.NewString()[:8])
	if resync.Logger == nil {
		resync.Logger = slog.Default()
	}
	runtime := convergence.NewRuntime(storemongo.NewChangeFeed(db), convergencestore.NewMongoLeaseStore(db), resync)
	return func(ctx context.Context) { runtime.Run(ctx, controllers...) }
}
