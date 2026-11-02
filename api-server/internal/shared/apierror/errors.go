package apierror

import "github.com/gofiber/fiber/v2"

// Code identifies a stable API error category clients may branch on.
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
	// that clients can tell "this installation is not wired up / the upstream is
	// down" apart from "swallow has a bug".
	CodeProviderUnavailable Code = "provider_unavailable"
	// CodeMethodNotAllowed means the path exists but does not accept the request's method.
	CodeMethodNotAllowed Code = "method_not_allowed"
	// CodePayloadTooLarge means the request body exceeds the size the route accepts.
	CodePayloadTooLarge Code = "payload_too_large"
)

// APIError is the client-safe error payload. RequestID correlates the response
// with structured server logs and is opaque to clients.
type APIError struct {
	Code      Code   `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
}

// ErrorResponse is the common non-success response envelope.
type ErrorResponse struct {
	Error APIError `json:"error"`
}

// HTTPStatus maps the stable error code to its transport status.
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
	case CodeMethodNotAllowed:
		return fiber.StatusMethodNotAllowed
	case CodePayloadTooLarge:
		return fiber.StatusRequestEntityTooLarge
	default:
		return fiber.StatusInternalServerError
	}
}

// New creates a client-safe API error.
func New(code Code, message string) *APIError {
	return &APIError{Code: code, Message: message}
}

// Respond writes the shared error envelope and includes the middleware request ID.
func Respond(c *fiber.Ctx, err *APIError) error {
	responseError := *err
	responseError.RequestID = c.GetRespHeader(fiber.HeaderXRequestID)
	return c.Status(err.HTTPStatus()).JSON(ErrorResponse{Error: responseError})
}
