package middleware

import (
	"io"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/shared/apierror"
)

// BodyLimit bounds the request body of every route except those exempt reports, for an app that
// reads request bodies as a stream (fiber.Config.StreamRequestBody).
//
// In stream mode fasthttp does not refuse a body over the server's body limit: it streams it, and
// Fiber's body accessors (c.Body, c.BodyParser) then read the whole stream into memory. Without
// this middleware any route — unauthenticated ones such as login included — would buffer a body of
// any size, so it must run before authentication and every handler. A declared Content-Length over
// limit is refused at once; otherwise at most limit bytes are read from the stream, which bounds a
// chunked body too, and installed as the buffered body, so handlers read it exactly as before.
//
// A refusal is answered here in the contract's error envelope — 413 payload_too_large, or 400
// validation_error for a stream that fails mid-read — rather than returned to the app's error
// handler, so the request log records the real status. Both close the connection after the
// response, because fasthttp would otherwise parse the unread rest of the body as the next request
// on the keep-alive connection. fasthttp does not linger to drain that rest, so a proxy that is
// still writing the body may see the connection reset instead of the 413 (the Vite development
// proxy reports a 500); the production nginx refuses such a body itself before it reaches the API.
//
// Exempt routes keep their stream untouched and must bound what they read themselves (the OS image
// upload content checks its Content-Length against the upload's declared size). A server built
// without StreamRequestBody has no stream; this middleware then does nothing and the server's own
// body limit applies.
func BodyLimit(limit int, exempt func(*fiber.Ctx) bool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		stream := c.Context().RequestBodyStream()
		if stream == nil || exempt(c) {
			return c.Next()
		}
		if c.Request().Header.ContentLength() > limit {
			return refuseBody(c, apierror.CodePayloadTooLarge, "The request body exceeds the size this route accepts.")
		}
		body, err := io.ReadAll(io.LimitReader(stream, int64(limit)+1))
		if err != nil {
			return refuseBody(c, apierror.CodeValidation, "The request body could not be read.")
		}
		if len(body) > limit {
			return refuseBody(c, apierror.CodePayloadTooLarge, "The request body exceeds the size this route accepts.")
		}
		c.Request().SetBodyRaw(body)
		return c.Next()
	}
}

// refuseBody answers a request whose body BodyLimit will not read and closes the connection, so the
// unread rest of the body is never parsed as another request.
func refuseBody(c *fiber.Ctx, code apierror.Code, message string) error {
	c.Context().SetConnectionClose()
	return apierror.Respond(c, apierror.New(code, message))
}
