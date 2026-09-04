package tests

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// seedPlatform registers a platform directly, standing in for the create endpoint.
func seedPlatform(t *testing.T, f *platformFixture, id, name, gpuStackOwner string) *platformdomain.Platform {
	t.Helper()
	now := time.Now().UTC()
	platform := &platformdomain.Platform{
		ID:            id,
		SiteID:        testSiteID,
		Name:          name,
		Type:          platformdomain.PlatformTypeKubernetes,
		IntegrationID: "integration-k8s",
		GPUStackOwner: platformdomain.GPUStackOwner(gpuStackOwner),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := f.platformRepo.Create(context.Background(), platform); err != nil {
		t.Fatalf("seed platform: %v", err)
	}
	return platform
}

// gpuStackOwner has no safe default: guessing would silently pick a side in a conflict
// that breaks hosts.
func TestCreatePlatform_RequiresGPUStackOwner(t *testing.T) {
	f := setupPlatform(t)
	siteID := createSite(t, f, "dc-east")

	resp := doRequest(t, f.app, "POST", "/api/v1/platforms/", map[string]any{
		"siteId": siteID,
		"name":   "prod-k8s",
		"type":   "kubernetes",
	}, f.adminAuth(t))

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	message, _ := parseBody(t, resp)["error"].(map[string]any)["message"].(string)
	if !strings.Contains(message, "gpu-operator") || !strings.Contains(message, "provisioning") {
		t.Errorf("the error should name both options, got %q", message)
	}
}

func TestCreatePlatform_Success(t *testing.T) {
	f := setupPlatform(t)
	siteID := createSite(t, f, "dc-east")

	resp := doRequest(t, f.app, "POST", "/api/v1/platforms/", map[string]any{
		"siteId":        siteID,
		"name":          "prod-k8s",
		"type":          "kubernetes",
		"gpuStackOwner": "gpu-operator",
	}, f.adminAuth(t))

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}
	body := parseBody(t, resp)
	if body["gpuStackOwner"] != "gpu-operator" {
		t.Errorf("gpuStackOwner: got %v", body["gpuStackOwner"])
	}
	// Registered but not yet reachable is a normal state.
	if body["integrationId"] != nil {
		t.Errorf("expected a null integrationId, got %v", body["integrationId"])
	}
}

func TestCreatePlatform_RejectsUnknownTypeAndDuplicateName(t *testing.T) {
	f := setupPlatform(t)
	siteID := createSite(t, f, "dc-east")
	auth := f.adminAuth(t)

	resp := doRequest(t, f.app, "POST", "/api/v1/platforms/", map[string]any{
		"siteId": siteID, "name": "x", "type": "mesos", "gpuStackOwner": "provisioning",
	}, auth)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown type: expected 400, got %d", resp.StatusCode)
	}

	create := map[string]any{
		"siteId": siteID, "name": "prod-k8s", "type": "kubernetes", "gpuStackOwner": "provisioning",
	}
	doRequest(t, f.app, "POST", "/api/v1/platforms/", create, auth)
	resp = doRequest(t, f.app, "POST", "/api/v1/platforms/", create, auth)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate name: expected 409, got %d", resp.StatusCode)
	}
}

