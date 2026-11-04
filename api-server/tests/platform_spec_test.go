package tests

import (
	"context"
	"net/http"
	"testing"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

// seedSpecSite stores the test Site the spec writes validate against.
func seedSpecSite(f *platformFixture) {
	now := time.Now().UTC()
	f.sites.sites[testSiteID] = &sitedomain.Site{ID: testSiteID, Name: "East", CreatedAt: now, UpdatedAt: now}
}

func kubernetesSpecBody(members ...map[string]any) map[string]any {
	list := make([]any, 0, len(members))
	for _, member := range members {
		list = append(list, member)
	}
	return map[string]any{"members": list, "settings": map[string]any{"k0sVersion": "v1.36.3+k0s.2"}}
}

func member(serverID string, roles ...string) map[string]any {
	list := make([]any, 0, len(roles))
	for _, role := range roles {
		list = append(list, role)
	}
	return map[string]any{"serverId": serverID, "roles": list}
}

// seedInstalledPlatform stores a Kubernetes Platform whose generation-1 spec is applied.
func seedInstalledPlatform(t *testing.T, f *platformFixture) *platformdomain.Platform {
	t.Helper()
	platform := seedPlatform(t, f, "platform-1", "prod-k8s", "provisioning")
	spec := platformdomain.PlatformSpec{
		Desired:  platformdomain.PlatformPresent,
		Members:  []platformdomain.PlatformMember{{ServerID: "srv-1", Roles: []string{"control-plane", "workloads"}}},
		Settings: map[string]any{"k0sVersion": "v1.36.3+k0s.2"},
	}
	applied := spec.Clone()
	platform.Spec, platform.Generation = &spec, 1
	platform.Status = platformdomain.PlatformStatus{ObservedGeneration: 1, Applied: &applied}
	f.platformLifecycle.snapshots[platform.ID] = platformdomain.LifecycleSnapshot{
		Origin: platformdomain.PlatformOriginDeployed, State: platformdomain.PlatformLifecycleActive, OperationID: "deploy-1",
		Deployment: &platformdomain.LifecycleOperation{ID: "deploy-1", Status: "succeeded", TargetServerIDs: []string{"srv-1"}, RequestedAt: time.Now().UTC()},
	}
	return platform
}

// POST /platforms validates the spec as a deployment, stores the Platform at generation 1, and
// answers 202 with the Change Plan that installs it.
func TestCreatePlatformProposesItsInstall(t *testing.T) {
	f := setupPlatform(t)
	seedSpecSite(f)
	f.seedServer("srv-1", "node-1", "10.0.1.10", nil)

	resp := doRequest(t, f.app, "POST", "/api/v1/platforms", map[string]any{
		"siteId": testSiteID, "name": "lab-k0s", "type": "kubernetes",
		"spec": kubernetesSpecBody(member("srv-1", "control-plane", "workloads")),
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", resp.StatusCode, rawBody(t, resp))
	}
	body := parseBody(t, resp)
	plan, _ := body["plan"].(map[string]any)
	platform, _ := body["platform"].(map[string]any)
	if plan["state"] != "pending_approval" || platform["generation"] != float64(1) {
		t.Fatalf("response = %v", body)
	}
	spec, _ := platform["spec"].(map[string]any)
	if members, _ := spec["members"].([]any); len(members) != 1 {
		t.Errorf("spec = %v, want the one member", spec)
	}

	resp = doRequest(t, f.app, "POST", "/api/v1/platforms", map[string]any{
		"siteId": testSiteID, "name": "lab-k0s-2", "type": "kubernetes", "spec": kubernetesSpecBody(member("srv-1", "captain")),
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown role status = %d, want 400: %s", resp.StatusCode, rawBody(t, resp))
	}
}

// PUT /platforms/{id}/spec writes a non-destructive change at the expected generation (200, no
// plan) and refuses a stale generation and an immutable field with 409.
func TestReplacePlatformSpec(t *testing.T) {
	f := setupPlatform(t)
	seedSpecSite(f)
	f.seedServer("srv-1", "node-1", "10.0.1.10", nil)
	f.seedServer("srv-2", "node-2", "10.0.1.11", nil)
	seedInstalledPlatform(t, f)

	grown := kubernetesSpecBody(member("srv-1", "control-plane", "workloads"), member("srv-2", "worker"))
	resp := doRequest(t, f.app, "PUT", "/api/v1/platforms/platform-1/spec", map[string]any{"generation": 1, "spec": grown}, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, rawBody(t, resp))
	}
	body := parseBody(t, resp)
	if body["plan"] != nil || body["platform"].(map[string]any)["generation"] != float64(2) {
		t.Fatalf("response = %v, want generation 2 without a plan", body)
	}

	resp = doRequest(t, f.app, "PUT", "/api/v1/platforms/platform-1/spec", map[string]any{"generation": 1, "spec": grown}, f.adminAuth(t))
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("stale generation status = %d, want 409", resp.StatusCode)
	}
	immutable := kubernetesSpecBody(member("srv-1", "control-plane", "workloads"), member("srv-2", "worker"))
	immutable["settings"].(map[string]any)["podCidr"] = "10.250.0.0/16"
	resp = doRequest(t, f.app, "PUT", "/api/v1/platforms/platform-1/spec", map[string]any{"generation": 2, "spec": immutable}, f.adminAuth(t))
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("immutable field status = %d, want 409: %s", resp.StatusCode, rawBody(t, resp))
	}
	resp = doRequest(t, f.app, "PUT", "/api/v1/platforms/platform-1/spec", map[string]any{"spec": grown}, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing generation status = %d, want 400", resp.StatusCode)
	}
}

// DELETE /platforms/{id} proposes the uninstall with the removal choices as a Change Plan; only an
// admin may ask; recordOnly=true removes only the record.
func TestDeletePlatformProposesItsUninstall(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "node-1", "10.0.1.10", nil)
	seedInstalledPlatform(t, f)

	resp := doRequest(t, f.app, "DELETE", "/api/v1/platforms/platform-1", map[string]any{"releaseServers": true},
		map[string]string{"Authorization": "Bearer " + userToken(t, f.jwtSvc)})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, want 403", resp.StatusCode)
	}
	resp = doRequest(t, f.app, "DELETE", "/api/v1/platforms/platform-1", map[string]any{
		"releaseServers": true, "releaseOptions": map[string]any{"erase": true},
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", resp.StatusCode, rawBody(t, resp))
	}
	body := parseBody(t, resp)
	plan := body["plan"].(map[string]any)
	actions := plan["actions"].([]any)
	wantEffect := "1 member is released to the provisioner, which removes its operating system; " +
		"its disks are erased and its static IP bindings are kept. The Platform record is deleted once the release succeeds."
	if effects := plan["effects"].([]any); actions[0].(map[string]any)["action"] != "uninstall" || len(effects) != 1 || effects[0] != wantEffect {
		t.Errorf("plan = %v, want an uninstall whose effect is %q", plan, wantEffect)
	}
	spec := body["platform"].(map[string]any)["spec"].(map[string]any)
	if spec["desired"] != "absent" || spec["removal"].(map[string]any)["erase"] != true {
		t.Errorf("spec = %v, want absent with the removal choices", spec)
	}

	resp = doRequest(t, f.app, "DELETE", "/api/v1/platforms/platform-1?recordOnly=true", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("record-only status = %d, want 204: %s", resp.StatusCode, rawBody(t, resp))
	}
	if _, err := f.platformRepo.FindByID(context.Background(), "platform-1"); err == nil {
		t.Error("the Platform's record remains after a record-only delete")
	}
}
