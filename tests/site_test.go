package tests

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func createSite(t *testing.T, f *platformFixture, name string) string {
	t.Helper()
	resp := doRequest(t, f.app, "POST", "/api/v1/sites/", map[string]string{
		"name": name,
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create site: expected 201, got %d", resp.StatusCode)
	}
	id, _ := parseBody(t, resp)["id"].(string)
	if id == "" {
		t.Fatal("expected a site id")
	}
	return id
}

func TestCreateSite_AndDuplicateName(t *testing.T) {
	f := setupPlatform(t)
	createSite(t, f, "dc-east")

	resp := doRequest(t, f.app, "POST", "/api/v1/sites/", map[string]string{
		"name": "dc-east",
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 on a duplicate name, got %d", resp.StatusCode)
	}
}

func TestCreateSite_RequiresName(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "POST", "/api/v1/sites/", map[string]string{}, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// Deleting a site out from under its integrations would orphan every server projected
// through them, so it is refused rather than cascaded.
func TestDeleteSite_RefusedWhileIntegrationsExist(t *testing.T) {
	f := setupPlatform(t)
	siteID := createSite(t, f, "dc-east")

	resp := doRequest(t, f.app, "POST", "/api/v1/integrations/", map[string]any{
		"siteId":       siteID,
		"kind":         "provisioner",
		"providerKind": "maas",
		"name":         "maas-east",
		"endpoint":     "http://maas:5240/MAAS",
		"credential":   "ck:tk:ts",
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create integration: expected 201, got %d", resp.StatusCode)
	}

	resp = doRequest(t, f.app, "DELETE", "/api/v1/sites/"+siteID, nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	if code := errorCode(t, resp); code != "conflict" {
		t.Fatalf("expected conflict, got %s", code)
	}
}

func TestCreateIntegration_ValidatesKindAndProviderKindTogether(t *testing.T) {
	f := setupPlatform(t)
	siteID := createSite(t, f, "dc-east")

	// Registering a MAAS endpoint as an automation controller is the mistake that
	// actually happens, and it must be caught here rather than at first use.
	resp := doRequest(t, f.app, "POST", "/api/v1/integrations/", map[string]any{
		"siteId":       siteID,
		"kind":         "automation",
		"providerKind": "maas",
		"name":         "wrong",
		"endpoint":     "http://maas:5240/MAAS",
	}, f.adminAuth(t))

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	message, _ := parseBody(t, resp)["error"].(map[string]any)["message"].(string)
	if !strings.Contains(message, "awx") {
		t.Errorf("the error should name the valid provider kinds, got %q", message)
	}
}

func TestCreateIntegration_RejectsUnknownKind(t *testing.T) {
	f := setupPlatform(t)
	siteID := createSite(t, f, "dc-east")

	resp := doRequest(t, f.app, "POST", "/api/v1/integrations/", map[string]any{
		"siteId":       siteID,
		"kind":         "telepathy",
		"providerKind": "maas",
		"name":         "nope",
		"endpoint":     "http://x",
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCreateIntegration_RejectsUnknownSite(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "POST", "/api/v1/integrations/", map[string]any{
		"siteId":       "nope",
		"kind":         "provisioner",
		"providerKind": "maas",
		"name":         "maas",
		"endpoint":     "http://maas:5240/MAAS",
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// The credential must not be readable through any response, in any form.
func TestIntegration_CredentialIsNeverReturned(t *testing.T) {
	f := setupPlatform(t)
	siteID := createSite(t, f, "dc-east")
	const credential = "consumer-key:token-key:token-secret"

	resp := doRequest(t, f.app, "POST", "/api/v1/integrations/", map[string]any{
		"siteId":       siteID,
		"kind":         "provisioner",
		"providerKind": "maas",
		"name":         "maas-east",
		"endpoint":     "http://maas:5240/MAAS",
		"credential":   credential,
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	created := parseBody(t, resp)
	integrationID, _ := created["id"].(string)

	if created["hasCredential"] != true {
		t.Error("an operator needs to know a credential is stored")
	}

	// Every read path.
	for _, path := range []string{
		"/api/v1/integrations/",
		"/api/v1/integrations/" + integrationID,
	} {
		body := rawBody(t, doRequest(t, f.app, "GET", path, nil, f.adminAuth(t)))
		for _, part := range []string{credential, "consumer-key", "token-secret"} {
			if strings.Contains(body, part) {
				t.Errorf("%s leaked %q", path, part)
			}
		}
	}
}

func TestIntegration_ReplaceCredential(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)

	resp := doRequest(t, f.app, "PUT", "/api/v1/integrations/"+testIntegrationID+"/credential",
		map[string]string{"credential": "new:key:here"}, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	stored, err := f.integrations.Credential(context.Background(), testIntegrationID)
	if err != nil {
		t.Fatalf("Credential: %v", err)
	}
	if stored != "new:key:here" {
		t.Fatalf("credential not replaced, got %q", stored)
	}
}

func TestIntegration_ReplaceCredentialRejectsEmpty(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)

	resp := doRequest(t, f.app, "PUT", "/api/v1/integrations/"+testIntegrationID+"/credential",
		map[string]string{"credential": ""}, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// Deleting an integration servers are keyed on would strand them.
func TestDeleteIntegration_RefusedWhileServersExist(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	f.seedServer("srv-1", "gpu-node-01", "10.0.1.10", nil)

	resp := doRequest(t, f.app, "DELETE", "/api/v1/integrations/"+testIntegrationID, nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestDeleteIntegration_SucceedsWithoutServers(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)

	resp := doRequest(t, f.app, "DELETE", "/api/v1/integrations/"+testIntegrationID, nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// Sync state is part of the API: a reader must be able to tell stale data from current.
func TestListIntegrations_ExposesSyncState(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	f.provider.withMachine(testMachine("m-1", "gpu-node-01"))

	doRequest(t, f.app, "POST", "/api/v1/provisioning/integrations/"+testIntegrationID+"/reconcile",
		nil, f.adminAuth(t))

	resp := doRequest(t, f.app, "GET", "/api/v1/integrations/", nil, f.adminAuth(t))
	items := parseArrayBody(t, resp)
	if len(items) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(items))
	}

	sync, ok := items[0]["sync"].(map[string]any)
	if !ok {
		t.Fatalf("expected a sync object, got %v", items[0]["sync"])
	}
	if sync["lastSucceededAt"] == nil {
		t.Error("a successful reconcile must be visible as freshness")
	}
	if sync["lastError"] != nil {
		t.Errorf("expected no error, got %v", sync["lastError"])
	}
}

func TestUpdateIntegration_CanPauseWithoutLosingConfig(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	disabled := false

	resp := doRequest(t, f.app, "PATCH", "/api/v1/integrations/"+testIntegrationID,
		map[string]any{"enabled": &disabled}, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := parseBody(t, resp)
	if body["enabled"] != false {
		t.Errorf("expected enabled=false, got %v", body["enabled"])
	}
	if body["endpoint"] == "" {
		t.Error("pausing must not clear the endpoint")
	}
	if body["hasCredential"] != true {
		t.Error("pausing must not clear the credential")
	}
}

func TestSiteRoutes_NonAdminForbidden(t *testing.T) {
	f := setupPlatform(t)

	resp := doRequest(t, f.app, "GET", "/api/v1/sites/", nil, map[string]string{
		"Authorization": "Bearer " + userToken(t, f.jwtSvc),
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}
