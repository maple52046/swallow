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

// The list reports each tag's kind and how many Servers carry it, and includes a spec tag the
// provisioner has not created yet. A maintained automatic tag (`virtual`) is removable; one computed
// from a rule is not.
func TestListServerTags_ReportsKindsAndServerCounts(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s1", Tags: []string{"rack-a", "virtual", "amd-gpu"}})
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s2", Tags: []string{"rack-a"}})
	f.provider.maintainedTags["virtual"] = true
	f.provider.autoTags["amd-gpu"] = true
	f.provider.autoTags["unused-rule"] = true
	f.seedServer("s1", "gpu-1", "10.0.0.1", func(s *serverdomain.Server) {
		s.Observed.Tags = []string{"rack-a", "virtual", "amd-gpu"}
		s.Observed.AutomaticTags = []string{"virtual", "amd-gpu"}
		s.Generation, s.Spec.Tags = 1, []string{"rack-a", "pending"}
	})
	f.seedServer("s2", "gpu-2", "10.0.0.2", func(s *serverdomain.Server) {
		s.Observed.Tags = []string{"rack-a"}
		s.Generation, s.Spec.Tags = 1, []string{"rack-a"}
	})

	resp := doRequest(t, f.app, "GET", "/api/v1/provisioning/tags?siteId="+testSiteID, nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (%s)", resp.StatusCode, rawBody(t, resp))
	}
	got := map[string]map[string]any{}
	for _, raw := range parseBody(t, resp)["tags"].([]any) {
		tag := raw.(map[string]any)
		got[tag["name"].(string)] = tag
	}
	want := map[string][3]any{ // editable, removable, serverCount
		"rack-a":      {true, true, float64(2)},
		"pending":     {true, true, float64(1)},
		"virtual":     {false, true, float64(1)},
		"amd-gpu":     {false, false, float64(1)},
		"unused-rule": {false, false, float64(0)},
	}
	if len(got) != len(want) {
		t.Errorf("tags: got %v, want %d tags", got, len(want))
	}
	for name, w := range want {
		tag, ok := got[name]
		if !ok {
			t.Errorf("tag %s missing from %v", name, got)
			continue
		}
		if tag["editable"] != w[0] || tag["removable"] != w[1] || tag["serverCount"] != w[2] {
			t.Errorf("tag %s = %v, want editable=%v removable=%v serverCount=%v", name, tag, w[0], w[1], w[2])
		}
	}
}

// Deleting a manual tag removes it from every spec that names it, in one write, and then from the
// provisioner. A Server whose spec does not name it is left alone.
func TestDeleteServerTag_RemovesAManualTagFromEverySpecThenTheProvisioner(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s1", Tags: []string{"gpu", "rack-a"}})
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s2", Tags: []string{"rack-a"}})
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s3", Tags: []string{"kept"}})
	f.seedServer("s1", "gpu-1", "10.0.0.1", func(s *serverdomain.Server) {
		s.Observed.Tags = []string{"gpu", "rack-a"}
		s.Generation, s.Spec = 2, serverdomain.ServerSpec{Tags: []string{"gpu", "rack-a"}, ZoneID: "z1"}
	})
	f.seedServer("s2", "gpu-2", "10.0.0.2", func(s *serverdomain.Server) {
		s.Observed.Tags = []string{"rack-a"}
		s.Generation, s.Spec.Tags = 1, []string{"rack-a"}
	})
	f.seedServer("s3", "gpu-3", "10.0.0.3", func(s *serverdomain.Server) {
		s.Observed.Tags = []string{"kept"}
		s.Generation, s.Spec.Tags = 1, []string{"kept"}
	})

	resp := doRequest(t, f.app, "DELETE", "/api/v1/provisioning/tags/rack-a?siteId="+testSiteID, nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (%s)", resp.StatusCode, rawBody(t, resp))
	}
	servers := parseBody(t, resp)["servers"].([]any)
	if len(servers) != 2 {
		t.Fatalf("servers: got %v, want s1 and s2", servers)
	}
	assertResponseTags(t, servers, "s1", []string{"gpu"})
	assertResponseTags(t, servers, "s2", []string{})

	if s := f.servers.servers["s1"]; s.Generation != 3 || !equalStringSet(s.Spec.Tags, []string{"gpu"}) || s.Spec.ZoneID != "z1" {
		t.Errorf("s1 spec = %+v at generation %d, want [gpu] in z1 at 3", s.Spec, s.Generation)
	}
	if s := f.servers.servers["s3"]; s.Generation != 1 {
		t.Errorf("s3 generation = %d, want 1: its spec does not name the tag", s.Generation)
	}
	assertActionCount(t, f.provider.actions, "deleteTag rack-a", 1)
}

