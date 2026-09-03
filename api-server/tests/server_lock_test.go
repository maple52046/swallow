package tests

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

func TestServerLock_ImmediatelyUpdatesProjectionAndUnlocks(t *testing.T) {
	f := setupPlatform(t)
	serverID := seedActionableServer(t, f)
	f.provider.machines["machine-"+serverID].Status = provisioningdomain.MachineStatusDeployed

	response := doRequest(t, f.app, http.MethodPost,
		"/api/v1/servers/"+serverID+"/lock", nil, f.adminAuth(t))
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("lock: got %d, want 202", response.StatusCode)
	}
	body := parseBody(t, response)
	if body["locked"] != true || !f.servers.servers[serverID].Provisioning.Locked {
		t.Fatalf("lock projection was not updated immediately: body=%v server=%+v",
			body, f.servers.servers[serverID].Provisioning)
	}

	response = doRequest(t, f.app, http.MethodPost,
		"/api/v1/servers/"+serverID+"/unlock", nil, f.adminAuth(t))
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("unlock: got %d, want 202", response.StatusCode)
	}
	if f.servers.servers[serverID].Provisioning.Locked {
		t.Fatal("unlock projection remained locked")
	}
}

func TestServerLock_BlocksMutationButKeepsReadOnlyPowerQuery(t *testing.T) {
	f := setupPlatform(t)
	serverID := seedActionableServer(t, f)
	f.provider.machines["machine-"+serverID].Locked = true

	response := doRequest(t, f.app, http.MethodPost,
		"/api/v1/servers/"+serverID+"/power-on", nil, f.adminAuth(t))
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("power-on: got %d, want 409", response.StatusCode)
	}
	if len(f.provider.actions) != 0 {
		t.Fatalf("locked mutation reached provider action: %v", f.provider.actions)
	}
	errorBody := parseBody(t, response)
	errorValue := errorBody["error"].(map[string]any)
	if errorValue["message"] != "Server \"gpu-node-01\" is locked. Unlock it before starting this action." {
		t.Fatalf("unexpected lock message: %v", errorValue["message"])
	}

	response = doRequest(t, f.app, http.MethodGet,
		"/api/v1/servers/"+serverID+"/power-state", nil, f.adminAuth(t))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("read-only power query: got %d, want 200", response.StatusCode)
	}

	response = doRequest(t, f.app, http.MethodPost,
		"/api/v1/servers/"+serverID+"/unlock", nil, f.adminAuth(t))
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("unlock: got %d, want 202", response.StatusCode)
	}
}

func TestServerLock_RefusesReadyServerBeforeProviderCall(t *testing.T) {
	f := setupPlatform(t)
	serverID := seedActionableServer(t, f)

	response := doRequest(t, f.app, http.MethodPost,
		"/api/v1/servers/"+serverID+"/lock", nil, f.adminAuth(t))
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("lock while ready: got %d, want 409", response.StatusCode)
	}
	if len(f.provider.actions) != 0 {
		t.Fatalf("ready lock reached provider action: %v", f.provider.actions)
	}
	message := parseBody(t, response)["error"].(map[string]any)["message"].(string)
	if !strings.Contains(message, "only when the Server is deployed") {
		t.Fatalf("lock message does not explain provider lifecycle requirement: %q", message)
	}
}

func TestServerLock_RefusesActiveProviderLifecycle(t *testing.T) {
	f := setupPlatform(t)
	serverID := seedActionableServer(t, f)
	f.provider.machines["machine-"+serverID].Status = provisioningdomain.MachineStatusDeploying

	response := doRequest(t, f.app, http.MethodPost,
		"/api/v1/servers/"+serverID+"/lock", nil, f.adminAuth(t))
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("lock while deploying: got %d, want 409", response.StatusCode)
	}
	if len(f.provider.actions) != 0 {
		t.Fatalf("lock reached provider during active lifecycle: %v", f.provider.actions)
	}
}

func TestServerLock_BatchDeploymentReadsExternalLockBeforeAnyWrite(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	seedReadyServer(t, f, "srv-1")
	f.provider.machines["machine-srv-1"].Locked = true

	response := doRequest(t, f.app, http.MethodPost,
		"/api/v1/provisioning/deployments/preflight", map[string]any{
			"serverIds": []string{"srv-1"},
		}, f.adminAuth(t))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("preflight: got %d, want 200", response.StatusCode)
	}
	body := parseBody(t, response)
	if body["valid"] != false {
		t.Fatalf("externally locked target passed preflight: %v", body)
	}
	issues := body["issues"].([]any)
	if len(issues) != 1 || issues[0].(map[string]any)["code"] != "locked" {
		t.Fatalf("preflight issues = %v, want locked", issues)
	}
	issueMessage := issues[0].(map[string]any)["message"].(string)
	if !strings.Contains(issueMessage, "srv-1") || !strings.Contains(issueMessage, "Unlock") {
		t.Fatalf("preflight lock message is not actionable: %q", issueMessage)
	}

	response = doRequest(t, f.app, http.MethodPost,
		"/api/v1/provisioning/deployments", map[string]any{
			"serverIds": []string{"srv-1"},
			"settings":  map[string]any{"imageId": "ubuntu/jammy"},
		}, f.adminAuth(t))
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("deploy locked target: got %d, want 409", response.StatusCode)
	}
	if len(f.provider.networkConfigureCalls) != 0 || len(f.provider.deployRequests) != 0 {
		t.Fatalf("locked batch reached provider writes: configure=%v deploy=%v",
			f.provider.networkConfigureCalls, f.provider.deployRequests)
	}
}

