package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	"github.com/maple52046/swallow/internal/core/store/storetest"
	"github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	"github.com/maple52046/swallow/internal/shared/apicontract"
)

// recordingAutomationRepo records the credential the handler asks the service to store.
type recordingAutomationRepo struct {
	workflowdomain.AutomationConfigurationRepository
	stored *workflowdomain.AutomationCredential
}

func (r *recordingAutomationRepo) ReplaceCredential(_ context.Context, _ string, credential workflowdomain.AutomationCredential) error {
	r.stored = &credential
	return nil
}

// TestPutAutomationCredential covers the site-automation contract after decision 041: the body
// carries only becomePassword, and any sshPrivateKey value is refused so no caller believes a
// Site key override took effect.
func TestPutAutomationCredential(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantStatus   int
		wantCode     string
		wantStored   bool
		wantPassword string
	}{
		{name: "become password is stored", body: `{"becomePassword":"pw"}`,
			wantStatus: http.StatusNoContent, wantStored: true, wantPassword: "pw"},
		{name: "empty body clears the credential", body: `{}`,
			wantStatus: http.StatusNoContent, wantStored: true},
		{name: "null sshPrivateKey is treated as absent", body: `{"sshPrivateKey":null,"becomePassword":"pw"}`,
			wantStatus: http.StatusNoContent, wantStored: true, wantPassword: "pw"},
		{name: "a private key is refused", body: `{"sshPrivateKey":"-----BEGIN OPENSSH PRIVATE KEY-----","becomePassword":"pw"}`,
			wantStatus: http.StatusBadRequest, wantCode: "validation_error"},
		{name: "an empty private key is refused too", body: `{"sshPrivateKey":""}`,
			wantStatus: http.StatusBadRequest, wantCode: "validation_error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &recordingAutomationRepo{}
			handler := NewExecutionHandler(nil, application.NewAutomationConfigurationService(repo, nil, nil))
			app := fiber.New()
			app.Use(apicontract.ResponseValidator(t))
			app.Put("/api/v1/sites/:id/automation/credential", handler.PutAutomationCredential)

			req := httptest.NewRequest(http.MethodPut, "/api/v1/sites/site-1/automation/credential", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("PUT credential %s: %v", tc.body, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("PUT credential %s status = %d, want %d", tc.body, resp.StatusCode, tc.wantStatus)
			}
			if tc.wantCode != "" {
				raw, _ := io.ReadAll(resp.Body)
				var envelope struct {
					Error struct {
						Code    string `json:"code"`
						Message string `json:"message"`
					} `json:"error"`
				}
				if err := json.Unmarshal(raw, &envelope); err != nil {
					t.Fatalf("decode error envelope %q: %v", raw, err)
				}
				if envelope.Error.Code != tc.wantCode || !strings.Contains(envelope.Error.Message, "Deployment Key") {
					t.Errorf("error = %+v, want code %q naming the Deployment Key", envelope.Error, tc.wantCode)
				}
			}
			if (repo.stored != nil) != tc.wantStored {
				t.Fatalf("stored = %+v, want stored %v", repo.stored, tc.wantStored)
			}
			if repo.stored != nil && (repo.stored.SSHPrivateKey != "" || repo.stored.BecomePassword != tc.wantPassword) {
				t.Errorf("stored credential = %+v, want only become password %q", repo.stored, tc.wantPassword)
			}
		})
	}
}

// contractWorkflowRepo is an in-memory schema-v3 store for the contract test. List honors only
// the filters the handler and WorkflowService.Create's busy-target check rely on (ActiveOnly and
// ServerID), and returns Workflows in ID order so listings are deterministic.
type contractWorkflowRepo struct {
	workflowdomain.WorkflowRepository
	workflows map[string]*workflowdomain.Workflow
	timeline  []workflowdomain.TimelineEvent
}

func (r *contractWorkflowRepo) FindByID(_ context.Context, id string) (*workflowdomain.Workflow, error) {
	workflow, ok := r.workflows[id]
	if !ok {
		return nil, workflowdomain.ErrWorkflowNotV3
	}
	return workflow, nil
}

