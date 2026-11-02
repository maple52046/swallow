// Package delivery exposes Change Plans over HTTP (Fiber), implementing the change-plans.md
// contract: list, read, approve, and reject (decision 076). The routes run behind authentication
// and AdminOnly; this package parses requests, names the deciding user from the Principal, and
// maps the convergence errors onto the shared envelope.
package delivery

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/core/convergence"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/middleware"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// ChangePlanHandler serves /api/v1/change-plans.
type ChangePlanHandler struct {
	plans *convergence.ChangePlanService
}

// NewChangePlanHandler wires the handler to the Change Plan service.
func NewChangePlanHandler(plans *convergence.ChangePlanService) *ChangePlanHandler {
	return &ChangePlanHandler{plans: plans}
}

// RegisterRoutes mounts the Change Plan routes under router (the /change-plans group).
func (h *ChangePlanHandler) RegisterRoutes(router fiber.Router) {
	router.Get("/", h.List)
	router.Get("/:id", h.Get)
	router.Post("/:id/approve", h.Approve)
	router.Post("/:id/reject", h.Reject)
}

// ResourceRefResponse names a resource in the contract.
type ResourceRefResponse struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// PlannedActionResponse is one action of a plan in the contract.
type PlannedActionResponse struct {
	Target      ResourceRefResponse `json:"target"`
	Action      string              `json:"action"`
	From        string              `json:"from"`
	To          string              `json:"to"`
	Destructive bool                `json:"destructive"`
	Reason      string              `json:"reason"`
}

// ChangePlanResponse is the contract's Change Plan resource. A resource API that answers a
// destructive spec write with 202 returns this shape for the plan it proposed.
type ChangePlanResponse struct {
	ID          string                  `json:"id"`
	Resource    ResourceRefResponse     `json:"resource"`
	Generation  int64                   `json:"generation"`
	ObservedAt  string                  `json:"observedAt"`
	Actions     []PlannedActionResponse `json:"actions"`
	Effects     []string                `json:"effects"`
	Blockers    []string                `json:"blockers"`
	State       string                  `json:"state"`
	StateReason string                  `json:"stateReason"`
	DecidedBy   string                  `json:"decidedBy"`
	DecidedAt   *string                 `json:"decidedAt"`
	WorkflowID  string                  `json:"workflowId"`
	ExpiresAt   string                  `json:"expiresAt"`
	CreatedAt   string                  `json:"createdAt"`
	UpdatedAt   string                  `json:"updatedAt"`
}

// ToChangePlanResponse maps a plan onto the contract.
func ToChangePlanResponse(plan *convergence.ChangePlan) ChangePlanResponse {
	actions := make([]PlannedActionResponse, len(plan.Actions))
	for i, action := range plan.Actions {
		actions[i] = PlannedActionResponse{
			Target: ResourceRefResponse{Kind: action.Target.Kind, ID: action.Target.ID}, Action: action.Action,
			From: action.From, To: action.To, Destructive: action.Destructive, Reason: action.Reason,
		}
	}
	return ChangePlanResponse{
		ID: plan.ID, Resource: ResourceRefResponse{Kind: plan.Resource.Kind, ID: plan.Resource.ID},
		Generation: plan.Generation, ObservedAt: wire.Time(plan.ObservedAt), Actions: actions,
		Effects: wire.Strings(plan.Effects), Blockers: wire.Strings(plan.Blockers),
		State: string(plan.State), StateReason: plan.StateReason, DecidedBy: plan.DecidedBy,
		DecidedAt: wire.OptionalTime(plan.DecidedAt), WorkflowID: plan.WorkflowID,
		ExpiresAt: wire.Time(plan.ExpiresAt), CreatedAt: wire.Time(plan.CreatedAt), UpdatedAt: wire.Time(plan.UpdatedAt),
	}
}

type changePlanList struct {
	Items []ChangePlanResponse `json:"items"`
}

// List handles GET /change-plans?resourceKind=&resourceId=&state=; state may repeat.
func (h *ChangePlanHandler) List(c *fiber.Ctx) error {
	filter := convergence.ChangePlanFilter{
		Resource: convergence.ResourceRef{Kind: c.Query("resourceKind"), ID: c.Query("resourceId")},
	}
	for _, raw := range c.Context().QueryArgs().PeekMulti("state") {
		state := convergence.ChangePlanState(raw)
		if !knownState(state) {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Unknown Change Plan state: "+string(raw)+"."))
		}
		filter.States = append(filter.States, state)
	}
	plans, err := h.plans.List(c.UserContext(), filter)
	if err != nil {
		return respondError(c, err)
	}
	items := make([]ChangePlanResponse, 0, len(plans))
	for _, plan := range plans {
		items = append(items, ToChangePlanResponse(plan))
	}
	return c.JSON(changePlanList{Items: items})
}

// Get handles GET /change-plans/:id.
func (h *ChangePlanHandler) Get(c *fiber.Ctx) error {
	plan, err := h.plans.Get(c.UserContext(), c.Params("id"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(ToChangePlanResponse(plan))
}

// Approve handles POST /change-plans/:id/approve.
func (h *ChangePlanHandler) Approve(c *fiber.Ctx) error {
	plan, err := h.plans.Approve(c.UserContext(), c.Params("id"), actor(c))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(ToChangePlanResponse(plan))
}

// Reject handles POST /change-plans/:id/reject.
func (h *ChangePlanHandler) Reject(c *fiber.Ctx) error {
	plan, err := h.plans.Reject(c.UserContext(), c.Params("id"), actor(c))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(ToChangePlanResponse(plan))
}

func knownState(state convergence.ChangePlanState) bool {
	switch state {
	case convergence.PlanPendingApproval, convergence.PlanApproved, convergence.PlanApplied,
		convergence.PlanRejected, convergence.PlanSuperseded, convergence.PlanExpired:
		return true
	}
	return false
}

func actor(c *fiber.Ctx) string {
	if principal := middleware.GetPrincipal(c); principal != nil {
		return principal.Username
	}
	return ""
}

// respondError maps the convergence errors: a missing plan is 404, and a plan in the wrong state,
// with blockers, or past its time is a state conflict (409).
func respondError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, convergence.ErrChangePlanNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Change Plan not found."))
	case errors.Is(err, convergence.ErrChangePlanState),
		errors.Is(err, convergence.ErrChangePlanBlocked),
		errors.Is(err, convergence.ErrChangePlanExpired):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()+"."))
	}
	return err
}
