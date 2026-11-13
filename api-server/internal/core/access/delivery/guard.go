// Package delivery exposes the access context over HTTP (Fiber): the authorization middleware every
// route group uses (identity-and-access.md §4), and the users, pool-access, and access contracts'
// handlers (decision 082).
//
// The middleware turns the authenticated Principal into the caller's Scope once per request and
// stores it, with a request-scoped Visibility that other contexts' handlers use to filter lists. It
// maps the Authorizer's errors onto the conventions: ErrNotVisible is 404 (indistinguishable from a
// missing resource), ErrForbidden is 403. Access rules live in the application Authorizer; this
// package only reads route parameters and request bodies to find the resources a route acts on.
package delivery

import (
	"context"
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"

	accessapp "github.com/maple52046/swallow/internal/core/access/application"
	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/middleware"
)

const visibilityKey = "access.visibility"

// Guard is the authorization middleware factory. Every guarded route runs Authenticator.Require,
// then Scope, then the check the route needs (Admin, Server, Platform, Workflow, Pool, or a body
// check). It holds no per-request state and is safe for concurrent use.
type Guard struct {
	authorizer *accessapp.Authorizer
}

// NewGuard returns the Guard over the Authorizer.
func NewGuard(authorizer *accessapp.Authorizer) *Guard {
	return &Guard{authorizer: authorizer}
}

// Visibility is one request's view of what its caller may reach: the Scope, plus the lookups list
// handlers need, cached for the request. It is request-scoped and must not outlive the request.
type Visibility struct {
	scope      accessdomain.Scope
	authorizer *accessapp.Authorizer
	servers    []string
	loaded     bool
}

// Scope returns the caller's Scope.
func (v *Visibility) Scope() accessdomain.Scope { return v.scope }

// Admin reports whether the caller is an admin, whom lists do not filter.
func (v *Visibility) Admin() bool { return v.scope.Admin }

// ReadablePools returns the Pools the caller may read (meaningful when it is not an admin).
func (v *Visibility) ReadablePools() []string { return v.scope.ReadablePools() }

// ReadableServers returns the ids of every Server the caller may read, read once per request
// (meaningful when it is not an admin).
func (v *Visibility) ReadableServers(ctx context.Context) ([]string, error) {
	if v.loaded {
		return v.servers, nil
	}
	servers, err := v.authorizer.ReadableServers(ctx, v.scope)
	if err != nil {
		return nil, err
	}
	v.servers, v.loaded = servers, true
	return servers, nil
}

// ReadsPool reports whether the caller may read the resources of poolID.
func (v *Visibility) ReadsPool(poolID string) bool { return v.scope.ReadsPool(poolID) }

// CanReadServers reports whether the caller may read every Server in ids.
func (v *Visibility) CanReadServers(ctx context.Context, ids []string) (bool, error) {
	return allowed(v.authorizer.RequireServers(ctx, v.scope, accessdomain.Read, ids))
}

// CanReadPlatform reports whether the caller may read a Platform.
func (v *Visibility) CanReadPlatform(ctx context.Context, platformID string) (bool, error) {
	return allowed(v.authorizer.RequirePlatform(ctx, v.scope, accessdomain.Read, platformID))
}

// RequireServers checks access to every Server in ids, returning the Authorizer's error.
func (v *Visibility) RequireServers(ctx context.Context, access accessdomain.Access, ids []string) error {
	return v.authorizer.RequireServers(ctx, v.scope, access, ids)
}

// RequirePlatform checks access to a Platform, returning the Authorizer's error.
func (v *Visibility) RequirePlatform(ctx context.Context, access accessdomain.Access, platformID string) error {
	return v.authorizer.RequirePlatform(ctx, v.scope, access, platformID)
}

// allowed turns an authorization error into a yes or no, keeping store failures as errors.
func allowed(err error) (bool, error) {
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, accessdomain.ErrNotVisible), errors.Is(err, accessdomain.ErrForbidden):
		return false, nil
	default:
		return false, err
	}
}

// VisibilityFrom returns the request's Visibility. A route without Guard.Scope has none, and a
// handler that filters must then refuse rather than show everything; ok reports which.
func VisibilityFrom(c *fiber.Ctx) (*Visibility, bool) {
	v, ok := c.Locals(visibilityKey).(*Visibility)
	return v, ok && v != nil
}

// Scope computes the caller's Scope from its current User Role and Pool Memberships and stores the
// request's Visibility. It must run after Authenticator.Require. A caller whose User no longer
// exists is 401, like a revoked credential.
func (g *Guard) Scope() fiber.Handler {
	return func(c *fiber.Ctx) error {
		principal := middleware.GetPrincipal(c)
		if principal == nil {
			return apierror.Respond(c, apierror.New(apierror.CodeUnauthorized, "Missing or invalid token."))
		}
		scope, err := g.authorizer.ScopeFor(c.UserContext(), *principal)
		if errors.Is(err, authdomain.ErrUserNotFound) {
			return apierror.Respond(c, apierror.New(apierror.CodeUnauthorized, "Missing or invalid token."))
		}
		if err != nil {
			return internal(c, "compute access scope", err)
		}
		c.Locals(visibilityKey, &Visibility{scope: scope, authorizer: g.authorizer})
		return c.Next()
	}
}

