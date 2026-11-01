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

// ChangePlanService proposes, decides, applies, and expires Change Plans (decision 076). A
// resource's spec write proposes a plan inside its own transaction; an operator approves or
// rejects it; the resource's controller applies an approved plan; the expiry controller ends a plan
// nobody decided in time.
type ChangePlanService struct {
	transactor store.Transactor
	plans      ChangePlanRepository
	events     EventRecorder
	requester  Requester
	planners   map[string]Planner
	ttl        time.Duration
	now        func() time.Time
}

// NewChangePlanService wires the service over the resource kinds' planners. A non-positive ttl is
// DefaultChangePlanTTL. events and requester may be nil (tests): no events are recorded and an
// approval does not wake the controller, which then applies at its next resync.
func NewChangePlanService(
	transactor store.Transactor,
	plans ChangePlanRepository,
	events EventRecorder,
	requester Requester,
	ttl time.Duration,
	planners ...Planner,
) *ChangePlanService {
	if ttl <= 0 {
		ttl = DefaultChangePlanTTL
	}
	byKind := make(map[string]Planner, len(planners))
	for _, planner := range planners {
		byKind[planner.ResourceKind()] = planner
	}
	return &ChangePlanService{
		transactor: transactor, plans: plans, events: events, requester: requester,
		planners: byKind, ttl: ttl, now: func() time.Time { return time.Now().UTC() },
	}
}

// Propose stores a pending_approval plan for a destructive spec change of resource and supersedes
// the resource's earlier unfinished plan. It must run inside the transaction that writes the spec,
// so the spec and its plan commit together.
func (s *ChangePlanService) Propose(ctx context.Context, resource ResourceRef, draft PlanDraft) (*ChangePlan, error) {
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
	plan := &ChangePlan{
		ID: uuid.NewString(), Resource: resource, Generation: draft.Generation, ObservedAt: draft.ObservedAt,
		Actions: draft.Actions, Effects: draft.Effects, Blockers: draft.Blockers,
		State: PlanPendingApproval, ExpiresAt: now.Add(s.ttl), CreatedAt: now, UpdatedAt: now,
	}
	if err := s.plans.Create(ctx, plan); err != nil {
		return nil, err
	}
	s.record(ctx, plan, EventPlanProposed, "a destructive change waits for approval")
	return plan, nil
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
		PlanTransition{DecidedBy: actor, DecidedAt: &now, At: now}); err != nil {
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

// decidable loads a plan an operator may decide, expiring it first when it is past its time.
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
// recomputes the plan: a changed generation or different actions supersede it; a held resource
// leaves it waiting. Otherwise the Workflow is written and the plan marked applied in one
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
		reason := "the resource's spec or observed state changed what it would do"
		if err := s.plans.Transition(ctx, plan.ID, []ChangePlanState{PlanApproved}, PlanSuperseded,
			PlanTransition{Reason: reason, At: s.now()}); err != nil {
			return "", err
		}
		s.record(ctx, plan, EventPlanSuperseded, reason)
		return "", nil
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
			PlanTransition{WorkflowID: id, At: s.now()})
	})
	if err != nil {
		return "", err
	}
	s.record(ctx, plan, EventPlanApplied, "applied as Workflow "+workflowID)
	return workflowID, nil
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
