package delivery

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/server/application"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
)

// BootMediaHandler serves the Boot Media routes of contract server-detail-actions.md
// (decisions 047 and 049): GET and PUT /api/v1/servers/{id}/boot-media and
// POST /api/v1/servers/{id}/redfish/probe. It is admin-only through its route group.
//
// No response or error carries a BMC credential; error messages come from BootMediaError, which
// names only the Server and repeats the BMC's own explanation.
type BootMediaHandler struct {
	bootMedia *application.BootMediaUseCase
}

// NewBootMediaHandler wires the handler to its use case.
func NewBootMediaHandler(bootMedia *application.BootMediaUseCase) *BootMediaHandler {
	return &BootMediaHandler{bootMedia: bootMedia}
}

// setBootMediaRequest is the PUT body. Enabled is a pointer so a body without it is a 400 rather
// than an accidental disable. ISOID names the Boot ISO to enable with (decision 049).
type setBootMediaRequest struct {
	Enabled *bool  `json:"enabled"`
	ISOID   string `json:"isoId"`
}

// setBootMediaResponse is the Boot Media after the change, plus the disable revert outcome.
type setBootMediaResponse struct {
	application.BootMediaItem
	Reverted    *bool  `json:"reverted,omitempty"`
	RevertError string `json:"revertError,omitempty"`
}

// Get returns the Server's Boot Media; `?live=true` also reads the BMC.
func (h *BootMediaHandler) Get(c *fiber.Ctx) error {
	view, err := h.bootMedia.Get(c.Context(), c.Params("id"), c.QueryBool("live", false))
	if err != nil {
		return respondBootMediaError(c, err)
	}
	return c.JSON(application.ToBootMediaItem(view))
}

// Set enables Boot Media with a Boot ISO (preflight), switches it to another, or disables it.
func (h *BootMediaHandler) Set(c *fiber.Ctx) error {
	var req setBootMediaRequest
	if err := c.BodyParser(&req); err != nil || req.Enabled == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "The request body must be a JSON object with a boolean enabled."))
	}
	change, err := h.bootMedia.SetEnabled(c.Context(), c.Params("id"), *req.Enabled, strings.TrimSpace(req.ISOID))
	if err != nil {
		return respondBootMediaError(c, err)
	}
	response := setBootMediaResponse{BootMediaItem: application.ToBootMediaItem(change.View)}
	if !*req.Enabled {
		reverted := change.Reverted
		response.Reverted, response.RevertError = &reverted, change.RevertError
	}
	return c.JSON(response)
}

// Probe re-probes the Server's BMC for Redfish capability and returns the stored result.
func (h *BootMediaHandler) Probe(c *fiber.Ctx) error {
	capability, err := h.bootMedia.Probe(c.Context(), c.Params("id"))
	if err != nil {
		return respondBootMediaError(c, err)
	}
	return c.JSON(fiber.Map{"redfish": application.ToRedfishCapabilityItem(capability)})
}

// respondBootMediaError maps the use case's errors onto the contract's statuses: a missing
// Server or Boot ISO is 404; enabling without a Boot ISO or with another Integration's is 400; a
// preflight already running, a lock, a Server without a usable BMC, a Boot ISO that is not served, or a BMC that refused the
// media are state conflicts (409); an unreachable BMC or provisioner, or an unknown lock state,
// is 503. Anything else is logged and reported as 500.
func respondBootMediaError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, serverdomain.ErrServerNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Server not found."))
	case errors.Is(err, serverdomain.ErrBootISOUnknown):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Boot ISO not found."))
	case errors.Is(err, serverdomain.ErrBootISORequired):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Choose a Boot ISO (isoId) to enable Boot Media."))
	case errors.Is(err, serverdomain.ErrBootISOWrongIntegration):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))
	case errors.Is(err, serverdomain.ErrBootMediaApplying):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, "Boot Media is already being applied to this Server; wait until it finishes."))
	case errors.Is(err, serverdomain.ErrServerLocked),
		errors.Is(err, serverdomain.ErrBootMediaNotConfigured),
		errors.Is(err, serverdomain.ErrNoBMC),
		errors.Is(err, serverdomain.ErrBMCCredentialUnavailable),
		errors.Is(err, serverdomain.ErrRedfishUnsupported),
		errors.Is(err, serverdomain.ErrBootMediaRejected):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))
	case errors.Is(err, serverdomain.ErrBMCUnreachable),
		errors.Is(err, serverdomain.ErrBMCConnectionUnavailable),
		errors.Is(err, serverdomain.ErrServerLockUnavailable):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, err.Error()))
	}
	slog.Error("boot media request failed", "server_id", c.Params("id"), "error", err)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "The Boot Media request could not be completed."))
}