// Deleting an automatic tag deletes it from the provisioner only; a spec that still names it from
// before it was recognized as automatic drops it.
func TestDeleteServerTag_DeletesAnAutomaticTagFromTheProvisioner(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s1", Tags: []string{"virtual"}})
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s2", Tags: []string{"virtual", "gpu"}})
	f.provider.maintainedTags["virtual"] = true
	f.seedServer("s1", "vm-1", "10.0.0.1", func(s *serverdomain.Server) {
		s.Observed.Tags, s.Observed.AutomaticTags = []string{"virtual"}, []string{"virtual"}
		s.Generation, s.Spec.Tags = 1, []string{"virtual"}
	})
	f.seedServer("s2", "vm-2", "10.0.0.2", func(s *serverdomain.Server) {
		s.Observed.Tags, s.Observed.AutomaticTags = []string{"virtual", "gpu"}, []string{"virtual"}
		s.Generation, s.Spec.Tags = 1, []string{"gpu"}
	})

	resp := doRequest(t, f.app, "DELETE", "/api/v1/provisioning/tags/virtual?siteId="+testSiteID, nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (%s)", resp.StatusCode, rawBody(t, resp))
	}
	servers := parseBody(t, resp)["servers"].([]any)
	if len(servers) != 1 {
		t.Fatalf("servers: got %v, want only s1, whose spec named the tag", servers)
	}
	assertResponseTags(t, servers, "s1", []string{})
	if s := f.servers.servers["s2"]; s.Generation != 1 {
		t.Errorf("s2 generation = %d, want 1", s.Generation)
	}
	assertActionCount(t, f.provider.actions, "deleteTag virtual", 1)
}

// A deletion that could not reach the provisioner answers 503 with the specs already written;
// repeating the request finishes it.
func TestDeleteServerTag_RepeatingAfterAnUnreachableProvisionerFinishes(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s1", Tags: []string{"rack-a"}})
	f.seedServer("s1", "gpu-1", "10.0.0.1", func(s *serverdomain.Server) {
		s.Observed.Tags = []string{"rack-a"}
		s.Generation, s.Spec.Tags = 1, []string{"rack-a"}
	})
	f.provider.tagChangeErr = &provisioningdomain.ProviderError{Kind: provisioningdomain.ProviderErrorUnavailable, Detail: "Could not reach MAAS."}

	path := "/api/v1/provisioning/tags/rack-a?siteId=" + testSiteID
	resp := doRequest(t, f.app, "DELETE", path, nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503 (%s)", resp.StatusCode, rawBody(t, resp))
	}
	if s := f.servers.servers["s1"]; s.Generation != 2 || len(s.Spec.Tags) != 0 {
		t.Errorf("s1 spec = %+v at generation %d, want no tags at 2: the specs are written first", s.Spec, s.Generation)
	}

	f.provider.tagChangeErr = nil
	resp = doRequest(t, f.app, "DELETE", path, nil, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("repeated status: got %d, want 200 (%s)", resp.StatusCode, rawBody(t, resp))
	}
	assertActionCount(t, f.provider.actions, "deleteTag rack-a", 1)
}

