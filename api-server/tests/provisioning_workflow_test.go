package tests

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

func createTemplate(
	t *testing.T,
	f *platformFixture,
	name, userData string,
) map[string]any {
	t.Helper()
	resp := doRequest(t, f.app, "POST", "/api/v1/provisioning/templates", map[string]any{
		"integrationId": testIntegrationID,
		"name":          name,
		"description":   "Ubuntu compute baseline",
		"imageId":       "ubuntu/jammy",
		"ephemeral":     false,
		"userData":      userData,
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create template: expected 201, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}
	return parseBody(t, resp)
}

func seedReadyServer(t *testing.T, f *platformFixture, id string) {
	t.Helper()
	f.seedServer(id, id, "10.0.0."+strings.TrimPrefix(id, "srv-"), func(server *serverdomain.Server) {
		server.Provisioning.State = "ready"
		server.Provisioning.ProviderState = "Ready"
		server.Provisioning.OSSystem = ""
		server.Provisioning.DistroSeries = ""
	})
	f.provider.withMachine(testMachine("machine-"+id, id))
}

func TestDeploymentTemplate_CRUDKeepsUserDataWriteOnly(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)

	item := createTemplate(t, f, "Compute baseline", "#cloud-config\\npassword: secret")
	if item["hasUserData"] != true {
		t.Fatalf("expected hasUserData, got %v", item["hasUserData"])
	}
	if _, leaked := item["userData"]; leaked {
		t.Fatal("create response must never contain userData")
	}

	id := item["id"].(string)
	resp := doRequest(t, f.app, "GET", "/api/v1/provisioning/templates/"+id, nil, f.adminAuth(t))
	raw := rawBody(t, resp)
	if strings.Contains(raw, "password: secret") || strings.Contains(raw, "userData") {
		t.Fatalf("template read leaked write-only content: %s", raw)
	}

	resp = doRequest(t, f.app, "PATCH", "/api/v1/provisioning/templates/"+id, map[string]any{
		"name":        "Compute baseline v2",
		"description": "Renamed",
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d", resp.StatusCode)
	}
	updated := parseBody(t, resp)
	if updated["name"] != "Compute baseline v2" || updated["hasUserData"] != true {
		t.Fatalf("unexpected update: %v", updated)
	}

	resp = doRequest(t, f.app, "PUT", "/api/v1/provisioning/templates/"+id+"/user-data", map[string]any{
		"userData": "#cloud-config\\nhostname: replaced",
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("replace user data: expected 204, got %d", resp.StatusCode)
	}
	if stored, err := f.templates.UserData(t.Context(), id); err != nil || stored != "#cloud-config\\nhostname: replaced" {
		t.Fatalf("write-only content was not replaced: value=%q err=%v", stored, err)
	}

	resp = doRequest(t, f.app, "DELETE", "/api/v1/provisioning/templates/"+id+"/user-data", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("clear user data: expected 204, got %d", resp.StatusCode)
	}
	resp = doRequest(t, f.app, "GET", "/api/v1/provisioning/templates/"+id, nil, f.adminAuth(t))
	if body := parseBody(t, resp); body["hasUserData"] != false {
		t.Fatalf("expected hasUserData false, got %v", body["hasUserData"])
	}

	resp = doRequest(t, f.app, "DELETE", "/api/v1/provisioning/templates/"+id, nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", resp.StatusCode)
	}
}

func TestDeploymentTemplate_NameIsCaseInsensitivePerIntegration(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	createTemplate(t, f, "Compute Baseline", "")

	resp := doRequest(t, f.app, "POST", "/api/v1/provisioning/templates", map[string]any{
		"integrationId": testIntegrationID,
		"name":          " compute baseline ",
		"imageId":       "ubuntu/jammy",
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusConflict || errorCode(t, resp) != "conflict" {
		t.Fatalf("expected case-insensitive conflict, got %d", resp.StatusCode)
	}
}

func TestDeploymentTemplate_FiltersAndAuthorization(t *testing.T) {
	f := setupPlatform(t)
	now := time.Now().UTC()
	f.sites.sites[testSiteID] = &sitedomain.Site{ID: testSiteID, Name: "East", CreatedAt: now, UpdatedAt: now}
	seedProvisionerIntegration(t, f)
	createTemplate(t, f, "East baseline", "")

	resp := doRequest(t, f.app, "GET", "/api/v1/provisioning/templates?siteId="+testSiteID, nil, f.adminAuth(t))
	if items := parseArrayBody(t, resp); len(items) != 1 || items[0]["siteId"] != testSiteID {
		t.Fatalf("unexpected scoped templates: %v", items)
	}

	resp = doRequest(t, f.app, "GET", "/api/v1/provisioning/templates?siteId=missing", nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown site: expected 404, got %d", resp.StatusCode)
	}

	resp = doRequest(t, f.app, "GET", "/api/v1/provisioning/templates", nil, map[string]string{
		"Authorization": "Bearer " + userToken(t, f.jwtSvc),
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin: expected 403, got %d", resp.StatusCode)
	}
}

func TestDeploymentTemplate_ValidatesLiveImageAndBlocksIntegrationDelete(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)

	resp := doRequest(t, f.app, "POST", "/api/v1/provisioning/templates", map[string]any{
		"integrationId": testIntegrationID,
		"name":          "Missing image",
		"imageId":       "ubuntu/missing",
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing image: expected 400, got %d", resp.StatusCode)
	}

	f.provider.imageErr = &provisioningdomain.ProviderError{
		Kind: provisioningdomain.ProviderErrorUnavailable, Detail: "Image service unavailable.",
	}
	resp = doRequest(t, f.app, "POST", "/api/v1/provisioning/templates", map[string]any{
		"integrationId": testIntegrationID,
		"name":          "Unavailable image",
		"imageId":       "ubuntu/jammy",
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("provider unavailable: expected 503, got %d", resp.StatusCode)
	}
	f.provider.imageErr = nil

	createTemplate(t, f, "Referenced template", "")
	resp = doRequest(t, f.app, "DELETE", "/api/v1/integrations/"+testIntegrationID, nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("referenced integration: expected 409, got %d", resp.StatusCode)
	}
}

func TestDeployServers_InlineAndTemplateModes(t *testing.T) {
	t.Run("inline replace", func(t *testing.T) {
		f := setupPlatform(t)
		seedProvisionerIntegration(t, f)
		seedReadyServer(t, f, "srv-1")
		seedReadyServer(t, f, "srv-2")

		resp := doRequest(t, f.app, "POST", "/api/v1/provisioning/deployments", map[string]any{
			"serverIds": []string{"srv-1", "srv-2"},
			"settings":  map[string]any{"imageId": "ubuntu/jammy", "ephemeral": true},
			"userData":  map[string]any{"mode": "replace", "value": "#cloud-config"},
		}, f.adminAuth(t))
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("expected 202, got %d: %s", resp.StatusCode, rawBody(t, resp))
		}
		body := parseBody(t, resp)
		if body["requested"] != float64(2) || len(body["accepted"].([]any)) != 2 || len(body["failed"].([]any)) != 0 {
			t.Fatalf("unexpected result: %v", body)
		}
		for _, request := range f.provider.deployRequests {
			if request.DistroSeries != "ubuntu/jammy" || request.UserData != "#cloud-config" || !request.Ephemeral {
				t.Fatalf("unexpected provider request: %+v", request)
			}
		}
	})

	t.Run("template inherit and omit", func(t *testing.T) {
		f := setupPlatform(t)
		seedProvisionerIntegration(t, f)
		seedReadyServer(t, f, "srv-1")
		template := createTemplate(t, f, "Cloud baseline", "#cloud-config\\nhostname: inherited")
		templateID := template["id"].(string)

		resp := doRequest(t, f.app, "POST", "/api/v1/provisioning/deployments", map[string]any{
			"serverIds":  []string{"srv-1"},
			"templateId": templateID,
			"userData":   map[string]any{"mode": "inherit"},
		}, f.adminAuth(t))
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("inherit: expected 202, got %d", resp.StatusCode)
		}
		if got := f.provider.deployRequests[0].UserData; got != "#cloud-config\\nhostname: inherited" {
			t.Fatalf("expected inherited cloud-init, got %q", got)
		}

		f.provider.deployRequests = nil
		f.servers.servers["srv-1"].Provisioning.State = "ready"
		resp = doRequest(t, f.app, "POST", "/api/v1/provisioning/deployments", map[string]any{
			"serverIds":  []string{"srv-1"},
			"templateId": templateID,
			"userData":   map[string]any{"mode": "omit"},
		}, f.adminAuth(t))
		if resp.StatusCode != http.StatusAccepted || f.provider.deployRequests[0].UserData != "" {
			t.Fatalf("omit must send no user data, status=%d request=%+v", resp.StatusCode, f.provider.deployRequests)
		}
	})
}
func TestDeployServers_ProviderReadinessRunsAfterNetworkConfiguration(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	seedReadyServer(t, f, "srv-1")
	f.provider.readinessErrByMachine["machine-srv-1"] = &provisioningdomain.ProviderError{
		Kind:   provisioningdomain.ProviderErrorRejected,
		Detail: "No MAAS interface is linked to a subnet. Configure the machine's Network in MAAS, then check deployment readiness again.",
	}

	resp := doRequest(t, f.app, "POST", "/api/v1/provisioning/deployments/preflight", map[string]any{
		"serverIds": []string{"srv-1"},
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("target preflight: expected 200, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}
	body := parseBody(t, resp)
	if body["valid"] != true || body["integrationId"] != testIntegrationID {
		t.Fatalf("unexpected target preflight result: %v", body)
	}
	issues := body["issues"].([]any)
	if len(issues) != 0 {
		t.Fatalf("read-only target preflight must defer provider network readiness: %v", issues)
	}

	resp = doRequest(t, f.app, "POST", "/api/v1/provisioning/deployments", map[string]any{
		"serverIds": []string{"srv-1"},
		"settings":  map[string]any{"imageId": "ubuntu/jammy"},
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("post-network readiness refusal: expected 202, got %d: %s", resp.StatusCode, rawBody(t, resp))
	}
	failure := parseBody(t, resp)["failed"].([]any)[0].(map[string]any)
	if failure["stage"] != "network_configuration" || !strings.Contains(failure["message"].(string), "Network in MAAS") {
		t.Fatalf("unexpected readiness failure: %v", failure)
	}
	if len(f.provider.deployRequests) != 0 {
		t.Fatalf("provider readiness failure must dispatch nothing, got %d calls", len(f.provider.deployRequests))
	}
}
func TestDeploymentTargetPreflight_PerformsLocalValidationBeforeNetworkWorkflow(t *testing.T) {
	t.Run("provider readiness is deferred", func(t *testing.T) {
		f := setupPlatform(t)
		seedProvisionerIntegration(t, f)
		seedReadyServer(t, f, "srv-1")
		f.provider.readinessErrByMachine["machine-srv-1"] = &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorUnavailable,
			Detail: "MAAS is unavailable.",
		}

		resp := doRequest(t, f.app, "POST", "/api/v1/provisioning/deployments/preflight", map[string]any{
			"serverIds": []string{"srv-1"},
		}, f.adminAuth(t))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("target preflight: expected 200, got %d: %s", resp.StatusCode, rawBody(t, resp))
		}
		body := parseBody(t, resp)
		if body["valid"] != true || len(body["issues"].([]any)) != 0 {
			t.Fatalf("provider readiness should be deferred: %v", body)
		}
	})

	t.Run("local conflict is not masked by provider failure", func(t *testing.T) {
		f := setupPlatform(t)
		seedProvisionerIntegration(t, f)
		f.seedServer("srv-1", "srv-1", "10.0.0.1", nil)
		f.provider.withMachine(testMachine("machine-srv-1", "srv-1"))
		f.provider.readinessErrByMachine["machine-srv-1"] = &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorUnavailable,
			Detail: "MAAS is unavailable.",
		}

		resp := doRequest(t, f.app, "POST", "/api/v1/provisioning/deployments/preflight", map[string]any{
			"serverIds": []string{"srv-1"},
		}, f.adminAuth(t))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("local conflict: expected 200, got %d: %s", resp.StatusCode, rawBody(t, resp))
		}
		issues := parseBody(t, resp)["issues"].([]any)
		if len(issues) != 1 || issues[0].(map[string]any)["code"] != "not_ready" {
			t.Fatalf("local conflict issues: got %v, want one not_ready issue", issues)
		}
	})
}

func TestDeployServers_PreflightIsAllOrNothing(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	seedReadyServer(t, f, "srv-1")
	f.seedServer("srv-2", "srv-2", "10.0.0.2", nil)
	f.provider.withMachine(testMachine("machine-srv-2", "srv-2"))

	resp := doRequest(t, f.app, "POST", "/api/v1/provisioning/deployments", map[string]any{
		"serverIds": []string{"srv-1", "srv-2"},
		"settings":  map[string]any{"imageId": "ubuntu/jammy"},
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	if len(f.provider.deployRequests) != 0 {
		t.Fatalf("preflight failure must dispatch nothing, got %d calls", len(f.provider.deployRequests))
	}

	tooMany := make([]string, 101)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("srv-%d", index)
	}
	resp = doRequest(t, f.app, "POST", "/api/v1/provisioning/deployments", map[string]any{
		"serverIds": tooMany,
		"settings":  map[string]any{"imageId": "ubuntu/jammy"},
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("101 targets: expected 400, got %d", resp.StatusCode)
	}
}

func TestDeployServers_BoundsConcurrencyAndReturnsPartialFailures(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	serverIDs := make([]string, 8)
	for index := range serverIDs {
		serverIDs[index] = fmt.Sprintf("srv-%d", index+1)
		seedReadyServer(t, f, serverIDs[index])
	}
	f.provider.deployDelay = 20 * time.Millisecond
	f.provider.deployErrByMachine["machine-srv-3"] = &provisioningdomain.ProviderError{
		Kind: provisioningdomain.ProviderErrorRejected, Detail: "Machine reservation changed.",
	}

	resp := doRequest(t, f.app, "POST", "/api/v1/provisioning/deployments", map[string]any{
		"serverIds": serverIDs,
		"settings":  map[string]any{"imageId": "ubuntu/jammy"},
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if len(body["accepted"].([]any)) != 7 || len(body["failed"].([]any)) != 1 {
		t.Fatalf("unexpected partial result: %v", body)
	}
	failure := body["failed"].([]any)[0].(map[string]any)
	if failure["serverId"] != "srv-3" || failure["code"] != "provider_rejected" {
		t.Fatalf("unexpected failure: %v", failure)
	}
	if f.provider.maxActiveDeploys > 4 {
		t.Fatalf("provider concurrency exceeded 4: %d", f.provider.maxActiveDeploys)
	}
	if f.provider.maxActiveDeploys != 4 {
		t.Fatalf("expected dispatcher to use four workers, got %d", f.provider.maxActiveDeploys)
	}
}
