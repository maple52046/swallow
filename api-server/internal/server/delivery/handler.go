package delivery

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/AFDEAPAC/swallow/internal/server/application"
	serverdomain "github.com/AFDEAPAC/swallow/internal/server/domain"
	"github.com/AFDEAPAC/swallow/internal/shared/apierror"
	"github.com/AFDEAPAC/swallow/internal/shared/pagination"
)

// ServerHandler serves the server projection. It is read-only: servers are produced by
// reconciliation, so there is nothing here to create or delete.
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

func (h *ServerHandler) List(c *fiber.Ctx) error {
	result, err := h.list.Execute(c.Context(), application.ListServersInput{
		SiteID:            c.Query("siteId"),
		IntegrationID:     c.Query("integrationId"),
		ProvisioningState: c.Query("provisioningState"),
		ClusterID:         c.Query("clusterId"),
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
