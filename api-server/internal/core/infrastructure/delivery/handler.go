// Package delivery exposes the Infrastructure grouping use cases over HTTP (Fiber).
//
// It parses requests, shapes responses, and maps domain and provider errors onto the shared API
// error envelope. Zone and Pool endpoints share generic helpers because the two resources are
// structurally identical; only the service method they call differs. No business rule lives here.
package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/core/infrastructure/application"
	infradomain "github.com/maple52046/swallow/internal/core/infrastructure/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
)

// InfrastructureHandler serves the Zone, Pool, and Server-placement endpoints.
type InfrastructureHandler struct {
	groups *application.GroupingService
}

// NewInfrastructureHandler wires the handler to the grouping service.
func NewInfrastructureHandler(groups *application.GroupingService) *InfrastructureHandler {
	return &InfrastructureHandler{groups: groups}
}

// createGroupRequest is the create body shared by Zones and Pools.
type createGroupRequest struct {
	SiteID      string `json:"siteId"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// updateGroupRequest is the patch body shared by Zones and Pools. A nil pointer means the field
// was omitted and is left unchanged; the service validates a present name.
type updateGroupRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// placementRequest assigns a Server to a Zone and/or Pool by swallow id. A field is kept raw so
// an omitted field (unchanged) can be told from an explicit null (back to the provisioner's
// default group); at least one must be present.
type placementRequest struct {
	ZoneID json.RawMessage `json:"zoneId"`
	PoolID json.RawMessage `json:"poolId"`
}

// placementChange decodes one raw placement field: absent leaves it unchanged, null or "" clears
// it, and a string sets it.
func placementChange(raw json.RawMessage) (application.PlacementChange, bool) {
	if len(raw) == 0 {
		return application.PlacementChange{}, true
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return application.PlacementChange{Set: true}, true
	}
	var id string
	if err := json.Unmarshal(raw, &id); err != nil {
		return application.PlacementChange{}, false
	}
	return application.PlacementChange{Set: true, ID: strings.TrimSpace(id)}, true
}

// --- Zones ---

func (h *InfrastructureHandler) CreateZone(c *fiber.Ctx) error {
	return h.createGroup(c, h.groups.CreateZone)
}

func (h *InfrastructureHandler) ListZones(c *fiber.Ctx) error {
	return h.listGroup(c, h.groups.ListZones)
}

func (h *InfrastructureHandler) GetZone(c *fiber.Ctx) error {
	return h.getGroup(c, h.groups.GetZone)
}

func (h *InfrastructureHandler) UpdateZone(c *fiber.Ctx) error {
	return h.updateGroup(c, h.groups.UpdateZone)
}

func (h *InfrastructureHandler) DeleteZone(c *fiber.Ctx) error {
	return h.deleteGroup(c, h.groups.DeleteZone)
}

// --- Pools ---

func (h *InfrastructureHandler) CreatePool(c *fiber.Ctx) error {
	return h.createGroup(c, h.groups.CreatePool)
}

func (h *InfrastructureHandler) ListPools(c *fiber.Ctx) error {
	return h.listGroup(c, h.groups.ListPools)
}

func (h *InfrastructureHandler) GetPool(c *fiber.Ctx) error {
	return h.getGroup(c, h.groups.GetPool)
}

func (h *InfrastructureHandler) UpdatePool(c *fiber.Ctx) error {
	return h.updateGroup(c, h.groups.UpdatePool)
}

func (h *InfrastructureHandler) DeletePool(c *fiber.Ctx) error {
	return h.deleteGroup(c, h.groups.DeletePool)
}

// --- Placement ---

// AssignServerPlacement handles PUT /servers/{id}/placement. It is defined in this feature
// because the placement references Zones and Pools, but it is mounted under the servers group
// where clients address a Server. It writes the Server's placement spec and returns it; the
// Server controller moves the machine afterwards.
func (h *InfrastructureHandler) AssignServerPlacement(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}
	var req placementRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}

	zone, zoneOK := placementChange(req.ZoneID)
	pool, poolOK := placementChange(req.PoolID)
	if !zoneOK || !poolOK {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "zoneId and poolId must be strings or null."))
	}
	result, err := h.groups.AssignServer(c.Context(), application.AssignServerInput{ServerID: id, Zone: zone, Pool: pool})
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(result)
}

// --- shared endpoint shapes ---

// createGroup handles a Zone/Pool create for the given service method. siteId and name are
// required here so the caller gets a precise validation message before the service runs.
func (h *InfrastructureHandler) createGroup(
	c *fiber.Ctx,
	create func(context.Context, application.CreateGroupInput) (*application.GroupItem, error),
) error {
	var req createGroupRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if strings.TrimSpace(req.SiteID) == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "siteId is required."))
	}
	if strings.TrimSpace(req.Name) == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "name is required."))
	}

	item, err := create(c.Context(), application.CreateGroupInput{
		SiteID:      req.SiteID,
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(item)
}

// listGroup handles a Zone/Pool list, optionally filtered by the siteId query parameter.
func (h *InfrastructureHandler) listGroup(
	c *fiber.Ctx,
	list func(context.Context, string) ([]application.GroupItem, error),
) error {
	items, err := list(c.Context(), c.Query("siteId"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(items)
}

// getGroup handles a Zone/Pool read by id.
func (h *InfrastructureHandler) getGroup(
	c *fiber.Ctx,
	get func(context.Context, string) (*application.GroupItem, error),
) error {
	item, err := get(c.Context(), c.Params("id"))
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(item)
}

// updateGroup handles a Zone/Pool patch by id.
func (h *InfrastructureHandler) updateGroup(
	c *fiber.Ctx,
	update func(context.Context, string, application.UpdateGroupInput) (*application.GroupItem, error),
) error {
	var req updateGroupRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	item, err := update(c.Context(), c.Params("id"), application.UpdateGroupInput{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(item)
}

// deleteGroup handles a Zone/Pool delete by id, returning 204 on success.
func (h *InfrastructureHandler) deleteGroup(
	c *fiber.Ctx,
	del func(context.Context, string) error,
) error {
	if err := del(c.Context(), c.Params("id")); err != nil {
		return respondError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// respondError maps grouping and provider failures onto the shared API error envelope.
//
// Not-found domain errors become 404; per-Site name collisions, a group still in use, and a
// placement that raced another spec change or a Server not adopted yet become 409; the
// validation-class domain errors (bad name, cross-Site placement, empty placement) become 400.
// Provider failures — only a delete calls a provisioner — reuse the provisioning surface's
// classification: a rejected request keeps the provider's own wording as a 400, and an
// unreachable provider or a missing credential is a 503.
func respondError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, infradomain.ErrZoneNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Zone not found."))
	case errors.Is(err, infradomain.ErrPoolNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Pool not found."))
	case errors.Is(err, infradomain.ErrSiteNotFound), errors.Is(err, sitedomain.ErrSiteNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Site not found."))
	case errors.Is(err, infradomain.ErrServerNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Server not found."))
	case errors.Is(err, sitedomain.ErrIntegrationNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Integration not found."))

	case errors.Is(err, infradomain.ErrZoneNameTaken):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"A zone with this name already exists in the site."))
	case errors.Is(err, infradomain.ErrPoolNameTaken):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"A pool with this name already exists in the site."))

	case errors.Is(err, infradomain.ErrInvalidGroup):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation,
			"A non-empty name (100 characters or fewer) and a siteId are required."))
	case errors.Is(err, infradomain.ErrGroupingSiteMismatch):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation,
			"The zone or pool belongs to a different site than the server."))
	case errors.Is(err, infradomain.ErrNothingToAssign):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation,
			"Specify a zoneId, a poolId, or both."))
	case errors.Is(err, infradomain.ErrGroupInUse):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"A server is still placed in this zone or pool; move it first."))
	case errors.Is(err, infradomain.ErrPlacementConflict):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"The server's spec changed during the request; read it and try again."))
	case errors.Is(err, infradomain.ErrServerNotAdopted):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict,
			"The server is still being adopted; try again in a moment."))

	case errors.Is(err, provisioningdomain.ErrMachineNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound,
			"The provisioner no longer has this machine."))
	case errors.Is(err, provisioningdomain.ErrIntegrationNotProvisioner),
		errors.Is(err, provisioningdomain.ErrProviderKindUnsupported):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))
	case errors.Is(err, sitedomain.ErrCredentialNotSet):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable,
			"This site's provisioner has no credential configured."))
	}

	// Provider transport/refusal: mirror the provisioning surface's classification so a
	// grouping write that MAAS refused reads the same as a deploy MAAS refused.
	var provErr *provisioningdomain.ProviderError
	if errors.As(err, &provErr) {
		if provErr.Kind == provisioningdomain.ProviderErrorRejected {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, provErr.Detail))
		}
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, provErr.Detail))
	}

	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
}
