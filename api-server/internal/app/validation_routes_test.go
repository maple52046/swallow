package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	accessapp "github.com/maple52046/swallow/internal/core/access/application"
	accessdelivery "github.com/maple52046/swallow/internal/core/access/delivery"
	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
	"github.com/maple52046/swallow/internal/core/store/storetest"
	validationapp "github.com/maple52046/swallow/internal/core/validation/application"
	validationdelivery "github.com/maple52046/swallow/internal/core/validation/delivery"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
	"github.com/maple52046/swallow/internal/core/validation/domain/validationtest"
	"github.com/maple52046/swallow/internal/shared/apicontract"
	"github.com/maple52046/swallow/internal/shared/jwt"
	"github.com/maple52046/swallow/internal/shared/middleware"
)

// The route table's Users: an admin; bob, an owner of pool-a; alice and dave, members of it; carol,
// in no Pool. Server s-1 is in pool-a, s-9 in pool-b.
var routeUsers = map[string]*authdomain.User{
	"u-admin": {ID: "u-admin", Username: "admin", Role: authdomain.RoleAdmin},
	"u-bob":   {ID: "u-bob", Username: "bob", Role: authdomain.RoleUser},
	"u-alice": {ID: "u-alice", Username: "alice", Role: authdomain.RoleUser},
	"u-dave":  {ID: "u-dave", Username: "dave", Role: authdomain.RoleUser},
	"u-carol": {ID: "u-carol", Username: "carol", Role: authdomain.RoleUser},
}

// routeTestUsers finds the route table's Users; the Authorizer asks nothing else of it.
type routeTestUsers struct{ authdomain.UserRepository }

func (routeTestUsers) FindByID(_ context.Context, id string) (*authdomain.User, error) {
	if user, ok := routeUsers[id]; ok {
		return user, nil
	}
	return nil, authdomain.ErrUserNotFound
}

// routeTestMemberships holds the route table's memberships of pool-a.
type routeTestMemberships struct {
	accessdomain.MembershipRepository
}

func (routeTestMemberships) ListByUser(_ context.Context, userID string) ([]*accessdomain.Membership, error) {
	roles := map[string]accessdomain.PoolRole{"u-bob": accessdomain.RoleOwner, "u-alice": accessdomain.RoleMember, "u-dave": accessdomain.RoleMember}
	role, ok := roles[userID]
	if !ok {
		return nil, nil
	}
	return []*accessdomain.Membership{{PoolID: "pool-a", UserID: userID, Role: role}}, nil
}

// routeTestPlacement places s-1 in pool-a and s-9 in pool-b, and knows no Platform or Workflow.
type routeTestPlacement struct{}

func (routeTestPlacement) PoolsOf(_ context.Context, ids []string) (map[string]string, error) {
	placement := map[string]string{"s-1": "pool-a", "s-9": "pool-b"}
	out := map[string]string{}
	for _, id := range ids {
		if pool, ok := placement[id]; ok {
			out[id] = pool
		}
	}
	return out, nil
}

func (routeTestPlacement) ServersIn(_ context.Context, pools []string) ([]string, error) {
	var ids []string
	for _, pool := range pools {
		switch pool {
		case "pool-a":
			ids = append(ids, "s-1")
		case "pool-b":
			ids = append(ids, "s-9")
		}
	}
	return ids, nil
}

func (routeTestPlacement) PoolOf(context.Context, string) (string, bool, error) {
	return "", false, nil
}

func (routeTestPlacement) TargetsOf(context.Context, string) ([]string, []string, bool, error) {
	return nil, nil, false, nil
}

// routeTestTargets knows the deployed Servers s-1 and s-9.
type routeTestTargets struct{ validationapp.Targets }

func (routeTestTargets) Server(_ context.Context, id string) (validationapp.TargetServer, error) {
	if id != "s-1" && id != "s-9" {
		return validationapp.TargetServer{}, validationdomain.ErrTargetNotFound
	}
	return validationapp.TargetServer{ID: id, Name: id, SiteID: "site-a", Deployed: true}, nil
}

type routeTestLauncher struct{}