// The platform is authoritative for membership: swallow records what it says.
func TestSyncMembership_WritesMembershipAxis(t *testing.T) {
	f := setupPlatform(t)
	platform := seedPlatform(t, f, "platform-1", "prod-k8s", "provisioning")
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.platformReader.readers[platform.ID] = &fakePlatformReader{
		members: []platformdomain.Member{
			{Name: "gpu-node-01", Role: "worker", State: "ready", Addresses: []string{"10.0.1.10"}},
		},
	}

	resp := doRequest(t, f.app, "POST", "/api/v1/platforms/platform-1/sync", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	report := parseBody(t, resp)
	if report["matched"].(float64) != 1 {
		t.Fatalf("expected 1 matched member, got %v", report["matched"])
	}

	membership := f.servers.servers["srv-1"].Membership
	if membership == nil {
		t.Fatal("expected the membership axis to be written")
	}
	if membership.PlatformID != "platform-1" || membership.Role != "worker" || membership.State != "ready" {
		t.Errorf("unexpected membership: %+v", membership)
	}
	// The platform's own node name is the join key for in-platform metrics.
	if membership.NodeName != "gpu-node-01" {
		t.Errorf("nodeName: got %q", membership.NodeName)
	}
}

func TestSyncMembership_MatchesByAddressWhenNameDiffers(t *testing.T) {
	f := setupPlatform(t)
	platform := seedPlatform(t, f, "platform-1", "prod-k8s", "provisioning")
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	f.platformReader.readers[platform.ID] = &fakePlatformReader{
		members: []platformdomain.Member{
			{Name: "k8s-worker-7", Role: "worker", State: "ready", Addresses: []string{"10.0.1.10"}},
		},
	}

	doRequest(t, f.app, "POST", "/api/v1/platforms/platform-1/sync", nil, f.adminAuth(t))

	membership := f.servers.servers["srv-1"].Membership
	if membership == nil {
		t.Fatal("expected an address match to work when the name does not")
	}
	if membership.NodeName != "k8s-worker-7" {
		t.Errorf("nodeName should be the platform's name for it, got %q", membership.NodeName)
	}
}

// A platform containing machines swallow does not manage is normal and worth surfacing
// rather than silently ignoring.
func TestSyncMembership_ReportsUnmatchedMembers(t *testing.T) {
	f := setupPlatform(t)
	platform := seedPlatform(t, f, "platform-1", "prod-k8s", "provisioning")
	f.platformReader.readers[platform.ID] = &fakePlatformReader{
		members: []platformdomain.Member{
			{Name: "someone-elses-node", Role: "worker", State: "ready"},
		},
	}

	resp := doRequest(t, f.app, "POST", "/api/v1/platforms/platform-1/sync", nil, f.adminAuth(t))
	report := parseBody(t, resp)

	if report["matched"].(float64) != 0 {
		t.Errorf("expected no matches, got %v", report["matched"])
	}
	unmatched := report["unmatched"].([]any)
	if len(unmatched) != 1 || unmatched[0] != "someone-elses-node" {
		t.Errorf("expected the unmatched member to be named, got %v", unmatched)
	}
}

// Leaving membership behind would show a server as part of a platform it was removed from.
func TestSyncMembership_ClearsMembershipWhenRemovedFromPlatform(t *testing.T) {
	f := setupPlatform(t)
	platform := seedPlatform(t, f, "platform-1", "prod-k8s", "provisioning")
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", func(s *serverdomain.Server) {
		s.Membership = &serverdomain.MembershipStatus{
			PlatformID: "platform-1", NodeName: "gpu-node-01", ObservedAt: time.Now().UTC(),
		}
	})
	f.platformReader.readers[platform.ID] = &fakePlatformReader{members: nil}

	resp := doRequest(t, f.app, "POST", "/api/v1/platforms/platform-1/sync", nil, f.adminAuth(t))
	if parseBody(t, resp)["cleared"].(float64) != 1 {
		t.Fatalf("expected 1 cleared, got %v", parseBody(t, resp)["cleared"])
	}
	if f.servers.servers["srv-1"].Membership != nil {
		t.Error("membership should have been cleared")
	}
}

// Attributing a membership to the wrong server would misdirect every operation aimed at
// it, so an ambiguous match is treated as no match.
func TestSyncMembership_AmbiguousHostnameIsNotMatched(t *testing.T) {
	f := setupPlatform(t)
	platform := seedPlatform(t, f, "platform-1", "prod-k8s", "provisioning")
	f.seedServer("srv-a", "gpu-node-01", "10.0.1.10", nil)
	f.seedServer("srv-b", "gpu-node-01", "10.0.1.11", nil)
	f.platformReader.readers[platform.ID] = &fakePlatformReader{
		members: []platformdomain.Member{{Name: "gpu-node-01", Role: "worker", State: "ready"}},
	}

	resp := doRequest(t, f.app, "POST", "/api/v1/platforms/platform-1/sync", nil, f.adminAuth(t))
	report := parseBody(t, resp)

	if report["matched"].(float64) != 0 {
		t.Errorf("an ambiguous name must not be matched, got %v", report["matched"])
	}
	if f.servers.servers["srv-a"].Membership != nil || f.servers.servers["srv-b"].Membership != nil {
		t.Error("neither server should have been given membership")
	}
}