func (r *contractWorkflowRepo) List(_ context.Context, filter workflowdomain.WorkflowFilter) ([]*workflowdomain.Workflow, int, error) {
	ids := make([]string, 0, len(r.workflows))
	for id := range r.workflows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var matched []*workflowdomain.Workflow
	for _, id := range ids {
		workflow := r.workflows[id]
		if filter.ActiveOnly && workflow.Status.Terminal() {
			continue
		}
		if filter.ServerID != "" && !slices.Contains(workflow.TargetServerIDs, filter.ServerID) {
			continue
		}
		matched = append(matched, workflow)
	}
	return matched, len(matched), nil
}

func (r *contractWorkflowRepo) Create(_ context.Context, workflow *workflowdomain.Workflow) error {
	r.workflows[workflow.ID] = workflow
	return nil
}

func (r *contractWorkflowRepo) AppendEvent(context.Context, workflowdomain.TimelineEvent) error {
	return nil
}

func (r *contractWorkflowRepo) Timeline(context.Context, string) ([]workflowdomain.TimelineEvent, error) {
	return r.timeline, nil
}

// RequestCancel and RequestRetry store a command the way the store does; the contract test only
// checks the responses.
func (r *contractWorkflowRepo) RequestCancel(_ context.Context, id string, command workflowdomain.CancelCommand) error {
	if w := r.workflows[id]; w != nil && w.Commands.Cancel == nil {
		w.Commands.Cancel = &command
		w.Commands.Sequence++
	}
	return nil
}

func (r *contractWorkflowRepo) RequestRetry(_ context.Context, id string, command workflowdomain.RetryCommand) error {
	if w := r.workflows[id]; w != nil {
		w.Commands.Retries = append(w.Commands.Retries, command)
		w.Commands.Sequence++
	}
	return nil
}

// contractOwnership reports one owned resource for any Workflow so GET by ID exercises the
// leases array.
type contractOwnership struct {
	workflowdomain.OwnershipRepository
	owned []workflowdomain.ResourceOwnership
}

func (o contractOwnership) FindByOwner(context.Context, string) ([]workflowdomain.ResourceOwnership, error) {
	return o.owned, nil
}

// contractSecrets lets Rerun clone an Operation that sealed no secrets.
type contractSecrets struct {
	workflowdomain.OperationSecretRepository
}

func (contractSecrets) CloneForOperation(context.Context, string, string) (map[string]string, error) {
	return map[string]string{}, nil
}

// contractExecutionRepo serves historical schema-v2 records; none of their targets is busy.
type contractExecutionRepo struct {
	workflowdomain.ExecutionRepository
	operations map[string]*workflowdomain.ExecutionOperation
}

func (r *contractExecutionRepo) FindByID(_ context.Context, id string) (*workflowdomain.ExecutionOperation, error) {
	operation, ok := r.operations[id]
	if !ok {
		return nil, workflowdomain.ErrWorkflowNotFound
	}
	return operation, nil
}

func (r *contractExecutionRepo) List(context.Context, workflowdomain.ExecutionListFilter) (workflowdomain.ExecutionListResult, error) {
	result := workflowdomain.ExecutionListResult{}
	for _, operation := range r.operations {
		result.Operations = append(result.Operations, operation)
	}
	result.Total = len(result.Operations)
	return result, nil
}

func (r *contractExecutionRepo) FindActiveByServerIDs(context.Context, []string) ([]*workflowdomain.ExecutionOperation, error) {
	return nil, nil
}

func (r *contractExecutionRepo) SecretVars(context.Context, string) (map[string]any, error) {
	return nil, nil
}

// contractServers places every Server in site-1 with no provisioning state, which the custom kind
// accepts.
type contractServers struct {
	serverdomain.ServerRepository
}

func (contractServers) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	return &serverdomain.Server{ID: id, Source: serverdomain.Source{SiteID: "site-1"}}, nil
}

