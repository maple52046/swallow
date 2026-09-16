package tests

import (
	"context"
	"net/http"
	"testing"

	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// The tag endpoints exercise the capability-first rule (decision 031) end to end: a MAAS-capable
// provisioner is driven directly and each Server refreshed, and the response carries the effective
// tags. The fallback (swallow-owned) path is exercised at the use-case level below, since it needs a
// provisioner that does not advertise tagging.

func TestEditServerTags_DrivesProviderAcrossSelection(t *testing.T) {
	f := setupPlatform(t)
	seedProvisionerIntegration(t, f)
	f.seedServer("s1", "gpu-1", "10.0.0.1", func(s *serverdomain.Server) { s.Observed.Tags = []string{"gpu"} })
	f.seedServer("s2", "gpu-2", "10.0.0.2", func(s *serverdomain.Server) { s.Observed.Tags = nil })
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
	// s1 kept nothing but rack-a (gpu removed); s2 gained rack-a.
	assertResponseTags(t, servers, "s1", []string{"rack-a"})
	assertResponseTags(t, servers, "s2", []string{"rack-a"})

	// The provider was driven once per changed tag across the whole selection, not per machine.
	assertActionCount(t, f.provider.actions, "addTag rack-a", 1)
	assertActionCount(t, f.provider.actions, "removeTag gpu", 1)

	// The Server projection reflects the change immediately (refreshed from the provider).
	if got := f.servers.servers["s1"].Observed.Tags; !equalStringSet(got, []string{"rack-a"}) {
		t.Errorf("s1 projected tags: got %v, want [rack-a]", got)
	}
	if got := f.servers.servers["s2"].Observed.Tags; !equalStringSet(got, []string{"rack-a"}) {
		t.Errorf("s2 projected tags: got %v, want [rack-a]", got)
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
	f.seedServer("s1", "gpu-1", "10.0.0.1", nil)
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

// TestEditServerTags_FallbackWritesSwallowOwnedOverlay exercises the swallow-owned half of the rule
// with a provisioner that does not advertise tagging: no provider is driven, the tags are stored as
// an overlay, and clearing them all deletes the overlay.
func TestEditServerTags_FallbackWritesSwallowOwnedOverlay(t *testing.T) {
	const integrationID = "int-no-tags"
	servers := newFakeServerRepo()
	servers.servers["s1"] = &serverdomain.Server{
		ID:     "s1",
		Source: serverdomain.Source{SiteID: testSiteID, IntegrationID: integrationID, ProviderMachineID: "m1"},
	}
	factory := newFakeProviderFactory()
	factory.providers[integrationID] = &minimalProvider{machines: map[string]*provisioningdomain.Machine{
		"m1": {ID: "m1"},
	}}
	overlays := newFakeServerTagOverlayRepo()
	uc := provisioningapp.NewEditServerTagsUseCase(servers, factory, overlays)

	if _, err := uc.Execute(context.Background(), provisioningapp.EditServerTagsInput{
		ServerIDs: []string{"s1"},
		Add:       []string{"owned"},
	}); err != nil {
		t.Fatalf("Execute add: %v", err)
	}
	overlay, err := overlays.Get(context.Background(), "s1")
	if err != nil {
		t.Fatalf("expected an overlay to be written: %v", err)
	}
	if !equalStringSet(overlay.Tags, []string{"owned"}) {
		t.Errorf("overlay tags: got %v, want [owned]", overlay.Tags)
	}
	if got := servers.servers["s1"].Observed.Tags; !equalStringSet(got, []string{"owned"}) {
		t.Errorf("projected tags: got %v, want [owned]", got)
	}

	// Removing the only owned tag deletes the overlay rather than storing an empty one.
	if _, err := uc.Execute(context.Background(), provisioningapp.EditServerTagsInput{
		ServerIDs: []string{"s1"},
		Remove:    []string{"owned"},
	}); err != nil {
		t.Fatalf("Execute remove: %v", err)
	}
	if _, err := overlays.Get(context.Background(), "s1"); err == nil {
		t.Errorf("expected the overlay to be deleted once empty")
	}
	if got := servers.servers["s1"].Observed.Tags; len(got) != 0 {
		t.Errorf("projected tags after clear: got %v, want empty", got)
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
