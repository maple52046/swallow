package convergence

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/core/store"
	"github.com/maple52046/swallow/internal/core/store/storetest"
)

// memoryPlans is a ChangePlanRepository in memory that enforces one unfinished plan per resource.
type memoryPlans struct {
	plans map[string]*ChangePlan
}

func newMemoryPlans() *memoryPlans { return &memoryPlans{plans: map[string]*ChangePlan{}} }

func (m *memoryPlans) Create(_ context.Context, plan *ChangePlan) error {
	for _, existing := range m.plans {
		if existing.Resource == plan.Resource && existing.State.Unfinished() {
			return ErrChangePlanConflict
		}
	}
	clone := *plan
	m.plans[plan.ID] = &clone
	return nil
}

func (m *memoryPlans) FindByID(_ context.Context, id string) (*ChangePlan, error) {
	if plan, ok := m.plans[id]; ok {
		clone := *plan
		return &clone, nil
	}
	return nil, ErrChangePlanNotFound
}

func (m *memoryPlans) FindUnfinished(_ context.Context, resource ResourceRef) (*ChangePlan, error) {
	for _, plan := range m.plans {
		if plan.Resource == resource && plan.State.Unfinished() {
			clone := *plan
			return &clone, nil
		}
	}
	return nil, ErrChangePlanNotFound
}

func (m *memoryPlans) List(context.Context, ChangePlanFilter) ([]*ChangePlan, error) { return nil, nil }

func (m *memoryPlans) ListExpired(_ context.Context, now time.Time) ([]*ChangePlan, error) {
	var out []*ChangePlan
	for _, plan := range m.plans {
		if plan.State.Unfinished() && !plan.ExpiresAt.After(now) {
			clone := *plan
			out = append(out, &clone)
		}
	}
	return out, nil
}

func (m *memoryPlans) Transition(_ context.Context, id string, from []ChangePlanState, to ChangePlanState, transition PlanTransition) error {
	plan, ok := m.plans[id]
	if !ok {
		return ErrChangePlanNotFound
	}
	if !slices.Contains(from, plan.State) {
		return ErrChangePlanState
	}
	plan.State, plan.StateReason, plan.UpdatedAt = to, transition.Reason, transition.At
	if transition.DecidedBy != "" {
		plan.DecidedBy, plan.DecidedAt = transition.DecidedBy, transition.DecidedAt
	}
	if transition.WorkflowID != "" {
		plan.WorkflowID = transition.WorkflowID
	}
	return nil
}

// fakePlanner plans one action per resource, at the generation the test sets.
type fakePlanner struct {
	generation int64
	action     string
	held       bool
	persisted  []string
}

type preparedFake struct{ id string }

func (p preparedFake) WorkflowID() string { return p.id }

func (f *fakePlanner) ResourceKind() string { return "widget" }
func (f *fakePlanner) Controller() string   { return "widget" }
func (f *fakePlanner) Plan(_ context.Context, id string) (PlanDraft, error) {
	return PlanDraft{Generation: f.generation, Actions: []PlannedAction{
		{Target: ResourceRef{Kind: "server", ID: id + "-member"}, Action: f.action, To: "absent", Destructive: true},
	}}, nil
}
func (f *fakePlanner) Held(context.Context, string) (bool, error) { return f.held, nil }
func (f *fakePlanner) PrepareApply(_ context.Context, plan *ChangePlan) (PreparedApply, error) {
	return preparedFake{id: "wf-" + plan.ID}, nil
}
func (f *fakePlanner) PersistApply(ctx context.Context, prepared PreparedApply) (string, error) {
	if !store.InTransaction(ctx) {
		return "", errors.New("the Workflow must be written in the apply transaction")
	}
	f.persisted = append(f.persisted, prepared.WorkflowID())
	return prepared.WorkflowID(), nil
}

func newTestPlans(planner *fakePlanner) (*ChangePlanService, *memoryPlans, *time.Time) {
	repo := newMemoryPlans()
	service := NewChangePlanService(&storetest.Transactor{}, repo, nil, nil, time.Hour, planner)
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return clock }
	return service, repo, &clock
}

var widget = ResourceRef{Kind: "widget", ID: "w1"}

// A new destructive change supersedes the resource's unfinished plan, keeping one at a time.
func TestProposeSupersedesTheUnfinishedPlan(t *testing.T) {
	planner := &fakePlanner{generation: 2, action: "remove-member"}
	service, repo, _ := newTestPlans(planner)
	ctx := context.Background()
	first, err := service.Propose(ctx, widget, PlanDraft{Generation: 2})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	second, err := service.Propose(ctx, widget, PlanDraft{Generation: 3})
	if err != nil {
		t.Fatalf("second Propose: %v", err)
	}
	if repo.plans[first.ID].State != PlanSuperseded || repo.plans[second.ID].State != PlanPendingApproval {
		t.Errorf("states = %s, %s; want superseded, pending_approval", repo.plans[first.ID].State, repo.plans[second.ID].State)
	}
}

