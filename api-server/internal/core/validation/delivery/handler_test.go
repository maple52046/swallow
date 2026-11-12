package delivery

import (
	"context"
	"encoding/json"
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

// newHandlerApp mounts the routes at their production prefix behind the contract validator, with a
// module suite for Servers, one for Slurm, and a disruptive one.
func newHandlerApp(t *testing.T) *fiber.App {
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
		Schedules: validationtest.NewSchedules(), Targets: handlerTargets{}, Launcher: handlerLauncher{}, Canceler: handlerCanceler{},
	})
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Use(apicontract.ResponseValidator(t))
	NewHandler(service).RegisterRoutes(app.Group("/api/v1/validation"))
	return app
}

func do(t *testing.T, app *fiber.App, method, path, body string) (int, map[string]any) {
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

// The catalog lists module and user suites; a user suite is created, revised, read, and retired.
func TestSuiteRoutes(t *testing.T) {
	app := newHandlerApp(t)
	status, body := do(t, app, http.MethodPost, "/suites", `{"name":"mounts","script":"#!/bin/sh\nmountpoint -q /data\n","timeoutSeconds":120}`)
	if status != http.StatusCreated || body["source"] != "user" || body["revision"] != float64(1) || body["timeoutSeconds"] != float64(120) {
		t.Fatalf("POST /suites = %d %v, want 201 with the user suite at revision 1", status, body)
	}
	id, _ := body["id"].(string)
	if status, body = do(t, app, http.MethodPut, "/suites/"+id, `{"name":"mounts","script":"#!/bin/sh\nexit 0\n"}`); status != http.StatusOK || body["revision"] != float64(2) {
		t.Errorf("PUT /suites/%s = %d %v, want 200 at revision 2", id, status, body)
	}
	if status, body = do(t, app, http.MethodGet, "/suites", ""); status != http.StatusOK || len(body["items"].([]any)) != 3 {
		t.Errorf("GET /suites = %d %v, want three suites", status, body)
	}
	if status, body = do(t, app, http.MethodGet, "/suites/host-baseline", ""); status != http.StatusOK || body["module"] == nil || body["experimental"] != true {
		t.Errorf("GET /suites/host-baseline = %d %v, want the experimental module suite", status, body)
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
		t.Errorf("DELETE /suites/%s = %d, want 204", id, status)
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
