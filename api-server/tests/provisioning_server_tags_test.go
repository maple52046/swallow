package tests

import (
	"net/http"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// A tag edit writes each Server's spec and never drives the provisioner: the Server controller
// converges the tags afterwards (decision 071). The spec is the base, whatever is observed, and
// the rest of the spec (the placement) is kept. The response carries each Server's new generation
// and complete manual tag set.
func TestEditServerTags_WritesSpecWithoutDrivingProvider(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	f.seedServer("s1", "gpu-1", "10.0.0.1", func(s *serverdomain.Server) {
		s.Observed.Tags = []string{"gpu", "observed-only"}
		s.Generation, s.Spec = 1, serverdomain.ServerSpec{Tags: []string{"gpu"}, ZoneID: "z1"}
	})
	f.seedServer("s2", "gpu-2", "10.0.0.2", func(s *serverdomain.Server) {
		s.Observed.Tags = nil
		s.Generation, s.Spec.Tags = 3, []string{"kept"}
	})
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s1", Tags: []string{"gpu"}})
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s2"})

	resp := doRequest(t, f.app, "POST", "/api/v1/provisioning/tags", map[string]any{
		"serverIds": []string{"s1", "s2"},
		"add":       []string{"rack-a"},
		"remove":    []string{"gpu"},
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (%s)", resp.StatusCode, rawBody(t, resp))
	}

	body := parseBody(t, resp)
	servers, ok := body["servers"].([]any)
	if !ok || len(servers) != 2 {
		t.Fatalf("expected 2 servers in response, got %v", body)
	}
	assertResponseTags(t, servers, "s1", []string{"rack-a"})
	assertResponseTags(t, servers, "s2", []string{"kept", "rack-a"})

	if n := len(f.provider.actions); n != 0 {
		t.Errorf("the provisioner was driven %d times (%v); a tag edit only writes the spec", n, f.provider.actions)
	}
	if s := f.servers.servers["s1"]; s.Generation != 2 || !equalStringSet(s.Spec.Tags, []string{"rack-a"}) || s.Spec.ZoneID != "z1" {
		t.Errorf("s1 spec = %+v at generation %d, want [rack-a] in z1 at 2", s.Spec, s.Generation)
	}
	if s := f.servers.servers["s2"]; s.Generation != 4 {
		t.Errorf("s2 generation = %d, want 4", s.Generation)
	}
}

// A Server not adopted yet has no spec to edit; the edit is refused as a retryable conflict and
// changes no Server.
func TestEditServerTags_RefusesAServerNotAdoptedYet(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	f.seedServer("s1", "gpu-1", "10.0.0.1", func(s *serverdomain.Server) { s.Generation, s.Spec.Tags = 1, []string{"a"} })
	f.seedServer("s2", "gpu-2", "10.0.0.2", nil)

	resp := doRequest(t, f.app, "POST", "/api/v1/provisioning/tags", map[string]any{
		"serverIds": []string{"s1", "s2"}, "add": []string{"rack-a"},
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status: got %d, want 409 (%s)", resp.StatusCode, rawBody(t, resp))
	}
	if s := f.servers.servers["s1"]; s.Generation != 1 {
		t.Errorf("s1 generation = %d, want 1: the edit is all or nothing", s.Generation)
	}
}

func TestListServerTags_ReportsEditableFlag(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s1", Tags: []string{"rack-a"}})
	// An automatic tag (a MAAS tag with a definition) is read-only through swallow.
	f.provider.autoTags["amd-gpu"] = true

	resp := doRequest(t, f.app, "GET", "/api/v1/provisioning/tags?siteId="+testSiteID, nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (%s)", resp.StatusCode, rawBody(t, resp))
	}
	body := parseBody(t, resp)
	tags, ok := body["tags"].([]any)
	if !ok {
		t.Fatalf("expected a tags array, got %v", body)
	}
	editable := map[string]bool{}
	found := map[string]bool{}
	for _, raw := range tags {
		tag := raw.(map[string]any)
		name := tag["name"].(string)
		found[name] = true
		editable[name] = tag["editable"].(bool)
	}
	if !found["rack-a"] || !editable["rack-a"] {
		t.Errorf("rack-a should be present and editable, got %v", tags)
	}
	if !found["amd-gpu"] || editable["amd-gpu"] {
		t.Errorf("amd-gpu should be present and not editable, got %v", tags)
	}
}

func TestEditServerTags_AutoTagRefusalIsValidationError(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	f.seedServer("s1", "gpu-1", "10.0.0.1", func(s *serverdomain.Server) { s.Generation = 1 })
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s1"})
	f.provider.autoTags["amd-gpu"] = true

	resp := doRequest(t, f.app, "POST", "/api/v1/provisioning/tags", map[string]any{
		"serverIds": []string{"s1"},
		"add":       []string{"amd-gpu"},
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (%s)", resp.StatusCode, rawBody(t, resp))
	}
	if code := errorCode(t, resp); code != "validation_error" {
		t.Errorf("error code: got %q, want validation_error", code)
	}
}

func TestEditServerTags_RejectsInvalidRequests(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	f.seedServer("s1", "gpu-1", "10.0.0.1", nil)
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s1"})

	cases := []struct {
		name string
		body map[string]any
	}{
		{"no servers", map[string]any{"serverIds": []string{}, "add": []string{"rack-a"}}},
		{"no diff", map[string]any{"serverIds": []string{"s1"}}},
		{"bad tag name", map[string]any{"serverIds": []string{"s1"}, "add": []string{"has space"}}},
		{"add and remove same", map[string]any{"serverIds": []string{"s1"}, "add": []string{"x"}, "remove": []string{"x"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doRequest(t, f.app, "POST", "/api/v1/provisioning/tags", tc.body, f.adminAuth(t))
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status: got %d, want 400 (%s)", resp.StatusCode, rawBody(t, resp))
			}
		})
	}
}

func assertResponseTags(t *testing.T, servers []any, serverID string, want []string) {
	t.Helper()
	for _, raw := range servers {
		item := raw.(map[string]any)
		if item["serverId"] != serverID {
			continue
		}
		var got []string
		for _, tag := range item["tags"].([]any) {
			got = append(got, tag.(string))
		}
		if !equalStringSet(got, want) {
			t.Errorf("%s response tags: got %v, want %v", serverID, got, want)
		}
		return
	}
	t.Errorf("server %s not found in response", serverID)
}

func assertActionCount(t *testing.T, actions []string, want string, count int) {
	t.Helper()
	got := 0
	for _, action := range actions {
		if action == want {
			got++
		}
	}
	if got != count {
		t.Errorf("action %q: happened %d times, want %d (actions=%v)", want, got, count, actions)
	}
}

func equalStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := map[string]int{}
	for _, v := range a {
		counts[v]++
	}
	for _, v := range b {
		counts[v]--
	}
	for _, c := range counts {
		if c != 0 {
			return false
		}
	}
	return true
}