// FixedScope stores a Visibility for scope without consulting the Authorizer, for handler tests that
// mount a route without the Guard. Only an admin scope (which never consults the Authorizer) or one
// whose checks a test does not exercise may be used with it; production routes use Scope.
func FixedScope(scope accessdomain.Scope) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals(visibilityKey, &Visibility{scope: scope})
		return c.Next()
	}
}

// Admin admits only a caller whose current User Role is admin.
func (g *Guard) Admin() fiber.Handler {
	return func(c *fiber.Ctx) error {
		v, ok := VisibilityFrom(c)
		if !ok {
			return internal(c, "admin check", errors.New("route has no access scope"))
		}
		if !v.Admin() {
			return apierror.Respond(c, apierror.New(apierror.CodeForbidden, "Admin access required."))
		}
		return c.Next()
	}
}

// Server admits a caller with access to the Server named by the route parameter param.
func (g *Guard) Server(access accessdomain.Access, param string) fiber.Handler {
	return g.check(func(c *fiber.Ctx, v *Visibility) error {
		return v.RequireServers(c.UserContext(), access, []string{c.Params(param)})
	})
}

// Platform admits a caller with access to the Platform named by the route parameter param.
func (g *Guard) Platform(access accessdomain.Access, param string) fiber.Handler {
	return g.check(func(c *fiber.Ctx, v *Visibility) error {
		return v.RequirePlatform(c.UserContext(), access, c.Params(param))
	})
}

// Workflow admits a caller with access to every target of the Workflow named by param.
func (g *Guard) Workflow(access accessdomain.Access, param string) fiber.Handler {
	return g.check(func(c *fiber.Ctx, v *Visibility) error {
		return g.authorizer.RequireWorkflow(c.UserContext(), v.scope, access, c.Params(param))
	})
}

// Pool admits a caller with access to the Pool named by param.
func (g *Guard) Pool(access accessdomain.Access, param string) fiber.Handler {
	return g.check(func(c *fiber.Ctx, v *Visibility) error {
		return g.authorizer.RequirePool(v.scope, access, c.Params(param))
	})
}

// ServersInBody admits a caller with access to every Server that extract finds in the request
// body. A body extract cannot read is left to the handler, which answers 400 for it; an empty
// result needs nothing (the handler validates that targets were given).
func (g *Guard) ServersInBody(access accessdomain.Access, extract func(body []byte) []string) fiber.Handler {
	return g.check(func(c *fiber.Ctx, v *Visibility) error {
		return v.RequireServers(c.UserContext(), access, extract(c.Body()))
	})
}

// PlatformMembersInBody admits a caller who may build a Platform of the Servers extract finds in the
// request body: every one placed in one Pool the caller owns (400 when they are not all in one Pool).
func (g *Guard) PlatformMembersInBody(extract func(body []byte) []string) fiber.Handler {
	return g.check(func(c *fiber.Ctx, v *Visibility) error {
		return g.authorizer.RequirePlatformMembers(c.UserContext(), v.scope, extract(c.Body()))
	})
}

// Custom admits a caller that check approves; check returns nil, an Authorizer error, or a store
// failure. Routes whose resource is named in a query or a body of their own use it.
func (g *Guard) Custom(check func(c *fiber.Ctx, v *Visibility) error) fiber.Handler {
	return g.check(check)
}

func (g *Guard) check(check func(c *fiber.Ctx, v *Visibility) error) fiber.Handler {
	return func(c *fiber.Ctx) error {
		v, ok := VisibilityFrom(c)
		if !ok {
			return internal(c, "access check", errors.New("route has no access scope"))
		}
		if v.Admin() {
			return c.Next()
		}
		if err := check(c, v); err != nil {
			return RespondDenied(c, err)
		}
		return c.Next()
	}
}

// RespondDenied answers an authorization error: 404 for a resource the caller may not see, 403 for
// one it may not operate, and 500 for anything else.
func RespondDenied(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, accessdomain.ErrNotVisible):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Not found."))
	case errors.Is(err, accessdomain.ErrForbidden):
		return apierror.Respond(c, apierror.New(apierror.CodeForbidden, "You may read this resource but not change it; ask an owner of its Pool."))
	case errors.Is(err, accessdomain.ErrMixedPools):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation,
			"Every member of a platform must be placed in the same pool, one you own; ask an admin to place the servers."))
	default:
		return internal(c, "access check", err)
	}
}

func internal(c *fiber.Ctx, action string, err error) error {
	slog.Error("access "+action+" failed", "requestId", c.GetRespHeader(fiber.HeaderXRequestID), "error", err)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
}