// contractAutomation reports site-1 as runnable: enabled, with the Deployment Key present.
type contractAutomation struct {
	workflowdomain.AutomationConfigurationRepository
}

func (contractAutomation) FindBySiteID(_ context.Context, siteID string) (*workflowdomain.AutomationConfiguration, error) {
	return &workflowdomain.AutomationConfiguration{
		SiteID: siteID, Enabled: true, CredentialSource: workflowdomain.CredentialSourceDeploymentKey,
	}, nil
}

// contractCatalog registers every playbook name in the release manifest.
type contractCatalog struct{}

func (contractCatalog) Resolve(name string) (string, error) { return "playbooks/" + name, nil }

// contractRunner serves fixed retained output and per-host results for any run.
type contractRunner struct {
	workflowdomain.Runner
	events []workflowdomain.TaskEvent
}

func (contractRunner) Logs(context.Context, string) (string, error) {
	return "PLAY [all] ****\n", nil
}

func (contractRunner) Stderr(context.Context, string) (string, error) {
	return "TASK [Download k0s] on server-1: failed\n", nil
}

func (r contractRunner) Events(context.Context, string) ([]workflowdomain.TaskEvent, error) {
	return r.events, nil
}

// newWorkflowContractHandler builds an ExecutionHandler over fresh fakes holding a running
// schema-v3 Workflow (wf-running: an ansible Task mid-run with a retryable error, live progress,
// and an artifact, plus Tasks of the other Runners), a failed one (wf-failed), and an
// indeterminate schema-v2 record (op-legacy). Nullable and omitted-when-empty fields appear both
// set and unset across the fixtures, so the validator sees every shape a consumer can receive;
// Task parameters, secret references, and an artifact's relative key are set too, and the closed
// response schemas fail the test if any of them is ever emitted. Every call returns independent
// state because Create, Rerun, and Retry write to the store.
func newWorkflowContractHandler() *ExecutionHandler {
	now := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	started := now.Add(time.Minute)
	finished := now.Add(2 * time.Minute)
	server := func(id string) []workflowdomain.ResourceReference {
		return []workflowdomain.ResourceReference{{Kind: "server", ID: id}}
	}
	running := &workflowdomain.Workflow{
		ID: "wf-running", SchemaVersion: 3, Kind: workflowdomain.WorkflowKindDeployKubernetes,
		Intent: map[string]any{
			"summary":   "Deploy k0s",
			"extraVars": map[string]any{"token": "plaintext"},
			"platform":  map[string]any{"name": "prod", "password": "plaintext"},
		},
		Definition: "deploy-kubernetes", DefinitionVersion: 2,
		Status: workflowdomain.WorkflowRunning, StartState: "started",
		Temporal: workflowdomain.TemporalReference{WorkflowID: "swallow-operation/wf-running", RunID: "temporal-run"},
		SiteID:   "site-1", PlatformID: "platform-1",
		TargetResources: append(server("server-1"), workflowdomain.ResourceReference{Kind: "platform", ID: "platform-1"}),
		TargetServerIDs: []string{"server-1"},
		Steps: []workflowdomain.Task{
			{
				ID: "wait-for-ssh", Kind: "wait-for-ssh", Name: "Wait for SSH", Job: "ensure-os",
				Executor: workflowdomain.RunnerKindInternal, Targets: server("server-1"),
				Status: workflowdomain.TaskSucceeded, Attempt: 1, Progress: 100,
				StartedAt: &started, FinishedAt: &started,
			},
			{
				ID: "install-platform", Kind: "ansible-playbook", Name: "Install k0s",
				Executor: workflowdomain.RunnerKindAnsible, DependsOn: []string{"wait-for-ssh"},
				ContinueOn: []workflowdomain.TaskStatus{workflowdomain.TaskSkipped}, Targets: server("server-1"),
				Parameters: map[string]any{"extraVars": map[string]any{"token": "plaintext"}},
				SecretRefs: map[string]string{"token": "secret-ref"},
				Status:     workflowdomain.TaskFailed, Attempt: 2,
				Error: &workflowdomain.NormalizedError{
					Code: "ansible_failed", Message: "Download k0s failed.", Retryable: true, Stage: "run",
				},
				ExternalExecution: &workflowdomain.ExternalExecutionReference{Provider: "ansible-runner", ID: "ansible-run", Generation: 2},
				Live: &workflowdomain.TaskLive{
					CurrentPlay: "Install k0s", CurrentTask: "Download k0s",
					Total: 3, OK: 2, Changed: 1, Failed: 1, UpdatedAt: started,
				},
				Artifacts: []workflowdomain.ArtifactMetadata{{
					ID: "stdout", Name: "stdout", MediaType: "text/plain", SizeBytes: 2048,
					RelativeKey: "ansible-run/stdout", CreatedAt: started,
				}},
				StartedAt: &started,
			},
			{
				ID: "provision-os", Kind: "provision-os", Name: "Provision OS",
				Executor: workflowdomain.RunnerKindProvisioner, DependsOn: []string{"install-platform"},
				Targets: server("server-1"), Status: workflowdomain.TaskWaitingDependency,
				WaitingReason: "waiting for install-platform",
			},
		},
		RequestedBy: "admin", RequestCorrelation: "request-1",
		RequestedAt: now, StartedAt: &started, UpdatedAt: started,
	}
	failed := &workflowdomain.Workflow{
		ID: "wf-failed", SchemaVersion: 3, Kind: workflowdomain.WorkflowKindDeployOS,
		Intent:     map[string]any{"summary": "Deploy Ubuntu"},
		Definition: "deploy-os", DefinitionVersion: 1,
		Status: workflowdomain.WorkflowFailed, StatusReason: "Provision OS failed.", StartState: "started",
		Temporal: workflowdomain.TemporalReference{WorkflowID: "swallow-operation/wf-failed"},
		SiteID:   "site-1", TargetResources: server("server-2"), TargetServerIDs: []string{"server-2"},
		Steps: []workflowdomain.Task{{
			ID: "provision-os", Kind: "provision-os", Name: "Provision OS",
			Executor: workflowdomain.RunnerKindProvisioner, Targets: server("server-2"),
			Status: workflowdomain.TaskFailed, Attempt: 1,
			Error:     &workflowdomain.NormalizedError{Code: "deployment_failed", Message: "The provider reported Failed deployment."},
			StartedAt: &started, FinishedAt: &finished,
		}},
		RetryOfOperationID: "wf-older", RequestedBy: "admin",
		RequestedAt: now, StartedAt: &started, FinishedAt: &finished, UpdatedAt: finished,
	}
	legacy := &workflowdomain.ExecutionOperation{
		ID: "op-legacy", Kind: workflowdomain.WorkflowKindCustom, Intent: "verify SSH connectivity",
		SiteID: "site-1", TargetServerIDs: []string{"server-3"},
		ExtraVars: map[string]any{"swallow_operation_id": "op-legacy", "probe": true},
		Execution: workflowdomain.Execution{
			RunID: "legacy-run", Playbook: "diagnostic-ping", Status: workflowdomain.StatusIndeterminate,
			StatusReason: "The API stopped while the run held its lease.", StartedAt: &started,
		},
		RequestedBy: "admin", RequestedAt: now.Add(-time.Hour), UpdatedAt: now,
	}

	workflows := &contractWorkflowRepo{
		workflows: map[string]*workflowdomain.Workflow{running.ID: running, failed.ID: failed},
		timeline: []workflowdomain.TimelineEvent{
			{ID: "event-1", OperationID: running.ID, Type: "operation_requested", Message: "Operation accepted.", CreatedAt: now},
			{
				ID: "event-2", OperationID: running.ID, StepID: "install-platform", Type: "step_status",
				Message: "Install k0s changed to failed.", CreatedAt: started,
				Details: map[string]any{"status": "failed", "attempt": 2, "code": "ansible_failed", "retryable": true},
			},
		},
	}
	orchestrations := application.NewWorkflowService(&storetest.Transactor{}, workflows, workflowdomain.PlanCatalog{}, contractSecrets{})
	orchestrations.AttachOwnershipReader(contractOwnership{owned: []workflowdomain.ResourceOwnership{{
		ResourceKey: "server:server-1", Owner: running.ID, AcquiredAt: started,
	}}})
	runner := contractRunner{events: []workflowdomain.TaskEvent{
		{Play: "Install k0s", Task: "Download k0s", Host: "server-1", Status: "ok", Changed: true, StartedAt: &started, EndedAt: &finished},
		{Play: "Install k0s", Task: "Start k0s", Host: "server-1", Status: "failed"},
	}}
	operations := application.NewExecutionService(
		&contractExecutionRepo{operations: map[string]*workflowdomain.ExecutionOperation{legacy.ID: legacy}},
		contractServers{}, contractAutomation{}, contractCatalog{}, runner, nil, nil,
	)
	operations.AttachWorkflow(orchestrations)
	return NewExecutionHandler(operations, nil, orchestrations)
}

