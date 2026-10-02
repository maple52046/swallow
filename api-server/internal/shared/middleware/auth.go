package middleware

import (
	"context"
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/identity"
	"github.com/maple52046/swallow/internal/shared/jwt"
)

type contextKey string

const principalKey contextKey = "principal"

// APIKeyPrefix starts every API Key secret and never starts an access token, so the
// authenticator can route a bearer value without trying both verifiers.
const APIKeyPrefix = "swk_"

// APIKeyVerifier resolves an API Key secret to the Principal it authenticates.
//
// Implementations return (nil, nil) when the key is unknown, expired, deleted, or its owner no
// longer exists — the request is then 401 — and a non-nil error only for infrastructure failures
// (answered 500, so an outage is not mistaken for a bad key). The returned Principal must have
// Method identity.MethodAPIKey and the owner's current role.
type APIKeyVerifier interface {
	VerifyAPIKey(ctx context.Context, secret string) (*identity.Principal, error)
}

// Authenticator verifies the bearer credential of a request (decision 042): a Session access
// token (JWT) or, when an APIKeyVerifier is configured, an API Key. Both produce one
// identity.Principal stored for GetPrincipal, so handlers never branch on the credential type.
// It holds no per-request state and is safe for concurrent use.
type Authenticator struct {
	tokens  *jwt.Service
	apiKeys APIKeyVerifier
}

// NewAuthenticator returns an Authenticator. A nil apiKeys rejects every API Key, which tests
// and JWT-only routes rely on.
func NewAuthenticator(tokens *jwt.Service, apiKeys APIKeyVerifier) *Authenticator {
	return &Authenticator{tokens: tokens, apiKeys: apiKeys}
}

// Require rejects a request without a valid bearer credential with `401 unauthorized`.
func (a *Authenticator) Require() fiber.Handler {
	return func(c *fiber.Ctx) error {
		principal, err := a.authenticate(c)
		if err != nil {
			slog.Error("authenticate request",
				"requestId", c.GetRespHeader(fiber.HeaderXRequestID), "error", err)
			return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
		}
		if principal == nil {
			return unauthorized(c)
		}
		c.Locals(string(principalKey), principal)
		return c.Next()
	}
}

// Optional records a Principal when the request carries a valid credential and otherwise lets
// the request through unauthenticated. Logout uses it so a stale or missing token never blocks
// ending a Session.
func (a *Authenticator) Optional() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if principal, err := a.authenticate(c); err == nil && principal != nil {
			c.Locals(string(principalKey), principal)
		}
		return c.Next()
	}
}

// authenticate returns (nil, nil) for a missing or invalid credential.
func (a *Authenticator) authenticate(c *fiber.Ctx) (*identity.Principal, error) {
	header := c.Get(fiber.HeaderAuthorization)
	if !strings.HasPrefix(header, "Bearer ") {
		return nil, nil
	}
	// Fiber header strings alias a request buffer that is reused after the handler returns;
	// copying keeps the credential valid for verifiers that retain it (for example in a cache).
	credential := strings.Clone(strings.TrimPrefix(header, "Bearer "))
	if strings.HasPrefix(credential, APIKeyPrefix) {
		if a.apiKeys == nil {
			return nil, nil
		}
		return a.apiKeys.VerifyAPIKey(c.UserContext(), credential)
	}
	claims, err := a.tokens.Verify(credential)
	if err != nil {
		return nil, nil
	}
	return &identity.Principal{
		UserID:    claims.UserID,
		Username:  claims.Username,
		Role:      claims.Role,
		Method:    identity.MethodSession,
		SessionID: claims.SessionID,
	}, nil
}

// Auth is a JWT-only Require for routes and tests that never accept API Keys.
func Auth(jwtSvc *jwt.Service) fiber.Handler {
	return NewAuthenticator(jwtSvc, nil).Require()
}

// BearerTokenFromQuery lets a browser EventSource authenticate a streaming endpoint.
// EventSource cannot set an Authorization header, so the stream accepts the access token in
// the named query parameter and this middleware promotes it to the standard Bearer header
// before the authenticator runs. It never overrides a real header (normal callers are
// unaffected) and it never validates the token: the authenticator remains the single verifier,
// so an invalid query token still fails there with the same unauthorized response. API Keys are
// never promoted, so key secrets are not accepted in URLs, where proxies and browsers log them.
func BearerTokenFromQuery(param string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if c.Get(fiber.HeaderAuthorization) == "" {
			if token := c.Query(param); token != "" && !strings.HasPrefix(token, APIKeyPrefix) {
				c.Request().Header.Set(fiber.HeaderAuthorization, "Bearer "+token)
			}
		}
		return c.Next()
	}
}

// AdminOnly answers `403 forbidden` unless the authenticated Principal has role admin. It must
// run after Require; without a Principal it answers 401.
func AdminOnly() fiber.Handler {
	return func(c *fiber.Ctx) error {
		principal := GetPrincipal(c)
		if principal == nil {
			return unauthorized(c)
		}
		if principal.Role != "admin" {
			return apierror.Respond(c, apierror.New(apierror.CodeForbidden, "Admin access required."))
		}
		return c.Next()
	}
}

// SessionOnly answers `403 forbidden` when the request authenticated with an API Key. It guards
// operations a leaked key must not perform, such as creating API Keys (api-keys contract).
func SessionOnly() fiber.Handler {
	return func(c *fiber.Ctx) error {
		principal := GetPrincipal(c)
		if principal == nil {
			return unauthorized(c)
		}
		if principal.Method == identity.MethodAPIKey {
			return apierror.Respond(c, apierror.New(apierror.CodeForbidden,
				"This action requires signing in with a password; an API key cannot perform it."))
		}
		return c.Next()
	}
}

// GetPrincipal returns the authenticated caller, or nil on a route without authentication.
func GetPrincipal(c *fiber.Ctx) *identity.Principal {
	principal, _ := c.Locals(string(principalKey)).(*identity.Principal)
	return principal
}

func unauthorized(c *fiber.Ctx) error {
	return apierror.Respond(c, apierror.New(apierror.CodeUnauthorized, "Missing or invalid token."))
}
