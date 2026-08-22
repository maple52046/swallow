package tests

import (
	"net/http"
	"strings"
	"testing"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

func setupOperations(t *testing.T) *platformFixture {
	t.Helper()
	f := setupPlatform(t)
	seedAutomation(t, f)
	return f
}

func TestCreateOperation_LaunchesAndMirrorsJob(t *testing.T) {
	f := setupOperations(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.seedServer("srv-2", "gpu-node-02", "10.0.1.11", nil)

	resp := doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "install-gpu-driver",
		"intent":          "bring the new rack up to the current driver",
		"targetServerIds": []string{"srv-1", "srv-2"},
	}, f.adminAuth(t))

	// Accepted, not OK: AWX has taken the job and the poller tracks it from here.
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}

	body := parseBody(t, resp)
	if body["kind"] != "install-gpu-driver" {
		t.Errorf("kind: got %v", body["kind"])
	}
	if body["requestedBy"] != "admin" {
		t.Errorf("requestedBy should come from the token, got %v", body["requestedBy"])
	}

	automation := body["automation"].(map[string]any)
	if automation["jobId"] == nil {
		t.Error("expected the controller's job id to be recorded")
	}
	if automation["status"] != "running" {
		t.Errorf("status should mirror the controller, got %v", automation["status"])
	}
	if automation["observedAt"] == nil {
		t.Error("a mirrored status must say when it was observed")
	}
	if automation["jobTemplateId"] != "10" {
		t.Errorf("expected the resolved template id, got %v", automation["jobTemplateId"])
	}
}

// The dynamic inventory keys hosts by server ID, so --limit works with server IDs
// directly and no translation is needed anywhere.
func TestCreateOperation_LimitsToTargetServerIDs(t *testing.T) {
	f := setupOperations(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)

	doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "install-gpu-driver",
		"targetServerIds": []string{"srv-1"},
	}, f.adminAuth(t))

	if len(f.controller.launches) != 1 {
		t.Fatalf("expected 1 launch, got %d", len(f.controller.launches))
	}
	launch := f.controller.launches[0]
	if len(launch.Limit) != 1 || launch.Limit[0] != "srv-1" {
		t.Errorf("limit should be server ids, got %v", launch.Limit)
	}

	// gdcm's identifiers reach the playbook so it can report back in them.
	if launch.ExtraVars["gdcm_operation_id"] == nil {
		t.Error("expected gdcm_operation_id in extra vars")
	}
	if launch.ExtraVars["gdcm_site_id"] != testSiteID {
		t.Errorf("gdcm_site_id: got %v", launch.ExtraVars["gdcm_site_id"])
	}
}

// Operator-supplied vars must not be able to overwrite the identifiers a playbook
// reports against.
func TestCreateOperation_ExtraVarsCannotOverrideGdcmIdentifiers(t *testing.T) {
	f := setupOperations(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)

	doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "install-gpu-driver",
		"targetServerIds": []string{"srv-1"},
		"extraVars": map[string]any{
			"driver_version":    "550.54.14",
			"gdcm_operation_id": "forged",
		},
	}, f.adminAuth(t))

	launch := f.controller.launches[0]
	if launch.ExtraVars["driver_version"] != "550.54.14" {
		t.Errorf("operator vars should pass through, got %v", launch.ExtraVars["driver_version"])
	}
	if launch.ExtraVars["gdcm_operation_id"] == "forged" {
		t.Error("a gdcm_ prefixed var must not be overridable")
	}
}

// AWX will happily run two jobs against the same host, because it sees two unrelated
// jobs. gdcm is the only thing that can refuse.
func TestCreateOperation_RefusesOverlappingTargets(t *testing.T) {
	f := setupOperations(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	auth := f.adminAuth(t)

	first := doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "install-gpu-driver",
		"targetServerIds": []string{"srv-1"},
	}, auth)
	if first.StatusCode != http.StatusAccepted {
		t.Fatalf("first operation: expected 202, got %d", first.StatusCode)
	}

	second := doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "deploy-kubernetes",
		"targetServerIds": []string{"srv-1"},
	}, auth)
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", second.StatusCode)
	}
	message, _ := parseBody(t, second)["error"].(map[string]any)["message"].(string)
	if !strings.Contains(message, "operation") {
		t.Errorf("the conflict should name the blocking operation, got %q", message)
	}
}

