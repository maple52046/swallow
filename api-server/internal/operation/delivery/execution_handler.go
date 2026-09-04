package delivery

import (
	"errors"
	"log"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/middleware"
	"github.com/maple52046/swallow/internal/shared/pagination"
	"github.com/maple52046/swallow/internal/shared/wire"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// ExecutionHandler serves the embedded-execution operation API.
type ExecutionHandler struct {
	operations *application.ExecutionService
	automation *application.AutomationConfigurationService
}

// NewExecutionHandler constructs the v2 operation handler.
func NewExecutionHandler(operations *application.ExecutionService, automation *application.AutomationConfigurationService) *ExecutionHandler {
	return &ExecutionHandler{operations: operations, automation: automation}
}

type createExecutionRequest struct {
	Kind            string   `json:"kind"`
	Intent          string   `json:"intent"`
	TargetServerIDs []string `json:"targetServerIds"`
	// PlatformID is canonical; ClusterID is the deprecated one-release alias. When
	// both are present PlatformID wins; otherwise ClusterID is accepted.
	PlatformID   string         `json:"platformId"`
	ClusterID    string         `json:"clusterId"`
	PlaybookName string         `json:"playbookName"`
	ExtraVars    map[string]any `json:"extraVars"`
}

// platformIDQuery resolves the platform filter, preferring the canonical platformId
// query key and falling back to the deprecated clusterId alias for one release.
func platformIDQuery(c *fiber.Ctx) string {
	if id := c.Query("platformId"); id != "" {
		return id
	}
	return c.Query("clusterId")
}

// firstNonEmpty returns the first non-empty argument, used to prefer a canonical
// field over its deprecated alias.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// Create persists an accepted operation as pending.
func (h *ExecutionHandler) Create(c *fiber.Ctx) error {
	var req createExecutionRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	requestedBy := ""
	if claims := middleware.GetClaims(c); claims != nil {
		requestedBy = claims.Username
	}
	item, err := h.operations.Create(c.Context(), application.CreateExecutionInput{
		Kind: req.Kind, Intent: req.Intent, TargetServerIDs: req.TargetServerIDs,
		PlatformID: firstNonEmpty(req.PlatformID, req.ClusterID), PlaybookName: req.PlaybookName,
		ExtraVars: req.ExtraVars, RequestedBy: requestedBy,
	})
	if err != nil {
		return respondExecutionError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(item)
}

// List returns operations.
func (h *ExecutionHandler) List(c *fiber.Ctx) error {
	result, err := h.operations.List(c.Context(), application.ListOperationsInput{
		SiteID: c.Query("siteId"), PlatformID: platformIDQuery(c),
		ServerID: c.Query("serverId"), Kind: c.Query("kind"), Status: c.Query("status"),
		Active: c.Query("active") == "true", Page: pagination.FromQuery(c),
	})
	if err != nil {
		return respondExecutionError(c, err)
	}
	return c.JSON(result)
}

// Get returns one operation.
func (h *ExecutionHandler) Get(c *fiber.Ctx) error {
	item, err := h.operations.Get(c.Context(), c.Params("id"))
	if err != nil {
		return respondExecutionError(c, err)
	}
	return c.JSON(item)
}

// Logs returns locally retained runner output.
func (h *ExecutionHandler) Logs(c *fiber.Ctx) error {
	logs, err := h.operations.Logs(c.Context(), c.Params("id"))
	if err != nil {
		return respondExecutionError(c, err)
	}
	c.Set(fiber.HeaderContentType, fiber.MIMETextPlainCharsetUTF8)
	return c.SendString(logs)
}

// Events returns a run's task-level progress.
func (h *ExecutionHandler) Events(c *fiber.Ctx) error {
	events, err := h.operations.Events(c.Context(), c.Params("id"))
	if err != nil {
		return respondExecutionError(c, err)
	}
	return c.JSON(events)
}

// Retry creates a new operation repeating a finished one.
func (h *ExecutionHandler) Retry(c *fiber.Ctx) error {
	requestedBy := ""
	if claims := middleware.GetClaims(c); claims != nil {
		requestedBy = claims.Username
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
	HasCredential    bool              `json:"hasCredential"`
	CreatedAt        string            `json:"createdAt"`
	UpdatedAt        string            `json:"updatedAt"`
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
	mappings := make(map[operationdomain.OperationKind]string, len(req.PlaybookMappings))
	for kind, playbook := range req.PlaybookMappings {
		mappings[operationdomain.OperationKind(kind)] = playbook
	}
	configuration, err := h.automation.Put(c.Context(), &operationdomain.AutomationConfiguration{
		SiteID: c.Params("id"), Enabled: req.Enabled, SSHUser: req.SSHUser,
		SSHPort: req.SSHPort, KnownHosts: req.KnownHosts, PlaybookMappings: mappings,
	})
	if err != nil {
		return respondExecutionError(c, err)
	}
	return c.JSON(toAutomationResponse(configuration))
}

// PutAutomationCredential stores secrets and never returns them.
func (h *ExecutionHandler) PutAutomationCredential(c *fiber.Ctx) error {
	var credential operationdomain.AutomationCredential
	if err := c.BodyParser(&credential); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if err := h.automation.ReplaceCredential(c.Context(), c.Params("id"), credential); err != nil {
		return respondExecutionError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func toAutomationResponse(configuration *operationdomain.AutomationConfiguration) automationResponse {
	mappings := make(map[string]string, len(configuration.PlaybookMappings))
	for kind, playbook := range configuration.PlaybookMappings {
		mappings[string(kind)] = playbook
	}
	return automationResponse{
		SiteID: configuration.SiteID, Enabled: configuration.Enabled,
		SSHUser: configuration.SSHUser, SSHPort: configuration.SSHPort,
		KnownHosts: configuration.KnownHosts, PlaybookMappings: mappings,
		HasCredential: configuration.HasCredential,
		CreatedAt:     wire.Time(configuration.CreatedAt), UpdatedAt: wire.Time(configuration.UpdatedAt),
	}
}

func respondExecutionError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, operationdomain.ErrOperationNotFound),
		errors.Is(err, operationdomain.ErrAutomationConfigNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, err.Error()))
	case errors.Is(err, operationdomain.ErrTargetsBusy),
		errors.Is(err, operationdomain.ErrPolicyConflict),
		errors.Is(err, operationdomain.ErrTargetLocked),
		errors.Is(err, serverdomain.ErrServerLocked):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))
	case errors.Is(err, application.ErrInvalidOperation),
		errors.Is(err, operationdomain.ErrTargetStateInvalid),
		errors.Is(err, operationdomain.ErrPlaybookNotAllowed):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))
	case errors.Is(err, operationdomain.ErrAutomationDisabled),
		errors.Is(err, operationdomain.ErrAutomationCredentialMissing):
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
