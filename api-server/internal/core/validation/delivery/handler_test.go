package delivery

import (
	"context"
	"encoding/json"
	accessdelivery "github.com/maple52046/swallow/internal/core/access/delivery"
	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	"github.com/maple52046/swallow/internal/core/store/storetest"
	validationapp "github.com/maple52046/swallow/internal/core/validation/application"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
	"github.com/maple52046/swallow/internal/core/validation/domain/validationtest"
	"github.com/maple52046/swallow/internal/shared/apicontract"
	"github.com/maple52046/swallow/internal/shared/apierror"
)

// handlerTargets knows deployed Server s-1 and the active Slurm Platform p.
type handlerTargets struct{}

func (handlerTargets) Server(_ context.Context, id string) (validationapp.TargetServer, error) {
	if id != "s-1" {
		return validationapp.TargetServer{}, validationdomain.ErrTargetNotFound
	}
	return validationapp.TargetServer{ID: id, Name: id, SiteID: "site-a", Deployed: true}, nil
}

func (handlerTargets) Platform(_ context.Context, id string) (validationapp.TargetPlatform, error) {
	if id != "p" {
		return validationapp.TargetPlatform{}, validationdomain.ErrTargetNotFound
	}
	return validationapp.TargetPlatform{ID: id, Name: id, SiteID: "site-a", Type: "slurm", Active: true,
		AccessServerIDs: []string{"login"}, Members: []validationapp.PlatformMember{{ServerID: "login", Roles: []string{"login"}}}}, nil
}

type handlerLauncher struct{}

func (handlerLauncher) PrepareRun(_ context.Context, request validationapp.LaunchRequest) (validationapp.PreparedRun, error) {
	return validationapp.PreparedRun{WorkflowID: "wf-" + request.Run.ID}, nil
}

func (handlerLauncher) PersistRun(context.Context, validationapp.PreparedRun) error { return nil }

type handlerCanceler struct{}

func (handlerCanceler) CancelWorkflow(context.Context, string, string) error { return nil }

// handlerStarters gives every User an account that may log in on s-1, which has the directory
// client.
type handlerStarters struct{}

func (handlerStarters) StarterAccount(_ context.Context, userID string, serverIDs []string) (validationapp.StarterAccount, error) {
	hosts := map[string]validationapp.StarterHost{}
	for _, id := range serverIDs {
		hosts[id] = validationapp.StarterHost{Login: id == "s-1", DirectoryClient: true}
	}
	return validationapp.StarterAccount{DirectoryConfigured: true, Username: strings.TrimPrefix(userID, "u-"), UIDNumber: 10005, Hosts: hosts}, nil
}

// The callers of the handler tests. FixedScope consults no Authorizer, so a non-admin caller here
// only takes paths that need none: suite writes, member-level starts, and canceling its own run.
var (
	adminScope = accessdomain.Scope{UserID: "u-admin", Username: "admin", Admin: true}
	aliceScope = accessdomain.Scope{UserID: "u-alice", Username: "alice", Pools: map[string]accessdomain.PoolRole{"pool": accessdomain.RoleMember}}
	bobScope   = accessdomain.Scope{UserID: "u-bob", Username: "bob", Pools: map[string]accessdomain.PoolRole{"pool": accessdomain.RoleMember}}
)

// handlerApp is the routes behind the contract validator, acting for caller.
type handlerApp struct {
	*fiber.App
	caller accessdomain.Scope
}

// as switches the caller of the next requests.
func (a *handlerApp) as(scope accessdomain.Scope) *handlerApp {
	a.caller = scope
	return a
}

