package delivery

import (
	"encoding/json"
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/core/convergence"
	convergencedelivery "github.com/maple52046/swallow/internal/core/convergence/delivery"
	"github.com/maple52046/swallow/internal/core/platform/application"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/middleware"
)

// AttachSpecService wires the Platform spec writes: create, replace the spec, and delete.
func (h *PlatformHandler) AttachSpecService(specs *application.PlatformSpecService) {
	h.specs = specs
}

// platformSpecRequest is a Platform spec as a client writes it (platforms.md, "Spec and status").
// The settings are the type's own fields; the machine preparation has the deploy request's shape.
type platformSpecRequest struct {
	Members            []platformMemberRequest    `json:"members"`
	Settings           map[string]any             `json:"settings"`
	MachinePreparation *machinePreparationRequest `json:"machinePreparation"`
}

type platformMemberRequest struct {
	ServerID string   `json:"serverId"`
	Roles    []string `json:"roles"`
}

// createPlatformRequest is POST /platforms: a new Platform declared by its spec.
type createPlatformRequest struct {
	SiteID string              `json:"siteId"`
	Name   string              `json:"name"`
	Type   string              `json:"type"`
	Spec   platformSpecRequest `json:"spec"`
}

// putPlatformSpecRequest is PUT /platforms/{id}/spec: the replacing spec and the generation it was
// read at.
type putPlatformSpecRequest struct {
	Generation *int64              `json:"generation"`
	Spec       platformSpecRequest `json:"spec"`
}

// removePlatformRequest is the optional DELETE /platforms/{id} body: how the uninstall treats the
// members.
type removePlatformRequest struct {
	ReleaseServers bool `json:"releaseServers"`
	ReleaseOptions struct {
		Erase           bool `json:"erase"`
		SecureErase     bool `json:"secureErase"`
		QuickErase      bool `json:"quickErase"`
		UnbindStaticIPs bool `json:"unbindStaticIps"`
	} `json:"releaseOptions"`
	CancelActiveWorkflows bool `json:"cancelActiveWorkflows"`
}

// specWriteResponse is a written spec: the Platform as it now reads, and the Change Plan the change
// waits on (null for a change the controller converges without approval).
type specWriteResponse struct {
	Platform *application.PlatformItem               `json:"platform"`
	Plan     *convergencedelivery.ChangePlanResponse `json:"plan"`
}

func (r platformSpecRequest) toDomain() (platformdomain.PlatformSpec, *apierror.APIError) {
	spec := platformdomain.PlatformSpec{Desired: platformdomain.PlatformPresent, Settings: r.Settings}
	for _, member := range r.Members {
		if member.ServerID == "" {
			return spec, apierror.New(apierror.CodeValidation, "Each member needs a serverId.")
		}
		spec.Members = append(spec.Members, platformdomain.PlatformMember{ServerID: member.ServerID, Roles: member.Roles})
	}
	if r.MachinePreparation != nil {
		preparation, ok := machinePreparationFromRequest(*r.MachinePreparation)
		if !ok {
			return spec, apierror.New(apierror.CodeValidation, `deployTarget must be "disk" or "ram".`)
		}
		spec.MachinePreparation = preparation
	}
	return spec, nil
}

// Create handles POST /platforms: it validates the Platform as a deployment would and answers 202
// with the Platform and the Change Plan that installs it, approved with the request when it sends
// approve=true and the policy allows it.
func (h *PlatformHandler) Create(c *fiber.Ctx) error {
	var req createPlatformRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if req.SiteID == "" || req.Name == "" || req.Type == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "siteId, name, and type are required."))
	}
	spec, invalid := req.Spec.toDomain()
	if invalid != nil {
		return apierror.Respond(c, invalid)
	}
	result, err := h.specs.Create(c.Context(), application.CreatePlatformSpecInput{
		SiteID: req.SiteID, Name: req.Name, Type: platformdomain.PlatformType(req.Type), Spec: spec, RequestedBy: requester(c),
		ApprovedBy: approver(c),
	})
	if err != nil {
		return respondError(c, err)
	}
	return h.respondSpecWrite(c, result)
}

