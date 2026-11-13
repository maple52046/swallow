package delivery

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	accessdelivery "github.com/maple52046/swallow/internal/core/access/delivery"
	"github.com/maple52046/swallow/internal/core/server/application"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/pagination"
)

// ServerHandler serves read-only projection queries. Servers are produced by
// reconciliation; provider-backed lifecycle operations, including permanent deletion,
// live in provisioning delivery rather than mutating this projection directly.
type ServerHandler struct {
	list *application.ListServersUseCase
	get  *application.GetServerUseCase
}

func NewServerHandler(
	list *application.ListServersUseCase,
	get *application.GetServerUseCase,
) *ServerHandler {
	return &ServerHandler{list: list, get: get}
}

// List implements GET /api/v1/servers. A caller that is not an admin sees only the Servers placed
// in the Pools it belongs to (servers-list.md, Authorization).
func (h *ServerHandler) List(c *fiber.Ctx) error {
	visibility, ok := accessdelivery.VisibilityFrom(c)
	if !ok {
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}
	input := application.ListServersInput{
		SiteID:            c.Query("siteId"),
		IntegrationID:     c.Query("integrationId"),
		ProvisioningState: c.Query("provisioningState"),
		PlatformID:        c.Query("platformId"),
		Keyword:           c.Query("keyword"),
		IncludeAbsent:     c.Query("includeAbsent") == "true",
		Page:              pagination.FromQuery(c),
	}
	if !visibility.Admin() {
		input.PoolIDs, input.RestrictPools = visibility.ReadablePools(), true
	}
	result, err := h.list.Execute(c.UserContext(), input)
	if err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}
	return c.JSON(result)
}

func (h *ServerHandler) Get(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}

	item, err := h.get.Execute(c.Context(), id)
	if errors.Is(err, serverdomain.ErrServerNotFound) {
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Server not found."))
	}
	if err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}
	return c.JSON(item)
}