// newHandlerApp mounts the routes at their production prefix behind the contract validator, with a
// module suite for Servers, one for Slurm, and a disruptive one; the caller is an admin until a test
// switches it.
func newHandlerApp(t *testing.T) *handlerApp {
	t.Helper()
	users := validationtest.NewSuites()
	parametersJSON := []byte(`{"type":"object","additionalProperties":false,"properties":{"maxOffsetMs":{"type":"integer","title":"Largest clock offset (ms)","minimum":1}}}`)
	parameters, err := moduledomain.ParseSpecSchema(parametersJSON)
	if err != nil {
		t.Fatal(err)
	}
	module := func(id string, target validationdomain.TargetKind, impact validationdomain.Impact) validationdomain.Suite {
		suite := validationdomain.Suite{ID: id, Source: validationdomain.SourceModule, Name: id, Description: "A suite.",
			Target: target, Impact: impact, BudgetSeconds: 900,
			Module:     &validationdomain.ModuleRef{Kind: "validation", ID: "host", Version: "0.1.0"},
			Checks:     []validationdomain.Check{{ID: "time-sync", Title: "Time sync", Description: "Clock in sync.", Gating: true}},
			Parameters: parameters, ParametersJSON: parametersJSON,
		}
		if target == validationdomain.TargetPlatform {
			suite.PlatformType = "slurm"
		}
		return suite
	}
	baseline := module("host-baseline", validationdomain.TargetServers, validationdomain.ImpactObserve)
	baseline.Experimental = true
	catalog, err := validationapp.NewCatalog([]validationdomain.Suite{
		baseline, module("slurm-acceptance", validationdomain.TargetPlatform, validationdomain.ImpactWorkload),
	}, users)
	if err != nil {
		t.Fatal(err)
	}
	service, err := validationapp.NewService(validationapp.ServiceDeps{
		Transactor: &storetest.Transactor{}, Catalog: catalog, Users: users, Runs: validationtest.NewRuns(),
		Schedules: validationtest.NewSchedules(), Targets: handlerTargets{}, Starters: handlerStarters{}, Launcher: handlerLauncher{},
		Canceler: handlerCanceler{},
	})
	if err != nil {
		t.Fatal(err)
	}
	app := &handlerApp{App: fiber.New(), caller: adminScope}
	app.Use(apicontract.ResponseValidator(t))
	app.Use(func(c *fiber.Ctx) error { return accessdelivery.FixedScope(app.caller)(c) })
	NewHandler(service).RegisterRoutes(app.Group("/api/v1/validation"))
	return app
}

func do(t *testing.T, app *handlerApp, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, "/api/v1/validation"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	decoded := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("%s %s: decode %q: %v", method, path, raw, err)
		}
	}
	return resp.StatusCode, decoded
}

func errorCode(body map[string]any) any {
	errBody, _ := body["error"].(map[string]any)
	return errBody["code"]
}

// The catalog lists module and user suites; any User creates a user suite, only its author revises
// it, its revisions are read back, and its author or an admin retires it.
func TestSuiteRoutes(t *testing.T) {
	app := newHandlerApp(t).as(aliceScope)
	status, body := do(t, app, http.MethodPost, "/suites", `{"name":"mounts","script":"#!/bin/sh\nmountpoint -q /data\n","timeoutSeconds":120}`)
	if status != http.StatusCreated || body["source"] != "user" || body["revision"] != float64(1) || body["timeoutSeconds"] != float64(120) ||
		body["createdBy"] != "alice" || body["createdById"] != "u-alice" || body["starterLevel"] != "owner" || body["sudo"] != false {
		t.Fatalf("POST /suites = %d %v, want 201 with alice's user suite at revision 1", status, body)
	}
	id, _ := body["id"].(string)
	revise := `{"name":"mounts","script":"#!/bin/sh\nexit 0\n","starterLevel":"member","sudo":true}`
	for _, caller := range []accessdomain.Scope{bobScope, adminScope} {
		if status, body = do(t, app.as(caller), http.MethodPut, "/suites/"+id, revise); status != http.StatusForbidden || errorCode(body) != string(apierror.CodeForbidden) {
			t.Errorf("PUT /suites/%s by %s = %d %v, want 403", id, caller.Username, status, body)
		}
	}
	app.as(aliceScope)
	if status, body = do(t, app, http.MethodPut, "/suites/"+id, revise); status != http.StatusOK || body["revision"] != float64(2) ||
		body["starterLevel"] != "member" || body["sudo"] != true {
		t.Errorf("PUT /suites/%s = %d %v, want 200 at revision 2", id, status, body)
	}
	if status, body = do(t, app, http.MethodPut, "/suites/"+id, `{"name":"mounts","script":"#!/bin/sh\nexit 2\n","revision":1}`); status != http.StatusConflict {
		t.Errorf("PUT /suites/%s at a stale revision = %d %v, want 409", id, status, body)
	}
	if status, body = do(t, app, http.MethodGet, "/suites/"+id+"/revisions/1", ""); status != http.StatusOK || body["revision"] != float64(1) ||
		!strings.Contains(body["script"].(string), "mountpoint") || body["starterLevel"] != "owner" {
		t.Errorf("GET revision 1 = %d %v, want the first script", status, body)
	}
	if status, body = do(t, app, http.MethodGet, "/suites/"+id+"/revisions/2", ""); status != http.StatusOK || body["sudo"] != true {
		t.Errorf("GET revision 2 = %d %v, want the sudo revision", status, body)
	}
	for _, path := range []string{"/suites/" + id + "/revisions/3", "/suites/" + id + "/revisions/first", "/suites/host-baseline/revisions/1"} {
		if status, body = do(t, app, http.MethodGet, path, ""); status != http.StatusNotFound {
			t.Errorf("GET %s = %d %v, want 404", path, status, body)
		}
	}
	if status, body = do(t, app.as(bobScope), http.MethodDelete, "/suites/"+id, ""); status != http.StatusForbidden {
		t.Errorf("DELETE /suites/%s by another user = %d %v, want 403", id, status, body)
	}
	app.as(adminScope)
	if status, body = do(t, app, http.MethodGet, "/suites", ""); status != http.StatusOK || len(body["items"].([]any)) != 3 {
		t.Errorf("GET /suites = %d %v, want three suites", status, body)
	}
	if status, body = do(t, app, http.MethodGet, "/suites/host-baseline", ""); status != http.StatusOK || body["module"] == nil || body["experimental"] != true ||
		body["createdById"] != nil || body["starterLevel"] != nil || body["sudo"] != nil {
		t.Errorf("GET /suites/host-baseline = %d %v, want the experimental module suite without user-suite fields", status, body)
	}
	if status, body = do(t, app, http.MethodGet, "/suites/"+id, ""); status != http.StatusOK || body["experimental"] != false {
		t.Errorf("GET /suites/%s = %d %v, want a user suite that is not experimental", id, status, body)
	}
	if status, body = do(t, app, http.MethodPut, "/suites/host-baseline", `{"name":"x","script":"x"}`); status != http.StatusConflict || errorCode(body) != string(apierror.CodeConflict) {
		t.Errorf("PUT module suite = %d %v, want 409", status, body)
	}
	if status, body = do(t, app, http.MethodPost, "/suites", `{"name":"empty"}`); status != http.StatusBadRequest {
		t.Errorf("POST /suites without a script = %d %v, want 400", status, body)
	}
	if status, _ = do(t, app, http.MethodDelete, "/suites/"+id, ""); status != http.StatusNoContent {
		t.Errorf("DELETE /suites/%s by an admin = %d, want 204", id, status)
	}
	if status, body = do(t, app, http.MethodGet, "/suites/missing", ""); status != http.StatusNotFound {
		t.Errorf("GET unknown suite = %d %v, want 404", status, body)
	}
}