// A failed read keeps the previous success timestamp, so a reader can see the view is
// stale rather than being told something untrue.
func TestSyncMembership_FailureKeepsPreviousSuccess(t *testing.T) {
	f := setupPlatform(t)
	platform := seedPlatform(t, f, "platform-1", "prod-k8s", "provisioning")
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)
	reader := &fakePlatformReader{
		members: []platformdomain.Member{{Name: "gpu-node-01", Role: "worker", State: "ready"}},
	}
	f.platformReader.readers[platform.ID] = reader
	auth := f.adminAuth(t)

	doRequest(t, f.app, "POST", "/api/v1/platforms/platform-1/sync", nil, auth)
	firstSuccess := f.platformRepo.platforms["platform-1"].Sync.LastSucceededAt
	if firstSuccess == nil {
		t.Fatal("expected the first sync to record success")
	}

	reader.err = &platformdomain.ReaderError{
		Kind:   platformdomain.ReaderErrorUnavailable,
		Detail: "Could not reach the Kubernetes API.",
	}
	resp := doRequest(t, f.app, "POST", "/api/v1/platforms/platform-1/sync", nil, auth)
	if parseBody(t, resp)["error"] == nil {
		t.Fatal("expected the report to carry the failure")
	}

	sync := f.platformRepo.platforms["platform-1"].Sync
	if sync.LastSucceededAt == nil || !sync.LastSucceededAt.Equal(*firstSuccess) {
		t.Error("a failed read must keep the previous success timestamp")
	}
	// The membership it already found must survive an unreachable API.
	if f.servers.servers["srv-1"].Membership == nil {
		t.Error("an unreachable platform API must not erase known membership")
	}
}

// Membership is a projection of the platform, so deleting the registration must not leave
// servers claiming to belong to something that no longer exists.
func TestDeletePlatform_ClearsMembership(t *testing.T) {
	f := setupPlatform(t)
	seedPlatform(t, f, "platform-1", "prod-k8s", "provisioning")
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", func(s *serverdomain.Server) {
		s.Membership = &serverdomain.MembershipStatus{
			PlatformID: "platform-1", NodeName: "gpu-node-01", ObservedAt: time.Now().UTC(),
		}
	})

	resp := doRequest(t, f.app, "DELETE", "/api/v1/platforms/platform-1", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if f.servers.servers["srv-1"].Membership != nil {
		t.Error("membership should have been cleared with the platform")
	}
}

