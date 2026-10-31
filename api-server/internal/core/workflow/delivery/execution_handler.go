package delivery

import (
	"errors"
	"log"
	"sort"

	"github.com/gofiber/fiber/v2"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	"github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/middleware"
	"github.com/maple52046/swallow/internal/shared/pagination"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// ExecutionHandler serves the embedded-execution operation API.
type ExecutionHandler struct {
	operations     *application.ExecutionService
	orchestrations *application.WorkflowService
	automation     *application.AutomationConfigurationService
}

// NewExecutionHandler constructs the v2 operation handler.
func NewExecutionHandler(operations *application.ExecutionService, automation *application.AutomationConfigurationService, orchestrations ...*application.WorkflowService) *ExecutionHandler {
	handler := &ExecutionHandler{operations: operations, automation: automation}
	if len(orchestrations) > 0 {
		handler.orchestrations = orchestrations[0]
	}
	return handler
}

type createExecutionRequest struct {
	Kind            string         `json:"kind"`
	Intent          string         `json:"intent"`
	TargetServerIDs []string       `json:"targetServerIds"`
	PlatformID      string         `json:"platformId"`
	ClusterID       string         `json:"clusterId"`
	PlaybookName    string         `json:"playbookName"`
	ExtraVars       map[string]any `json:"extraVars"`
}

// Create persists an accepted operation as pending.
func (h *ExecutionHandler) Create(c *fiber.Ctx) error {
	var req createExecutionRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	requestedBy := ""
	if principal := middleware.GetPrincipal(c); principal != nil {
		requestedBy = principal.Username
	}
	item, err := h.operations.Create(c.Context(), application.CreateExecutionInput{
		Kind: req.Kind, Intent: req.Intent, TargetServerIDs: req.TargetServerIDs,
		PlatformID: firstNonEmpty(req.PlatformID, req.ClusterID), PlaybookName: req.PlaybookName,
		ExtraVars: req.ExtraVars, RequestedBy: requestedBy,
		RequestCorrelation: c.GetRespHeader(fiber.HeaderXRequestID),
	})
	if err != nil {
		return respondExecutionError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(item)
}

// List returns operations.
// platformIDQuery accepts the deprecated Operation filter without exposing it to the
// application model. Canonical platformId wins when both aliases are present.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func platformIDQuery(c *fiber.Ctx) string {
	if id := c.Query("platformId"); id != "" {
		return id
	}
	return c.Query("clusterId")
}

// taskParam returns the Task identifier from either the canonical `taskId` route param
// (`/workflows/:id/tasks/:taskId`) or the deprecated `stepId` alias
// (`/operations/:id/steps/:stepId`), so both route trees share one handler.
func taskParam(c *fiber.Ctx) string {
	if id := c.Params("taskId"); id != "" {
		return id
	}
	return c.Params("stepId")
}

func (h *ExecutionHandler) List(c *fiber.Ctx) error {
	input := application.ListOperationsInput{
		SiteID: c.Query("siteId"), PlatformID: platformIDQuery(c), ServerID: c.Query("serverId"),
		Kind: c.Query("kind"), Status: c.Query("status"), Active: c.Query("active") == "true", Page: pagination.FromQuery(c),
	}
	if h.orchestrations == nil {
		result, err := h.operations.List(c.Context(), input)
		if err != nil {
			return respondExecutionError(c, err)
		}
		return c.JSON(result)
	}
	requestedPage := input.Page
	input.Page = pagination.Page{Page: 1, PageSize: 0}
	v3, v3Total, err := h.orchestrations.List(c.Context(), input)
	if err != nil {
		return respondExecutionError(c, err)
	}
	v2, err := h.operations.List(c.Context(), input)
	if err != nil {
		return respondExecutionError(c, err)
	}
	items := make([]any, 0, len(v3)+len(v2.Items))
	for index := range v3 {
		items = append(items, v3[index])
	}
	for index := range v2.Items {
		items = append(items, v2.Items[index])
	}
	sort.SliceStable(items, func(i, j int) bool { return operationRequestedAt(items[i]) > operationRequestedAt(items[j]) })
	start := requestedPage.Offset()
	if start > len(items) {
		start = len(items)
	}
	end := start + requestedPage.PageSize
	if end > len(items) {
		end = len(items)
	}
	return c.JSON(pagination.NewResult(items[start:end], v3Total+v2.Total, requestedPage))
}

// Get returns one operation.
func (h *ExecutionHandler) Get(c *fiber.Ctx) error {
	if h.orchestrations != nil {
		item, err := h.orchestrations.Get(c.Context(), c.Params("id"))
		if err == nil {
			return c.JSON(item)
		}
		if !application.IsNotV3(err) {
			return respondExecutionError(c, err)
		}
	}
	item, err := h.operations.Get(c.Context(), c.Params("id"))
	if err != nil {
		return respondExecutionError(c, err)
	}
	return c.JSON(item)
}

func operationRequestedAt(item any) string {
	switch value := item.(type) {
	case application.WorkflowItem:
		return value.RequestedAt
	case application.ExecutionOperationItem:
		return value.RequestedAt
	default:
		return ""
	}
}

// Logs returns locally retained runner output.
func (h *ExecutionHandler) Logs(c *fiber.Ctx) error {
	if handled, err := h.rejectOrchestratedLegacy(c, "Orchestrated Operations expose logs per Step; use the Step logs endpoint."); handled {
		return err
	}
	logs, err := h.operations.Logs(c.Context(), c.Params("id"))
	if err != nil {
		return respondExecutionError(c, err)
	}
	c.Set(fiber.HeaderContentType, fiber.MIMETextPlainCharsetUTF8)
	return c.SendString(logs)
}

// Events returns a run's task-level progress.
func (h *ExecutionHandler) Events(c *fiber.Ctx) error {
	if handled, err := h.rejectOrchestratedLegacy(c, "Orchestrated Operations expose events per Step; use the Step events endpoint."); handled {
		return err
	}
	events, err := h.operations.Events(c.Context(), c.Params("id"))
	if err != nil {
		return respondExecutionError(c, err)
	}
	return c.JSON(events)
}

// rejectOrchestratedLegacy answers an operation-level legacy endpoint called for a schema-v3
// Workflow, which exposes logs and events per Task instead: 409 for a Workflow, and the lookup
// error for any failure other than "not v3". handled reports that it wrote the response, so the
// caller returns err as is; apierror.Respond returns nil once it has written, which is why a
// nil error cannot mean "not handled" (DF-05: the 409 used to be overwritten with a 404).
func (h *ExecutionHandler) rejectOrchestratedLegacy(c *fiber.Ctx, message string) (handled bool, err error) {
	if h.orchestrations == nil {
		return false, nil
	}
	if _, lookupErr := h.orchestrations.Get(c.Context(), c.Params("id")); lookupErr == nil {
		return true, apierror.Respond(c, apierror.New(apierror.CodeConflict, message))
	} else if !application.IsNotV3(lookupErr) {
		return true, respondExecutionError(c, lookupErr)
	}
	return false, nil
}

// Timeline returns normalized durable events for an orchestration Operation.
func (h *ExecutionHandler) Timeline(c *fiber.Ctx) error {
	if h.orchestrations == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Operation timeline not found."))
	}
	events, err := h.orchestrations.Timeline(c.Context(), c.Params("id"))
	if err != nil {
		return respondExecutionError(c, err)
	}
	return c.JSON(events)
}