func TestCreateOperation_AllowsTargetsOncePreviousFinished(t *testing.T) {
	f := setupOperations(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	auth := f.adminAuth(t)

	doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "install-gpu-driver",
		"targetServerIds": []string{"srv-1"},
	}, auth)

	// Finish the first job.
	for _, operation := range f.operationRepo.operations {
		operation.Automation.Status = operationdomain.StatusSucceeded
	}

	resp := doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "deploy-kubernetes",
		"targetServerIds": []string{"srv-1"},
	}, auth)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 once the previous operation finished, got %d", resp.StatusCode)
	}
}

// Running post-install automation against a machine mid-deployment fails slowly and
// confusingly. Refusing says why immediately.
func TestCreateOperation_RefusesTargetInWrongProvisioningState(t *testing.T) {
	f := setupOperations(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", func(s *serverdomain.Server) {
		s.Provisioning.State = "deploying"
	})

	resp := doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "install-gpu-driver",
		"targetServerIds": []string{"srv-1"},
	}, f.adminAuth(t))

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	message, _ := parseBody(t, resp)["error"].(map[string]any)["message"].(string)
	if !strings.Contains(message, "deploying") || !strings.Contains(message, "deployed") {
		t.Errorf("the error should name the actual and required state, got %q", message)
	}
}

// An operation runs through one site's controller, so targets cannot span sites.
func TestCreateOperation_RefusesTargetsAcrossSites(t *testing.T) {
	f := setupOperations(t)
	f.seedServer("srv-east", "gpu-node-01", "10.0.1.10", nil)
	f.seedServer("srv-west", "gpu-node-02", "10.0.2.10", func(s *serverdomain.Server) {
		s.Source.SiteID = "site-2"
	})

	resp := doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "install-gpu-driver",
		"targetServerIds": []string{"srv-east", "srv-west"},
	}, f.adminAuth(t))

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// Installing drivers on a server whose cluster delegates that to the GPU operator would
// leave two owners on one host, so it is refused.
func TestCreateOperation_RefusesDriverInstallInGPUOperatorCluster(t *testing.T) {
	f := setupOperations(t)
	seedCluster(t, f, "cluster-op", "prod-k8s", "gpu-operator")
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", func(s *serverdomain.Server) {
		s.Membership = &serverdomain.MembershipStatus{
			ClusterID: "cluster-op", NodeName: "gpu-node-01", Role: "worker",
			ObservedAt: time.Now().UTC(),
		}
	})

	resp := doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "install-gpu-driver",
		"targetServerIds": []string{"srv-1"},
	}, f.adminAuth(t))

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}
	message, _ := parseBody(t, resp)["error"].(map[string]any)["message"].(string)
	if !strings.Contains(message, "prod-k8s") {
		t.Errorf("the conflict should name the cluster, got %q", message)
	}
	if len(f.controller.launches) != 0 {
		t.Error("nothing should have been launched")
	}
}

// The provisioning-owned mode is exactly for servers not in a cluster, so a spare is
// allowed.
func TestCreateOperation_AllowsDriverInstallOutsideAnyCluster(t *testing.T) {
	f := setupOperations(t)
	seedCluster(t, f, "cluster-op", "prod-k8s", "gpu-operator")
	f.seedServer("srv-spare", "gpu-node-99", "10.0.1.99", nil)

	resp := doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "install-gpu-driver",
		"targetServerIds": []string{"srv-spare"},
	}, f.adminAuth(t))

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}
}

// A cluster that delegates drivers to provisioning is the case the operation is for.
func TestCreateOperation_AllowsDriverInstallInProvisioningOwnedCluster(t *testing.T) {
	f := setupOperations(t)
	seedCluster(t, f, "cluster-prov", "hpc-slurm", "provisioning")
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", func(s *serverdomain.Server) {
		s.Membership = &serverdomain.MembershipStatus{
			ClusterID: "cluster-prov", NodeName: "gpu-node-01",
			ObservedAt: time.Now().UTC(),
		}
	})

	resp := doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "install-gpu-driver",
		"targetServerIds": []string{"srv-1"},
	}, f.adminAuth(t))

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}
}

