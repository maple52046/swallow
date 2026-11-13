package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	accessapp "github.com/maple52046/swallow/internal/core/access/application"
	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
	"github.com/maple52046/swallow/internal/core/convergence/convergencetest"
	"github.com/maple52046/swallow/internal/core/store/storetest"
	"github.com/maple52046/swallow/internal/shared/apicontract"
	"github.com/maple52046/swallow/internal/shared/jwt"
	"github.com/maple52046/swallow/internal/shared/middleware"
)

// The access routes answer in the contract's shapes and enforce their classes: admin for users,
// settings, audit, and the Directory; any Pool Role to read a Pool's members; an owner to change
// them; only a Session to change one's own password.
func TestAccessRoutes(t *testing.T) {
	f := newRouteFixture(t)

	resp := f.do(t, "GET", "/api/v1/users", "admin-1", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list users as admin: %d", resp.StatusCode)
	}
	if resp := f.do(t, "GET", "/api/v1/users", "alice-1", ""); resp.StatusCode != http.StatusForbidden {
		t.Errorf("list users as a user: %d, want 403", resp.StatusCode)
	}
	resp = f.do(t, "POST", "/api/v1/users", "admin-1", `{"username":"carol","password":"longenough","role":"user"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create user: %d", resp.StatusCode)
	}
	created := decode(t, resp)
	if created["uidNumber"].(float64) != 10002 {
		t.Errorf("created uid = %v, want 10002", created["uidNumber"])
	}
	if resp := f.do(t, "POST", "/api/v1/users", "admin-1", `{"username":"root","password":"longenough","role":"user"}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("create reserved user: %d, want 400", resp.StatusCode)
	}
	if resp := f.do(t, "POST", "/api/v1/users", "admin-1", `{"username":"carol","password":"longenough","role":"user"}`); resp.StatusCode != http.StatusConflict {
		t.Errorf("create duplicate user: %d, want 409", resp.StatusCode)
	}
	if resp := f.do(t, "PATCH", "/api/v1/users/admin-1", "admin-1", `{"role":"user"}`); resp.StatusCode != http.StatusConflict {
		t.Errorf("demote the last admin: %d, want 409", resp.StatusCode)
	}

	f.memberships.add("pool-a", "alice-1", accessdomain.RoleOwner)
	resp = f.do(t, "POST", "/api/v1/infrastructure/pools/pool-a/members", "alice-1", `{"username":"carol","role":"member"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("owner adds a member: %d", resp.StatusCode)
	}
	carolID := decode(t, resp)["userId"].(string)
	if resp := f.do(t, "POST", "/api/v1/infrastructure/pools/pool-b/members", "alice-1", `{"username":"carol","role":"member"}`); resp.StatusCode != http.StatusNotFound {
		t.Errorf("adding to another Pool: %d, want 404", resp.StatusCode)
	}
	if resp := f.do(t, "GET", "/api/v1/infrastructure/pools/pool-a/members", carolID, ""); resp.StatusCode != http.StatusOK {
		t.Errorf("member lists its Pool's members: %d, want 200", resp.StatusCode)
	}
	if resp := f.do(t, "PATCH", "/api/v1/infrastructure/pools/pool-a/members/"+carolID, carolID, `{"sudo":"on"}`); resp.StatusCode != http.StatusForbidden {
		t.Errorf("member changes a membership: %d, want 403", resp.StatusCode)
	}
	resp = f.do(t, "PATCH", "/api/v1/infrastructure/pools/pool-a/members/"+carolID, "alice-1", `{"sudo":"on","sudoPasswordless":"off"}`)
	if body := decode(t, resp); body["sudo"].(map[string]any)["effective"] != true {
		t.Errorf("updated member = %v, want sudo on", body)
	}
	if resp := f.do(t, "PUT", "/api/v1/infrastructure/pools/pool-a/access-policy", "alice-1", `{"memberSudo":true,"sudoPasswordless":false}`); resp.StatusCode != http.StatusOK {
		t.Errorf("owner sets the policy: %d", resp.StatusCode)
	}
	if resp := f.do(t, "GET", "/api/v1/infrastructure/pools/pool-a/access-policy", carolID, ""); resp.StatusCode != http.StatusOK {
		t.Errorf("member reads the policy: %d", resp.StatusCode)
	}
	if resp := f.do(t, "DELETE", "/api/v1/infrastructure/pools/pool-a/members/"+carolID, "alice-1", ""); resp.StatusCode != http.StatusNoContent {
		t.Errorf("owner removes a member: %d", resp.StatusCode)
	}

	if resp := f.do(t, "PUT", "/api/v1/access/settings", "admin-1", `{"adminSudoPasswordless":true}`); resp.StatusCode != http.StatusOK {
		t.Errorf("set access settings: %d", resp.StatusCode)
	}
	resp = f.do(t, "GET", "/api/v1/access/audit?limit=10", "admin-1", "")
	if items := decode(t, resp)["items"].([]any); len(items) < 5 {
		t.Errorf("audit has %d entries, want the changes above", len(items))
	}
	if resp := f.do(t, "GET", "/api/v1/access/directory", "admin-1", ""); resp.StatusCode != http.StatusOK {
		t.Errorf("read the directory: %d", resp.StatusCode)
	}
	if resp := f.do(t, "POST", "/api/v1/access/directory/sync", "admin-1", ""); resp.StatusCode != http.StatusConflict {
		t.Errorf("sync without a Directory: %d, want 409", resp.StatusCode)
	}

	if resp := f.do(t, "PUT", "/api/v1/auth/me/password", "alice-1", `{"currentPassword":"wrong","newPassword":"another-pass"}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("change password with the wrong one: %d, want 400", resp.StatusCode)
	}
	if resp := f.do(t, "PUT", "/api/v1/auth/me/password", "alice-1", `{"currentPassword":"alice-pass","newPassword":"another-pass"}`); resp.StatusCode != http.StatusNoContent {
		t.Errorf("change password: %d, want 204", resp.StatusCode)
	}
}

