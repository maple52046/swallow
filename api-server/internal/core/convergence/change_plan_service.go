package convergence

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/maple52046/swallow/internal/core/store"
)

// The reasons a pending plan waits, as change-plans.md names them (Approval On Submit).
const (
	reasonSubmittedWithoutApproval = "submitted without approval"
	reasonPolicyRequiresApproval   = "the Change Plan policy requires a separate approval"
	reasonBlocked                  = "it has blockers and cannot be approved"
	reasonActionsChanged           = "the approved plan no longer matched what the change would do"
)

// ChangePlanService proposes, decides, applies, and expires Change Plans (decision 076). A
// resource's spec write proposes a plan inside its own transaction, approved with the write when its
// user approved it and the Change Plan policy allows that; a user approves or rejects a plan that
// waits; the resource's controller applies an approved plan; the expiry controller ends a plan
// nobody decided in time.
type ChangePlanService struct {
	transactor store.Transactor
	plans      ChangePlanRepository
	policies   ChangePlanPolicyStore
	events     EventRecorder
	requester  Requester
	planners   map[string]Planner
	ttl        time.Duration
	now        func() time.Time
}

// ChangePlanServiceDeps are the ports of a ChangePlanService. Transactor, Plans, and Policies are
// required: Policies decides whether a write may approve its own plan, so no process runs without
// it. Events and Requester may be nil (tests): no events are recorded and an approval does not wake
// the controller, which then applies at its next resync. A non-positive TTL is DefaultChangePlanTTL.
type ChangePlanServiceDeps struct {
	Transactor store.Transactor
	Plans      ChangePlanRepository
	Policies   ChangePlanPolicyStore
	Events     EventRecorder
	Requester  Requester
	TTL        time.Duration
	Planners   []Planner
}

// NewChangePlanService wires the service over the resource kinds' planners; a missing required port
// is an error.
func NewChangePlanService(deps ChangePlanServiceDeps) (*ChangePlanService, error) {
	switch {
	case deps.Transactor == nil:
		return nil, errors.New("change plan service: a transactor is required")
	case deps.Plans == nil:
		return nil, errors.New("change plan service: a plan repository is required")
	case deps.Policies == nil:
		return nil, errors.New("change plan service: a policy store is required")
	}
	ttl := deps.TTL
	if ttl <= 0 {
		ttl = DefaultChangePlanTTL
	}
	byKind := make(map[string]Planner, len(deps.Planners))
	for _, planner := range deps.Planners {
		byKind[planner.ResourceKind()] = planner
	}
	return &ChangePlanService{
		transactor: deps.Transactor, plans: deps.Plans, policies: deps.Policies, events: deps.Events,
		requester: deps.Requester, planners: byKind, ttl: ttl, now: func() time.Time { return time.Now().UTC() },
	}, nil
}

// Propose stores the plan of a destructive spec change of resource and supersedes the resource's
// earlier unfinished plan. approvedBy is the user who approved the change with the write that
// submits it; the plan is then stored approved, decided by that user, unless it has blockers or the
// Change Plan policy requires a separate approval. An empty approvedBy leaves the plan waiting. The
// stored plan's StateReason says why a waiting plan waits.
//
// Propose must run inside the transaction that writes the spec, so the spec and its plan commit
// together. It does not wake the resource's controller: the caller asks for convergence after the
// commit, so the controller never reads an uncommitted plan.
func (s *ChangePlanService) Propose(ctx context.Context, resource ResourceRef, draft PlanDraft, approvedBy string) (*ChangePlan, error) {
	now := s.now()
	if earlier, err := s.plans.FindUnfinished(ctx, resource); err == nil {
		if err := s.plans.Transition(ctx, earlier.ID, []ChangePlanState{PlanPendingApproval, PlanApproved}, PlanSuperseded,
			PlanTransition{Reason: fmt.Sprintf("the spec changed to generation %d", draft.Generation), At: now}); err != nil {
			return nil, err
		}
		s.record(ctx, earlier, EventPlanSuperseded, "superseded by a newer spec change")
	} else if !errors.Is(err, ErrChangePlanNotFound) {
		return nil, err
	}
	waits := reasonSubmittedWithoutApproval
	switch {
	case len(draft.Blockers) > 0:
		waits = reasonBlocked
	case approvedBy != "":
		policy, err := s.Policy(ctx)
		if err != nil {
			return nil, err
		}
		waits = ""
		if !policy.ApproveOnSubmit {
			waits = reasonPolicyRequiresApproval
		}
	}
	if waits != "" {
		plan, err := s.storeWaiting(ctx, resource, draft, waits)
		if err != nil {
			return nil, err
		}
		s.record(ctx, plan, EventPlanProposed, "a destructive change waits for approval: "+waits)
		return plan, nil
	}
	plan := newPlan(resource, draft, now, s.ttl)
	plan.State, plan.DecidedBy, plan.DecidedAt = PlanApproved, approvedBy, &now
	plan.StateReason = "approved by " + approvedBy + " with the change"
	if err := s.plans.Create(ctx, plan); err != nil {
		return nil, err
	}
	s.record(ctx, plan, EventPlanProposed, "a destructive change was proposed")
	s.record(ctx, plan, EventPlanApproved, plan.StateReason)
	return plan, nil
}