// The policy only governs drivers. Deploying a cluster touches nothing a GPU operator
// manages.
func TestCreateOperation_PolicyDoesNotBlockUnrelatedKinds(t *testing.T) {
	f := setupOperations(t)
	seedCluster(t, f, "cluster-op", "prod-k8s", "gpu-operator")
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", func(s *serverdomain.Server) {
		s.Membership = &serverdomain.MembershipStatus{
			ClusterID: "cluster-op", NodeName: "gpu-node-01",
			ObservedAt: time.Now().UTC(),
		}
	})

	resp := doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "deploy-kubernetes",
		"targetServerIds": []string{"srv-1"},
	}, f.adminAuth(t))

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}
}

// A missing template is reported at creation, not by a job that never starts.
func TestCreateOperation_MissingJobTemplateFailsAtCreation(t *testing.T) {
	f := setupOperations(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	delete(f.controller.templates, "install-gpu-driver")

	resp := doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "install-gpu-driver",
		"targetServerIds": []string{"srv-1"},
	}, f.adminAuth(t))

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if len(f.operationRepo.operations) != 0 {
		t.Error("nothing should be recorded when the template does not exist")
	}
}

// If the controller cannot be reached, nothing is created: a half-state that has to be
// reconciled later is worse than a failed request.
func TestCreateOperation_ControllerUnavailableCreatesNothing(t *testing.T) {
	f := setupOperations(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.controller.launchErr = &operationdomain.ControllerError{
		Kind:   operationdomain.ControllerErrorUnavailable,
		Detail: "Could not reach AWX.",
	}

	resp := doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "install-gpu-driver",
		"targetServerIds": []string{"srv-1"},
	}, f.adminAuth(t))

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
	if len(f.operationRepo.operations) != 0 {
		t.Error("no operation should exist after a failed launch")
	}
}

func TestCreateOperation_NoAutomationIntegration(t *testing.T) {
	f := setupPlatform(t) // deliberately without seedAutomation
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)

	resp := doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "install-gpu-driver",
		"targetServerIds": []string{"srv-1"},
	}, f.adminAuth(t))

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
	if code := errorCode(t, resp); code != "provider_unavailable" {
		t.Fatalf("expected provider_unavailable, got %s", code)
	}
}

func TestCreateOperation_CustomKindRequiresTemplateName(t *testing.T) {
	f := setupOperations(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)

	resp := doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "custom",
		"targetServerIds": []string{"srv-1"},
	}, f.adminAuth(t))

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCreateOperation_RejectsUnknownKindAndEmptyTargets(t *testing.T) {
	f := setupOperations(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	auth := f.adminAuth(t)

	resp := doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "make-coffee",
		"targetServerIds": []string{"srv-1"},
	}, auth)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown kind: expected 400, got %d", resp.StatusCode)
	}

	resp = doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "install-gpu-driver",
		"targetServerIds": []string{},
	}, auth)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty targets: expected 400, got %d", resp.StatusCode)
	}
}

// A job the controller no longer has becomes indeterminate. Absence is not an outcome.
func TestRefreshOperation_VanishedJobBecomesIndeterminate(t *testing.T) {
	f := setupOperations(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)

	created := parseBody(t, doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "install-gpu-driver",
		"targetServerIds": []string{"srv-1"},
	}, f.adminAuth(t)))
	operationID := created["id"].(string)

	// The job disappears from the controller.
	f.controller.jobStates = map[string]operationdomain.JobState{}

	resp := doRequest(t, f.app, "POST", "/api/v1/operations/"+operationID+"/refresh", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	automation := parseBody(t, resp)["automation"].(map[string]any)
	if automation["status"] != "indeterminate" {
		t.Fatalf("a vanished job must not be reported as failed, got %v", automation["status"])
	}
	if automation["terminal"] != true {
		t.Error("indeterminate is terminal: there is nothing left to poll")
	}
}

// An unreachable controller must leave the last known status in place rather than
// inventing an outcome.
func TestRefreshOperation_ControllerFailureKeepsLastKnownStatus(t *testing.T) {
	f := setupOperations(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)

	created := parseBody(t, doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "install-gpu-driver",
		"targetServerIds": []string{"srv-1"},
	}, f.adminAuth(t)))
	operationID := created["id"].(string)

	f.controller.stateErr = &operationdomain.ControllerError{
		Kind:   operationdomain.ControllerErrorUnavailable,
		Detail: "Could not reach AWX.",
	}

	resp := doRequest(t, f.app, "POST", "/api/v1/operations/"+operationID+"/refresh", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}

	if f.operationRepo.operations[operationID].Automation.Status != operationdomain.StatusRunning {
		t.Error("the last known status must survive an unreachable controller")
	}
}

