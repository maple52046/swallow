package delivery

import (
	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/provisioning/application"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/middleware"
)

// BootISOHandler serves the Boot ISO routes of contract boot-isos.md (decision 049):
// GET/POST /api/v1/provisioning/boot-isos and GET/DELETE /api/v1/provisioning/boot-isos/{id}.
// It is admin-only through its route group; the unauthenticated file route is served by the
// composition root, not here. Errors go through the shared provisioning mapper (RespondError).
type BootISOHandler struct {
	isos *application.BootISOService
}

// NewBootISOHandler wires the handler to its service.
func NewBootISOHandler(isos *application.BootISOService) *BootISOHandler {
	return &BootISOHandler{isos: isos}
}

// createBootISORequest is the POST body.
type createBootISORequest struct {
	Name          string `json:"name"`
	IntegrationID string `json:"integrationId"`
	RackAddress   string `json:"rackAddress"`
}

// List returns the builder's availability and the Boot ISOs, narrowed by the optional siteId and
// integrationId query parameters.
func (h *BootISOHandler) List(c *fiber.Ctx) error {
	list, err := h.isos.List(c.Context(), c.Query("siteId"), c.Query("integrationId"))
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(list)
}

// Create builds a Boot ISO synchronously and answers 201 with it. The operator's username is
// recorded as createdBy.
func (h *BootISOHandler) Create(c *fiber.Ctx) error {
	var req createBootISORequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	createdBy := ""
	if principal := middleware.GetPrincipal(c); principal != nil {
		createdBy = principal.Username
	}
	item, err := h.isos.Create(c.Context(), application.CreateBootISOInput{
		Name: req.Name, IntegrationID: req.IntegrationID, RackAddress: req.RackAddress, CreatedBy: createdBy,
	})
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(item)
}

// Get returns one Boot ISO.
func (h *BootISOHandler) Get(c *fiber.Ctx) error {
	item, err := h.isos.Get(c.Context(), c.Params("id"))
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(item)
}

// Delete removes a Boot ISO that no Server's enabled Boot Media uses, answering 204.
func (h *BootISOHandler) Delete(c *fiber.Ctx) error {
	if err := h.isos.Delete(c.Context(), c.Params("id")); err != nil {
		return RespondError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
