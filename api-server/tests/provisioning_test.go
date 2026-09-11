package tests

import (
	"net/http"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// Deploy is addressed by server ID; which provisioner to call is derived from the
// server's source. That mapping is what swallow exists to hold.
func TestDeployServer_ResolvesProviderFromServerSource(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.provider.withMachine(testMachine("machine-srv-1", "gpu-node-01"))

	resp := doRequest(t, f.app, "POST", "/api/v1/servers/srv-1/deploy", map[string]string{
		"osSystem":     "ubuntu",
		"distroSeries": "jammy",
		"userData":     "#cloud-config",
	}, f.adminAuth(t))

	// Accepted, not OK: the provisioner has taken the request and the reconciler
	// tracks it from here.
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if body["serverId"] != "srv-1" {
		t.Errorf("serverId: got %v", body["serverId"])
	}
	if body["state"] != "deploying" {
		t.Errorf("state: got %v, want deploying", body["state"])
	}

	if len(f.provider.deployRequests) != 1 {
		t.Fatalf("expected 1 deploy call, got %d", len(f.provider.deployRequests))
	}
	req := f.provider.deployRequests[0]
	if req.MachineID != "machine-srv-1" {
		t.Errorf("the provider must be called with its own machine ID, got %q", req.MachineID)
	}
	if req.DistroSeries != "jammy" || req.UserData != "#cloud-config" {
		t.Errorf("unexpected request: %+v", req)
	}
}

// The axis is written straight away so a caller re-reading the server sees "deploying"
// rather than the previous state until the next reconcile pass.
func TestDeployServer_UpdatesProvisioningAxisImmediately(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.provider.withMachine(testMachine("machine-srv-1", "gpu-node-01"))

	doRequest(t, f.app, "POST", "/api/v1/servers/srv-1/deploy", map[string]string{
		"distroSeries": "jammy",
	}, f.adminAuth(t))

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/srv-1", nil, f.adminAuth(t))
	provisioning := parseBody(t, resp)["provisioning"].(map[string]any)
	if provisioning["state"] != "deploying" {
		t.Fatalf("expected the axis to reflect the deployment, got %v", provisioning["state"])
	}
}

// Deploying from memory leaves the disks untouched and loses everything on reboot, so
// the request has to reach the provisioner and the result has to be visible afterwards.
func TestDeployServer_EphemeralIsForwardedAndProjected(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.provider.withMachine(testMachine("machine-srv-1", "gpu-node-01"))

	resp := doRequest(t, f.app, "POST", "/api/v1/servers/srv-1/deploy", map[string]any{
		"distroSeries": "jammy",
		"ephemeral":    true,
	}, f.adminAuth(t))

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	if body := parseBody(t, resp); body["ephemeral"] != true {
		t.Errorf("the response must report the deployment as ephemeral, got %v", body["ephemeral"])
	}
	if len(f.provider.deployRequests) != 1 {
		t.Fatalf("expected 1 deploy call, got %d", len(f.provider.deployRequests))
	}
	if !f.provider.deployRequests[0].Ephemeral {
		t.Error("the ephemeral request must reach the provisioner, not be dropped on the way")
	}

	// Readable afterwards: nothing else on the axis distinguishes this from a machine
	// with the same OS installed on disk.
	resp = doRequest(t, f.app, "GET", "/api/v1/servers/srv-1", nil, f.adminAuth(t))
	provisioning := parseBody(t, resp)["provisioning"].(map[string]any)
	if provisioning["ephemeral"] != true {
		t.Errorf("the axis must show ephemerality, got %v", provisioning["ephemeral"])
	}
}

func TestDeployServer_IsNotEphemeralUnlessAsked(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.provider.withMachine(testMachine("machine-srv-1", "gpu-node-01"))

	doRequest(t, f.app, "POST", "/api/v1/servers/srv-1/deploy", map[string]any{
		"distroSeries": "jammy",
	}, f.adminAuth(t))

	if f.provider.deployRequests[0].Ephemeral {
		t.Error("a deployment must not become ephemeral by default")
	}
}

// The one deploy option where being ignored produces the opposite of the instruction:
// asking for "nothing is written to disk" and getting a disk installation. So it is
// refused before the provisioner is called at all.
func TestDeployServer_EphemeralRefusedWhenProvisionerCannotDoIt(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.provider.withMachine(testMachine("machine-srv-1", "gpu-node-01"))
	f.provider.capabilities = provisioningdomain.ProviderCapabilities{EphemeralDeploy: false}

	resp := doRequest(t, f.app, "POST", "/api/v1/servers/srv-1/deploy", map[string]any{
		"distroSeries": "jammy",
		"ephemeral":    true,
	}, f.adminAuth(t))

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if len(f.provider.deployRequests) != 0 {
		t.Fatalf("no deployment may be started at all, got %+v", f.provider.deployRequests)
	}
}

func TestDeployServer_RequiresDistroSeries(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)

	resp := doRequest(t, f.app, "POST", "/api/v1/servers/srv-1/deploy", map[string]string{
		"osSystem": "ubuntu",
	}, f.adminAuth(t))

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if code := errorCode(t, resp); code != "validation_error" {
		t.Fatalf("expected validation_error, got %s", code)
	}
}

