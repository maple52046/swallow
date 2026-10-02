package middleware

import (
	"crypto/subtle"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/identity"
	"github.com/maple52046/swallow/internal/shared/jwt"
)

// MachineAuth guards endpoints whose callers are other systems rather than people,
// accepting either an admin JWT or a static machine token.
//
// The static token exists because those callers — Prometheus scraping discovery and external Ansible diagnostics
// posting a job notification — hold one credential in their configuration and cannot
// log in to refresh a JWT. It is accepted only on those endpoints, and is deliberately
// not a second way into the rest of the API.
//
// When no token is configured the endpoints still work for an admin JWT, so that an
// operator can inspect them without provisioning a credential first. API Keys are not accepted
// here: the discovery contracts name only the machine token and an admin access token.
func MachineAuth(jwtSvc *jwt.Service, machineToken string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		header := c.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			return apierror.Respond(c, apierror.New(apierror.CodeUnauthorized, "Missing or invalid token."))
		}
		presented := strings.TrimPrefix(header, "Bearer ")

		if machineToken != "" &&
			subtle.ConstantTimeCompare([]byte(presented), []byte(machineToken)) == 1 {
			return c.Next()
		}

		claims, err := jwtSvc.Verify(presented)
		if err != nil {
			return apierror.Respond(c, apierror.New(apierror.CodeUnauthorized, "Missing or invalid token."))
		}
		if claims.Role != "admin" {
			return apierror.Respond(c, apierror.New(apierror.CodeForbidden, "Admin access required."))
		}

		c.Locals(string(principalKey), &identity.Principal{
			UserID: claims.UserID, Username: claims.Username, Role: claims.Role,
			Method: identity.MethodSession, SessionID: claims.SessionID,
		})
		return c.Next()
	}
}