func (routeTestLauncher) PrepareRun(_ context.Context, request validationapp.LaunchRequest) (validationapp.PreparedRun, error) {
	return validationapp.PreparedRun{WorkflowID: "wf-" + request.Run.ID}, nil
}

func (routeTestLauncher) PersistRun(context.Context, validationapp.PreparedRun) error { return nil }

type routeTestCanceler struct{}

func (routeTestCanceler) CancelWorkflow(context.Context, string, string) error { return nil }

// routeTestStarters gives every User an account that may log in on its Pool's Servers, which have
// the directory client.
type routeTestStarters struct{}

func (routeTestStarters) StarterAccount(_ context.Context, userID string, serverIDs []string) (validationapp.StarterAccount, error) {
	user := routeUsers[userID]
	hosts := map[string]validationapp.StarterHost{}
	for _, id := range serverIDs {
		hosts[id] = validationapp.StarterHost{Login: id == "s-1", DirectoryClient: true}
	}
	return validationapp.StarterAccount{DirectoryConfigured: true, Username: user.Username, UIDNumber: 10005, Hosts: hosts}, nil
}

// routeTable is the API's real route table (registerRoutes) with the real authentication and
// authorization middleware, serving only the validation routes over in-memory stores.
type routeTable struct {
	t      *testing.T
	app    *fiber.App
	tokens *jwt.Service
}

func newRouteTable(t *testing.T) routeTable {
	t.Helper()
	users := validationtest.NewSuites()
	catalog, err := validationapp.NewCatalog([]validationdomain.Suite{{
		ID: "host-baseline", Source: validationdomain.SourceModule, Name: "Host baseline", Target: validationdomain.TargetServers,
		Impact: validationdomain.ImpactObserve, BudgetSeconds: 900, Module: &validationdomain.ModuleRef{Kind: "validation", ID: "host", Version: "1"},
		Checks: []validationdomain.Check{{ID: "time-sync", Title: "Time sync"}},
	}}, users)
	if err != nil {
		t.Fatal(err)
	}
	service, err := validationapp.NewService(validationapp.ServiceDeps{
		Transactor: &storetest.Transactor{}, Catalog: catalog, Users: users, Runs: validationtest.NewRuns(),
		Schedules: validationtest.NewSchedules(), Targets: routeTestTargets{}, Starters: routeTestStarters{},
		Launcher: routeTestLauncher{}, Canceler: routeTestCanceler{},
	})
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := accessapp.NewAuthorizer(accessapp.AuthorizerDeps{
		Users: routeTestUsers{}, Memberships: routeTestMemberships{}, Servers: routeTestPlacement{},
		Platforms: routeTestPlacement{}, Workflows: routeTestPlacement{},
	})
	if err != nil {
		t.Fatal(err)
	}
	tokens := jwt.NewService("route-table-test-secret", time.Hour)
	app := fiber.New()
	app.Use(apicontract.ResponseValidator(t))
	// The other features' handlers are nil: registerRoutes only stores their method values.
	registerRoutes(app, routeDeps{
		authenticator:  middleware.NewAuthenticator(tokens, nil),
		access:         &accessAPI{guard: accessdelivery.NewGuard(authorizer), handler: &accessdelivery.Handler{}},
		validation:     validationdelivery.NewHandler(service),
		validationRuns: validationRunAccess{runs: service},
	})
	return routeTable{t: t, app: app, tokens: tokens}
}

// do sends one request as userID and returns the status and the decoded body.
func (r routeTable) do(userID, method, path, body string) (int, map[string]any) {
	r.t.Helper()
	user := routeUsers[userID]
	token, err := r.tokens.Sign(user.ID, user.Username, string(user.Role))
	if err != nil {
		r.t.Fatal(err)
	}
	req := httptest.NewRequest(method, "/api/v1/validation"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := r.app.Test(req)
	if err != nil {
		r.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		r.t.Fatal(err)
	}
	decoded := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			r.t.Fatalf("%s %s: decode %q: %v", method, path, raw, err)
		}
	}
	return resp.StatusCode, decoded
}