// Cancel asks Temporal to cancel work; Mongo status is updated only by the workflow.
func (h *ExecutionHandler) Cancel(c *fiber.Ctx) error {
	if h.orchestrations == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Operation not found."))
	}
	if err := h.orchestrations.Cancel(c.Context(), c.Params("id")); err != nil {
		return respondExecutionError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"operationId": c.Params("id")})
}

// RetryStep signals a safe failed Step to increment its attempt in the same Operation.
func (h *ExecutionHandler) RetryStep(c *fiber.Ctx) error {
	if h.orchestrations == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Operation not found."))
	}
	if err := h.orchestrations.RetryStep(c.Context(), c.Params("id"), taskParam(c)); err != nil {
		return respondExecutionError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"workflowId": c.Params("id"), "operationId": c.Params("id"), "taskId": taskParam(c), "stepId": taskParam(c)})
}

// Rerun recovers an orchestration Operation that can no longer advance by launching a new
// durable Operation for the same intent on the same Platform, linked back to the original. It
// is the schema-v3 counterpart to Retry: Retry re-runs a finished legacy operation, while Rerun
// re-runs a failed, canceled, or execution-lost orchestration Operation without deleting its
// Platform. It returns 202 with the new Operation projection; conflicts (a still-running or
// already-succeeded Operation) and validation problems are mapped by respondExecutionError.
func (h *ExecutionHandler) Rerun(c *fiber.Ctx) error {
	if h.orchestrations == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Operation not found."))
	}
	requestedBy := ""
	if principal := middleware.GetPrincipal(c); principal != nil {
		requestedBy = principal.Username
	}
	item, err := h.orchestrations.Rerun(c.Context(), c.Params("id"), requestedBy)
	if err != nil {
		return respondExecutionError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(item)
}