// A run is accepted as pending, read with its results, listed without them, and canceled.
func TestRunRoutes(t *testing.T) {
	app := newHandlerApp(t)
	status, body := do(t, app, http.MethodPost, "/runs", `{"suiteId":"host-baseline","serverIds":["s-1"],"parameters":{"maxOffsetMs":50}}`)
	if status != http.StatusAccepted || body["status"] != "pending" || body["mode"] != "report" {
		t.Fatalf("POST /runs = %d %v, want 202 pending report run", status, body)
	}
	id, _ := body["id"].(string)
	if status, body = do(t, app, http.MethodGet, "/runs/"+id, ""); status != http.StatusOK || body["results"] == nil {
		t.Errorf("GET /runs/%s = %d %v, want the run with results", id, status, body)
	}
	status, body = do(t, app, http.MethodGet, "/runs?active=true", "")
	items, _ := body["items"].([]any)
	if status != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["results"] != nil {
		t.Errorf("GET /runs = %d %v, want one summary without results", status, body)
	}
	if status, body = do(t, app, http.MethodGet, "/runs?siteId=site-a", ""); status != http.StatusOK || body["total"] != float64(1) {
		t.Errorf("GET /runs?siteId=site-a = %d %v, want one run", status, body)
	}
	if status, body = do(t, app, http.MethodGet, "/runs?siteId=unknown", ""); status != http.StatusOK || body["total"] != float64(0) {
		t.Errorf("GET /runs?siteId=unknown = %d %v, want an empty page", status, body)
	}
	if status, body = do(t, app, http.MethodGet, "/runs?status=finished", ""); status != http.StatusBadRequest {
		t.Errorf("GET /runs?status=finished = %d %v, want 400", status, body)
	}
	if status, body = do(t, app, http.MethodPost, "/runs/"+id+"/cancel", ""); status != http.StatusAccepted {
		t.Errorf("POST /runs/%s/cancel = %d %v, want 202", id, status, body)
	}
	if status, body = do(t, app, http.MethodPost, "/runs", `{"suiteId":"host-baseline","serverIds":["gone"]}`); status != http.StatusNotFound {
		t.Errorf("POST /runs on an unknown Server = %d %v, want 404", status, body)
	}
	if status, body = do(t, app, http.MethodPost, "/runs", `{"serverIds":["s-1"]}`); status != http.StatusBadRequest {
		t.Errorf("POST /runs without a suite = %d %v, want 400", status, body)
	}
	if status, body = do(t, app, http.MethodGet, "/runs?requestedBy=me", ""); status != http.StatusOK || body["total"] != float64(1) {
		t.Errorf("GET /runs?requestedBy=me = %d %v, want the admin's one run", status, body)
	}
	if status, body = do(t, app, http.MethodGet, "/runs?requestedBy=bob", ""); status != http.StatusBadRequest {
		t.Errorf("GET /runs?requestedBy=bob = %d %v, want 400", status, body)
	}
	if status, body = do(t, app, http.MethodPost, "/runs", `{"suiteId":"host-baseline","serverIds":["s-1"],"suiteRevision":1}`); status != http.StatusBadRequest {
		t.Errorf("POST /runs pinning a module suite = %d %v, want 400", status, body)
	}
}