func TestOperationLogs_ProxiedFromController(t *testing.T) {
	f := setupOperations(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)

	created := parseBody(t, doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "install-gpu-driver",
		"targetServerIds": []string{"srv-1"},
	}, f.adminAuth(t)))
	operationID := created["id"].(string)
	jobID := f.operationRepo.operations[operationID].Automation.JobID
	f.controller.logs[jobID] = "PLAY [install driver] ***"

	resp := doRequest(t, f.app, "GET", "/api/v1/operations/"+operationID+"/logs", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if body := rawBody(t, resp); !strings.Contains(body, "install driver") {
		t.Fatalf("expected the controller's output, got %q", body)
	}
}

// The webhook is a hint to go and look, not a source of truth: a payload claiming
// success must not be able to mark an operation successful.
func TestWebhook_IgnoresPayloadStatusAndRereadsFromController(t *testing.T) {
	f := setupOperations(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)

	created := parseBody(t, doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind":            "install-gpu-driver",
		"targetServerIds": []string{"srv-1"},
	}, f.adminAuth(t)))
	operationID := created["id"].(string)
	jobID := f.operationRepo.operations[operationID].Automation.JobID

	// The controller actually reports a failure, while the webhook claims success.
	finished := time.Now().UTC()
	f.controller.jobStates[jobID] = operationdomain.JobState{
		Status: operationdomain.StatusFailed, FinishedAt: &finished,
	}

	resp := doRequest(t, f.app, "POST", "/api/v1/webhooks/automation/"+testAutomationID,
		map[string]any{"id": jobIDAsInt(t, jobID), "status": "successful"},
		map[string]string{"Authorization": "Bearer " + testMachineToken})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if got := f.operationRepo.operations[operationID].Automation.Status; got != operationdomain.StatusFailed {
		t.Fatalf("status must come from gdcm's own read, got %q", got)
	}
}

// A notification about a job gdcm did not start is acknowledged, so AWX does not retry
// something gdcm will never care about.
func TestWebhook_UnknownJobIsAcknowledged(t *testing.T) {
	f := setupOperations(t)

	resp := doRequest(t, f.app, "POST", "/api/v1/webhooks/automation/"+testAutomationID,
		map[string]any{"id": 999999},
		map[string]string{"Authorization": "Bearer " + testMachineToken})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if parseBody(t, resp)["ignored"] != true {
		t.Error("expected the notification to be acknowledged as ignored")
	}
}

func TestWebhook_RequiresMachineAuth(t *testing.T) {
	f := setupOperations(t)

	resp := doRequest(t, f.app, "POST", "/api/v1/webhooks/automation/"+testAutomationID,
		map[string]any{"id": 1}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestListOperations_FilterByServer(t *testing.T) {
	f := setupOperations(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.seedServer("srv-2", "gpu-node-02", "10.0.1.11", nil)
	auth := f.adminAuth(t)

	doRequest(t, f.app, "POST", "/api/v1/operations/", map[string]any{
		"kind": "install-gpu-driver", "targetServerIds": []string{"srv-1"},
	}, auth)

	resp := doRequest(t, f.app, "GET", "/api/v1/operations/?serverId=srv-1", nil, auth)
	if total := parseBody(t, resp)["total"].(float64); total != 1 {
		t.Errorf("expected 1 operation for srv-1, got %v", total)
	}

	resp = doRequest(t, f.app, "GET", "/api/v1/operations/?serverId=srv-2", nil, auth)
	if total := parseBody(t, resp)["total"].(float64); total != 0 {
		t.Errorf("expected no operations for srv-2, got %v", total)
	}
}

func jobIDAsInt(t *testing.T, jobID string) int {
	t.Helper()
	value := 0
	for _, r := range jobID {
		if r < '0' || r > '9' {
			t.Fatalf("job id %q is not numeric", jobID)
		}
		value = value*10 + int(r-'0')
	}
	return value
}
