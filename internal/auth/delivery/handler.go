package delivery

import (
	"github.com/gofiber/fiber/v2"

	"github.com/AFDEAPAC/swallow/internal/auth/application"
	authdomain "github.com/AFDEAPAC/swallow/internal/auth/domain"
	"github.com/AFDEAPAC/swallow/internal/shared/apierror"
	"github.com/AFDEAPAC/swallow/internal/shared/middleware"
)

type AuthHandler struct {
	login *application.LoginUseCase
	me    *application.MeUseCase
}

func NewAuthHandler(login *application.LoginUseCase, me *application.MeUseCase) *AuthHandler {
	return &AuthHandler{login: login, me: me}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req loginRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if req.Username == "" || req.Password == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "username and password are required."))
	}

	out, err := h.login.Execute(c.Context(), application.LoginInput{
		Username: req.Username,
		Password: req.Password,
	})
	if err == authdomain.ErrInvalidCredentials {
		return apierror.Respond(c, apierror.New(apierror.CodeUnauthorized, "Invalid credentials."))
	}
	if err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}

	return c.JSON(fiber.Map{"accessToken": out.AccessToken})
}

func (h *AuthHandler) Me(c *fiber.Ctx) error {
	claims := middleware.GetClaims(c)
	if claims == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeUnauthorized, "Missing or invalid token."))
	}

	out, err := h.me.Execute(c.Context(), claims.UserID)
	if err == authdomain.ErrUserNotFound {
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "User not found."))
	}
	if err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}

	return c.JSON(out)
}
