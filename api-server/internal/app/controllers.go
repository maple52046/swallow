package app

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/mongo"

	storemongo "github.com/maple52046/swallow/internal/adapters/store/mongodb"
	convergencestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/convergence"
	"github.com/maple52046/swallow/internal/core/convergence"
)

// workerRuntime is the worker process's convergence runtime (desired-state-model.md §7.6): it runs
// the desired-state controllers and the periodic observation passes. Neither runs in the API
// process: the runtime gives each one a singleton store lease, so even several worker processes
// keep one active instance of each.
//
// The holder name is unique per process start, so a restarted worker competes for the leases as a
// new holder and waits at most one lease TTL for its predecessor's leases to expire.
func workerRuntime(db *mongo.Database, opts convergence.RuntimeOptions) *convergence.Runtime {
	host, _ := os.Hostname()
	opts.Holder = fmt.Sprintf("%s/%d/%s", host, os.Getpid(), uuid.NewString()[:8])
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return convergence.NewRuntime(storemongo.NewChangeFeed(db), convergencestore.NewMongoLeaseStore(db), opts)
}