func TestDeleteServerTag_RefusesRequestsItCannotApply(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s1", Tags: []string{"fresh"}})
	// A newly discovered Server has no spec yet; its adoption would bring the tag back.
	f.seedServer("s1", "gpu-1", "10.0.0.1", func(s *serverdomain.Server) { s.Observed.Tags = []string{"fresh"} })

	cases := []struct {
		name string
		path string
		want int
	}{
		{name: "missing siteId", path: "/api/v1/provisioning/tags/fresh", want: http.StatusBadRequest},
		{name: "invalid name", path: "/api/v1/provisioning/tags/bad%20name?siteId=" + testSiteID, want: http.StatusBadRequest},
		{name: "unknown tag", path: "/api/v1/provisioning/tags/nowhere?siteId=" + testSiteID, want: http.StatusNotFound},
		{name: "site without a provisioner", path: "/api/v1/provisioning/tags/fresh?siteId=elsewhere", want: http.StatusNotFound},
		{name: "carried by a Server not adopted yet", path: "/api/v1/provisioning/tags/fresh?siteId=" + testSiteID, want: http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doRequest(t, f.app, "DELETE", tc.path, nil, f.adminAuth(t))
			if resp.StatusCode != tc.want {
				t.Errorf("DELETE %s: got %d, want %d (%s)", tc.path, resp.StatusCode, tc.want, rawBody(t, resp))
			}
		})
	}
	if n := len(f.provider.actions); n != 0 {
		t.Errorf("the provisioner was driven %d times (%v); every request was refused", n, f.provider.actions)
	}
}

// Renaming a manual tag rewrites every spec that names it and renames it on the provisioner, so a
// name with a dot works in the path too.
func TestRenameServerTag_RenamesInEverySpecAndTheProvisioner(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s1", Tags: []string{"rack.a", "gpu"}})
	f.seedServer("s1", "gpu-1", "10.0.0.1", func(s *serverdomain.Server) {
		s.Observed.Tags = []string{"rack.a", "gpu"}
		s.Generation, s.Spec.Tags = 1, []string{"rack.a", "gpu"}
	})

	resp := doRequest(t, f.app, "PATCH", "/api/v1/provisioning/tags/rack.a?siteId="+testSiteID,
		map[string]any{"name": "rack-b"}, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (%s)", resp.StatusCode, rawBody(t, resp))
	}
	assertResponseTags(t, parseBody(t, resp)["servers"].([]any), "s1", []string{"rack-b", "gpu"})
	assertActionCount(t, f.provider.actions, "renameTag rack.a rack-b", 1)
	if got := f.provider.machines["machine-s1"].Tags; !equalStringSet(got, []string{"rack-b", "gpu"}) {
		t.Errorf("machine tags = %v, want [rack-b gpu]", got)
	}
}

// Renaming onto an existing manual tag merges the two in every spec.
func TestRenameServerTag_MergesIntoAnExistingTag(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s1", Tags: []string{"rack_a", "rack-a"}})
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s2", Tags: []string{"rack_a"}})
	f.seedServer("s1", "gpu-1", "10.0.0.1", func(s *serverdomain.Server) {
		s.Generation, s.Spec.Tags = 1, []string{"rack_a", "rack-a"}
	})
	f.seedServer("s2", "gpu-2", "10.0.0.2", func(s *serverdomain.Server) {
		s.Generation, s.Spec.Tags = 1, []string{"rack_a"}
	})

	resp := doRequest(t, f.app, "PATCH", "/api/v1/provisioning/tags/rack_a?siteId="+testSiteID,
		map[string]any{"name": "rack-a"}, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (%s)", resp.StatusCode, rawBody(t, resp))
	}
	servers := parseBody(t, resp)["servers"].([]any)
	assertResponseTags(t, servers, "s1", []string{"rack-a"})
	assertResponseTags(t, servers, "s2", []string{"rack-a"})
}