// storeWaiting stores a pending_approval plan of draft that waits for the given reason, recording no
// event; the caller has already ended the resource's earlier unfinished plan.
func (s *ChangePlanService) storeWaiting(ctx context.Context, resource ResourceRef, draft PlanDraft, waits string) (*ChangePlan, error) {
	plan := newPlan(resource, draft, s.now(), s.ttl)
	plan.StateReason = waits
	if err := s.plans.Create(ctx, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

// newPlan is a pending_approval plan of draft computed at now, valid for ttl.
func newPlan(resource ResourceRef, draft PlanDraft, now time.Time, ttl time.Duration) *ChangePlan {
	return &ChangePlan{
		ID: uuid.NewString(), Resource: resource, Generation: draft.Generation, ObservedAt: draft.ObservedAt,
		Actions: draft.Actions, Effects: draft.Effects, Blockers: draft.Blockers,
		State: PlanPendingApproval, ExpiresAt: now.Add(ttl), CreatedAt: now, UpdatedAt: now,
	}
}

// Policy returns the installation's Change Plan policy; one never stored is
// DefaultChangePlanPolicy. It may run inside a spec write's transaction.
func (s *ChangePlanService) Policy(ctx context.Context) (ChangePlanPolicy, error) {
	policy, err := s.policies.Find(ctx)
	if errors.Is(err, ErrChangePlanPolicyNotFound) {
		return DefaultChangePlanPolicy(), nil
	}
	return policy, err
}

// SetPolicy replaces the installation's Change Plan policy on behalf of actor, an admin. It applies
// to the writes that follow; plans already proposed keep their state.
func (s *ChangePlanService) SetPolicy(ctx context.Context, approveOnSubmit bool, actor string) (ChangePlanPolicy, error) {
	policy := ChangePlanPolicy{ApproveOnSubmit: approveOnSubmit, UpdatedAt: s.now(), UpdatedBy: actor}
	if err := s.policies.Save(ctx, policy); err != nil {
		return ChangePlanPolicy{}, err
	}
	return policy, nil
}

// Withdraw supersedes the resource's unfinished plan, if any, because a newer spec needs no
// approval. Like Propose it runs inside the transaction that writes the spec.
func (s *ChangePlanService) Withdraw(ctx context.Context, resource ResourceRef, reason string) error {
	plan, err := s.plans.FindUnfinished(ctx, resource)
	if errors.Is(err, ErrChangePlanNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.plans.Transition(ctx, plan.ID, []ChangePlanState{PlanPendingApproval, PlanApproved}, PlanSuperseded,
		PlanTransition{Reason: reason, At: s.now()}); err != nil {
		return err
	}
	s.record(ctx, plan, EventPlanSuperseded, reason)
	return nil
}

// Get returns a plan.
func (s *ChangePlanService) Get(ctx context.Context, id string) (*ChangePlan, error) {
	return s.plans.FindByID(ctx, id)
}

// List returns the plans matching filter, newest first.
func (s *ChangePlanService) List(ctx context.Context, filter ChangePlanFilter) ([]*ChangePlan, error) {
	return s.plans.List(ctx, filter)
}

// Approve approves a pending plan as a whole, which freezes it, and asks the resource's controller
// to apply it. A plan with blockers, an expired one, or one in another state is refused.
func (s *ChangePlanService) Approve(ctx context.Context, id, actor string) (*ChangePlan, error) {
	plan, err := s.decidable(ctx, id)
	if err != nil {
		return nil, err
	}
	if plan.State != PlanPendingApproval {
		return nil, fmt.Errorf("%w: it is %s", ErrChangePlanState, plan.State)
	}
	if len(plan.Blockers) > 0 {
		return nil, fmt.Errorf("%w: %v", ErrChangePlanBlocked, plan.Blockers)
	}
	now := s.now()
	if err := s.plans.Transition(ctx, id, []ChangePlanState{PlanPendingApproval}, PlanApproved,
		PlanTransition{Reason: "approved by " + actor, DecidedBy: actor, DecidedAt: &now, At: now}); err != nil {
		return nil, err
	}
	s.record(ctx, plan, EventPlanApproved, "approved by "+actor)
	if planner, ok := s.planners[plan.Resource.Kind]; ok && s.requester != nil {
		if err := s.requester.Request(ctx, planner.Controller()); err != nil {
			// The controller still applies it at its next resync.
			slog.Warn("request convergence after a plan approval", "planId", id, "error", err)
		}
	}
	return s.plans.FindByID(ctx, id)
}

// ApproveResubmitted approves plan, the waiting plan of a resource whose user sent the write that
// proposed it again and approved it this time, as Propose would have approved it the first time. It
// returns the plan as it now is: approved, or unchanged when approvedBy is empty, the plan is not
// pending_approval, has blockers, is past its time (the expiry controller ends it), or the Change Plan
// policy requires a separate approval.
func (s *ChangePlanService) ApproveResubmitted(ctx context.Context, plan *ChangePlan, approvedBy string) (*ChangePlan, error) {
	if approvedBy == "" || plan.State != PlanPendingApproval || len(plan.Blockers) > 0 || !s.now().Before(plan.ExpiresAt) {
		return plan, nil
	}
	policy, err := s.Policy(ctx)
	if err != nil || !policy.ApproveOnSubmit {
		return plan, err
	}
	return s.Approve(ctx, plan.ID, approvedBy)
}

// Reject declines a plan that has not been applied.
func (s *ChangePlanService) Reject(ctx context.Context, id, actor string) (*ChangePlan, error) {
	plan, err := s.decidable(ctx, id)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if err := s.plans.Transition(ctx, id, []ChangePlanState{PlanPendingApproval, PlanApproved}, PlanRejected,
		PlanTransition{DecidedBy: actor, DecidedAt: &now, Reason: "rejected by " + actor, At: now}); err != nil {
		return nil, err
	}
	s.record(ctx, plan, EventPlanRejected, "rejected by "+actor)
	return s.plans.FindByID(ctx, id)
}

// decidable loads a plan a user may decide, expiring it first when it is past its time.
func (s *ChangePlanService) decidable(ctx context.Context, id string) (*ChangePlan, error) {
	plan, err := s.plans.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !plan.State.Unfinished() {
		return nil, fmt.Errorf("%w: it is %s", ErrChangePlanState, plan.State)
	}
	if !s.now().Before(plan.ExpiresAt) {
		if err := s.expire(ctx, plan); err != nil {
			return nil, err
		}
		return nil, ErrChangePlanExpired
	}
	return plan, nil
}

// ApplyApproved is how a resource's controller carries out the resource's approved plan. It
// recomputes the plan: a changed generation or different actions supersede it, and when only the
// actions moved and the recomputed change is still destructive, a new plan waiting for approval is
// proposed in the same transaction, since nobody approved what the change now does. A held resource
// leaves the plan waiting. Otherwise the Workflow is written and the plan marked applied in one
// transaction. It reports the Workflow id, or "" when nothing was applied.
func (s *ChangePlanService) ApplyApproved(ctx context.Context, resource ResourceRef) (string, error) {
	planner, ok := s.planners[resource.Kind]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNoPlanner, resource.Kind)
	}
	plan, err := s.plans.FindUnfinished(ctx, resource)
	if errors.Is(err, ErrChangePlanNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !s.now().Before(plan.ExpiresAt) {
		return "", s.expire(ctx, plan)
	}
	if plan.State != PlanApproved {
		return "", nil
	}
	draft, err := planner.Plan(ctx, resource.ID)
	if err != nil {
		return "", err
	}
	if !plan.SameActions(draft) {
		return "", s.supersedeChanged(ctx, plan, draft)
	}
	held, err := planner.Held(ctx, resource.ID)
	if err != nil || held {
		return "", err
	}
	prepared, err := planner.PrepareApply(ctx, plan)
	if err != nil {
		return "", err
	}
	var workflowID string
	err = s.transactor.InTransaction(ctx, func(ctx context.Context) error {
		id, err := planner.PersistApply(ctx, prepared)
		if err != nil {
			return err
		}
		workflowID = id
		return s.plans.Transition(ctx, plan.ID, []ChangePlanState{PlanApproved}, PlanApplied,
			PlanTransition{Reason: "applied as Workflow " + id, WorkflowID: id, At: s.now()})
	})
	if err != nil {
		return "", err
	}
	s.record(ctx, plan, EventPlanApplied, "applied as Workflow "+workflowID)
	return workflowID, nil
}

// supersedeChanged ends an approved plan whose recomputation draft no longer matches it. When the
// draft is of the same generation and still destructive, the plan it should now be is proposed,
// waiting for approval, in the same transaction; a newer generation's write already proposed its
// own plan, and a draft that is no longer destructive converges without one.
func (s *ChangePlanService) supersedeChanged(ctx context.Context, plan *ChangePlan, draft PlanDraft) error {
	reason := "the resource's spec or observed state changed what it would do"
	repropose := plan.Generation == draft.Generation && draft.Destructive()
	var proposed *ChangePlan
	err := s.transactor.InTransaction(ctx, func(ctx context.Context) error {
		proposed = nil
		if err := s.plans.Transition(ctx, plan.ID, []ChangePlanState{PlanApproved}, PlanSuperseded,
			PlanTransition{Reason: reason, At: s.now()}); err != nil {
			return err
		}
		if !repropose {
			return nil
		}
		var err error
		proposed, err = s.storeWaiting(ctx, plan.Resource, draft, reasonActionsChanged)
		return err
	})
	if err != nil {
		return err
	}
	s.record(ctx, plan, EventPlanSuperseded, reason)
	if proposed != nil {
		s.record(ctx, proposed, EventPlanProposed, "a destructive change waits for approval: "+reasonActionsChanged)
	}
	return nil
}

// ExpireDue ends every unfinished plan past its time.
func (s *ChangePlanService) ExpireDue(ctx context.Context) error {
	plans, err := s.plans.ListExpired(ctx, s.now())
	if err != nil {
		return err
	}
	var errs []error
	for _, plan := range plans {
		errs = append(errs, s.expire(ctx, plan))
	}
	return errors.Join(errs...)
}

func (s *ChangePlanService) expire(ctx context.Context, plan *ChangePlan) error {
	err := s.plans.Transition(ctx, plan.ID, []ChangePlanState{PlanPendingApproval, PlanApproved}, PlanExpired,
		PlanTransition{Reason: "not applied within its validity period", At: s.now()})
	if errors.Is(err, ErrChangePlanState) {
		return nil
	}
	if err == nil {
		s.record(ctx, plan, EventPlanExpired, "not applied within its validity period")
	}
	return err
}

// record writes a plan event to its resource's timeline; a failure is logged, never returned.
func (s *ChangePlanService) record(ctx context.Context, plan *ChangePlan, eventType EventType, message string) {
	if s.events == nil {
		return
	}
	if err := s.events.Record(ctx, Event{
		ResourceKind: plan.Resource.Kind, ResourceID: plan.Resource.ID, Type: eventType,
		Message: "Change Plan " + plan.ID + ": " + message, Generation: plan.Generation, At: s.now(),
	}); err != nil {
		slog.Warn("record change plan event", "planId", plan.ID, "type", eventType, "error", err)
	}
}

// ChangePlanExpiryController ends the plans nobody decided in time. It has one key and acts on
// every resync.
type ChangePlanExpiryController struct {
	plans *ChangePlanService
}

var _ Controller = ChangePlanExpiryController{}

// NewChangePlanExpiryController returns the controller over the service.
func NewChangePlanExpiryController(plans *ChangePlanService) ChangePlanExpiryController {
	return ChangePlanExpiryController{plans: plans}
}

// Name is "change-plan-expiry".
func (ChangePlanExpiryController) Name() string { return "change-plan-expiry" }

// Collections is none: expiry follows the clock, not a change.
func (ChangePlanExpiryController) Collections() []string { return nil }

// KeysForChange is never called, as the controller watches no collection.
func (ChangePlanExpiryController) KeysForChange(context.Context, store.Change) ([]string, error) {
	return nil, nil
}

// Keys is the single key "all".
func (ChangePlanExpiryController) Keys(context.Context) ([]string, error) {
	return []string{"all"}, nil
}

// Reconcile expires the due plans.
func (c ChangePlanExpiryController) Reconcile(ctx context.Context, _ Request) error {
	return c.plans.ExpireDue(ctx)
}

// ResyncInterval checks every five minutes, well inside a plan's validity.
func (ChangePlanExpiryController) ResyncInterval() time.Duration { return 5 * time.Minute }
