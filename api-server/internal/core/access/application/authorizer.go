package application

import (
	"context"
	"errors"
	"fmt"

	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
	"github.com/maple52046/swallow/internal/shared/identity"
)

// Authorizer decides what a caller may reach (identity-and-access.md §4). ScopeFor reads the
// caller's current User Role and Pool Memberships, so a demotion or a removed membership takes
// effect on the next request even for an access token issued before it; the Require* methods check
// a Scope against the Pool a Server, Platform, or Workflow belongs to.
//
// The Require* methods return accessdomain.ErrNotVisible when the caller may not read the resource
// (or it does not exist: the two are indistinguishable on purpose, so a caller cannot probe other
// Pools), accessdomain.ErrForbidden when it may read but not operate it, and a wrapped error for a
// store failure. It holds no per-request state and is safe for concurrent use.
type Authorizer struct {
	users       authdomain.UserRepository
	memberships accessdomain.MembershipRepository
	servers     accessdomain.ServerPools
	platforms   accessdomain.PlatformPools
	workflows   accessdomain.WorkflowTargets
}

// AuthorizerDeps are the Authorizer's dependencies; every field is required, because a missing one
// would turn an authorization check into a silent allow or deny.
type AuthorizerDeps struct {
	Users       authdomain.UserRepository
	Memberships accessdomain.MembershipRepository
	Servers     accessdomain.ServerPools
	Platforms   accessdomain.PlatformPools
	Workflows   accessdomain.WorkflowTargets
}

// NewAuthorizer wires the Authorizer, refusing a missing dependency.
func NewAuthorizer(deps AuthorizerDeps) (*Authorizer, error) {
	if deps.Users == nil || deps.Memberships == nil || deps.Servers == nil || deps.Platforms == nil || deps.Workflows == nil {
		return nil, errors.New("access authorizer: every dependency is required")
	}
	return &Authorizer{
		users: deps.Users, memberships: deps.Memberships, servers: deps.Servers,
		platforms: deps.Platforms, workflows: deps.Workflows,
	}, nil
}

// ScopeFor computes the caller's Scope. A caller whose User no longer exists gets
// authdomain.ErrUserNotFound, which delivery answers as an authentication failure.
func (a *Authorizer) ScopeFor(ctx context.Context, principal identity.Principal) (accessdomain.Scope, error) {
	user, err := a.users.FindByID(ctx, principal.UserID)
	if err != nil {
		if errors.Is(err, authdomain.ErrUserNotFound) {
			return accessdomain.Scope{}, err
		}
		return accessdomain.Scope{}, fmt.Errorf("find user: %w", err)
	}
	scope := accessdomain.Scope{UserID: user.ID, Username: user.Username, Admin: user.Role == authdomain.RoleAdmin, Pools: map[string]accessdomain.PoolRole{}}
	if scope.Admin {
		return scope, nil
	}
	memberships, err := a.memberships.ListByUser(ctx, user.ID)
	if err != nil {
		return accessdomain.Scope{}, fmt.Errorf("list memberships: %w", err)
	}
	for _, membership := range memberships {
		scope.Pools[membership.PoolID] = membership.Role
	}
	return scope, nil
}

// RequireServers checks access to every Server in ids: all of them must be visible, and with
// Operate all must be operable. An empty list needs nothing.
func (a *Authorizer) RequireServers(ctx context.Context, scope accessdomain.Scope, access accessdomain.Access, ids []string) error {
	if scope.Admin || len(ids) == 0 {
		return nil
	}
	pools, err := a.servers.PoolsOf(ctx, ids)
	if err != nil {
		return fmt.Errorf("read server pools: %w", err)
	}
	for _, id := range ids {
		pool, known := pools[id]
		if !known || !scope.ReadsPool(pool) {
			return accessdomain.ErrNotVisible
		}
	}
	if access == accessdomain.Read {
		return nil
	}
	for _, id := range ids {
		if !scope.OperatesPool(pools[id]) {
			return accessdomain.ErrForbidden
		}
	}
	return nil
}

// RequirePlatform checks access to one Platform through the Pool it belongs to; a Platform whose
// nodes are not all in one Pool belongs to admins only.
func (a *Authorizer) RequirePlatform(ctx context.Context, scope accessdomain.Scope, access accessdomain.Access, platformID string) error {
	if scope.Admin {
		return nil
	}
	pool, found, err := a.platforms.PoolOf(ctx, platformID)
	if err != nil {
		return fmt.Errorf("read platform pool: %w", err)
	}
	if !found || !scope.ReadsPool(pool) {
		return accessdomain.ErrNotVisible
	}
	if !scope.Allows(access, pool) {
		return accessdomain.ErrForbidden
	}
	return nil
}

// PlatformPool returns the Pool a Platform belongs to ("" for none), for a list that filters
// Platforms; found is false for an unknown Platform.
func (a *Authorizer) PlatformPool(ctx context.Context, platformID string) (string, bool, error) {
	return a.platforms.PoolOf(ctx, platformID)
}

// RequireWorkflow checks access to a Workflow through its targets: every target Server and Platform
// must be visible, and with Operate operable. A Workflow without targets belongs to admins only.
func (a *Authorizer) RequireWorkflow(ctx context.Context, scope accessdomain.Scope, access accessdomain.Access, workflowID string) error {
	if scope.Admin {
		return nil
	}
	servers, platforms, found, err := a.workflows.TargetsOf(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("read workflow targets: %w", err)
	}
	if !found || len(servers)+len(platforms) == 0 {
		return accessdomain.ErrNotVisible
	}
	if err := a.RequireServers(ctx, scope, access, servers); err != nil {
		return err
	}
	for _, platformID := range platforms {
		if err := a.RequirePlatform(ctx, scope, access, platformID); err != nil {
			return err
		}
	}
	return nil
}

// ReadableServers returns the ids of every Server the caller may read, for a list filtered by its
// targets; it is meaningful only for a caller that is not an admin.
func (a *Authorizer) ReadableServers(ctx context.Context, scope accessdomain.Scope) ([]string, error) {
	pools := scope.ReadablePools()
	if len(pools) == 0 {
		return nil, nil
	}
	return a.servers.ServersIn(ctx, pools)
}

// RequirePlatformMembers checks that a caller who is not an admin may build a Platform of the
// Servers in ids (identity-and-access.md §5): it must see every one, they must all be placed in one
// Pool (ErrMixedPools otherwise, also when any is in none), and it must own that Pool.
func (a *Authorizer) RequirePlatformMembers(ctx context.Context, scope accessdomain.Scope, ids []string) error {
	if scope.Admin || len(ids) == 0 {
		return nil
	}
	if err := a.RequireServers(ctx, scope, accessdomain.Read, ids); err != nil {
		return err
	}
	pools, err := a.servers.PoolsOf(ctx, ids)
	if err != nil {
		return fmt.Errorf("read server pools: %w", err)
	}
	pool := pools[ids[0]]
	for _, id := range ids {
		if pools[id] == "" || pools[id] != pool {
			return accessdomain.ErrMixedPools
		}
	}
	if !scope.OperatesPool(pool) {
		return accessdomain.ErrForbidden
	}
	return nil
}

// RequirePool checks access to a Pool itself (its members and Sudo Policy).
func (a *Authorizer) RequirePool(scope accessdomain.Scope, access accessdomain.Access, poolID string) error {
	if !scope.ReadsPool(poolID) {
		return accessdomain.ErrNotVisible
	}
	if !scope.Allows(access, poolID) {
		return accessdomain.ErrForbidden
	}
	return nil
}
