package delivery

import (
	"github.com/gofiber/fiber/v2"

	"github.com/AFDEAPAC/swallow/internal/server/application"
	serverdomain "github.com/AFDEAPAC/swallow/internal/server/domain"
	"github.com/AFDEAPAC/swallow/internal/shared/apierror"
	"github.com/AFDEAPAC/swallow/internal/shared/pagination"
)

type ServerHandler struct {
	create *application.CreateServerUseCase
	list   *application.ListServersUseCase
	delete *application.DeleteServerUseCase
}

func NewServerHandler(
	create *application.CreateServerUseCase,
	list *application.ListServersUseCase,
	del *application.DeleteServerUseCase,
) *ServerHandler {
	return &ServerHandler{create: create, list: list, delete: del}
}

type createServerRequest struct {
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
}

func (h *ServerHandler) Create(c *fiber.Ctx) error {
	var req createServerRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if req.Hostname == "" || req.IP == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "hostname and ip are required."))
	}

	out, err := h.create.Execute(c.Context(), application.CreateServerInput{
		Hostname: req.Hostname,
		IP:       req.IP,
	})
	if err == serverdomain.ErrHostnameTaken {
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, "Hostname already exists."))
	}
	if err == serverdomain.ErrIPTaken {
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, "IP address already exists."))
	}
	if err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}

	return c.Status(fiber.StatusCreated).JSON(out)
}

func (h *ServerHandler) List(c *fiber.Ctx) error {
	page := pagination.FromQuery(c)
	input := application.ListServersInput{
		Status:  c.Query("status"),
		Keyword: c.Query("keyword"),
		Page:    page,
	}

	result, err := h.list.Execute(c.Context(), input)
	if err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}

	return c.JSON(result)
}

func (h *ServerHandler) Delete(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}

	err := h.delete.Execute(c.Context(), id)
	if err == serverdomain.ErrServerNotFound {
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Server not found."))
	}
	if err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}

	return c.JSON(fiber.Map{"success": true})
}