func TestUpdatePlatform_ChangeGPUStackOwner(t *testing.T) {
	f := setupPlatform(t)
	seedPlatform(t, f, "platform-1", "prod-k8s", "provisioning")
	owner := "gpu-operator"

	resp := doRequest(t, f.app, "PATCH", "/api/v1/platforms/platform-1",
		map[string]any{"gpuStackOwner": &owner}, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if parseBody(t, resp)["gpuStackOwner"] != "gpu-operator" {
		t.Errorf("gpuStackOwner: got %v", parseBody(t, resp)["gpuStackOwner"])
	}
}

func TestUpdatePlatform_RejectsUnknownGPUStackOwner(t *testing.T) {
	f := setupPlatform(t)
	seedPlatform(t, f, "platform-1", "prod-k8s", "provisioning")
	owner := "whoever"

	resp := doRequest(t, f.app, "PATCH", "/api/v1/platforms/platform-1",
		map[string]any{"gpuStackOwner": &owner}, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// Servers can be listed by the platform they belong to, which is what makes membership
// useful for targeting.
func TestListServers_FilterByPlatform(t *testing.T) {
	f := setupPlatform(t)
	seedPlatform(t, f, "platform-1", "prod-k8s", "provisioning")
	f.seedServer("srv-member", "gpu-node-01", "10.0.1.10", func(s *serverdomain.Server) {
		s.Membership = &serverdomain.MembershipStatus{
			PlatformID: "platform-1", NodeName: "gpu-node-01", ObservedAt: time.Now().UTC(),
		}
	})
	f.seedServer("srv-spare", "gpu-node-02", "10.0.1.11", nil)

	resp := doRequest(t, f.app, "GET", "/api/v1/servers/?platformId=platform-1", nil, f.adminAuth(t))
	body := parseBody(t, resp)
	if body["total"].(float64) != 1 {
		t.Fatalf("expected 1 platform member, got %v", body["total"])
	}
	item := body["items"].([]any)[0].(map[string]any)
	if item["id"] != "srv-member" {
		t.Errorf("wrong server returned: %v", item["id"])
	}
}

func TestUninstallPlatform_AcceptsOriginalDeploymentTargets(t *testing.T) {
	f := setupPlatform(t)
	platform := seedPlatform(t, f, "platform-1", "prod-k8s", "provisioning")
	platform.ExporterOwner = platformdomain.ExporterOwnerK8s
	f.seedServer("srv-1", "node-1", "10.0.1.10", nil)
	f.seedServer("srv-2", "node-2", "10.0.1.11", nil)
	now := time.Now().UTC()
	f.platformLifecycle.snapshots[platform.ID] = platformdomain.LifecycleSnapshot{
		Origin:      platformdomain.PlatformOriginDeployed,
		State:       platformdomain.PlatformLifecycleActive,
		OperationID: "deploy-1",
		Deployment: &platformdomain.LifecycleOperation{
			ID: "deploy-1", Status: "succeeded",
			TargetServerIDs: []string{"srv-1", "srv-2"},
			RequestedAt:     now,
		},
	}

	resp := doRequest(
		t, f.app, "POST", "/api/v1/platforms/platform-1/uninstall", nil, f.adminAuth(t),
	)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}
	body := parseBody(t, resp)
	if body["clusterId"] != "platform-1" || body["operationId"] != "operation-uninstall-1" {
		t.Errorf("response = %v", body)
	}
	launch := f.uninstallLauncher.lastLaunch
	if launch == nil || len(launch.TargetServerIDs) != 2 ||
		launch.TargetServerIDs[0] != "srv-1" || launch.TargetServerIDs[1] != "srv-2" {
		t.Fatalf("launch = %+v", launch)
	}
	if !launch.RestoreExporters {
		t.Error("k8s-owned exporters must be restored through a separate operation")
	}
}

func TestUninstallPlatform_RejectsRegisteredAndNonAdminRequests(t *testing.T) {
	f := setupPlatform(t)
	seedPlatform(t, f, "platform-1", "external-k8s", "provisioning")

	resp := doRequest(
		t, f.app, "POST", "/api/v1/platforms/platform-1/uninstall", nil,
		map[string]string{"Authorization": "Bearer " + userToken(t, f.jwtSvc)},
	)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, want 403", resp.StatusCode)
	}

	resp = doRequest(
		t, f.app, "POST", "/api/v1/platforms/platform-1/uninstall", nil, f.adminAuth(t),
	)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("registered platform status = %d, want 409", resp.StatusCode)
	}
	if code := errorCode(t, resp); code != "conflict" {
		t.Errorf("error code = %q", code)
	}
}

func TestPlatformResponseIncludesLifecycleProjection(t *testing.T) {
	f := setupPlatform(t)
	platform := seedPlatform(t, f, "platform-1", "prod-k8s", "provisioning")
	f.platformLifecycle.snapshots[platform.ID] = platformdomain.LifecycleSnapshot{
		Origin: "deployed", State: "uninstall_failed", OperationID: "uninstall-2",
		Deployment: &platformdomain.LifecycleOperation{
			ID: "deploy-1", Status: "succeeded", TargetServerIDs: []string{"srv-1"},
		},
		Uninstall: &platformdomain.LifecycleOperation{
			ID: "uninstall-2", Status: "failed", TargetServerIDs: []string{"srv-1"},
		},
	}

	resp := doRequest(
		t, f.app, "GET", "/api/v1/platforms/platform-1", nil, f.adminAuth(t),
	)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if body["origin"] != "deployed" || body["lifecycleState"] != "uninstall_failed" ||
		body["lifecycleOperationId"] != "uninstall-2" {
		t.Errorf("lifecycle response = %v", body)
	}
}
