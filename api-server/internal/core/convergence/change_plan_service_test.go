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

// fakePlanner plans one action per resource, at the generation the test sets; the action is
// destructive unless nonDestructive.
type fakePlanner struct {
	generation     int64
	action         string
	nonDestructive bool
	held           bool
	persisted      []string
}

type preparedFake struct{ id string }

func (p preparedFake) WorkflowID() string { return p.id }

func (f *fakePlanner) ResourceKind() string { return "widget" }
func (f *fakePlanner) Controller() string   { return "widget" }
func (f *fakePlanner) Plan(_ context.Context, id string) (PlanDraft, error) {
	return PlanDraft{Generation: f.generation, Actions: []PlannedAction{
		{Target: ResourceRef{Kind: "server", ID: id + "-member"}, Action: f.action, To: "absent", Destructive: !f.nonDestructive},
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

// memoryPolicies is a ChangePlanPolicyStore in memory; nil policy means none was stored.
type memoryPolicies struct {
	policy  *ChangePlanPolicy
	findErr error
}

func (m *memoryPolicies) Find(context.Context) (ChangePlanPolicy, error) {
	if m.findErr != nil {
		return ChangePlanPolicy{}, m.findErr
	}
	if m.policy == nil {
		return ChangePlanPolicy{}, ErrChangePlanPolicyNotFound
	}
	return *m.policy, nil
}

func (m *memoryPolicies) Save(_ context.Context, policy ChangePlanPolicy) error {
	m.policy = &policy
	return nil
}

// recordedEvents is an EventRecorder that keeps the event types in order.
type recordedEvents struct{ types []EventType }

func (r *recordedEvents) Record(_ context.Context, event Event) error {
	r.types = append(r.types, event.Type)
	return nil
}

func (r *recordedEvents) List(context.Context, string, string, int) ([]Event, error) { return nil, nil }

// testPlans is a ChangePlanService over in-memory ports with a clock the test moves.
type testPlans struct {
	service  *ChangePlanService
	repo     *memoryPlans
	policies *memoryPolicies
	events   *recordedEvents
	clock    *time.Time
}

func newTestPlans(t *testing.T, planner *fakePlanner) testPlans {
	t.Helper()
	repo, policies, events := newMemoryPlans(), &memoryPolicies{}, &recordedEvents{}
	service, err := NewChangePlanService(ChangePlanServiceDeps{
		Transactor: &storetest.Transactor{}, Plans: repo, Policies: policies, Events: events, TTL: time.Hour,
		Planners: []Planner{planner},
	})
	if err != nil {
		t.Fatalf("NewChangePlanService() error = %v", err)
	}
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return clock }
	return testPlans{service: service, repo: repo, policies: policies, events: events, clock: &clock}
}

var widget = ResourceRef{Kind: "widget", ID: "w1"}

// The service refuses to run without the ports approval depends on.
func TestNewChangePlanServiceRequiresItsPorts(t *testing.T) {
	complete := ChangePlanServiceDeps{Transactor: &storetest.Transactor{}, Plans: newMemoryPlans(), Policies: &memoryPolicies{}}
	tests := []struct {
		name string
		drop func(*ChangePlanServiceDeps)
	}{
		{name: "no transactor", drop: func(d *ChangePlanServiceDeps) { d.Transactor = nil }},
		{name: "no plan repository", drop: func(d *ChangePlanServiceDeps) { d.Plans = nil }},
		{name: "no policy store", drop: func(d *ChangePlanServiceDeps) { d.Policies = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			deps := complete
			tc.drop(&deps)
			if _, err := NewChangePlanService(deps); err == nil {
				t.Errorf("NewChangePlanService(%s) error = nil, want an error", tc.name)
			}
		})
	}
	if _, err := NewChangePlanService(complete); err != nil {
		t.Errorf("NewChangePlanService(complete) error = %v", err)
	}
}

// A new destructive change supersedes the resource's unfinished plan, keeping one at a time.
func TestProposeSupersedesTheUnfinishedPlan(t *testing.T) {
	planner := &fakePlanner{generation: 2, action: "remove-member"}
	h := newTestPlans(t, planner)
	ctx := context.Background()
	first, err := h.service.Propose(ctx, widget, PlanDraft{Generation: 2}, "admin")
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	second, err := h.service.Propose(ctx, widget, PlanDraft{Generation: 3}, "")
	if err != nil {
		t.Fatalf("second Propose: %v", err)
	}
	if h.repo.plans[first.ID].State != PlanSuperseded || h.repo.plans[second.ID].State != PlanPendingApproval {
		t.Errorf("states = %s, %s; want superseded, pending_approval", h.repo.plans[first.ID].State, h.repo.plans[second.ID].State)
	}
}

// A write its user approved stores its plan approved, decided by that user, unless the plan has
// blockers or the policy requires a separate approval; every waiting plan says why it waits.
func TestProposeApprovesOnSubmit(t *testing.T) {
	tests := []struct {
		name       string
		approvedBy string
		blockers   []string
		policy     *ChangePlanPolicy
		wantState  ChangePlanState
		wantReason string
		wantEvents []EventType
	}{
		{
			name: "approved with the write", approvedBy: "admin", wantState: PlanApproved,
			wantReason: "approved by admin with the change", wantEvents: []EventType{EventPlanProposed, EventPlanApproved},
		},
		{
			name: "approved with the write under a stored approving policy", approvedBy: "admin",
			policy: &ChangePlanPolicy{ApproveOnSubmit: true}, wantState: PlanApproved,
			wantReason: "approved by admin with the change", wantEvents: []EventType{EventPlanProposed, EventPlanApproved},
		},
		{
			name: "submitted without approval", wantState: PlanPendingApproval,
			wantReason: reasonSubmittedWithoutApproval, wantEvents: []EventType{EventPlanProposed},
		},
		{
			name: "the safety switch is on", approvedBy: "admin", policy: &ChangePlanPolicy{ApproveOnSubmit: false},
			wantState: PlanPendingApproval, wantReason: reasonPolicyRequiresApproval, wantEvents: []EventType{EventPlanProposed},
		},
		{
			name: "blocked", approvedBy: "admin", blockers: []string{"swallow cannot carry out remove-member yet"},
			wantState: PlanPendingApproval, wantReason: reasonBlocked, wantEvents: []EventType{EventPlanProposed},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestPlans(t, &fakePlanner{generation: 1, action: "uninstall"})
			h.policies.policy = tc.policy
			plan, err := h.service.Propose(context.Background(), widget, PlanDraft{Generation: 1, Blockers: tc.blockers}, tc.approvedBy)
			if err != nil {
				t.Fatalf("Propose(approvedBy %q) error = %v", tc.approvedBy, err)
			}
			stored := h.repo.plans[plan.ID]
			if stored.State != tc.wantState || stored.StateReason != tc.wantReason {
				t.Errorf("Propose(approvedBy %q) stored %s %q, want %s %q", tc.approvedBy, stored.State, stored.StateReason, tc.wantState, tc.wantReason)
			}
			if wantDecided := tc.wantState == PlanApproved; wantDecided != (stored.DecidedBy == tc.approvedBy && stored.DecidedAt != nil) {
				t.Errorf("Propose(approvedBy %q) decided by %q at %v", tc.approvedBy, stored.DecidedBy, stored.DecidedAt)
			}
			if !slices.Equal(h.events.types, tc.wantEvents) {
				t.Errorf("Propose(approvedBy %q) events = %v, want %v", tc.approvedBy, h.events.types, tc.wantEvents)
			}
		})
	}
}

// A policy the store cannot read fails the write instead of approving it.
func TestProposeFailsWhenThePolicyCannotBeRead(t *testing.T) {
	h := newTestPlans(t, &fakePlanner{generation: 1})
	h.policies.findErr = errors.New("store unavailable")
	if _, err := h.service.Propose(context.Background(), widget, PlanDraft{Generation: 1}, "admin"); !errors.Is(err, h.policies.findErr) {
		t.Errorf("Propose() error = %v, want the policy read error", err)
	}
	if len(h.repo.plans) != 0 {
		t.Errorf("plans = %d, want none stored", len(h.repo.plans))
	}
}

// The policy reads as approve on submit until an admin replaces it; the replacement records who and
// when.
func TestChangePlanPolicy(t *testing.T) {
	h := newTestPlans(t, &fakePlanner{generation: 1})
	ctx := context.Background()
	if got, err := h.service.Policy(ctx); err != nil || got != DefaultChangePlanPolicy() {
		t.Fatalf("Policy() = %+v, %v; want the default", got, err)
	}
	set, err := h.service.SetPolicy(ctx, false, "admin")
	if err != nil {
		t.Fatalf("SetPolicy() error = %v", err)
	}
	want := ChangePlanPolicy{ApproveOnSubmit: false, UpdatedAt: *h.clock, UpdatedBy: "admin"}
	if set != want {
		t.Errorf("SetPolicy() = %+v, want %+v", set, want)
	}
	if got, _ := h.service.Policy(ctx); got != want {
		t.Errorf("Policy() after SetPolicy = %+v, want %+v", got, want)
	}
}

// Repeating a write with approval approves the plan it left waiting, as Propose would have; a plan
// the write could not have approved stays as it is.
func TestApproveResubmitted(t *testing.T) {
	tests := []struct {
		name       string
		approvedBy string
		blockers   []string
		policy     *ChangePlanPolicy
		late       bool
		wantState  ChangePlanState
	}{
		{name: "approved", approvedBy: "admin", wantState: PlanApproved},
		{name: "without approval", wantState: PlanPendingApproval},
		{name: "the safety switch is on", approvedBy: "admin", policy: &ChangePlanPolicy{}, wantState: PlanPendingApproval},
		{name: "blocked", approvedBy: "admin", blockers: []string{"w1 is locked"}, wantState: PlanPendingApproval},
		{name: "past its time", approvedBy: "admin", late: true, wantState: PlanPendingApproval},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestPlans(t, &fakePlanner{generation: 1})
			ctx := context.Background()
			plan, err := h.service.Propose(ctx, widget, PlanDraft{Generation: 1, Blockers: tc.blockers}, "")
			if err != nil {
				t.Fatalf("Propose() error = %v", err)
			}
			h.policies.policy = tc.policy
			if tc.late {
				*h.clock = h.clock.Add(2 * time.Hour)
			}
			got, err := h.service.ApproveResubmitted(ctx, plan, tc.approvedBy)
			if err != nil {
				t.Fatalf("ApproveResubmitted(%q) error = %v", tc.approvedBy, err)
			}
			if got.State != tc.wantState || h.repo.plans[plan.ID].State != tc.wantState {
				t.Errorf("ApproveResubmitted(%q) = %s, stored %s; want %s", tc.approvedBy, got.State, h.repo.plans[plan.ID].State, tc.wantState)
			}
		})
	}
}

