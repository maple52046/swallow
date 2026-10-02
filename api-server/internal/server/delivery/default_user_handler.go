package delivery

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/server/application"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
)

// DefaultUserHandler serves PUT and DELETE /api/v1/servers/{id}/default-user (contract
// server-detail-actions.md, decision 045). It is admin-only through its route group.
//
// The request's password is handed to the use case for one login and never logged, echoed, or
// included in an error; error messages come from DefaultUserError, which carries only the Server
// name and the account.
type DefaultUserHandler struct {
	defaultUsers *application.DefaultUserUseCase
}

// NewDefaultUserHandler wires the handler to its use case.
func NewDefaultUserHandler(defaultUsers *application.DefaultUserUseCase) *DefaultUserHandler {
	return &DefaultUserHandler{defaultUsers: defaultUsers}
}

// setDefaultUserRequest is the PUT body; password is optional.
type setDefaultUserRequest struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

// setDefaultUserResponse reports the effective default user after the change, whether the key was
// installed with the password, and the verified sudo access.
type setDefaultUserResponse struct {
	DefaultUser  *application.DefaultUserItem `json:"defaultUser"`
	KeyInstalled bool                         `json:"keyInstalled"`
	Sudo         string                       `json:"sudo"`
}

// Set verifies and saves the Server Default User, installing the Deployment Key first when a
// password is given.
func (h *DefaultUserHandler) Set(c *fiber.Ctx) error {
	var req setDefaultUserRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "The request body must be a JSON object with user and an optional password."))
	}
	result, err := h.defaultUsers.Set(c.Context(), application.SetDefaultUserInput{
		ServerID: c.Params("id"), User: req.User, Password: req.Password,
	})
	if err != nil {
		return respondDefaultUserError(c, err)
	}
	return c.JSON(setDefaultUserResponse{
		DefaultUser:  application.ToDefaultUserItem(result.Server),
		KeyInstalled: result.KeyInstalled,
		Sudo:         string(result.Sudo),
	})
}

// Clear removes the value set on the Server; 204 also when nothing was set.
func (h *DefaultUserHandler) Clear(c *fiber.Ctx) error {
	if err := h.defaultUsers.Clear(c.Context(), c.Params("id")); err != nil {
		return respondDefaultUserError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// respondDefaultUserError maps the use case's errors onto the contract's statuses. A rejected
// password is the caller's input, so 400; a rejected Deployment Key, a Server that is not deployed,
// a lock, or a missing key are state conflicts, 409; an unreachable host or an unknown lock state is
// 503. Anything else is logged (without request data) and reported as 500.
func respondDefaultUserError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, serverdomain.ErrInvalidDefaultUser),
		errors.Is(err, serverdomain.ErrHostPasswordRejected):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))
	case errors.Is(err, serverdomain.ErrServerNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Server not found."))
	case errors.Is(err, serverdomain.ErrDefaultUserNotDeployed),
		errors.Is(err, serverdomain.ErrDeploymentKeyRejected),
		errors.Is(err, serverdomain.ErrDeploymentKeyMissing),
		errors.Is(err, serverdomain.ErrServerLocked):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))
	case errors.Is(err, serverdomain.ErrHostUnreachable),
		errors.Is(err, serverdomain.ErrServerLockUnavailable):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, err.Error()))
	}
	slog.Error("server default user change failed", "server_id", c.Params("id"), "error", err)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "The default user could not be changed."))
}