// Retry creates a new operation repeating a finished one.
func (h *ExecutionHandler) Retry(c *fiber.Ctx) error {
	if h.orchestrations != nil {
		if _, err := h.orchestrations.Get(c.Context(), c.Params("id")); err == nil {
			return apierror.Respond(c, apierror.New(apierror.CodeConflict, "Orchestrated Operations can only retry a failed, retryable Step."))
		} else if !application.IsNotV3(err) {
			return respondExecutionError(c, err)
		}
	}
	requestedBy := ""
	if principal := middleware.GetPrincipal(c); principal != nil {
		requestedBy = principal.Username
	}
	item, err := h.operations.Retry(c.Context(), c.Params("id"), requestedBy)
	if err != nil {
		return respondExecutionError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(item)
}

type automationRequest struct {
	Enabled          bool              `json:"enabled"`
	SSHUser          string            `json:"sshUser"`
	SSHPort          int               `json:"sshPort"`
	KnownHosts       string            `json:"knownHosts"`
	PlaybookMappings map[string]string `json:"playbookMappings"`
}

type automationResponse struct {
	SiteID           string            `json:"siteId"`
	Enabled          bool              `json:"enabled"`
	SSHUser          string            `json:"sshUser"`
	SSHPort          int               `json:"sshPort"`
	KnownHosts       string            `json:"knownHosts"`
	PlaybookMappings map[string]string `json:"playbookMappings"`
	// HasCredential reports that the Site stores a become password.
	HasCredential bool `json:"hasCredential"`
	// CredentialSource is the key automation uses for the Site: deploymentKey, or none before
	// the Deployment Key exists (decision 041).
	CredentialSource string `json:"credentialSource"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
}

// GetAutomation returns non-secret site settings.
func (h *ExecutionHandler) GetAutomation(c *fiber.Ctx) error {
	configuration, err := h.automation.Get(c.Context(), c.Params("id"))
	if err != nil {
		return respondExecutionError(c, err)
	}
	return c.JSON(toAutomationResponse(configuration))
}

// PutAutomation creates or replaces non-secret site settings.
func (h *ExecutionHandler) PutAutomation(c *fiber.Ctx) error {
	var req automationRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	mappings := make(map[workflowdomain.WorkflowKind]string, len(req.PlaybookMappings))
	for kind, playbook := range req.PlaybookMappings {
		mappings[workflowdomain.WorkflowKind(kind)] = playbook
	}
	configuration, err := h.automation.Put(c.Context(), &workflowdomain.AutomationConfiguration{
		SiteID: c.Params("id"), Enabled: req.Enabled, SSHUser: req.SSHUser,
		SSHPort: req.SSHPort, KnownHosts: req.KnownHosts, PlaybookMappings: mappings,
	})
	if err != nil {
		return respondExecutionError(c, err)
	}
	return c.JSON(toAutomationResponse(configuration))
}

// automationCredentialRequest is the body of PUT /sites/{id}/automation/credential. Only the
// become password is accepted. SSHPrivateKey exists solely to detect callers that still send the
// Site key removed by decision 041: any non-null value is refused rather than ignored, so no
// caller believes an override took effect.
type automationCredentialRequest struct {
	SSHPrivateKey  *string `json:"sshPrivateKey"`
	BecomePassword string  `json:"becomePassword"`
}

// PutAutomationCredential replaces the Site's write-only become password (204, never echoed).
// A body carrying sshPrivateKey is 400 validation_error pointing at the Deployment Key, the only
// automation key; an unknown Site configuration is 404 not_found.
func (h *ExecutionHandler) PutAutomationCredential(c *fiber.Ctx) error {
	var req automationCredentialRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if req.SSHPrivateKey != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation,
			"sshPrivateKey is not accepted: automation always uses the Deployment Key. Replace it with PUT /api/v1/ssh-keys/deployment."))
	}
	if err := h.automation.ReplaceCredential(c.Context(), c.Params("id"), req.BecomePassword); err != nil {
		return respondExecutionError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func toAutomationResponse(configuration *workflowdomain.AutomationConfiguration) automationResponse {
	mappings := make(map[string]string, len(configuration.PlaybookMappings))
	for kind, playbook := range configuration.PlaybookMappings {
		mappings[string(kind)] = playbook
	}
	return automationResponse{
		SiteID: configuration.SiteID, Enabled: configuration.Enabled,
		SSHUser: configuration.SSHUser, SSHPort: configuration.SSHPort,
		KnownHosts: configuration.KnownHosts, PlaybookMappings: mappings,
		HasCredential:    configuration.HasCredential,
		CredentialSource: string(configuration.CredentialSource),
		CreatedAt:        wire.Time(configuration.CreatedAt), UpdatedAt: wire.Time(configuration.UpdatedAt),
	}
}

func respondExecutionError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, workflowdomain.ErrWorkflowNotFound),
		errors.Is(err, workflowdomain.ErrWorkflowNotV3),
		errors.Is(err, workflowdomain.ErrTaskNotFound),
		errors.Is(err, workflowdomain.ErrAutomationConfigNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, err.Error()))
	case errors.Is(err, sitedomain.ErrSiteNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Site not found."))
	case errors.Is(err, workflowdomain.ErrTargetsBusy),
		errors.Is(err, workflowdomain.ErrPolicyConflict),
		errors.Is(err, workflowdomain.ErrWorkflowControlConflict),
		errors.Is(err, workflowdomain.ErrTaskRetryUnsafe),
		errors.Is(err, workflowdomain.ErrTargetLocked),
		errors.Is(err, workflowdomain.ErrTargetStateInvalid),
		errors.Is(err, serverdomain.ErrServerLocked):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))
	case errors.Is(err, application.ErrInvalidOperation),
		errors.Is(err, workflowdomain.ErrPlaybookNotAllowed):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))
	case errors.Is(err, workflowdomain.ErrAutomationDisabled),
		errors.Is(err, workflowdomain.ErrAutomationCredentialMissing):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, err.Error()))
	case errors.Is(err, serverdomain.ErrServerNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "A target server was not found."))
	case errors.Is(err, sitedomain.ErrCredentialNotSet):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable,
			"The site automation credential is not configured."))
	case errors.Is(err, serverdomain.ErrServerLockUnavailable):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, err.Error()))
	case errors.Is(err, sitedomain.ErrIntegrationNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Integration not found."))
	}

	log.Printf("operation: unhandled error: %v", err)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
}

func (h *ExecutionHandler) orchestrationStep(c *fiber.Ctx) (*application.WorkflowItem, *workflowdomain.Task, error) {
	if h.orchestrations == nil {
		return nil, nil, workflowdomain.ErrWorkflowNotV3
	}
	operation, err := h.orchestrations.Get(c.Context(), c.Params("id"))
	if err != nil {
		return nil, nil, err
	}
	for index := range operation.Steps {
		if operation.Steps[index].ID == taskParam(c) {
			return operation, &operation.Steps[index], nil
		}
	}
	return nil, nil, workflowdomain.ErrTaskNotFound
}

// StepLogs returns retained stdout for one durable Ansible Step. Other executors have no log artifact.
func (h *ExecutionHandler) StepLogs(c *fiber.Ctx) error {
	_, step, err := h.orchestrationStep(c)
	if err != nil {
		return respondExecutionError(c, err)
	}
	logs := ""
	if step.Executor == workflowdomain.RunnerKindAnsible && step.ExternalExecution != nil {
		logs, err = h.operations.LogsForRun(c.Context(), step.ExternalExecution.ID)
		if err != nil {
			return respondExecutionError(c, err)
		}
	}
	c.Set(fiber.HeaderContentType, fiber.MIMETextPlainCharsetUTF8)
	return c.SendString(logs)
}

// StepStderr returns the error-only report (failed and unreachable tasks with their
// stderr/stdout) for one durable Ansible Step. Non-Ansible Steps, or Steps that never ran,
// have no report and return an empty body.
func (h *ExecutionHandler) StepStderr(c *fiber.Ctx) error {
	_, step, err := h.orchestrationStep(c)
	if err != nil {
		return respondExecutionError(c, err)
	}
	report := ""
	if step.Executor == workflowdomain.RunnerKindAnsible && step.ExternalExecution != nil {
		report, err = h.operations.StderrForRun(c.Context(), step.ExternalExecution.ID)
		if err != nil {
			return respondExecutionError(c, err)
		}
	}
	c.Set(fiber.HeaderContentType, fiber.MIMETextPlainCharsetUTF8)
	return c.SendString(report)
}

// StepEvents returns secret-safe task events for one durable Ansible Step.
func (h *ExecutionHandler) StepEvents(c *fiber.Ctx) error {
	_, step, err := h.orchestrationStep(c)
	if err != nil {
		return respondExecutionError(c, err)
	}
	if step.Executor != workflowdomain.RunnerKindAnsible || step.ExternalExecution == nil {
		return c.JSON(&application.OperationEventsItem{
			Status: string(step.Status), Events: []application.TaskEventItem{},
		})
	}
	events, err := h.operations.EventsForRun(c.Context(), step.ExternalExecution.ID, string(step.Status))
	if err != nil {
		return respondExecutionError(c, err)
	}
	return c.JSON(events)
}

// StepArtifacts lists artifact metadata without exposing server-side paths.
func (h *ExecutionHandler) StepArtifacts(c *fiber.Ctx) error {
	_, step, err := h.orchestrationStep(c)
	if err != nil {
		return respondExecutionError(c, err)
	}
	return c.JSON(step.Artifacts)
}
