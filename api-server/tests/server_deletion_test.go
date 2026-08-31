package tests

import (
	"errors"
	"net/http"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

func TestDeleteServer_RemovesProviderMachineBeforeProjection(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.provider.withMachine(testMachine("machine-srv-1", "gpu-node-01"))

	resp := doRequest(t, f.app, http.MethodDelete, "/api/v1/servers/srv-1", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE server status = %d, want 204", resp.StatusCode)
	}
	if len(f.provider.deleteCalls) != 1 || f.provider.deleteCalls[0] != "machine-srv-1" {
		t.Fatalf("provider delete calls = %v, want machine-srv-1", f.provider.deleteCalls)
	}
	if _, ok := f.provider.machines["machine-srv-1"]; ok {
		t.Error("provider Machine remains after successful deletion")
	}
	if _, ok := f.servers.servers["srv-1"]; ok {
		t.Error("Server projection remains after provider deletion")
	}
}

func TestDeleteServer_ProviderRefusalRetainsProjection(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.provider.withMachine(testMachine("machine-srv-1", "gpu-node-01"))
	f.provider.deleteErr = &provisioningdomain.ProviderError{
		Kind:   provisioningdomain.ProviderErrorRejected,
		Detail: "MAAS refused the request: Machine cannot be deleted while hosting virtual machines.",
	}

	resp := doRequest(t, f.app, http.MethodDelete, "/api/v1/servers/srv-1", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("DELETE server status = %d, want 400", resp.StatusCode)
	}
	if _, ok := f.provider.machines["machine-srv-1"]; !ok {
		t.Error("provider refusal must retain the Machine")
	}
	if _, ok := f.servers.servers["srv-1"]; !ok {
		t.Error("provider refusal must retain the Server projection")
	}
}

func TestDeleteServer_MissingProviderMachineCleansStaleProjection(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)

	resp := doRequest(t, f.app, http.MethodDelete, "/api/v1/servers/srv-1", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE stale server status = %d, want 204", resp.StatusCode)
	}
	if _, ok := f.servers.servers["srv-1"]; ok {
		t.Error("stale Server projection remains after provider reports Machine missing")
	}
}

func TestDeleteServer_LocalFailureCanConvergeOnRetry(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.provider.withMachine(testMachine("machine-srv-1", "gpu-node-01"))
	f.servers.deleteErr = errors.New("database write failed")

	resp := doRequest(t, f.app, http.MethodDelete, "/api/v1/servers/srv-1", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("first DELETE status = %d, want 500", resp.StatusCode)
	}
	if _, ok := f.provider.machines["machine-srv-1"]; ok {
		t.Error("provider deletion must complete before the local write is attempted")
	}
	if _, ok := f.servers.servers["srv-1"]; !ok {
		t.Error("failed local delete must leave the projection for retry")
	}

	f.servers.deleteErr = nil
	resp = doRequest(t, f.app, http.MethodDelete, "/api/v1/servers/srv-1", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("retry DELETE status = %d, want 204", resp.StatusCode)
	}
}

func TestDeleteServer_UnsupportedProviderRetainsProjection(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.factory.providers[testIntegrationID] = &minimalProvider{}

	resp := doRequest(t, f.app, http.MethodDelete, "/api/v1/servers/srv-1", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("DELETE unsupported server status = %d, want 400", resp.StatusCode)
	}
	if _, ok := f.servers.servers["srv-1"]; !ok {
		t.Error("unsupported provider must retain the Server projection")
	}
}

func TestDeleteServer_UnknownServerIs404(t *testing.T) {
	f := setupPlatform(t)
	resp := doRequest(t, f.app, http.MethodDelete, "/api/v1/servers/nope", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("DELETE unknown server status = %d, want 404", resp.StatusCode)
	}
}
