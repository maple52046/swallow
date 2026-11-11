package command

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Deleting and renaming a tag address it in the path and its Site in the siteId query
// (server-tags.md); a rename carries only the new name in its body.
func TestTagsDeleteAndRenameSendTheContractRequests(t *testing.T) {
	type request struct {
		method, path, siteID string
		body                 map[string]any
	}
	var got []request
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, request{method: r.Method, path: r.URL.Path, siteID: r.URL.Query().Get("siteId"), body: body})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"servers":[]}`))
	})

	if err := runCommand(t, provisioningTagsCmd(), "delete", "rack.a", "--site-id", "site-a"); err != nil {
		t.Fatalf("tags delete: %v", err)
	}
	if err := runCommand(t, provisioningTagsCmd(), "rename", "rack.a", "rack-b", "--site-id", "site-a"); err != nil {
		t.Fatalf("tags rename: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("requests = %+v, want a DELETE and a PATCH", got)
	}
	if r := got[0]; r.method != http.MethodDelete || r.path != "/api/v1/provisioning/tags/rack.a" || r.siteID != "site-a" || r.body != nil {
		t.Errorf("delete request = %+v, want DELETE /api/v1/provisioning/tags/rack.a?siteId=site-a without a body", r)
	}
	if r := got[1]; r.method != http.MethodPatch || r.path != "/api/v1/provisioning/tags/rack.a" || r.siteID != "site-a" || r.body["name"] != "rack-b" || len(r.body) != 1 {
		t.Errorf("rename request = %+v, want PATCH /api/v1/provisioning/tags/rack.a?siteId=site-a with {name: rack-b}", r)
	}
}

// Without a Site the command fails before sending anything, since every tag route needs one.
func TestTagsDeleteRequiresASite(t *testing.T) {
	calls := 0
	useTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	})

	if err := runCommand(t, provisioningTagsCmd(), "delete", "rack-a"); err == nil {
		t.Errorf("tags delete without a Site: got nil error, want one")
	}
	if calls != 0 {
		t.Errorf("tags delete without a Site sent %d requests, want 0", calls)
	}
}