type routeFixture struct {
	app         *fiber.App
	tokens      *jwt.Service
	users       *routeUsers
	memberships *routeMemberships
}

func newRouteFixture(t *testing.T) *routeFixture {
	t.Helper()
	hash, err := authdomain.HashPassword("alice-pass")
	if err != nil {
		t.Fatal(err)
	}
	users := &routeUsers{users: map[string]*authdomain.User{
		"admin-1": {ID: "admin-1", Username: "admin", Role: authdomain.RoleAdmin, UIDNumber: 10000},
		"alice-1": {ID: "alice-1", Username: "alice", Role: authdomain.RoleUser, UIDNumber: 10001, PasswordHash: hash},
	}}
	memberships := &routeMemberships{}
	audit := &routeAudit{}
	pools := routePools{"pool-a": {ID: "pool-a", SiteID: "s", Name: "a"}, "pool-b": {ID: "pool-b", SiteID: "s", Name: "b"}}
	authorizer, err := accessapp.NewAuthorizer(accessapp.AuthorizerDeps{
		Users: users, Memberships: memberships, Servers: noServers{}, Platforms: noPlatforms{}, Workflows: noWorkflows{},
	})
	if err != nil {
		t.Fatal(err)
	}
	userService, err := accessapp.NewUserService(accessapp.UserServiceDeps{
		ServiceAccounts: []string{}, Transactor: &storetest.Transactor{}, Users: users, UIDs: &routeUIDs{next: 10002},
		Memberships: memberships, Cleanup: noCleanup{}, Audit: audit,
	})
	if err != nil {
		t.Fatal(err)
	}
	memberService, err := accessapp.NewMembershipService(accessapp.MembershipServiceDeps{
		Transactor: &storetest.Transactor{}, Users: users, Memberships: memberships, Policies: &routePolicies{},
		Settings: &routeSettings{}, Audit: audit, Pools: pools,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(HandlerDeps{
		Users: userService, Memberships: memberService, Statuses: routeStatuses{}, Requests: &convergencetest.Requests{},
		POSIXIDStart: func() int { return 10000 },
	})
	tokens := jwt.NewService("test-secret", time.Hour)
	g := NewGuard(authorizer)
	authed := []fiber.Handler{middleware.Auth(tokens), g.Scope()}
	admin := append(append([]fiber.Handler{}, authed...), g.Admin())
	r := func(chain []fiber.Handler, h fiber.Handler) []fiber.Handler {
		return append(append([]fiber.Handler{}, chain...), h)
	}
	app := fiber.New()
	app.Use(apicontract.ResponseValidator(t))
	app.Put("/api/v1/auth/me/password", r(append(append([]fiber.Handler{}, authed...), middleware.SessionOnly()), handler.ChangeOwnPassword)...)
	app.Get("/api/v1/users", r(admin, handler.ListUsers)...)
	app.Post("/api/v1/users", r(admin, handler.CreateUser)...)
	app.Patch("/api/v1/users/:id", r(admin, handler.UpdateUser)...)
	app.Get("/api/v1/infrastructure/pools/:id/members", r(authed, handler.ListPoolMembers)...)
	app.Post("/api/v1/infrastructure/pools/:id/members", r(authed, handler.AddPoolMember)...)
	app.Patch("/api/v1/infrastructure/pools/:id/members/:userId", r(authed, handler.UpdatePoolMember)...)
	app.Delete("/api/v1/infrastructure/pools/:id/members/:userId", r(authed, handler.RemovePoolMember)...)
	app.Get("/api/v1/infrastructure/pools/:id/access-policy", r(authed, handler.GetPoolAccessPolicy)...)
	app.Put("/api/v1/infrastructure/pools/:id/access-policy", r(authed, handler.SetPoolAccessPolicy)...)
	app.Put("/api/v1/access/settings", r(admin, handler.SetAccessSettings)...)
	app.Get("/api/v1/access/audit", r(admin, handler.ListAccessAudit)...)
	app.Get("/api/v1/access/directory", r(admin, handler.GetDirectory)...)
	app.Post("/api/v1/access/directory/sync", r(admin, handler.SyncDirectory)...)
	return &routeFixture{app: app, tokens: tokens, users: users, memberships: memberships}
}

func (f *routeFixture) do(t *testing.T, method, path, userID, body string) *http.Response {
	t.Helper()
	token, err := f.tokens.Sign(userID, userID, "user")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := f.app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func decode(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

type routeUsers struct {
	mu    sync.Mutex
	users map[string]*authdomain.User
}

func (r *routeUsers) FindByUsername(_ context.Context, name string) (*authdomain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if u.Username == name {
			c := *u
			return &c, nil
		}
	}
	return nil, authdomain.ErrUserNotFound
}

func (r *routeUsers) FindByID(_ context.Context, id string) (*authdomain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.users[id]; ok {
		c := *u
		return &c, nil
	}
	return nil, authdomain.ErrUserNotFound
}

func (r *routeUsers) Create(ctx context.Context, user *authdomain.User) error {
	if _, err := r.FindByUsername(ctx, user.Username); err == nil {
		return authdomain.ErrUsernameTaken
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	c := *user
	r.users[user.ID] = &c
	return nil
}

func (r *routeUsers) ExistsByUsername(ctx context.Context, name string) (bool, error) {
	_, err := r.FindByUsername(ctx, name)
	return err == nil, nil
}

func (r *routeUsers) List(context.Context) ([]*authdomain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*authdomain.User
	for _, u := range r.users {
		c := *u
		out = append(out, &c)
	}
	return out, nil
}

func (r *routeUsers) Update(_ context.Context, user *authdomain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := *user
	r.users[user.ID] = &c
	return nil
}

func (r *routeUsers) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.users, id)
	return nil
}

func (r *routeUsers) CountByRole(_ context.Context, role authdomain.Role) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, u := range r.users {
		if u.Role == role {
			n++
		}
	}
	return n, nil
}

type routeMemberships struct {
	mu   sync.Mutex
	rows []accessdomain.Membership
}

func (m *routeMemberships) add(poolID, userID string, role accessdomain.PoolRole) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, accessdomain.Membership{PoolID: poolID, UserID: userID, Role: role, Sudo: accessdomain.SudoInherit, SudoPasswordless: accessdomain.SudoInherit})
}

