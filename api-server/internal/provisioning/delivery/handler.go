package delivery

import (
	"context"
	"errors"
	"log"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

type ProvisioningHandler struct {
	deploy    *application.DeployServerUseCase
	release   *application.ReleaseServerUseCase
	images    *application.ListOSImagesUseCase
	reconcile *application.ReconcileUseCase
	detail    *application.GetProvisionerDetailUseCase
	actions   *application.MachineActionsUseCase
}

func NewProvisioningHandler(
	deploy *application.DeployServerUseCase,
	release *application.ReleaseServerUseCase,
	images *application.ListOSImagesUseCase,
	reconcile *application.ReconcileUseCase,
	detail *application.GetProvisionerDetailUseCase,
	actions *application.MachineActionsUseCase,
) *ProvisioningHandler {
	return &ProvisioningHandler{
		deploy:    deploy,
		release:   release,
		images:    images,
		reconcile: reconcile,
		detail:    detail,
		actions:   actions,
	}
}

type deployRequest struct {
	OSSystem     string `json:"osSystem"`
	DistroSeries string `json:"distroSeries"`
	UserData     string `json:"userData"`
	Comment      string `json:"comment"`
	// Ephemeral runs the OS from memory and leaves the disks untouched. Refused, not
	// ignored, when the provisioner cannot do it.
	Ephemeral bool `json:"ephemeral"`
}

// Deploy starts an OS deployment on a server. Responds 202: the provisioner has
// accepted the request, and the reconciler tracks it from there.
func (h *ProvisioningHandler) Deploy(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}

	var req deployRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	// Required rather than defaulted: letting the provisioner pick an OS makes a
	// deployment a surprise.
	if req.DistroSeries == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "distroSeries is required."))
	}

	item, err := h.deploy.Execute(c.Context(), application.DeployServerInput{
		ServerID:     id,
		OSSystem:     req.OSSystem,
		DistroSeries: req.DistroSeries,
		UserData:     req.UserData,
		Comment:      req.Comment,
		Ephemeral:    req.Ephemeral,
	})
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(item)
}

func (h *ProvisioningHandler) Release(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}

	item, err := h.release.Execute(c.Context(), id)
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(item)
}

// ProvisionerDetail proxies the provisioner for one machine's full detail, plus the
// capabilities that tell a client which actions to offer. Read live, so it reflects the
// provisioner exactly and gdcm keeps no schema for it.
func (h *ProvisioningHandler) ProvisionerDetail(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}

	item, err := h.detail.Execute(c.Context(), id)
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(item)
}

// serverAction runs one lifecycle action addressed by server ID and returns the resulting
// provisioning snapshot with 202: the provisioner has accepted the request, and the
// reconciler tracks it from there. The shared shape keeps each action's handler to its
// intent.
func (h *ProvisioningHandler) serverAction(
	c *fiber.Ctx,
	run func(ctx context.Context, id string) (*application.ProvisioningStateItem, error),
) error {
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}
	item, err := run(c.Context(), id)
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(item)
}

func (h *ProvisioningHandler) PowerOn(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.PowerOn)
}

func (h *ProvisioningHandler) PowerOff(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.PowerOff)
}

// PowerState reads the live BMC power state. It is a GET because it changes nothing.
func (h *ProvisioningHandler) PowerState(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}
	item, err := h.actions.QueryPower(c.Context(), id)
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(item)
}

func (h *ProvisioningHandler) Commission(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.Commission)
}

func (h *ProvisioningHandler) Test(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.Test)
}

func (h *ProvisioningHandler) Abort(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.Abort)
}

func (h *ProvisioningHandler) OverrideFailedTesting(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.OverrideFailedTesting)
}

func (h *ProvisioningHandler) Lock(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.Lock)
}

func (h *ProvisioningHandler) Unlock(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.Unlock)
}

func (h *ProvisioningHandler) MarkBroken(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.MarkBroken)
}

func (h *ProvisioningHandler) MarkFixed(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.MarkFixed)
}

func (h *ProvisioningHandler) RescueMode(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.RescueMode)
}

func (h *ProvisioningHandler) ExitRescueMode(c *fiber.Ctx) error {
	return h.serverAction(c, h.actions.ExitRescueMode)
}

// ListImages returns the images one provisioner can deploy. The integration must be
// named: images differ per site, and a merged list would offer images the target site
// cannot deploy.
func (h *ProvisioningHandler) ListImages(c *fiber.Ctx) error {
	integrationID := c.Query("integrationId")
	if integrationID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "integrationId is required."))
	}

	items, err := h.images.Execute(c.Context(), integrationID)
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(items)
}

// Reconcile runs a projection pass immediately instead of waiting for the interval.
//
// The report is returned rather than just an acknowledgement, because the interesting
// output is what it refused to do: conflicts need an operator, and hiding them behind
// a 202 would leave them unnoticed.
func (h *ProvisioningHandler) Reconcile(c *fiber.Ctx) error {
	integrationID := c.Params("id")
	if integrationID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}

	report, err := h.reconcile.Execute(c.Context(), integrationID)
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(report)
}

// ReconcileAll runs a pass over every enabled provisioner.
func (h *ProvisioningHandler) ReconcileAll(c *fiber.Ctx) error {
	reports, err := h.reconcile.ExecuteAll(c.Context())
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(reports)
}

// RespondError maps provisioning failures onto the API error contract. Exported so
// that routes mounted under other resources can share one translation.
func RespondError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, serverdomain.ErrServerNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Server not found."))

	case errors.Is(err, provisioningdomain.ErrMachineNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound,
			"The provisioner no longer has this machine."))

	case errors.Is(err, sitedomain.ErrIntegrationNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Integration not found."))

	case errors.Is(err, provisioningdomain.ErrIntegrationNotProvisioner),
		errors.Is(err, provisioningdomain.ErrProviderKindUnsupported):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))

	case errors.Is(err, sitedomain.ErrCredentialNotSet):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable,
			"This integration has no credential configured."))
	}

	var provErr *provisioningdomain.ProviderError
	if errors.As(err, &provErr) {
		// The provider's own wording is preserved: it explains a refusal far better
		// than gdcm can, and the adapter keeps credentials out of it.
		if provErr.Kind == provisioningdomain.ProviderErrorRejected {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, provErr.Detail))
		}
		// The underlying cause is logged rather than returned: it can name internal
		// addresses, and a client can act on neither.
		log.Printf("provisioning provider %s: %v", provErr.Kind, provErr.Err)
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, provErr.Detail))
	}

	log.Printf("provisioning: unhandled error: %v", err)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
}