// The validation routes' guards in api.go, end to end (contract validation.md, Authentication and
// Authorization): any User writes a user suite and only its author changes it; a run needs Pool read
// on its targets, then the suite's starter level; a run is canceled by its starter, an owner, or an
// admin; schedules stay admin.
func TestValidationRouteGuards(t *testing.T) {
	r := newRouteTable(t)
	status, body := r.do("u-alice", http.MethodPost, "/suites", `{"name":"id","script":"#!/bin/sh\nid\n","starterLevel":"member"}`)
	if status != http.StatusCreated {
		t.Fatalf("POST /suites by a member = %d %v, want 201", status, body)
	}
	suiteID, _ := body["id"].(string)
	member := `{"suiteId":"` + suiteID + `","serverIds":["s-1"]}`
	module := `{"suiteId":"host-baseline","serverIds":["s-1"]}`
	start := func(userID, request string) (int, string) {
		t.Helper()
		status, body := r.do(userID, http.MethodPost, "/runs", request)
		id, _ := body["id"].(string)
		return status, id
	}
	_, aliceRun := start("u-alice", member)
	_, aliceSecond := start("u-alice", member)
	for _, tc := range []struct {
		name, user, method, path, body string
		want                           int
	}{
		{name: "another user replaces the suite", user: "u-bob", method: http.MethodPut, path: "/suites/" + suiteID, body: `{"name":"x","script":"x"}`, want: http.StatusForbidden},
		{name: "an admin replaces the suite", user: "u-admin", method: http.MethodPut, path: "/suites/" + suiteID, body: `{"name":"x","script":"x"}`, want: http.StatusForbidden},
		{name: "anyone reads its revision", user: "u-carol", method: http.MethodGet, path: "/suites/" + suiteID + "/revisions/1", want: http.StatusOK},
		{name: "a member starts a member-level suite", user: "u-dave", method: http.MethodPost, path: "/runs", body: member, want: http.StatusAccepted},
		{name: "a member starts a module suite", user: "u-alice", method: http.MethodPost, path: "/runs", body: module, want: http.StatusForbidden},
		{name: "an owner starts a module suite", user: "u-bob", method: http.MethodPost, path: "/runs", body: module, want: http.StatusAccepted},
		{name: "a User outside the Pool starts on its Server", user: "u-carol", method: http.MethodPost, path: "/runs", body: member, want: http.StatusNotFound},
		{name: "a member starts on another Pool's Server", user: "u-alice", method: http.MethodPost, path: "/runs",
			body: `{"suiteId":"` + suiteID + `","serverIds":["s-9"]}`, want: http.StatusNotFound},
		{name: "another member cancels a run", user: "u-dave", method: http.MethodPost, path: "/runs/" + aliceRun + "/cancel", want: http.StatusForbidden},
		{name: "a User outside the Pool cancels a run", user: "u-carol", method: http.MethodPost, path: "/runs/" + aliceRun + "/cancel", want: http.StatusNotFound},
		{name: "an owner cancels a member's run", user: "u-bob", method: http.MethodPost, path: "/runs/" + aliceRun + "/cancel", want: http.StatusAccepted},
		{name: "the starter cancels its run", user: "u-alice", method: http.MethodPost, path: "/runs/" + aliceSecond + "/cancel", want: http.StatusAccepted},
		{name: "a member lists schedules", user: "u-alice", method: http.MethodGet, path: "/schedules", want: http.StatusForbidden},
		{name: "another user retires the suite", user: "u-bob", method: http.MethodDelete, path: "/suites/" + suiteID, want: http.StatusForbidden},
		{name: "an admin retires the suite", user: "u-admin", method: http.MethodDelete, path: "/suites/" + suiteID, want: http.StatusNoContent},
	} {
		if status, body := r.do(tc.user, tc.method, tc.path, tc.body); status != tc.want {
			t.Errorf("%s: %s %s = %d %v, want %d", tc.name, tc.method, tc.path, status, body, tc.want)
		}
	}
	if status, body := r.do("u-alice", http.MethodGet, "/runs?requestedBy=me", ""); status != http.StatusOK || body["total"] != float64(2) {
		t.Errorf("GET /runs?requestedBy=me by alice = %d %v, want her two runs", status, body)
	}
}