func (m *routeMemberships) Create(_ context.Context, x *accessdomain.Membership) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.PoolID == x.PoolID && r.UserID == x.UserID {
			return accessdomain.ErrMembershipExists
		}
	}
	m.rows = append(m.rows, *x)
	return nil
}

func (m *routeMemberships) Find(_ context.Context, poolID, userID string) (*accessdomain.Membership, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.PoolID == poolID && r.UserID == userID {
			c := r
			return &c, nil
		}
	}
	return nil, accessdomain.ErrMembershipNotFound
}

func (m *routeMemberships) where(match func(accessdomain.Membership) bool) []*accessdomain.Membership {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*accessdomain.Membership
	for _, r := range m.rows {
		if match(r) {
			c := r
			out = append(out, &c)
		}
	}
	return out
}

func (m *routeMemberships) ListByPool(_ context.Context, poolID string) ([]*accessdomain.Membership, error) {
	return m.where(func(r accessdomain.Membership) bool { return r.PoolID == poolID }), nil
}

func (m *routeMemberships) ListByUser(_ context.Context, userID string) ([]*accessdomain.Membership, error) {
	return m.where(func(r accessdomain.Membership) bool { return r.UserID == userID }), nil
}

func (m *routeMemberships) ListAll(context.Context) ([]*accessdomain.Membership, error) {
	return m.where(func(accessdomain.Membership) bool { return true }), nil
}