// Approval is whole-plan and refused for a plan with blockers, one already decided, or one past its
// time, which it expires.
func TestApproveRules(t *testing.T) {
	planner := &fakePlanner{generation: 1, action: "uninstall"}
	h := newTestPlans(t, planner)
	service, repo, clock := h.service, h.repo, h.clock
	ctx := context.Background()

	blocked, _ := service.Propose(ctx, widget, PlanDraft{Generation: 1, Blockers: []string{"w1 is locked"}}, "")
	if _, err := service.Approve(ctx, blocked.ID, "admin"); !errors.Is(err, ErrChangePlanBlocked) {
		t.Errorf("blocked plan: err = %v, want ErrChangePlanBlocked", err)
	}
	if _, err := service.Reject(ctx, blocked.ID, "admin"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if _, err := service.Approve(ctx, blocked.ID, "admin"); !errors.Is(err, ErrChangePlanState) {
		t.Errorf("rejected plan: err = %v, want ErrChangePlanState", err)
	}

	plan, _ := service.Propose(ctx, widget, PlanDraft{Generation: 1}, "")
	approved, err := service.Approve(ctx, plan.ID, "admin")
	if err != nil || approved.State != PlanApproved || approved.DecidedBy != "admin" || approved.StateReason != "approved by admin" {
		t.Fatalf("Approve = %+v, %v", approved, err)
	}

	late, _ := service.Propose(ctx, ResourceRef{Kind: "widget", ID: "w2"}, PlanDraft{Generation: 1}, "")
	*clock = clock.Add(2 * time.Hour)
	if _, err := service.Approve(ctx, late.ID, "admin"); !errors.Is(err, ErrChangePlanExpired) || repo.plans[late.ID].State != PlanExpired {
		t.Errorf("late approval: err = %v, state %s; want ErrChangePlanExpired, expired", err, repo.plans[late.ID].State)
	}
}

// The controller applies an approved plan whose recomputation matches, writing the Workflow in the
// transaction that marks it applied; a hold makes it wait. A plan approved with its write is
// applied the same way.
func TestApplyApproved(t *testing.T) {
	planner := &fakePlanner{generation: 1, action: "uninstall"}
	h := newTestPlans(t, planner)
	service, repo := h.service, h.repo
	ctx := context.Background()
	plan, _ := service.Propose(ctx, widget, PlanDraft{Generation: 1, Actions: []PlannedAction{
		{Target: ResourceRef{Kind: "server", ID: "w1-member"}, Action: "uninstall", From: "present", To: "absent", Destructive: true},
	}}, "")
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
	if got := repo.plans[plan.ID].StateReason; got != "applied as Workflow "+id {
		t.Errorf("applied plan reason = %q, want it to name the Workflow", got)
	}

	onSubmit, _ := service.Propose(ctx, ResourceRef{Kind: "widget", ID: "w2"}, PlanDraft{Generation: 1, Actions: []PlannedAction{
		{Target: ResourceRef{Kind: "server", ID: "w2-member"}, Action: "uninstall", To: "absent", Destructive: true},
	}}, "admin")
	if id, err := service.ApplyApproved(ctx, ResourceRef{Kind: "widget", ID: "w2"}); err != nil || id == "" || repo.plans[onSubmit.ID].State != PlanApplied {
		t.Errorf("approved on submit: %q, %v, %s; want it applied", id, err, repo.plans[onSubmit.ID].State)
	}
}

// An approved plan whose recomputation does something else is superseded. When the change is still
// destructive at the same generation, the plan it now is waits for approval, since nobody approved
// it; one that is no longer destructive converges without a plan.
func TestApplyApprovedReproposesAChangedPlan(t *testing.T) {
	approvedUninstall := PlanDraft{Generation: 1, Actions: []PlannedAction{
		{Target: ResourceRef{Kind: "server", ID: "w1-member"}, Action: "uninstall", To: "absent", Destructive: true},
	}}
	t.Run("still destructive", func(t *testing.T) {
		planner := &fakePlanner{generation: 1, action: "remove-member"}
		h := newTestPlans(t, planner)
		ctx := context.Background()
		approved, _ := h.service.Propose(ctx, widget, approvedUninstall, "admin")
		if id, err := h.service.ApplyApproved(ctx, widget); err != nil || id != "" {
			t.Fatalf("ApplyApproved() = %q, %v; want nothing applied", id, err)
		}
		if h.repo.plans[approved.ID].State != PlanSuperseded {
			t.Errorf("approved plan state = %s, want superseded", h.repo.plans[approved.ID].State)
		}
		waiting, err := h.repo.FindUnfinished(ctx, widget)
		if err != nil {
			t.Fatalf("FindUnfinished() error = %v, want the re-proposed plan", err)
		}
		if waiting.State != PlanPendingApproval || waiting.StateReason != reasonActionsChanged || waiting.DecidedBy != "" ||
			waiting.Actions[0].Action != "remove-member" {
			t.Errorf("re-proposed plan = %+v, want a pending remove-member plan nobody decided", waiting)
		}
		if id, err := h.service.ApplyApproved(ctx, widget); err != nil || id != "" || len(planner.persisted) != 0 {
			t.Errorf("ApplyApproved() of the waiting plan = %q, %v, persisted %v; want nothing", id, err, planner.persisted)
		}
		wantEvents := []EventType{EventPlanProposed, EventPlanApproved, EventPlanSuperseded, EventPlanProposed}
		if !slices.Equal(h.events.types, wantEvents) {
			t.Errorf("events = %v, want %v", h.events.types, wantEvents)
		}
	})
	t.Run("no longer destructive", func(t *testing.T) {
		planner := &fakePlanner{generation: 1, action: "remove-member", nonDestructive: true}
		h := newTestPlans(t, planner)
		ctx := context.Background()
		approved, _ := h.service.Propose(ctx, widget, approvedUninstall, "admin")
		if _, err := h.service.ApplyApproved(ctx, widget); err != nil {
			t.Fatalf("ApplyApproved() error = %v", err)
		}
		if h.repo.plans[approved.ID].State != PlanSuperseded {
			t.Errorf("approved plan state = %s, want superseded", h.repo.plans[approved.ID].State)
		}
		if _, err := h.repo.FindUnfinished(ctx, widget); !errors.Is(err, ErrChangePlanNotFound) {
			t.Errorf("FindUnfinished() error = %v, want no plan for a non-destructive change", err)
		}
	})
	t.Run("a newer generation", func(t *testing.T) {
		planner := &fakePlanner{generation: 2, action: "remove-member"}
		h := newTestPlans(t, planner)
		ctx := context.Background()
		approved, _ := h.service.Propose(ctx, widget, approvedUninstall, "admin")
		if _, err := h.service.ApplyApproved(ctx, widget); err != nil {
			t.Fatalf("ApplyApproved() error = %v", err)
		}
		if h.repo.plans[approved.ID].State != PlanSuperseded {
			t.Errorf("approved plan state = %s, want superseded", h.repo.plans[approved.ID].State)
		}
		if _, err := h.repo.FindUnfinished(ctx, widget); !errors.Is(err, ErrChangePlanNotFound) {
			t.Errorf("FindUnfinished() error = %v, want the newer write to propose its own plan", err)
		}
	})
}

// The expiry controller ends unfinished plans past their time and leaves the rest.
func TestExpireDue(t *testing.T) {
	h := newTestPlans(t, &fakePlanner{generation: 1})
	service, repo, clock := h.service, h.repo, h.clock
	ctx := context.Background()
	old, _ := service.Propose(ctx, widget, PlanDraft{Generation: 1}, "")
	*clock = clock.Add(30 * time.Minute)
	fresh, _ := service.Propose(ctx, ResourceRef{Kind: "widget", ID: "w2"}, PlanDraft{Generation: 1}, "")
	*clock = clock.Add(45 * time.Minute)
	if err := NewChangePlanExpiryController(service).Reconcile(ctx, Request{Key: "all"}); err != nil {
		t.Fatal(err)
	}
	if repo.plans[old.ID].State != PlanExpired || repo.plans[fresh.ID].State != PlanPendingApproval {
		t.Errorf("states = %s, %s; want expired, pending_approval", repo.plans[old.ID].State, repo.plans[fresh.ID].State)
	}
}
