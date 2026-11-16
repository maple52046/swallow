// Package delivery exposes the Validation HTTP surface (contract validation.md): the suite catalog
// and user suites, Validation Runs, and Validation Schedules. It maps requests to the application
// inputs and the domain to the contract's wire shape, and validation and workflow errors to the
// shared API error envelope.
package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	accessdelivery "github.com/maple52046/swallow/internal/core/access/delivery"

	"github.com/maple52046/swallow/internal/core/convergence"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	validationapp "github.com/maple52046/swallow/internal/core/validation/application"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/pagination"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// Handler serves /api/v1/validation.
type Handler struct {
	service *validationapp.Service
}

// NewHandler builds the handler over the validation service.
func NewHandler(service *validationapp.Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes mounts the validation routes on routes without authorization, for handler tests
// that install the caller's Visibility first. Production registers each route with its own guard in
// internal/app/api.go (contract validation.md, Authentication and Authorization).
func (h *Handler) RegisterRoutes(routes fiber.Router) {
	routes.Get("/suites", h.ListSuites)
	routes.Post("/suites", h.CreateSuite)
	routes.Get("/suites/:suiteId", h.GetSuite)
	routes.Put("/suites/:suiteId", h.ReplaceSuite)
	routes.Delete("/suites/:suiteId", h.DeleteSuite)
	routes.Get("/suites/:suiteId/revisions/:revision", h.GetSuiteRevision)
	routes.Get("/runs", h.ListRuns)
	routes.Post("/runs", h.StartRun)
	routes.Get("/runs/:runId", h.GetRun)
	routes.Post("/runs/:runId/cancel", h.CancelRun)
	routes.Get("/schedules", h.ListSchedules)
	routes.Post("/schedules", h.CreateSchedule)
	routes.Get("/schedules/:scheduleId", h.GetSchedule)
	routes.Put("/schedules/:scheduleId", h.ReplaceSchedule)
	routes.Delete("/schedules/:scheduleId", h.DeleteSchedule)
}

// ListSuites returns the catalog.
func (h *Handler) ListSuites(c *fiber.Ctx) error {
	suites, err := h.service.Catalog().List(c.Context(), c.Query("includeRetired") == "true")
	if err != nil {
		return respondError(c, err)
	}
	items := make([]suiteResponse, 0, len(suites))
	for _, suite := range suites {
		items = append(items, toSuiteResponse(suite))
	}
	return c.JSON(fiber.Map{"items": items})
}

// GetSuite returns one suite.
func (h *Handler) GetSuite(c *fiber.Ctx) error {
	suite, err := h.service.Catalog().Get(c.Context(), c.Params("suiteId"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(toSuiteResponse(suite))
}

// userSuiteRequest is the body of a user suite create or replace (ValidationUserSuiteRequest).
// Revision is the replace's optional precondition and is ignored on create.
type userSuiteRequest struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	Script         string `json:"script"`
	Sudo           bool   `json:"sudo"`
	StarterLevel   string `json:"starterLevel"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	Impact         string `json:"impact"`
	Revision       int    `json:"revision"`
}

func (r userSuiteRequest) input() validationdomain.UserSuiteInput {
	return validationdomain.UserSuiteInput{
		Name: r.Name, Description: r.Description, Script: r.Script, Sudo: r.Sudo,
		StarterLevel: validationdomain.StarterLevel(r.StarterLevel), TimeoutSeconds: r.TimeoutSeconds,
		Impact: validationdomain.Impact(r.Impact),
	}
}

// CreateSuite creates a user suite whose author is the caller (any authenticated User).
func (h *Handler) CreateSuite(c *fiber.Ctx) error {
	var request userSuiteRequest
	if err := c.BodyParser(&request); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	actor, ok := actorOf(c)
	if !ok {
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}
	suite, err := h.service.CreateSuite(c.UserContext(), request.input(), validationdomain.Author{UserID: actor.UserID, Username: actor.Username})
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toSuiteResponse(suite))
}

// ReplaceSuite replaces a user suite; the use case admits only its author (403 otherwise).
func (h *Handler) ReplaceSuite(c *fiber.Ctx) error {
	var request userSuiteRequest
	if err := c.BodyParser(&request); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if request.Revision < 0 {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "revision must be positive."))
	}
	actor, ok := actorOf(c)
	if !ok {
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}
	suite, err := h.service.ReplaceSuite(c.UserContext(), c.Params("suiteId"),
		validationapp.SuiteReplace{Input: request.input(), ExpectedRevision: request.Revision}, actor)
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(toSuiteResponse(suite))
}

// DeleteSuite retires a user suite; the use case admits its author or an admin (403 otherwise).
func (h *Handler) DeleteSuite(c *fiber.Ctx) error {
	actor, ok := actorOf(c)
	if !ok {
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}
	if err := h.service.RetireSuite(c.UserContext(), c.Params("suiteId"), actor); err != nil {
		return respondError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// GetSuiteRevision returns one kept revision of a user suite (Reference: any User). A revision that
// is not a positive number names no revision, so it is 404 like one that was never kept.
func (h *Handler) GetSuiteRevision(c *fiber.Ctx) error {
	number, err := strconv.Atoi(c.Params("revision"))
	if err != nil || number < 1 {
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "No such revision."))
	}
	revision, err := h.service.SuiteRevision(c.UserContext(), c.Params("suiteId"), number)
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(suiteRevisionResponse{
		SuiteID: revision.SuiteID, Revision: revision.Revision, Script: revision.Script, ScriptSHA256: revision.ScriptSHA256,
		Sudo: revision.Sudo, StarterLevel: string(revision.StarterLevel), TimeoutSeconds: revision.TimeoutSeconds,
		Impact: string(revision.Impact), CreatedAt: wire.Time(revision.CreatedAt),
	})
}

type runRequest struct {
	SuiteID       string         `json:"suiteId"`
	ServerIDs     []string       `json:"serverIds"`
	PlatformID    string         `json:"platformId"`
	Parameters    map[string]any `json:"parameters"`
	Confirm       bool           `json:"confirm"`
	SuiteRevision int            `json:"suiteRevision"`
}

// StartRun accepts a report run started by the caller; the route guard has required Pool read on
// every target, and the use case checks the rest (starter level, the caller's account).
func (h *Handler) StartRun(c *fiber.Ctx) error {
	var request runRequest
	if err := c.BodyParser(&request); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if strings.TrimSpace(request.SuiteID) == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "suiteId is required."))
	}
	if request.SuiteRevision < 0 {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "suiteRevision must be positive."))
	}
	actor, ok := actorOf(c)
	if !ok {
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}
	run, err := h.service.Start(c.UserContext(), validationapp.StartInput{
		SuiteID: request.SuiteID, ServerIDs: request.ServerIDs, PlatformID: request.PlatformID,
		Parameters: request.Parameters, Confirm: request.Confirm, SuiteRevision: request.SuiteRevision, Starter: &actor,
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(toRunResponse(run, true))
}

// ListRuns returns one page of runs without their results.
func (h *Handler) ListRuns(c *fiber.Ctx) error {
	page := pagination.FromQuery(c)
	filter := validationdomain.RunFilter{
		SiteID: c.Query("siteId"), SuiteID: c.Query("suiteId"), ServerID: c.Query("serverId"),
		PlatformID: c.Query("platformId"),
		ScheduleID: c.Query("scheduleId"), Status: validationdomain.Status(c.Query("status")),
		Verdict: validationdomain.Verdict(c.Query("verdict")), Trigger: validationdomain.TriggerKind(c.Query("trigger")),
		ActiveOnly: c.Query("active") == "true", Offset: page.Offset(), Limit: page.PageSize,
	}
	if err := validateRunFilter(filter); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))
	}
	visibility, ok := accessdelivery.VisibilityFrom(c)
	if !ok {
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}
	switch requested := c.Query("requestedBy"); requested {
	case "":
	case requestedByMe:
		filter.RequestedByID = visibility.Scope().UserID
	default:
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "requestedBy must be me."))
	}
	if !visibility.Admin() {
		servers, err := visibility.ReadableServers(c.UserContext())
		if err != nil {
			return respondError(c, err)
		}
		filter.TargetsWithin, filter.RestrictTargets = servers, true
	}
	runs, total, err := h.service.List(c.UserContext(), filter)
	if err != nil {
		return respondError(c, err)
	}
	items := make([]runResponse, 0, len(runs))
	for _, run := range runs {
		items = append(items, toRunResponse(run, false))
	}
	return c.JSON(pagination.NewResult(items, total, page))
}

// GetRun returns one run with its results.
func (h *Handler) GetRun(c *fiber.Ctx) error {
	run, err := h.service.Get(c.Context(), c.Params("runId"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(toRunResponse(run, true))
}

// CancelRun cancels a report run; the route guard has required Pool read on its targets, and the use
// case admits its starter, an owner of its targets, or an admin.
func (h *Handler) CancelRun(c *fiber.Ctx) error {
	actor, ok := actorOf(c)
	if !ok {
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}
	run, err := h.service.Cancel(c.UserContext(), c.Params("runId"), actor)
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(toRunResponse(run, true))
}

type scheduleRequest struct {
	Name       string         `json:"name"`
	SuiteID    string         `json:"suiteId"`
	ServerIDs  []string       `json:"serverIds"`
	PlatformID string         `json:"platformId"`
	Parameters map[string]any `json:"parameters"`
	Trigger    struct {
		Kind     string `json:"kind"`
		Cron     string `json:"cron"`
		Timezone string `json:"timezone"`
	} `json:"trigger"`
	Enabled    *bool `json:"enabled"`
	Generation int64 `json:"generation"`
}

func (r scheduleRequest) write() validationapp.ScheduleWrite {
	enabled := r.Enabled == nil || *r.Enabled
	return validationapp.ScheduleWrite{
		Spec: validationdomain.ScheduleSpec{
			Name: r.Name, SuiteID: r.SuiteID, ServerIDs: r.ServerIDs, PlatformID: r.PlatformID, Parameters: r.Parameters,
			Trigger: validationdomain.ScheduleTrigger{
				Kind: validationdomain.ScheduleTriggerKind(r.Trigger.Kind), Cron: r.Trigger.Cron, Timezone: r.Trigger.Timezone,
			},
			Enabled: enabled,
		},
		ExpectedGeneration: r.Generation,
	}
}

// ListSchedules returns schedules, optionally scoped to one Site.
func (h *Handler) ListSchedules(c *fiber.Ctx) error {
	schedules, err := h.service.ListSchedules(c.Context(), validationdomain.ScheduleFilter{SiteID: c.Query("siteId")})
	if err != nil {
		return respondError(c, err)
	}
	items := make([]scheduleResponse, 0, len(schedules))
	for _, schedule := range schedules {
		items = append(items, toScheduleResponse(schedule))
	}
	return c.JSON(fiber.Map{"items": items})
}

// CreateSchedule creates a schedule.
func (h *Handler) CreateSchedule(c *fiber.Ctx) error {
	var request scheduleRequest
	if err := c.BodyParser(&request); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	schedule, err := h.service.CreateSchedule(c.Context(), request.write())
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toScheduleResponse(schedule))
}

// GetSchedule returns one schedule.
func (h *Handler) GetSchedule(c *fiber.Ctx) error {
	schedule, err := h.service.GetSchedule(c.Context(), c.Params("scheduleId"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(toScheduleResponse(schedule))
}

// ReplaceSchedule replaces a schedule's spec.
func (h *Handler) ReplaceSchedule(c *fiber.Ctx) error {
	var request scheduleRequest
	if err := c.BodyParser(&request); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	schedule, err := h.service.ReplaceSchedule(c.Context(), c.Params("scheduleId"), request.write())
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(toScheduleResponse(schedule))
}

// DeleteSchedule deletes a schedule.
func (h *Handler) DeleteSchedule(c *fiber.Ctx) error {
	if err := h.service.DeleteSchedule(c.Context(), c.Params("scheduleId")); err != nil {
		return respondError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func validateRunFilter(filter validationdomain.RunFilter) error {
	switch filter.Status {
	case "", validationdomain.StatusPending, validationdomain.StatusRunning, validationdomain.StatusCompleted, validationdomain.StatusCanceled:
	default:
		return errors.New("status must be pending, running, completed, or canceled")
	}
	switch filter.Verdict {
	case "", validationdomain.VerdictPassed, validationdomain.VerdictFailed, validationdomain.VerdictError:
	default:
		return errors.New("verdict must be passed, failed, or error")
	}
	switch filter.Trigger {
	case "", validationdomain.TriggerManual, validationdomain.TriggerSchedule, validationdomain.TriggerDeployment:
	default:
		return errors.New("trigger must be manual, schedule, or deployment")
	}
	return nil
}

// requestedByMe is the only value of the run list's requestedBy filter: the caller's own runs.
const requestedByMe = "me"

// actorOf builds the use cases' Actor from the request's Visibility (installed by the route's guard,
// or by FixedScope in handler tests): the caller's id, username, and admin role from the Scope
// computed on this request, and its Pool operate checks. ok is false on a route mounted without a
// Visibility, which must answer an internal error rather than act for an unknown caller.
func actorOf(c *fiber.Ctx) (validationapp.Actor, bool) {
	visibility, ok := accessdelivery.VisibilityFrom(c)
	if !ok {
		return validationapp.Actor{}, false
	}
	scope := visibility.Scope()
	return validationapp.Actor{
		UserID: scope.UserID, Username: scope.Username, Admin: scope.Admin, Targets: visibilityTargets{visibility},
	}, true
}

// visibilityTargets answers validationapp.TargetAuthorizer with the request's Visibility.
type visibilityTargets struct {
	visibility *accessdelivery.Visibility
}

// OperatesTargets implements validationapp.TargetAuthorizer.
func (t visibilityTargets) OperatesTargets(ctx context.Context, serverIDs []string, platformID string) (bool, error) {
	return t.visibility.CanOperateTargets(ctx, serverIDs, platformID)
}

// respondError maps validation and workflow errors to the shared envelope (validation.md, Errors).
func respondError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, validationdomain.ErrInvalidRequest),
		errors.Is(err, workflowapp.ErrTargetsSpanSites):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))
	case errors.Is(err, validationdomain.ErrNotSuiteAuthor),
		errors.Is(err, validationdomain.ErrRunForbidden):
		return apierror.Respond(c, apierror.New(apierror.CodeForbidden, err.Error()))
	case errors.Is(err, validationdomain.ErrSuiteNotFound),
		errors.Is(err, validationdomain.ErrRevisionNotFound),
		errors.Is(err, validationdomain.ErrRunNotFound),
		errors.Is(err, validationdomain.ErrScheduleNotFound),
		errors.Is(err, validationdomain.ErrTargetNotFound),
		errors.Is(err, serverdomain.ErrServerNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, err.Error()))
	case errors.Is(err, validationdomain.ErrSuiteImmutable),
		errors.Is(err, validationdomain.ErrSuiteInUse),
		errors.Is(err, validationdomain.ErrRunNotCancelable),
		errors.Is(err, validationdomain.ErrTargetNotRunnable),
		errors.Is(err, validationdomain.ErrGenerationConflict),
		errors.Is(err, validationdomain.ErrRevisionConflict),
		errors.Is(err, workflowdomain.ErrTargetsBusy),
		errors.Is(err, workflowdomain.ErrTargetLocked),
		errors.Is(err, workflowdomain.ErrTargetStateInvalid),
		errors.Is(err, workflowdomain.ErrWorkflowControlConflict),
		errors.Is(err, serverdomain.ErrServerLocked):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))
	case errors.Is(err, workflowdomain.ErrAutomationConfigNotFound),
		errors.Is(err, workflowdomain.ErrAutomationDisabled),
		errors.Is(err, workflowdomain.ErrAutomationCredentialMissing),
		errors.Is(err, workflowdomain.ErrPlaybookNotAllowed),
		errors.Is(err, workflowapp.ErrInvalidOperation),
		errors.Is(err, serverdomain.ErrServerLockUnavailable):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, err.Error()))
	}
	slog.Error("validation: unhandled error", "error", err)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
}

type suiteModuleResponse struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version string `json:"version"`
}

type checkResponse struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Gating      bool   `json:"gating"`
}

type suiteResponse struct {
	ID             string               `json:"id"`
	Source         string               `json:"source"`
	Module         *suiteModuleResponse `json:"module"`
	Name           string               `json:"name"`
	Description    string               `json:"description"`
	Target         string               `json:"target"`
	PlatformType   *string              `json:"platformType"`
	Impact         string               `json:"impact"`
	Checks         []checkResponse      `json:"checks"`
	Parameters     json.RawMessage      `json:"parameters"`
	BudgetSeconds  int                  `json:"budgetSeconds"`
	Script         *string              `json:"script"`
	Sudo           *bool                `json:"sudo"`
	StarterLevel   *string              `json:"starterLevel"`
	TimeoutSeconds *int                 `json:"timeoutSeconds"`
	Revision       *int                 `json:"revision"`
	ScriptSHA256   *string              `json:"scriptSha256"`
	Experimental   bool                 `json:"experimental"`
	Retired        bool                 `json:"retired"`
	CreatedAt      *string              `json:"createdAt"`
	UpdatedAt      *string              `json:"updatedAt"`
	CreatedBy      *string              `json:"createdBy"`
	CreatedByID    *string              `json:"createdById"`
}

// suiteRevisionResponse is one kept revision of a user suite (ValidationSuiteRevision).
type suiteRevisionResponse struct {
	SuiteID        string `json:"suiteId"`
	Revision       int    `json:"revision"`
	Script         string `json:"script"`
	ScriptSHA256   string `json:"scriptSha256"`
	Sudo           bool   `json:"sudo"`
	StarterLevel   string `json:"starterLevel"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	Impact         string `json:"impact"`
	CreatedAt      string `json:"createdAt"`
}

// toSuiteResponse shapes a suite as ValidationSuite: a module suite's user-suite fields are null.
// A user suite's createdById is null when its author could not be identified (a suite from before
// authors were recorded by id).
func toSuiteResponse(suite validationdomain.Suite) suiteResponse {
	parameters := json.RawMessage("null")
	if len(suite.ParametersJSON) > 0 {
		parameters = json.RawMessage(suite.ParametersJSON)
	}
	response := suiteResponse{
		ID: suite.ID, Source: string(suite.Source), Name: suite.Name, Description: suite.Description,
		Target: string(suite.Target), PlatformType: wire.String(suite.PlatformType), Impact: string(suite.Impact),
		Checks: toCheckResponses(suite.Checks), Parameters: parameters, BudgetSeconds: suite.BudgetSeconds,
		Experimental: suite.Experimental, Retired: suite.Retired,
	}
	if suite.Module != nil {
		response.Module = &suiteModuleResponse{Kind: suite.Module.Kind, ID: suite.Module.ID, Version: suite.Module.Version}
	}
	if suite.Source == validationdomain.SourceUser {
		sudo, level, timeout, revision := suite.Sudo, string(suite.RequiredLevel()), suite.TimeoutSeconds, suite.Revision
		response.Script, response.Sudo, response.StarterLevel, response.TimeoutSeconds = &suite.Script, &sudo, &level, &timeout
		response.Revision, response.ScriptSHA256 = &revision, wire.String(suite.ScriptSHA256)
		response.CreatedAt, response.UpdatedAt = wire.TimePtr(suite.CreatedAt), wire.TimePtr(suite.UpdatedAt)
		response.CreatedBy, response.CreatedByID = &suite.CreatedBy, wire.String(suite.CreatedByID)
	}
	return response
}

func toCheckResponses(checks []validationdomain.Check) []checkResponse {
	responses := make([]checkResponse, 0, len(checks))
	for _, check := range checks {
		responses = append(responses, checkResponse{ID: check.ID, Title: check.Title, Description: check.Description, Gating: check.Gating})
	}
	return responses
}

type runSuiteResponse struct {
	ID           string  `json:"id"`
	Source       string  `json:"source"`
	Name         string  `json:"name"`
	Version      *string `json:"version"`
	Revision     *int    `json:"revision"`
	ScriptSHA256 *string `json:"scriptSha256"`
}

type runTargetResponse struct {
	Kind       string   `json:"kind"`
	ServerIDs  []string `json:"serverIds"`
	PlatformID *string  `json:"platformId"`
}

type runTriggerResponse struct {
	Kind       string  `json:"kind"`
	ScheduleID *string `json:"scheduleId"`
	WorkflowID *string `json:"workflowId"`
}

type countsResponse struct {
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Error   int `json:"error"`
	Skipped int `json:"skipped"`
}

type resultTargetResponse struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type measurementResponse struct {
	Name     string  `json:"name"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit"`
	Expected string  `json:"expected"`
}

type resultResponse struct {
	Check        string                `json:"check"`
	Target       resultTargetResponse  `json:"target"`
	Verdict      string                `json:"verdict"`
	Summary      string                `json:"summary"`
	Measurements []measurementResponse `json:"measurements"`
	Evidence     string                `json:"evidence"`
	ObservedAt   string                `json:"observedAt"`
	TaskID       *string               `json:"taskId"`
}

// runResponse is a Validation Run; Results is omitted from list items (ValidationRunSummary) and
// present, never null, on a read of one run (ValidationRun).
type runResponse struct {
	ID            string             `json:"id"`
	SiteID        string             `json:"siteId"`
	Suite         runSuiteResponse   `json:"suite"`
	Checks        []checkResponse    `json:"checks"`
	Target        runTargetResponse  `json:"target"`
	Parameters    map[string]any     `json:"parameters"`
	Mode          string             `json:"mode"`
	Trigger       runTriggerResponse `json:"trigger"`
	WorkflowID    string             `json:"workflowId"`
	Status        string             `json:"status"`
	StatusReason  *string            `json:"statusReason"`
	Verdict       *string            `json:"verdict"`
	Counts        countsResponse     `json:"counts"`
	Results       *[]resultResponse  `json:"results,omitempty"`
	RequestedBy   string             `json:"requestedBy"`
	RequestedByID *string            `json:"requestedById"`
	RunAs         *runAsResponse     `json:"runAs"`
	RequestedAt   string             `json:"requestedAt"`
	StartedAt     *string            `json:"startedAt"`
	FinishedAt    *string            `json:"finishedAt"`
}

// runAsResponse is the account a user suite run uses on its targets (ValidationRunAs).
type runAsResponse struct {
	Username  string `json:"username"`
	UIDNumber int    `json:"uidNumber"`
	Sudo      bool   `json:"sudo"`
}

func toRunResponse(run validationdomain.Run, withResults bool) runResponse {
	counts := run.Counts()
	response := runResponse{
		ID: run.ID, SiteID: run.SiteID,
		Suite: runSuiteResponse{ID: run.Suite.ID, Source: string(run.Suite.Source), Name: run.Suite.Name,
			Version: wire.String(run.Suite.Version), ScriptSHA256: wire.String(run.Suite.ScriptSHA256)},
		Checks: toCheckResponses(run.Checks),
		Target: runTargetResponse{Kind: string(run.Target.Kind), ServerIDs: wire.Strings(run.Target.ServerIDs),
			PlatformID: wire.String(run.Target.PlatformID)},
		Parameters: run.Parameters, Mode: string(run.Mode),
		Trigger: runTriggerResponse{Kind: string(run.Trigger.Kind), ScheduleID: wire.String(run.Trigger.ScheduleID),
			WorkflowID: wire.String(run.Trigger.WorkflowID)},
		WorkflowID: run.WorkflowID, Status: string(run.Status), StatusReason: wire.String(run.StatusReason),
		Verdict:     wire.String(string(run.Verdict)),
		Counts:      countsResponse{Passed: counts.Passed, Failed: counts.Failed, Error: counts.Error, Skipped: counts.Skipped},
		RequestedBy: run.RequestedBy, RequestedByID: wire.String(run.RequestedByID), RequestedAt: wire.Time(run.RequestedAt),
		StartedAt: wire.OptionalTime(run.StartedAt), FinishedAt: wire.OptionalTime(run.FinishedAt),
	}
	if run.RunAs != nil {
		response.RunAs = &runAsResponse{Username: run.RunAs.Username, UIDNumber: run.RunAs.UIDNumber, Sudo: run.RunAs.Sudo}
	}
	if response.Parameters == nil {
		response.Parameters = map[string]any{}
	}
	if run.Suite.Source == validationdomain.SourceUser {
		revision := run.Suite.Revision
		response.Suite.Revision = &revision
	}
	if withResults {
		results := make([]resultResponse, 0, len(run.Results))
		for _, result := range run.Results {
			measurements := make([]measurementResponse, 0, len(result.Measurements))
			for _, measurement := range result.Measurements {
				measurements = append(measurements, measurementResponse{
					Name: measurement.Name, Value: measurement.Value, Unit: measurement.Unit, Expected: measurement.Expected,
				})
			}
			results = append(results, resultResponse{
				Check: result.Check, Target: resultTargetResponse{Kind: result.Target.Kind, ID: result.Target.ID},
				Verdict: string(result.Verdict), Summary: result.Summary, Measurements: measurements, Evidence: result.Evidence,
				ObservedAt: wire.Time(result.ObservedAt), TaskID: wire.String(result.TaskID),
			})
		}
		response.Results = &results
	}
	return response
}

type scheduleTriggerResponse struct {
	Kind     string  `json:"kind"`
	Cron     *string `json:"cron"`
	Timezone *string `json:"timezone"`
}

type scheduleSpecResponse struct {
	Name       string                  `json:"name"`
	SuiteID    string                  `json:"suiteId"`
	ServerIDs  []string                `json:"serverIds"`
	PlatformID *string                 `json:"platformId"`
	Parameters map[string]any          `json:"parameters"`
	Trigger    scheduleTriggerResponse `json:"trigger"`
	Enabled    bool                    `json:"enabled"`
}

type conditionResponse struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	Message            string `json:"message"`
	LastTransitionTime string `json:"lastTransitionTime"`
	ObservedGeneration int64  `json:"observedGeneration"`
}

type scheduleStatusResponse struct {
	ObservedGeneration int64               `json:"observedGeneration"`
	NextRunAt          *string             `json:"nextRunAt"`
	LastRunID          *string             `json:"lastRunId"`
	LastRunAt          *string             `json:"lastRunAt"`
	LastSkipReason     *string             `json:"lastSkipReason"`
	Conditions         []conditionResponse `json:"conditions"`
}

type scheduleResponse struct {
	ID         string                 `json:"id"`
	SiteID     string                 `json:"siteId"`
	Generation int64                  `json:"generation"`
	Spec       scheduleSpecResponse   `json:"spec"`
	Status     scheduleStatusResponse `json:"status"`
	CreatedAt  string                 `json:"createdAt"`
	UpdatedAt  string                 `json:"updatedAt"`
}

func toScheduleResponse(schedule validationdomain.Schedule) scheduleResponse {
	spec := schedule.Spec
	parameters := spec.Parameters
	if parameters == nil {
		parameters = map[string]any{}
	}
	return scheduleResponse{
		ID: schedule.ID, SiteID: schedule.SiteID, Generation: schedule.Generation,
		Spec: scheduleSpecResponse{
			Name: spec.Name, SuiteID: spec.SuiteID, ServerIDs: wire.Strings(spec.ServerIDs), PlatformID: wire.String(spec.PlatformID),
			Parameters: parameters, Enabled: spec.Enabled,
			Trigger: scheduleTriggerResponse{Kind: string(spec.Trigger.Kind), Cron: wire.String(spec.Trigger.Cron), Timezone: wire.String(spec.Trigger.Timezone)},
		},
		Status: scheduleStatusResponse{
			ObservedGeneration: schedule.Status.ObservedGeneration, NextRunAt: wire.OptionalTime(schedule.Status.NextRunAt),
			LastRunID: wire.String(schedule.Status.LastRunID), LastRunAt: wire.OptionalTime(schedule.Status.LastRunAt),
			LastSkipReason: wire.String(schedule.Status.LastSkipReason), Conditions: toConditions(schedule.Status.Conditions),
		},
		CreatedAt: wire.Time(schedule.CreatedAt), UpdatedAt: wire.Time(schedule.UpdatedAt),
	}
}

func toConditions(conditions []convergence.Condition) []conditionResponse {
	responses := make([]conditionResponse, 0, len(conditions))
	for _, condition := range conditions {
		responses = append(responses, conditionResponse{
			Type: string(condition.Type), Status: string(condition.Status), Reason: condition.Reason, Message: condition.Message,
			LastTransitionTime: wire.Time(condition.LastTransitionTime), ObservedGeneration: condition.ObservedGeneration,
		})
	}
	return responses
}