// Approval is whole-plan and refused for a plan with blockers, one already decided, or one past its
// time, which it expires.
func TestApproveRules(t *testing.T) {
	planner := &fakePlanner{generation: 1, action: "uninstall"}
	service, repo, clock := newTestPlans(planner)
	ctx := context.Background()

	blocked, _ := service.Propose(ctx, widget, PlanDraft{Generation: 1, Blockers: []string{"w1 is locked"}})
	if _, err := service.Approve(ctx, blocked.ID, "admin"); !errors.Is(err, ErrChangePlanBlocked) {
		t.Errorf("blocked plan: err = %v, want ErrChangePlanBlocked", err)
	}
	if _, err := service.Reject(ctx, blocked.ID, "admin"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if _, err := service.Approve(ctx, blocked.ID, "admin"); !errors.Is(err, ErrChangePlanState) {
		t.Errorf("rejected plan: err = %v, want ErrChangePlanState", err)
	}

	plan, _ := service.Propose(ctx, widget, PlanDraft{Generation: 1})
	approved, err := service.Approve(ctx, plan.ID, "admin")
	if err != nil || approved.State != PlanApproved || approved.DecidedBy != "admin" {
		t.Fatalf("Approve = %+v, %v", approved, err)
	}

	late, _ := service.Propose(ctx, ResourceRef{Kind: "widget", ID: "w2"}, PlanDraft{Generation: 1})
	*clock = clock.Add(2 * time.Hour)
	if _, err := service.Approve(ctx, late.ID, "admin"); !errors.Is(err, ErrChangePlanExpired) || repo.plans[late.ID].State != PlanExpired {
		t.Errorf("late approval: err = %v, state %s; want ErrChangePlanExpired, expired", err, repo.plans[late.ID].State)
	}
}

// The controller applies an approved plan whose recomputation matches, writing the Workflow in the
// transaction that marks it applied; a hold makes it wait; a changed recomputation supersedes it.
func TestApplyApproved(t *testing.T) {
	planner := &fakePlanner{generation: 1, action: "uninstall"}
	service, repo, _ := newTestPlans(planner)
	ctx := context.Background()
	plan, _ := service.Propose(ctx, widget, PlanDraft{Generation: 1, Actions: []PlannedAction{
		{Target: ResourceRef{Kind: "server", ID: "w1-member"}, Action: "uninstall", From: "present", To: "absent", Destructive: true},
	}})
	if id, err := service.ApplyApproved(ctx, widget); err != nil || id != "" {
		t.Fatalf("pending plan applied: %q, %v", id, err)
	}
	if _, err := service.Approve(ctx, plan.ID, "admin"); err != nil {
		t.Fatal(err)
	}

	planner.held = true
	if id, err := service.ApplyApproved(ctx, widget); err != nil || id != "" || repo.plans[plan.ID].State != PlanApproved {
		t.Fatalf("held: %q, %v, %s; want the plan to wait", id, err, repo.plans[plan.ID].State)
	}
	planner.held = false
	id, err := service.ApplyApproved(ctx, widget)
	if err != nil || id == "" || repo.plans[plan.ID].State != PlanApplied || repo.plans[plan.ID].WorkflowID != id {
		t.Fatalf("apply: %q, %v, %+v", id, err, repo.plans[plan.ID])
	}

	changed, _ := service.Propose(ctx, widget, PlanDraft{Generation: 1, Actions: []PlannedAction{
		{Target: ResourceRef{Kind: "server", ID: "w1-member"}, Action: "uninstall", To: "absent"},
	}})
	if _, err := service.Approve(ctx, changed.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	planner.action = "remove-member"
	if id, err := service.ApplyApproved(ctx, widget); err != nil || id != "" || repo.plans[changed.ID].State != PlanSuperseded {
		t.Errorf("changed actions: %q, %v, %s; want superseded", id, err, repo.plans[changed.ID].State)
	}
	if len(planner.persisted) != 1 {
		t.Errorf("persisted %v, want one Workflow", planner.persisted)
	}
}

// The expiry controller ends unfinished plans past their time and leaves the rest.
func TestExpireDue(t *testing.T) {
	service, repo, clock := newTestPlans(&fakePlanner{generation: 1})
	ctx := context.Background()
	old, _ := service.Propose(ctx, widget, PlanDraft{Generation: 1})
	*clock = clock.Add(30 * time.Minute)
	fresh, _ := service.Propose(ctx, ResourceRef{Kind: "widget", ID: "w2"}, PlanDraft{Generation: 1})
	*clock = clock.Add(45 * time.Minute)
	if err := NewChangePlanExpiryController(service).Reconcile(ctx, Request{Key: "all"}); err != nil {
		t.Fatal(err)
	}
	if repo.plans[old.ID].State != PlanExpired || repo.plans[fresh.ID].State != PlanPendingApproval {
		t.Errorf("states = %s, %s; want expired, pending_approval", repo.plans[old.ID].State, repo.plans[fresh.ID].State)
	}
}
