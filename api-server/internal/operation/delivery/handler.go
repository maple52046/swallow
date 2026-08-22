package delivery

import (
	"errors"
	"log"
	"strconv"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/middleware"
	"github.com/maple52046/swallow/internal/shared/pagination"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

type OperationHandler struct {
	operations *application.OperationService
}

func NewOperationHandler(operations *application.OperationService) *OperationHandler {
	return &OperationHandler{operations: operations}
}

type createOperationRequest struct {
	Kind            string         `json:"kind"`
	Intent          string         `json:"intent"`
	TargetServerIDs []string       `json:"targetServerIds"`
	ClusterID       string         `json:"clusterId"`
	JobTemplateName string         `json:"jobTemplateName"`
	ExtraVars       map[string]any `json:"extraVars"`
}

// Create launches an operation. Responds 202: the controller has accepted the job and
// the poller tracks it from here.
func (h *OperationHandler) Create(c *fiber.Ctx) error {
	var req createOperationRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}

	requestedBy := ""
	if claims := middleware.GetClaims(c); claims != nil {
		requestedBy = claims.Username
	}

	item, err := h.operations.Create(c.Context(), application.CreateOperationInput{
		Kind:            req.Kind,
		Intent:          req.Intent,
		TargetServerIDs: req.TargetServerIDs,
		ClusterID:       req.ClusterID,
		JobTemplateName: req.JobTemplateName,
		ExtraVars:       req.ExtraVars,
		RequestedBy:     requestedBy,
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(item)
}

func (h *OperationHandler) List(c *fiber.Ctx) error {
	result, err := h.operations.List(c.Context(), application.ListOperationsInput{
		SiteID:    c.Query("siteId"),
		ClusterID: c.Query("clusterId"),
		ServerID:  c.Query("serverId"),
		Kind:      c.Query("kind"),
		Status:    c.Query("status"),
		Active:    c.Query("active") == "true",
		Page:      pagination.FromQuery(c),
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(result)
}

func (h *OperationHandler) Get(c *fiber.Ctx) error {
	item, err := h.operations.Get(c.Context(), c.Params("id"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(item)
}

// Logs proxies the controller's output as plain text. Nothing is stored by swallow.
func (h *OperationHandler) Logs(c *fiber.Ctx) error {
	logs, err := h.operations.Logs(c.Context(), c.Params("id"))
	if err != nil {
		return respondError(c, err)
	}
	c.Set(fiber.HeaderContentType, fiber.MIMETextPlainCharsetUTF8)
	return c.SendString(logs)
}

// Refresh re-reads the job state now instead of waiting for the poller.
func (h *OperationHandler) Refresh(c *fiber.Ctx) error {
	item, err := h.operations.Refresh(c.Context(), c.Params("id"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(item)
}

// webhookRequest is the part of an AWX notification swallow reads.
//
// Only the job id is used. The payload's status is deliberately ignored: the webhook is
// a hint to go and look, and the status swallow records always comes from a read it made
// itself. That keeps one unauthenticated-ish payload from being able to mark an
// operation successful.
type webhookRequest struct {
	ID int `json:"id"`
}

func (h *OperationHandler) Webhook(c *fiber.Ctx) error {
	integrationID := c.Params("integrationId")
	if integrationID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "integrationId is required."))
	}

	var req webhookRequest
	if err := c.BodyParser(&req); err != nil || req.ID == 0 {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation,
			"Expected an AWX notification payload with a job id."))
	}

	err := h.operations.RefreshByJob(c.Context(), integrationID, strconv.Itoa(req.ID))
	if errors.Is(err, operationdomain.ErrOperationNotFound) {
		// A job swallow did not start. Acknowledged rather than rejected, so that AWX
		// does not retry a notification swallow will never care about.
		return c.JSON(fiber.Map{"ignored": true})
	}
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(fiber.Map{"refreshed": true})
}

func respondError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, operationdomain.ErrOperationNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Operation not found."))

	case errors.Is(err, serverdomain.ErrServerNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "A target server was not found."))

	case errors.Is(err, operationdomain.ErrTargetsBusy):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))

	case errors.Is(err, operationdomain.ErrTargetStateInvalid),
		errors.Is(err, application.ErrInvalidOperation):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))

	case errors.Is(err, operationdomain.ErrPolicyConflict):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))

	case errors.Is(err, operationdomain.ErrNotLaunched):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation,
			"This operation has no job yet, so it has no output."))

	case errors.Is(err, operationdomain.ErrJobTemplateNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))

	case errors.Is(err, operationdomain.ErrNoAutomationIntegration):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable,
			"This site has no automation controller registered, so there is nothing to execute with."))

	case errors.Is(err, sitedomain.ErrCredentialNotSet):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable,
			"The automation integration has no credential configured."))

	case errors.Is(err, sitedomain.ErrIntegrationNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Integration not found."))
	}

	var ctrlErr *operationdomain.ControllerError
	if errors.As(err, &ctrlErr) {
		if ctrlErr.Kind == operationdomain.ControllerErrorRejected {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, ctrlErr.Detail))
		}
		log.Printf("automation controller %s: %v", ctrlErr.Kind, ctrlErr.Err)
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, ctrlErr.Detail))
	}

	log.Printf("operation: unhandled error: %v", err)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
}
