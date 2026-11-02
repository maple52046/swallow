package convergence

import (
	"context"
	"errors"
	"slices"
	"time"
)

// ChangePlanState is where a Change Plan is in its life (glossary: Change Plan).
type ChangePlanState string

// The Change Plan states. pending_approval and approved are unfinished; the others are final.
const (
	PlanPendingApproval ChangePlanState = "pending_approval"
	PlanApproved        ChangePlanState = "approved"
	PlanApplied         ChangePlanState = "applied"
	PlanRejected        ChangePlanState = "rejected"
	PlanSuperseded      ChangePlanState = "superseded"
	PlanExpired         ChangePlanState = "expired"
)

// Unfinished reports whether the plan can still be approved or applied; a resource has at most one
// unfinished plan.
func (s ChangePlanState) Unfinished() bool {
	return s == PlanPendingApproval || s == PlanApproved
}

// DefaultChangePlanTTL is how long a plan stays valid after it is computed (decision 076).
const DefaultChangePlanTTL = 24 * time.Hour

// ResourceRef names the resource a plan or action is about.
type ResourceRef struct {
	Kind string
	ID   string
}

// PlannedAction is one thing realizing the spec will do to one target.
type PlannedAction struct {
	Target ResourceRef
	// Action names the change, for example "uninstall" or "remove-member".
	Action string
	// From and To are the current and desired values, for people.
	From string
	To   string
	// Destructive says whether the action is destructive (desired-state-model §5.1).
	Destructive bool
	Reason      string
}

// sameAs reports whether two actions do the same thing: the target, the action, and the desired
// value. The current value is an observation and does not count.
func (a PlannedAction) sameAs(other PlannedAction) bool {
	return a.Target == other.Target && a.Action == other.Action && a.To == other.To
}

// PlanDraft is what a resource's Planner computes from its spec and observed state.
type PlanDraft struct {
	// Generation is the spec generation the draft realizes.
	Generation int64
	// ObservedAt is when the observed state it relied on was read.
	ObservedAt time.Time
	Actions    []PlannedAction
	// Effects are the expected consequences, for people (for example "lab-1 leaves service").
	Effects []string
	// Blockers are failed preflight checks; a plan with blockers cannot be approved.
	Blockers []string
}

// ChangePlan is a preview of what realizing a destructive spec change will do, which a user
// approves as a whole before the resource's controller carries it out as a Workflow (decision 076).
// Once approved its content is immutable.
type ChangePlan struct {
	ID         string
	Resource   ResourceRef
	Generation int64
	ObservedAt time.Time
	Actions    []PlannedAction
	Effects    []string
	Blockers   []string
	State      ChangePlanState
	// StateReason says, for people, why the plan reached its state.
	StateReason string
	// DecidedBy and DecidedAt record the user who approved or rejected it.
	DecidedBy  string
	DecidedAt  *time.Time
	WorkflowID string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SameActions reports whether draft would do what the plan does: the same generation and the same
// actions, in order. Observation times alone do not count.
func (p *ChangePlan) SameActions(draft PlanDraft) bool {
	return p.Generation == draft.Generation &&
		slices.EqualFunc(p.Actions, draft.Actions, func(a, b PlannedAction) bool { return a.sameAs(b) })
}

// ChangePlanFilter narrows a listing; empty fields match everything.
type ChangePlanFilter struct {
	Resource ResourceRef
	States   []ChangePlanState
}

// PlanTransition is what moving a plan to a new state records besides the state.
type PlanTransition struct {
	Reason     string
	DecidedBy  string
	DecidedAt  *time.Time
	WorkflowID string
	At         time.Time
}

// ChangePlanRepository stores Change Plans and keeps at most one unfinished plan per resource.
type ChangePlanRepository interface {
	// Create stores a new plan; a second unfinished plan for the same resource is
	// ErrChangePlanConflict.
	Create(ctx context.Context, plan *ChangePlan) error
	// FindByID returns the plan or ErrChangePlanNotFound.
	FindByID(ctx context.Context, id string) (*ChangePlan, error)
	// FindUnfinished returns the resource's unfinished plan, or ErrChangePlanNotFound.
	FindUnfinished(ctx context.Context, resource ResourceRef) (*ChangePlan, error)
	// List returns the plans matching filter, newest first.
	List(ctx context.Context, filter ChangePlanFilter) ([]*ChangePlan, error)
	// ListExpired returns the unfinished plans whose ExpiresAt is not after now.
	ListExpired(ctx context.Context, now time.Time) ([]*ChangePlan, error)
	// Transition moves the plan to state when it is in one of from, recording transition; a plan in
	// another state is ErrChangePlanState.
	Transition(ctx context.Context, id string, from []ChangePlanState, to ChangePlanState, transition PlanTransition) error
}

// PreparedApply is the Workflow an approved plan becomes, planned but not written; only the
// Planner that prepared it can persist it.
type PreparedApply interface {
	WorkflowID() string
}

// Planner is the port a resource kind that has destructive changes implements (decision 076).
type Planner interface {
	// ResourceKind is the kind of resource it plans.
	ResourceKind() string
	// Controller is the name of the resource kind's controller, which applies approved plans.
	Controller() string
	// Plan computes the resource's draft from its current spec and observed state. It reads outside
	// any transaction and writes nothing.
	Plan(ctx context.Context, id string) (PlanDraft, error)
	// Held reports whether the resource is held, during which an approved plan waits.
	Held(ctx context.Context, id string) (bool, error)
	// PrepareApply plans the Workflow that carries out an approved plan, writing nothing.
	PrepareApply(ctx context.Context, plan *ChangePlan) (PreparedApply, error)
	// PersistApply writes a prepared Workflow; it runs inside the transaction that marks the plan
	// applied, and the Workflow records the plan id.
	PersistApply(ctx context.Context, prepared PreparedApply) (workflowID string, err error)
}

var (
	// ErrChangePlanNotFound means no plan has the id, or the resource has no unfinished plan.
	ErrChangePlanNotFound = errors.New("change plan not found")
	// ErrChangePlanState means the plan is not in a state that allows the request.
	ErrChangePlanState = errors.New("change plan is not in a state that allows this")
	// ErrChangePlanConflict means the resource already has an unfinished plan.
	ErrChangePlanConflict = errors.New("the resource already has an unfinished change plan")
	// ErrChangePlanBlocked means the plan has blockers and cannot be approved.
	ErrChangePlanBlocked = errors.New("change plan has blockers")
	// ErrChangePlanExpired means the plan outlived its validity and must be recomputed.
	ErrChangePlanExpired = errors.New("change plan has expired")
	// ErrNoPlanner means no Planner is registered for the resource kind.
	ErrNoPlanner = errors.New("no planner for the resource kind")
)