// A member starts a member-level user suite as their own account, pinned to the revision they read,
// and cancels the run they started; a stale pin is a conflict.
func TestUserSuiteRunRoutes(t *testing.T) {
	app := newHandlerApp(t).as(bobScope)
	status, body := do(t, app, http.MethodPost, "/suites", `{"name":"id","script":"#!/bin/sh\nid\n","starterLevel":"member"}`)
	if status != http.StatusCreated {
		t.Fatalf("POST /suites = %d %v", status, body)
	}
	suiteID, _ := body["id"].(string)
	app.as(aliceScope)
	status, body = do(t, app, http.MethodPost, "/runs", `{"suiteId":"`+suiteID+`","serverIds":["s-1"],"suiteRevision":1}`)
	runAs, _ := body["runAs"].(map[string]any)
	if status != http.StatusAccepted || body["requestedBy"] != "alice" || body["requestedById"] != "u-alice" ||
		runAs["username"] != "alice" || runAs["uidNumber"] != float64(10005) || runAs["sudo"] != false {
		t.Fatalf("POST /runs = %d %v, want 202 run as alice", status, body)
	}
	runID, _ := body["id"].(string)
	if status, body = do(t, app, http.MethodPost, "/runs", `{"suiteId":"`+suiteID+`","serverIds":["s-1"],"suiteRevision":2}`); status != http.StatusConflict {
		t.Errorf("POST /runs at a revision the suite is not at = %d %v, want 409", status, body)
	}
	if status, body = do(t, app, http.MethodPost, "/runs/"+runID+"/cancel", ""); status != http.StatusAccepted {
		t.Errorf("POST /runs/%s/cancel by its starter = %d %v, want 202", runID, status, body)
	}
}

// A cron schedule is created, read, listed, replaced at its generation, and deleted.
func TestScheduleRoutes(t *testing.T) {
	app := newHandlerApp(t)
	status, body := do(t, app, http.MethodPost, "/schedules",
		`{"name":"nightly","suiteId":"slurm-acceptance","platformId":"p","trigger":{"kind":"cron","cron":"0 2 * * *","timezone":"Asia/Taipei"}}`)
	if status != http.StatusCreated || body["generation"] != float64(1) {
		t.Fatalf("POST /schedules = %d %v, want 201 at generation 1", status, body)
	}
	id, _ := body["id"].(string)
	if status, body = do(t, app, http.MethodGet, "/schedules/"+id, ""); status != http.StatusOK {
		t.Errorf("GET /schedules/%s = %d %v", id, status, body)
	}
	if status, body = do(t, app, http.MethodGet, "/schedules", ""); status != http.StatusOK || len(body["items"].([]any)) != 1 {
		t.Errorf("GET /schedules = %d %v, want one schedule", status, body)
	}
	if status, body = do(t, app, http.MethodGet, "/schedules?siteId=site-a", ""); status != http.StatusOK || len(body["items"].([]any)) != 1 {
		t.Errorf("GET /schedules?siteId=site-a = %d %v, want one schedule", status, body)
	}
	if status, body = do(t, app, http.MethodGet, "/schedules?siteId=unknown", ""); status != http.StatusOK || len(body["items"].([]any)) != 0 {
		t.Errorf("GET /schedules?siteId=unknown = %d %v, want an empty list", status, body)
	}
	replace := `{"name":"nightly","suiteId":"slurm-acceptance","platformId":"p","trigger":{"kind":"cron","cron":"0 3 * * *","timezone":"UTC"},"enabled":false,"generation":%d}`
	if status, body = do(t, app, http.MethodPut, "/schedules/"+id, strings.Replace(replace, "%d", "1", 1)); status != http.StatusOK || body["generation"] != float64(2) {
		t.Errorf("PUT /schedules/%s = %d %v, want 200 at generation 2", id, status, body)
	}
	if status, body = do(t, app, http.MethodPut, "/schedules/"+id, strings.Replace(replace, "%d", "1", 1)); status != http.StatusConflict {
		t.Errorf("PUT a stale generation = %d %v, want 409", status, body)
	}
	if status, body = do(t, app, http.MethodPost, "/schedules", `{"name":"bad","suiteId":"host-baseline","serverIds":["s-1"],"trigger":{"kind":"cron","cron":"61 * * * *"}}`); status != http.StatusBadRequest {
		t.Errorf("POST a bad cron = %d %v, want 400", status, body)
	}
	if status, _ = do(t, app, http.MethodDelete, "/schedules/"+id, ""); status != http.StatusNoContent {
		t.Errorf("DELETE /schedules/%s = %d, want 204", id, status)
	}
}