func TestRenameServerTag_RefusesAutomaticAndUnchangedNames(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s1", Tags: []string{"rack-a", "virtual"}})
	f.provider.maintainedTags["virtual"] = true
	f.seedServer("s1", "gpu-1", "10.0.0.1", func(s *serverdomain.Server) {
		s.Observed.Tags, s.Observed.AutomaticTags = []string{"rack-a", "virtual"}, []string{"virtual"}
		s.Generation, s.Spec.Tags = 1, []string{"rack-a"}
	})

	cases := []struct {
		name, from, to string
	}{
		{name: "an automatic tag", from: "virtual", to: "vm"},
		{name: "onto an automatic tag", from: "rack-a", to: "virtual"},
		{name: "to the same name", from: "rack-a", to: "rack-a"},
		{name: "to an invalid name", from: "rack-a", to: "has space"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doRequest(t, f.app, "PATCH", "/api/v1/provisioning/tags/"+tc.from+"?siteId="+testSiteID,
				map[string]any{"name": tc.to}, f.adminAuth(t))
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("rename %s to %s: got %d, want 400 (%s)", tc.from, tc.to, resp.StatusCode, rawBody(t, resp))
			}
		})
	}
	if s := f.servers.servers["s1"]; s.Generation != 1 {
		t.Errorf("s1 generation = %d, want 1: every rename was refused", s.Generation)
	}
}

// Removing an automatic tag the provisioner maintains is an action on the machines, not a spec
// change: the spec and its generation stay, and the provisioner takes the tag off.
func TestEditServerTags_RemovesAMaintainedAutomaticTagThroughTheProvisioner(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s1", Tags: []string{"gpu", "virtual"}})
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s2", Tags: []string{"gpu"}})
	f.provider.maintainedTags["virtual"] = true
	f.seedServer("s1", "vm-1", "10.0.0.1", func(s *serverdomain.Server) {
		s.Observed.Tags, s.Observed.AutomaticTags = []string{"gpu", "virtual"}, []string{"virtual"}
		s.Generation, s.Spec.Tags = 1, []string{"gpu"}
	})
	f.seedServer("s2", "gpu-2", "10.0.0.2", func(s *serverdomain.Server) {
		s.Observed.Tags = []string{"gpu"}
		s.Generation, s.Spec.Tags = 1, []string{"gpu"}
	})

	resp := doRequest(t, f.app, "POST", "/api/v1/provisioning/tags", map[string]any{
		"serverIds": []string{"s1", "s2"}, "remove": []string{"virtual"},
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (%s)", resp.StatusCode, rawBody(t, resp))
	}
	servers := parseBody(t, resp)["servers"].([]any)
	assertResponseTags(t, servers, "s1", []string{"gpu"})
	assertResponseTags(t, servers, "s2", []string{"gpu"})
	for _, id := range []string{"s1", "s2"} {
		if s := f.servers.servers[id]; s.Generation != 1 {
			t.Errorf("%s generation = %d, want 1: its manual tags did not change", id, s.Generation)
		}
	}
	assertActionCount(t, f.provider.actions, "removeTag virtual", 1)
	if got := f.provider.machines["machine-s1"].Tags; !equalStringSet(got, []string{"gpu"}) {
		t.Errorf("machine-s1 tags = %v, want [gpu]", got)
	}
}

// An automatic tag computed from a rule cannot be removed from a Server, only deleted.
func TestEditServerTags_RefusesRemovingARuleTag(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	f.provider.withMachine(&provisioningdomain.Machine{ID: "machine-s1", Tags: []string{"amd-gpu"}})
	f.provider.autoTags["amd-gpu"] = true
	f.seedServer("s1", "gpu-1", "10.0.0.1", func(s *serverdomain.Server) {
		s.Observed.Tags, s.Observed.AutomaticTags = []string{"amd-gpu"}, []string{"amd-gpu"}
		s.Generation = 1
	})

	resp := doRequest(t, f.app, "POST", "/api/v1/provisioning/tags", map[string]any{
		"serverIds": []string{"s1"}, "remove": []string{"amd-gpu"},
	}, f.adminAuth(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 (%s)", resp.StatusCode, rawBody(t, resp))
	}
	if n := len(f.provider.actions); n != 0 {
		t.Errorf("the provisioner was driven %d times (%v); the removal was refused", n, f.provider.actions)
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
