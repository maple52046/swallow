package app

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/shared/apierror"
)

// httpErrorHandler is the Fiber error handler: it answers every error no handler answered with the
// contract's error envelope (conventions.md, Error Response). Fiber's own errors keep their meaning:
// a path no route matches is 404 not_found, a path whose route takes another method 405
// method_not_allowed, a body over the size limit 413 payload_too_large, and any other client error
// 400 validation_error. Anything else is a defect: 500 internal_error, logged with the request ID,
// and its detail never reaches the client.
func httpErrorHandler(c *fiber.Ctx, err error) error {
	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) {
		switch {
		case fiberErr.Code == fiber.StatusNotFound:
			return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "No route matches this path."))
		case fiberErr.Code == fiber.StatusMethodNotAllowed:
			return apierror.Respond(c, apierror.New(apierror.CodeMethodNotAllowed, "This path does not accept the request's method."))
		case fiberErr.Code == fiber.StatusRequestEntityTooLarge:
			return apierror.Respond(c, apierror.New(apierror.CodePayloadTooLarge, "The request body exceeds the size this route accepts."))
		case fiberErr.Code >= 400 && fiberErr.Code < 500:
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, "The request is malformed."))
		}
	}
	slog.Error("unhandled HTTP error", "requestId", c.GetRespHeader(fiber.HeaderXRequestID), "error", err)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "An unexpected error occurred."))
}