// newWorkflowContractApp mounts handler at the production paths of the /workflows surface and its
// deprecated /operations alias (internal/app registerRoutes), behind the API contract validator,
// so every response is checked against the operation documented for its route. Authentication
// is left out: the handlers read no principal beyond an optional requester name.
func newWorkflowContractApp(t *testing.T, handler *ExecutionHandler) *fiber.App {
	t.Helper()
	app := fiber.New()
	app.Use(apicontract.ResponseValidator(t))

	workflows := app.Group("/api/v1/workflows")
	workflows.Post("/", handler.Create)
	workflows.Get("/", handler.List)
	workflows.Get("/:id", handler.Get)
	workflows.Get("/:id/timeline", handler.Timeline)
	workflows.Post("/:id/cancel", handler.Cancel)
	workflows.Post("/:id/rerun", handler.Rerun)
	workflows.Post("/:id/tasks/:taskId/retry", handler.RetryStep)
	workflows.Get("/:id/tasks/:taskId/logs", handler.StepLogs)
	workflows.Get("/:id/tasks/:taskId/stderr", handler.StepStderr)
	workflows.Get("/:id/tasks/:taskId/events", handler.StepEvents)
	workflows.Get("/:id/tasks/:taskId/artifacts", handler.StepArtifacts)

	operations := app.Group("/api/v1/operations")
	operations.Use(func(c *fiber.Ctx) error {
		c.Set("Deprecation", "true")
		c.Set("Link", `</api/v1/workflows>; rel="successor-version"`)
		return c.Next()
	})
	operations.Post("/", handler.Create)
	operations.Get("/", handler.List)
	operations.Get("/:id", handler.Get)
	operations.Get("/:id/timeline", handler.Timeline)
	operations.Post("/:id/cancel", handler.Cancel)
	operations.Post("/:id/rerun", handler.Rerun)
	operations.Post("/:id/steps/:stepId/retry", handler.RetryStep)
	operations.Get("/:id/steps/:stepId/logs", handler.StepLogs)
	operations.Get("/:id/steps/:stepId/stderr", handler.StepStderr)
	operations.Get("/:id/steps/:stepId/events", handler.StepEvents)
	operations.Get("/:id/steps/:stepId/artifacts", handler.StepArtifacts)
	operations.Get("/:id/logs", handler.Logs)
	operations.Get("/:id/events", handler.Events)
	operations.Post("/:id/retry", handler.Retry)
	return app
}

