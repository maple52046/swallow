package delivery

import (
	"net/url"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/shared/apierror"
)

// pathParam returns a path parameter with its percent-encoding removed, falling back to the raw
// value when it is not valid encoding.
func pathParam(c *fiber.Ctx, name string) string {
	raw := c.Params(name)
	if decoded, err := url.PathUnescape(raw); err == nil {
		return decoded
	}
	return raw
}

// invalidBody answers a request whose body does not decode.
func invalidBody(c *fiber.Ctx) error {
	return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
}
