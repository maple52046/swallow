package app

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/mongo"

	convergencestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/convergence"
	"github.com/maple52046/swallow/internal/core/convergence"
	"github.com/maple52046/swallow/internal/core/store"
)

// newChangePlanService builds the Change Plan service the api and the worker share (decision 076):
// the api proposes and decides plans and keeps the Change Plan policy, and the worker's controllers
// apply and expire them. Both read the same stored policy, so a switch an admin flips through the
// api applies to the next spec write without a restart. The resources that plan register their
// planners here as they become spec/status resources.
func newChangePlanService(ctx context.Context, db *mongo.Database, transactor store.Transactor, ttl time.Duration, planners ...convergence.Planner) (*convergence.ChangePlanService, error) {
	plans, err := convergencestore.NewMongoChangePlanRepo(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("change plan repo init: %w", err)
	}
	events, err := convergencestore.NewMongoEventRecorder(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("convergence event store init: %w", err)
	}
	return convergence.NewChangePlanService(convergence.ChangePlanServiceDeps{
		Transactor: transactor, Plans: plans, Policies: convergencestore.NewMongoChangePlanPolicyRepo(db),
		Events: events, Requester: convergencestore.NewMongoRequester(db), TTL: ttl, Planners: planners,
	})
}
