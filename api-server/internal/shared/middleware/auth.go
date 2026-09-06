package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/jwt"
)

type contextKey string

const claimsKey contextKey = "claims"

func Auth(jwtSvc *jwt.Service) fiber.Handler {
	return func(c *fiber.Ctx) error {
		header := c.Get("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			return apierror.Respond(c, apierror.New(apierror.CodeUnauthorized, "Missing or invalid token."))
		}
		tokenStr := strings.TrimPrefix(header, "Bearer ")
		claims, err := jwtSvc.Verify(tokenStr)
		if err != nil {
			return apierror.Respond(c, apierror.New(apierror.CodeUnauthorized, "Missing or invalid token."))
		}
		c.Locals(string(claimsKey), claims)
		return c.Next()
	}
}

// BearerTokenFromQuery lets a browser EventSource authenticate a streaming endpoint.
// EventSource cannot set an Authorization header, so the stream accepts the access token in
// the named query parameter and this middleware promotes it to the standard Bearer header
// before Auth runs. It never overrides a real header (normal callers are unaffected) and it
// never validates the token: Auth remains the single verifier, so an invalid query token
// still fails there with the same unauthorized response.
func BearerTokenFromQuery(param string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if c.Get("Authorization") == "" {
			if token := c.Query(param); token != "" {
				c.Request().Header.Set("Authorization", "Bearer "+token)
			}
		}
		return c.Next()
	}
}

func AdminOnly() fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals(string(claimsKey)).(*jwt.Claims)
		if !ok || claims == nil {
			return apierror.Respond(c, apierror.New(apierror.CodeUnauthorized, "Missing or invalid token."))
		}
		if claims.Role != "admin" {
			return apierror.Respond(c, apierror.New(apierror.CodeForbidden, "Admin access required."))
		}
		return c.Next()
	}
}

func GetClaims(c *fiber.Ctx) *jwt.Claims {
	claims, _ := c.Locals(string(claimsKey)).(*jwt.Claims)
	return claims
}