func TestDeployServer_UnknownServer(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "POST", "/api/v1/servers/nope/deploy", map[string]string{
		"distroSeries": "jammy",
	}, f.adminAuth(t))

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestDeployServer_ProviderRejectionKeepsItsOwnWording(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.provider.withMachine(testMachine("machine-srv-1", "gpu-node-01"))
	f.provider.deployErr = &provisioningdomain.ProviderError{
		Kind:   provisioningdomain.ProviderErrorRejected,
		Detail: "MAAS refused the request: storage: Mount the root '/' filesystem.",
	}

	resp := doRequest(t, f.app, "POST", "/api/v1/servers/srv-1/deploy", map[string]string{
		"distroSeries": "jammy",
	}, f.adminAuth(t))

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	message := parseBody(t, resp)["error"].(map[string]any)["message"]
	if message != "MAAS refused the request: storage: Mount the root '/' filesystem." {
		t.Fatalf("the provider's explanation is the actionable part and was lost: %v", message)
	}
}

func TestDeployServer_ProviderUnavailable(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.provider.withMachine(testMachine("machine-srv-1", "gpu-node-01"))
	f.provider.deployErr = &provisioningdomain.ProviderError{
		Kind:   provisioningdomain.ProviderErrorUnavailable,
		Detail: "Could not reach MAAS.",
	}

	resp := doRequest(t, f.app, "POST", "/api/v1/servers/srv-1/deploy", map[string]string{
		"distroSeries": "jammy",
	}, f.adminAuth(t))

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
	if code := errorCode(t, resp); code != "provider_unavailable" {
		t.Fatalf("expected provider_unavailable, got %s", code)
	}
}

