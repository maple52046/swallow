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

// approve=true approves the proposed plan with the write, decided by the requesting admin, on a
// create and on a delete; any other value of the parameter approves nothing.
func TestSpecWritesApprovedWithTheRequest(t *testing.T) {
	f := setupPlatform(t)
	seedSpecSite(f)
	f.seedServer("srv-1", "node-1", "10.0.1.10", nil)
	f.seedServer("srv-2", "node-2", "10.0.1.11", nil)

	for _, tc := range []struct {
		query, name, serverID, wantState string
	}{
		{query: "?approve=true", name: "approved", serverID: "srv-1", wantState: "approved"},
		{query: "?approve=yes", name: "not-approved", serverID: "srv-2", wantState: "pending_approval"},
	} {
		resp := doRequest(t, f.app, "POST", "/api/v1/platforms"+tc.query, map[string]any{
			"siteId": testSiteID, "name": tc.name, "type": "kubernetes",
			"spec": kubernetesSpecBody(member(tc.serverID, "control-plane", "workloads")),
		}, f.adminAuth(t))
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("POST /platforms%s status = %d, want 202: %s", tc.query, resp.StatusCode, rawBody(t, resp))
		}
		plan := parseBody(t, resp)["plan"].(map[string]any)
		if plan["state"] != tc.wantState {
			t.Errorf("POST /platforms%s plan state = %v, want %s", tc.query, plan["state"], tc.wantState)
		}
		if tc.wantState == "approved" && (plan["decidedBy"] != "admin" || plan["decidedAt"] == nil) {
			t.Errorf("POST /platforms%s plan decided by %v at %v, want the requesting admin", tc.query, plan["decidedBy"], plan["decidedAt"])
		}
		if tc.wantState == "pending_approval" && plan["stateReason"] != "submitted without approval" {
			t.Errorf("POST /platforms%s plan reason = %v, want why it waits", tc.query, plan["stateReason"])
		}
	}

	f2 := setupPlatform(t)
	f2.seedServer("srv-1", "node-1", "10.0.1.10", nil)
	seedInstalledPlatform(t, f2)
	resp := doRequest(t, f2.app, "DELETE", "/api/v1/platforms/platform-1?approve=true", map[string]any{"releaseServers": true}, f2.adminAuth(t))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("DELETE ?approve=true status = %d, want 202: %s", resp.StatusCode, rawBody(t, resp))
	}
	if plan := parseBody(t, resp)["plan"].(map[string]any); plan["state"] != "approved" || plan["decidedBy"] != "admin" {
		t.Errorf("DELETE ?approve=true plan = %v, want the uninstall approved by admin", plan)
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

// A Platform whose deployment started is uninstalled rather than deleted at once: DELETE answers 202
// with the uninstall's plan and the record stays. Deleting an absent Platform while its uninstall runs
// changes nothing and answers 200 without a plan.
func TestDeletePlatformWhoseDeploymentStarted(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "node-1", "10.0.1.10", nil)
	platform := seedInstalledPlatform(t, f)
	platform.Status = platformdomain.PlatformStatus{}
	deployment := f.platformLifecycle.snapshots[platform.ID].Deployment
	deployment.Status = "running"
	f.platformLifecycle.snapshots[platform.ID] = platformdomain.LifecycleSnapshot{
		Origin: platformdomain.PlatformOriginDeployed, State: platformdomain.PlatformLifecycleDeploying, OperationID: deployment.ID, Deployment: deployment,
	}

	resp := doRequest(t, f.app, "DELETE", "/api/v1/platforms/platform-1", map[string]any{"cancelActiveWorkflows": true}, f.adminAuth(t))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("deploying status = %d, want 202: %s", resp.StatusCode, rawBody(t, resp))
	}
	if plan, _ := parseBody(t, resp)["plan"].(map[string]any); plan == nil || plan["actions"].([]any)[0].(map[string]any)["action"] != "uninstall" {
		t.Errorf("deploying plan = %v, want the uninstall", plan)
	}
	if _, err := f.platformRepo.FindByID(context.Background(), platform.ID); err != nil {
		t.Fatalf("the record was deleted before its uninstall: %v", err)
	}

	f2 := setupPlatform(t)
	f2.seedServer("srv-1", "node-1", "10.0.1.10", nil)
	absent := seedInstalledPlatform(t, f2)
	absent.Spec.Desired = platformdomain.PlatformAbsent
	snapshot := f2.platformLifecycle.snapshots[absent.ID]
	snapshot.State = platformdomain.PlatformLifecycleUninstalling
	snapshot.Uninstall = &platformdomain.LifecycleOperation{ID: "uninstall-1", Status: "running", RequestedAt: snapshot.Deployment.RequestedAt.Add(time.Minute)}
	f2.platformLifecycle.snapshots[absent.ID] = snapshot
	resp = doRequest(t, f2.app, "DELETE", "/api/v1/platforms/platform-1", map[string]any{"releaseServers": true}, f2.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("uninstalling status = %d, want 200: %s", resp.StatusCode, rawBody(t, resp))
	}
	body := parseBody(t, resp)
	if body["plan"] != nil || body["platform"].(map[string]any)["generation"] != float64(1) {
		t.Errorf("uninstalling response = %v, want the Platform unchanged without a plan", body)
	}
}

// DELETE /platforms/{id} proposes the uninstall with the removal choices as a Change Plan; a User
// who cannot see the Platform gets 404 (decision 082); recordOnly=true removes only the record.
func TestDeletePlatformProposesItsUninstall(t *testing.T) {
	f := setupPlatform(t)
	f.seedServer("srv-1", "node-1", "10.0.1.10", nil)
	seedInstalledPlatform(t, f)

	resp := doRequest(t, f.app, "DELETE", "/api/v1/platforms/platform-1", map[string]any{"releaseServers": true},
		map[string]string{"Authorization": "Bearer " + userToken(t, f.jwtSvc)})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status for a User outside the Platform's Pool = %d, want 404", resp.StatusCode)
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
