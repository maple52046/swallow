package convergence

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
)

// A resource has at most one open plan; a transition is a compare-and-set on the state and closes
// the plan, after which a new one may open; expired and listed plans come back whole.
func TestChangePlanRepoKeepsOneOpenPlanPerResource(t *testing.T) {
	ctx := context.Background()
	repo, err := NewMongoChangePlanRepo(ctx, testDatabase(t))
	if err != nil {
		t.Fatalf("NewMongoChangePlanRepo: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	resource := convergence.ResourceRef{Kind: "platform", ID: "p1"}
	plan := func(id string) *convergence.ChangePlan {
		return &convergence.ChangePlan{
			ID: id, Resource: resource, Generation: 2, ObservedAt: now, State: convergence.PlanPendingApproval,
			Actions: []convergence.PlannedAction{{Target: convergence.ResourceRef{Kind: "server", ID: "s1"}, Action: "remove-member", To: "absent", Destructive: true}},
			Effects: []string{"s1 leaves the Platform"}, ExpiresAt: now.Add(-time.Minute), CreatedAt: now, UpdatedAt: now,
		}
	}
	if err := repo.Create(ctx, plan("a")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Create(ctx, plan("b")); !errors.Is(err, convergence.ErrChangePlanConflict) {
		t.Fatalf("second open plan: err = %v, want ErrChangePlanConflict", err)
	}
	open, err := repo.FindUnfinished(ctx, resource)
	if err != nil || open.ID != "a" || len(open.Actions) != 1 || open.Actions[0].Target.ID != "s1" || open.Effects[0] != "s1 leaves the Platform" {
		t.Fatalf("FindUnfinished = %+v, %v", open, err)
	}
	expired, err := repo.ListExpired(ctx, now)
	if err != nil || len(expired) != 1 {
		t.Fatalf("ListExpired = %d plans, %v", len(expired), err)
	}

	decided := now.Add(time.Second)
	if err := repo.Transition(ctx, "a", []convergence.ChangePlanState{convergence.PlanPendingApproval}, convergence.PlanApproved,
		convergence.PlanTransition{DecidedBy: "admin", DecidedAt: &decided, At: decided}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := repo.Transition(ctx, "a", []convergence.ChangePlanState{convergence.PlanPendingApproval}, convergence.PlanRejected,
		convergence.PlanTransition{At: decided}); !errors.Is(err, convergence.ErrChangePlanState) {
		t.Fatalf("reject an approved plan as pending: err = %v, want ErrChangePlanState", err)
	}
	if err := repo.Transition(ctx, "a", []convergence.ChangePlanState{convergence.PlanApproved}, convergence.PlanApplied,
		convergence.PlanTransition{WorkflowID: "wf-1", At: decided}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := repo.Create(ctx, plan("b")); err != nil {
		t.Fatalf("a new plan after the first closed: %v", err)
	}
	listed, err := repo.List(ctx, convergence.ChangePlanFilter{Resource: resource, States: []convergence.ChangePlanState{convergence.PlanApplied}})
	if err != nil || len(listed) != 1 || listed[0].WorkflowID != "wf-1" || listed[0].DecidedBy != "admin" || listed[0].DecidedAt == nil {
		t.Fatalf("List applied = %+v, %v", listed, err)
	}
	if _, err := repo.FindByID(ctx, "missing"); !errors.Is(err, convergence.ErrChangePlanNotFound) {
		t.Errorf("FindByID(missing) = %v, want ErrChangePlanNotFound", err)
	}
}