// PutSpec handles PUT /platforms/{id}/spec: 200 when the change converges without approval, 202
// with the Change Plan when it is destructive (approved with the write under approve=true).
func (h *PlatformHandler) PutSpec(c *fiber.Ctx) error {
	var req putPlatformSpecRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if req.Generation == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "generation is required."))
	}
	spec, invalid := req.Spec.toDomain()
	if invalid != nil {
		return apierror.Respond(c, invalid)
	}
	result, err := h.specs.UpdateSpec(c.Context(), c.Params("id"), *req.Generation, spec, approver(c))
	if err != nil {
		return respondError(c, err)
	}
	return h.respondSpecWrite(c, result)
}

// Delete handles DELETE /platforms/{id}. By default it declares the Platform absent: 202 with the
// Change Plan that uninstalls it (approved with the request under approve=true), or 204 when it was
// never installed and is gone already. With recordOnly=true it removes only the record and its
// claims and leaves the hosts as they are.
func (h *PlatformHandler) Delete(c *fiber.Ctx) error {
	if c.Query("recordOnly") == "true" {
		if err := h.platforms.Delete(c.Context(), c.Params("id")); err != nil {
			return respondError(c, err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
	var req removePlatformRequest
	if len(c.Body()) > 0 {
		if err := json.Unmarshal(c.Body(), &req); err != nil {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
		}
	}
	var removal *platformdomain.PlatformRemoval
	if req.ReleaseServers || req.CancelActiveWorkflows {
		removal = &platformdomain.PlatformRemoval{
			ReleaseServers: req.ReleaseServers, CancelActiveWorkflows: req.CancelActiveWorkflows,
			ReleaseOptions: platformdomain.ServerReleaseOptions{
				Erase: req.ReleaseOptions.Erase, SecureErase: req.ReleaseOptions.SecureErase,
				QuickErase: req.ReleaseOptions.QuickErase, UnbindStaticIPs: req.ReleaseOptions.UnbindStaticIPs,
			},
		}
	}
	result, err := h.specs.Delete(c.Context(), c.Params("id"), removal, approver(c))
	if err != nil {
		return respondError(c, err)
	}
	if result.Platform == nil {
		return c.SendStatus(fiber.StatusNoContent)
	}
	return h.respondSpecWrite(c, result)
}

// respondSpecWrite answers a spec write with the Platform as it now reads: 202 with the plan when
// the write proposed one, whether it waits for approval or was approved with the write, 200
// otherwise.
func (h *PlatformHandler) respondSpecWrite(c *fiber.Ctx, result *application.SpecWriteResult) error {
	item, err := h.platforms.Get(c.Context(), result.Platform.ID)
	if err != nil {
		return respondError(c, err)
	}
	response := specWriteResponse{Platform: item}
	status := fiber.StatusOK
	if result.Plan != nil {
		plan := convergencedelivery.ToChangePlanResponse(result.Plan)
		response.Plan, status = &plan, fiber.StatusAccepted
	}
	return c.Status(status).JSON(response)
}

func requester(c *fiber.Ctx) string {
	if principal := middleware.GetPrincipal(c); principal != nil {
		return principal.Username
	}
	return ""
}

// approver is the user who approves a spec write's Change Plan with the write: the requester when
// the request sends approve=true (change-plans.md, Approval On Submit), "" otherwise. Any other
// value of the parameter approves nothing, so a typo leaves the plan waiting rather than running.
func approver(c *fiber.Ctx) string {
	if c.Query("approve") != "true" {
		return ""
	}
	return requester(c)
}

// respondSpecError maps the spec write refusals; ok is false for an error that is not one.
func respondSpecError(c *fiber.Ctx, err error) (error, bool) {
	switch {
	case errors.Is(err, platformdomain.ErrGenerationConflict):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, "The platform spec changed since it was read; read it again and retry.")), true
	case errors.Is(err, platformdomain.ErrImmutableSpecField), errors.Is(err, application.ErrUnsupportedChange),
		errors.Is(err, application.ErrPlatformRemoving), errors.Is(err, application.ErrPlatformNotAdopted),
		errors.Is(err, convergence.ErrChangePlanConflict):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error())), true
	case errors.Is(err, platformdomain.ErrInvalidSpec):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error())), true
	}
	return nil, false
}