func (m *routeMemberships) Update(_ context.Context, x *accessdomain.Membership) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.rows {
		if r.PoolID == x.PoolID && r.UserID == x.UserID {
			m.rows[i] = *x
			return nil
		}
	}
	return accessdomain.ErrMembershipNotFound
}

func (m *routeMemberships) Delete(_ context.Context, poolID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.rows {
		if r.PoolID == poolID && r.UserID == userID {
			m.rows = append(m.rows[:i], m.rows[i+1:]...)
			return nil
		}
	}
	return accessdomain.ErrMembershipNotFound
}

func (m *routeMemberships) DeleteByUser(context.Context, string) error { return nil }
func (m *routeMemberships) DeleteByPool(context.Context, string) error { return nil }

type routePolicies struct {
	policies map[string]accessdomain.PoolPolicy
}

func (p *routePolicies) Find(_ context.Context, poolID string) (accessdomain.PoolPolicy, error) {
	if policy, ok := p.policies[poolID]; ok {
		return policy, nil
	}
	return accessdomain.PoolPolicy{PoolID: poolID}, nil
}
func (p *routePolicies) ListAll(context.Context) ([]accessdomain.PoolPolicy, error) { return nil, nil }
func (p *routePolicies) Save(_ context.Context, policy accessdomain.PoolPolicy) error {
	if p.policies == nil {
		p.policies = map[string]accessdomain.PoolPolicy{}
	}
	p.policies[policy.PoolID] = policy
	return nil
}
func (p *routePolicies) Delete(context.Context, string) error { return nil }

type routeSettings struct{ settings accessdomain.Settings }

func (s *routeSettings) Find(context.Context) (accessdomain.Settings, error) { return s.settings, nil }
func (s *routeSettings) Save(_ context.Context, settings accessdomain.Settings) error {
	s.settings = settings
	return nil
}

type routeAudit struct{ entries []accessdomain.AuditEntry }

func (a *routeAudit) Record(_ context.Context, e accessdomain.AuditEntry) error {
	a.entries = append(a.entries, e)
	return nil
}
func (a *routeAudit) List(_ context.Context, limit int) ([]accessdomain.AuditEntry, error) {
	if len(a.entries) < limit {
		return a.entries, nil
	}
	return a.entries[:limit], nil
}

type routeUIDs struct{ next int }

func (u *routeUIDs) AllocateUID(context.Context) (int, error) { u.next++; return u.next - 1, nil }

type noCleanup struct{}

func (noCleanup) RemoveUserData(context.Context, string) error { return nil }

type routePools map[string]accessdomain.PoolRef

func (p routePools) Pool(_ context.Context, id string) (accessdomain.PoolRef, bool, error) {
	pool, ok := p[id]
	return pool, ok, nil
}

type routeStatuses struct{}

func (routeStatuses) Find(context.Context) (accessdomain.DirectoryStatus, error) {
	return accessdomain.DirectoryStatus{}, nil
}
func (routeStatuses) Save(context.Context, accessdomain.DirectoryStatus) error { return nil }

type noServers struct{}

func (noServers) PoolsOf(context.Context, []string) (map[string]string, error) { return nil, nil }
func (noServers) ServersIn(context.Context, []string) ([]string, error)        { return nil, nil }

type noPlatforms struct{}

func (noPlatforms) PoolOf(context.Context, string) (string, bool, error) { return "", false, nil }

type noWorkflows struct{}

func (noWorkflows) TargetsOf(context.Context, string) ([]string, []string, bool, error) {
	return nil, nil, false, nil
}