func TestReleaseServer_DoesNotRemoveTheServer(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.provider.withMachine(testMachine("machine-srv-1", "gpu-node-01"))

	resp := doRequest(t, f.app, "POST", "/api/v1/servers/srv-1/release", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	if len(f.provider.releaseCalls) != 1 || f.provider.releaseCalls[0] != "machine-srv-1" {
		t.Fatalf("unexpected release calls: %v", f.provider.releaseCalls)
	}

	// The physical machine still exists and swallow still manages it.
	if _, ok := f.servers.servers["srv-1"]; !ok {
		t.Fatal("release must not delete the server")
	}
}

func TestReleaseServer_PassesDiskErasureOptions(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.provider.withMachine(testMachine("machine-srv-1", "gpu-node-01"))

	resp := doRequest(t, f.app, "POST", "/api/v1/servers/srv-1/release", map[string]any{
		"erase": true, "secureErase": true, "quickErase": true,
		"comment": "retire from test pool",
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	if len(f.provider.releaseRequests) != 1 {
		t.Fatalf("release requests: got %d, want 1", len(f.provider.releaseRequests))
	}
	request := f.provider.releaseRequests[0]
	if request.MachineID != "machine-srv-1" || !request.Erase || !request.SecureErase || !request.QuickErase || request.Comment != "retire from test pool" {
		t.Fatalf("unexpected release request: %+v", request)
	}
}

func TestReleaseServer_RejectsEraseModesWithoutErase(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.provider.withMachine(testMachine("machine-srv-1", "gpu-node-01"))

	resp := doRequest(t, f.app, "POST", "/api/v1/servers/srv-1/release", map[string]any{
		"secureErase": true,
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if code := errorCode(t, resp); code != "validation_error" {
		t.Fatalf("expected validation_error, got %s", code)
	}
	if len(f.provider.releaseCalls) != 0 {
		t.Fatalf("provider must not be called for invalid release options: %v", f.provider.releaseCalls)
	}
}

// Images differ per site, so a merged list would offer images the target site cannot
// deploy. The integration has to be named.
func TestListImages_RequiresIntegrationID(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/provisioning/images", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestListImages_Success(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app,
		"GET", "/api/v1/provisioning/images?integrationId="+testIntegrationID, nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	items := parseArrayBody(t, resp)
	if len(items) != 1 || items[0]["id"] != "ubuntu/jammy" {
		t.Fatalf("unexpected images: %v", items)
	}
}

func TestListImages_UnknownIntegration(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app,
		"GET", "/api/v1/provisioning/images?integrationId=nope", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// An image is identified by integration, ID, and architecture together — the same identity
// the catalog returns — so the delete request carries all three as query parameters.
func TestDeleteImage_Success(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "DELETE",
		"/api/v1/provisioning/images?integrationId="+testIntegrationID+"&imageId=ubuntu-24.04-rocm&architecture=amd64",
		nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	if got := f.provider.deletedImages; len(got) != 1 || got[0] != [2]string{"ubuntu-24.04-rocm", "amd64"} {
		t.Fatalf("provider deletions: got %+v, want one (ubuntu-24.04-rocm, amd64)", got)
	}
}

// The image ID and architecture are required: without them the request cannot name an image,
// so it is refused before reaching the provider.
func TestDeleteImage_RequiresImageIdentity(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "DELETE",
		"/api/v1/provisioning/images?integrationId="+testIntegrationID, nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if len(f.provider.deletedImages) != 0 {
		t.Fatalf("provider must not be asked to delete on an invalid request: %+v", f.provider.deletedImages)
	}
}

// The reconcile endpoint returns the report rather than a bare acknowledgement,
// because conflicts need an operator and would otherwise go unnoticed.
func TestReconcileEndpoint_ReturnsReportWithConflicts(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)

	f.servers.servers["srv-a"] = &serverdomain.Server{
		ID:       "srv-a",
		Source:   serverdomain.Source{SiteID: testSiteID, IntegrationID: testIntegrationID, ProviderMachineID: "a"},
		Hardware: serverdomain.Hardware{SystemUUID: "uuid-dup"},
	}
	f.servers.servers["srv-b"] = &serverdomain.Server{
		ID:       "srv-b",
		Source:   serverdomain.Source{SiteID: testSiteID, IntegrationID: testIntegrationID, ProviderMachineID: "b"},
		Hardware: serverdomain.Hardware{SystemUUID: "uuid-dup"},
	}

	machine := testMachine("c", "gpu-node-03")
	machine.SystemUUID = "uuid-dup"
	f.provider.withMachine(machine)

	resp := doRequest(t, f.app,
		"POST", "/api/v1/provisioning/integrations/"+testIntegrationID+"/reconcile", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	conflicts, ok := parseBody(t, resp)["conflicts"].([]any)
	if !ok || len(conflicts) != 1 {
		t.Fatalf("expected the report to surface 1 conflict, got %v", parseBody(t, resp)["conflicts"])
	}
	conflict := conflicts[0].(map[string]any)
	if conflict["reason"] == "" {
		t.Error("a conflict must explain itself: an operator has to resolve it")
	}
}

func TestReconcileEndpoint_RejectsNonProvisioner(t *testing.T) {
	f := setupPlatform(t)
	seedNonProvisionerIntegration(t, f)

	resp := doRequest(t, f.app,
		"POST", "/api/v1/provisioning/integrations/"+testNonProvisionerIntegrationID+"/reconcile", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// A targeted refresh reads the provider directly, so an accepted asynchronous action
// can converge without waiting for the fleet-wide inventory interval.
func TestRefreshServer_AdvancesLifecycleAndVolatileObservations(t *testing.T) {
	f := setupPlatform(t)
	server := f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", func(server *serverdomain.Server) {
		server.Provisioning.State = "releasing"
		server.Provisioning.ProviderState = "Releasing"
		server.Provisioning.Ephemeral = true
	})
	machine := testMachine("machine-srv-1", "provider-renamed-node")
	machine.Status = provisioningdomain.MachineStatusReady
	machine.ProviderStatus = "Ready"
	machine.Ephemeral = true
	machine.IPAddresses = nil
	f.provider.withMachine(machine)

	resp := doRequest(t, f.app, "POST", "/api/v1/servers/srv-1/refresh", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if body["state"] != "ready" {
		t.Fatalf("refresh state: got %v, want ready", body["state"])
	}
	if server.Provisioning.State != "ready" || server.Provisioning.ProviderState != "Ready" {
		t.Fatalf("projection was not advanced: %+v", server.Provisioning)
	}
	if server.Provisioning.Ephemeral {
		t.Fatal("a Ready Server must not retain a stale ephemeral deployment qualifier")
	}
	if len(server.Observed.Addresses) != 0 {
		t.Fatalf("targeted refresh addresses: got %v, want none", server.Observed.Addresses)
	}
	if server.Observed.Hostname != "gpu-node-01" {
		t.Fatalf("targeted refresh must not rewrite inventory identity, got %q", server.Observed.Hostname)
	}
}

func TestRefreshServer_UnknownServer(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "POST", "/api/v1/servers/missing/refresh", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}
