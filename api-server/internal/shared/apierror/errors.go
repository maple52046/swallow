package apierror

import "github.com/gofiber/fiber/v2"

type Code string

const (
	CodeValidation   Code = "validation_error"
	CodeUnauthorized Code = "unauthorized"
	CodeForbidden    Code = "forbidden"
	CodeNotFound     Code = "not_found"
	CodeConflict     Code = "conflict"
	CodeInternal     Code = "internal_error"
	// CodeProviderUnavailable means an upstream integration swallow depends on is
	// not configured or cannot be reached. It is distinct from CodeInternal so
	// that clients can tell "this deployment is not wired up / the upstream is
	// down" apart from "swallow has a bug".
	CodeProviderUnavailable Code = "provider_unavailable"
)

type APIError struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error APIError `json:"error"`
}

func (e *APIError) HTTPStatus() int {
	switch e.Code {
	case CodeValidation:
		return fiber.StatusBadRequest
	case CodeUnauthorized:
		return fiber.StatusUnauthorized
	case CodeForbidden:
		return fiber.StatusForbidden
	case CodeNotFound:
		return fiber.StatusNotFound
	case CodeConflict:
		return fiber.StatusConflict
	case CodeProviderUnavailable:
		return fiber.StatusServiceUnavailable
	default:
		return fiber.StatusInternalServerError
	}
}

func New(code Code, message string) *APIError {
	return &APIError{Code: code, Message: message}
}

func Respond(c *fiber.Ctx, err *APIError) error {
	return c.Status(err.HTTPStatus()).JSON(ErrorResponse{Error: *err})
}