func TestServerLock_RefusesActiveOperationsAndProvisioningTasks(t *testing.T) {
	tests := []struct {
		name string
		work provisioningapp.ActiveServerWork
		want string
	}{
		{name: "operation", work: provisioningapp.ActiveServerWork{OperationIDs: []string{"operation-1"}}, want: "operation-1"},
		{name: "task", work: provisioningapp.ActiveServerWork{TaskIDs: []string{"task-1"}}, want: "task-1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := setupPlatform(t)
			serverID := seedActionableServer(t, f)
			f.provider.machines["machine-"+serverID].Status = provisioningdomain.MachineStatusDeployed
			f.activeWork.work[serverID] = test.work

			response := doRequest(t, f.app, http.MethodPost,
				"/api/v1/servers/"+serverID+"/lock", nil, f.adminAuth(t))
			if response.StatusCode != http.StatusConflict {
				t.Fatalf("lock with active work: got %d, want 409", response.StatusCode)
			}
			if len(f.provider.actions) != 0 {
				t.Fatalf("lock with active work reached provider: %v", f.provider.actions)
			}
			body := parseBody(t, response)
			message := body["error"].(map[string]any)["message"].(string)
			if !strings.Contains(message, test.want) {
				t.Fatalf("lock message %q does not identify active work %q", message, test.want)
			}
		})
	}
}
func TestServerLock_RequiresAdmin(t *testing.T) {
	f := setupPlatform(t)
	serverID := seedActionableServer(t, f)

	for _, path := range []string{"lock", "unlock"} {
		t.Run(path+" unauthenticated", func(t *testing.T) {
			response := doRequest(t, f.app, http.MethodPost,
				"/api/v1/servers/"+serverID+"/"+path, nil, nil)
			if response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("%s without token: got %d, want 401", path, response.StatusCode)
			}
		})
		t.Run(path+" non-admin", func(t *testing.T) {
			response := doRequest(t, f.app, http.MethodPost,
				"/api/v1/servers/"+serverID+"/"+path, nil, map[string]string{
					"Authorization": "Bearer " + userToken(t, f.jwtSvc),
				})
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("%s as non-admin: got %d, want 403", path, response.StatusCode)
			}
		})
	}
}
func TestServerLock_FailsClosedWhenProviderStateIsUnavailable(t *testing.T) {
	f := setupPlatform(t)
	serverID := seedActionableServer(t, f)
	f.provider.machineErr = errors.New("provider offline")

	response := doRequest(t, f.app, http.MethodPost,
		"/api/v1/servers/"+serverID+"/power-on", nil, f.adminAuth(t))
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("power-on without lock state: got %d, want 503", response.StatusCode)
	}
	if len(f.provider.actions) != 0 {
		t.Fatalf("unknown lock state reached provider action: %v", f.provider.actions)
	}
	body := parseBody(t, response)
	errorValue := body["error"].(map[string]any)
	if errorValue["code"] != "provider_unavailable" ||
		!strings.Contains(errorValue["message"].(string), "could not be confirmed") {
		t.Fatalf("unexpected fail-closed response: %v", errorValue)
	}
}
func TestServerLock_BlocksProvisionerLifecycleAndDeletion(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{
			name: "legacy deploy", method: http.MethodPost, path: "/deploy",
			body: map[string]string{"osSystem": "ubuntu", "distroSeries": "jammy"},
		},
		{name: "release", method: http.MethodPost, path: "/release"},
		{name: "delete", method: http.MethodDelete},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := setupPlatform(t)
			serverID := seedActionableServer(t, f)
			f.provider.machines["machine-"+serverID].Locked = true

			response := doRequest(t, f.app, test.method,
				"/api/v1/servers/"+serverID+test.path, test.body, f.adminAuth(t))
			if response.StatusCode != http.StatusConflict {
				t.Fatalf("%s: got %d, want 409", test.name, response.StatusCode)
			}
			if len(f.provider.deployRequests) != 0 ||
				len(f.provider.releaseCalls) != 0 ||
				len(f.provider.deleteCalls) != 0 {
				t.Fatalf("locked %s reached provider write: deploy=%v release=%v delete=%v",
					test.name, f.provider.deployRequests, f.provider.releaseCalls, f.provider.deleteCalls)
			}
		})
	}
}
