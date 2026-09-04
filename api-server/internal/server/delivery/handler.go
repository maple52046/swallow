package delivery

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/server/application"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
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

// platformIDQuery resolves the platform filter, preferring the canonical
// platformId query key and falling back to the deprecated clusterId alias that
// remains supported for one release.
func platformIDQuery(c *fiber.Ctx) string {
	if id := c.Query("platformId"); id != "" {
		return id
	}
	return c.Query("clusterId")
}

func (h *ServerHandler) List(c *fiber.Ctx) error {
	result, err := h.list.Execute(c.Context(), application.ListServersInput{
		SiteID:            c.Query("siteId"),
		IntegrationID:     c.Query("integrationId"),
		ProvisioningState: c.Query("provisioningState"),
		PlatformID:        platformIDQuery(c),
		Keyword:           c.Query("keyword"),
		IncludeAbsent:     c.Query("includeAbsent") == "true",
		Page:              pagination.FromQuery(c),
	})
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