// TestWorkflowRoutesMatchTheContract drives every Workflow route, on the canonical surface and the
// deprecated alias, through each response shape it can produce (schema-v3 and schema-v2 records,
// text and JSON diagnostics, accepted controls, and the documented error statuses). The contract
// validator fails the test when a response carries an undocumented status, Content-Type, or
// property, which is how the workflows contract is kept equal to what the handlers emit.
func TestWorkflowRoutesMatchTheContract(t *testing.T) {
	const customBody = `{"kind":"custom","intent":"verify SSH","targetServerIds":["server-9"],` +
		`"platformId":null,"playbookName":"diagnostic-ping","extraVars":{"probe":true}}`
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{name: "create returns the compatibility shape", method: http.MethodPost, path: "/api/v1/workflows", body: customBody, wantStatus: http.StatusAccepted},
		{name: "create rejects an unknown kind", method: http.MethodPost, path: "/api/v1/workflows", body: `{"kind":"nope","targetServerIds":["server-9"]}`, wantStatus: http.StatusBadRequest},
		{name: "list merges both schema versions", method: http.MethodGet, path: "/api/v1/workflows", wantStatus: http.StatusOK},
		{name: "list pages active work", method: http.MethodGet, path: "/api/v1/workflows?active=true&page=1&pageSize=1", wantStatus: http.StatusOK},
		{name: "get returns a Workflow with its leases", method: http.MethodGet, path: "/api/v1/workflows/wf-running", wantStatus: http.StatusOK},
		{name: "get returns a finished Workflow", method: http.MethodGet, path: "/api/v1/workflows/wf-failed", wantStatus: http.StatusOK},
		{name: "get returns a schema-v2 record", method: http.MethodGet, path: "/api/v1/workflows/op-legacy", wantStatus: http.StatusOK},
		{name: "get of an unknown ID is not found", method: http.MethodGet, path: "/api/v1/workflows/missing", wantStatus: http.StatusNotFound},
		{name: "timeline lists events", method: http.MethodGet, path: "/api/v1/workflows/wf-running/timeline", wantStatus: http.StatusOK},
		{name: "a schema-v2 record has no timeline", method: http.MethodGet, path: "/api/v1/workflows/op-legacy/timeline", wantStatus: http.StatusNotFound},
		{name: "cancel is accepted", method: http.MethodPost, path: "/api/v1/workflows/wf-running/cancel", wantStatus: http.StatusAccepted},
		{name: "cancel of a finished Workflow conflicts", method: http.MethodPost, path: "/api/v1/workflows/wf-failed/cancel", wantStatus: http.StatusConflict},
		{name: "rerun returns the new Workflow", method: http.MethodPost, path: "/api/v1/workflows/wf-failed/rerun", wantStatus: http.StatusAccepted},
		{name: "rerun of an advancing Workflow conflicts", method: http.MethodPost, path: "/api/v1/workflows/wf-running/rerun", wantStatus: http.StatusConflict},
		{name: "retry of a retryable Task is accepted", method: http.MethodPost, path: "/api/v1/workflows/wf-running/tasks/install-platform/retry", wantStatus: http.StatusAccepted},
		{name: "retry of a non-retryable Task conflicts", method: http.MethodPost, path: "/api/v1/workflows/wf-failed/tasks/provision-os/retry", wantStatus: http.StatusConflict},
		{name: "Task logs are plain text", method: http.MethodGet, path: "/api/v1/workflows/wf-running/tasks/install-platform/logs", wantStatus: http.StatusOK},
		{name: "a non-Ansible Task has empty logs", method: http.MethodGet, path: "/api/v1/workflows/wf-running/tasks/wait-for-ssh/logs", wantStatus: http.StatusOK},
		{name: "Task stderr is plain text", method: http.MethodGet, path: "/api/v1/workflows/wf-running/tasks/install-platform/stderr", wantStatus: http.StatusOK},
		{name: "Task events list per-host results", method: http.MethodGet, path: "/api/v1/workflows/wf-running/tasks/install-platform/events", wantStatus: http.StatusOK},
		{name: "a non-Ansible Task has no events", method: http.MethodGet, path: "/api/v1/workflows/wf-running/tasks/provision-os/events", wantStatus: http.StatusOK},
		{name: "Task artifacts are metadata", method: http.MethodGet, path: "/api/v1/workflows/wf-running/tasks/install-platform/artifacts", wantStatus: http.StatusOK},
		{name: "an unknown Task is not found", method: http.MethodGet, path: "/api/v1/workflows/wf-running/tasks/missing/artifacts", wantStatus: http.StatusNotFound},
		{name: "alias create", method: http.MethodPost, path: "/api/v1/operations", body: customBody, wantStatus: http.StatusAccepted},
		{name: "alias list", method: http.MethodGet, path: "/api/v1/operations", wantStatus: http.StatusOK},
		{name: "alias get", method: http.MethodGet, path: "/api/v1/operations/op-legacy", wantStatus: http.StatusOK},
		{name: "alias timeline", method: http.MethodGet, path: "/api/v1/operations/wf-running/timeline", wantStatus: http.StatusOK},
		{name: "alias cancel", method: http.MethodPost, path: "/api/v1/operations/wf-running/cancel", wantStatus: http.StatusAccepted},
		{name: "alias rerun", method: http.MethodPost, path: "/api/v1/operations/wf-failed/rerun", wantStatus: http.StatusAccepted},
		{name: "alias step retry", method: http.MethodPost, path: "/api/v1/operations/wf-running/steps/install-platform/retry", wantStatus: http.StatusAccepted},
		{name: "alias step logs", method: http.MethodGet, path: "/api/v1/operations/wf-running/steps/install-platform/logs", wantStatus: http.StatusOK},
		{name: "alias step stderr", method: http.MethodGet, path: "/api/v1/operations/wf-running/steps/install-platform/stderr", wantStatus: http.StatusOK},
		{name: "alias step events", method: http.MethodGet, path: "/api/v1/operations/wf-running/steps/install-platform/events", wantStatus: http.StatusOK},
		{name: "alias step artifacts", method: http.MethodGet, path: "/api/v1/operations/wf-running/steps/install-platform/artifacts", wantStatus: http.StatusOK},
		{name: "schema-v2 logs are plain text", method: http.MethodGet, path: "/api/v1/operations/op-legacy/logs", wantStatus: http.StatusOK},
		// A Workflow exposes logs and events per Task; the legacy operation-level endpoints refuse it
		// with a conflict rather than falling through to the schema-v2 reader's not_found (DF-05).
		{name: "schema-v2 logs of a Workflow conflict", method: http.MethodGet, path: "/api/v1/operations/wf-running/logs", wantStatus: http.StatusConflict},
		{name: "schema-v2 events list per-host results", method: http.MethodGet, path: "/api/v1/operations/op-legacy/events", wantStatus: http.StatusOK},
		{name: "schema-v2 events of a Workflow conflict", method: http.MethodGet, path: "/api/v1/operations/wf-running/events", wantStatus: http.StatusConflict},
		{name: "schema-v2 retry creates a Workflow", method: http.MethodPost, path: "/api/v1/operations/op-legacy/retry", wantStatus: http.StatusAccepted},
		{name: "schema-v2 retry of a Workflow conflicts", method: http.MethodPost, path: "/api/v1/operations/wf-running/retry", wantStatus: http.StatusConflict},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := newWorkflowContractApp(t, newWorkflowContractHandler())
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("%s %s: %v", tc.method, tc.path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				raw, _ := io.ReadAll(resp.Body)
				t.Errorf("%s %s status = %d, want %d; body %s", tc.method, tc.path, resp.StatusCode, tc.wantStatus, raw)
			}
		})
	}
}

// noSites is a Site store that knows no Site.
type noSites struct{ sitedomain.SiteRepository }

func (noSites) FindByID(context.Context, string) (*sitedomain.Site, error) {
	return nil, sitedomain.ErrSiteNotFound
}

// DF-05: configuring automation for a Site that does not exist is 404, not an internal error.
func TestPutAutomationOfAnUnknownSiteIsNotFound(t *testing.T) {
	handler := NewExecutionHandler(nil, application.NewAutomationConfigurationService(&recordingAutomationRepo{}, noSites{}, nil))
	app := fiber.New()
	app.Use(apicontract.ResponseValidator(t))
	app.Put("/api/v1/sites/:id/automation", handler.PutAutomation)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/sites/site-x/automation", strings.NewReader(`{"enabled":false,"sshPort":22}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("PUT automation: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
