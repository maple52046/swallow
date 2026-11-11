package maas

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// The tagging capability wraps MAAS's global tag endpoints. The tests assert the request the
// adapter actually sends (method, path, operation, and repeated form fields), because a wrong verb,
// field name, or a non-repeated body would silently do nothing or the wrong thing against a live
// MAAS.

func TestTaggingCapability_Advertised(t *testing.T) {
	provider := &Provider{}
	if !provider.Capabilities().Tagging {
		t.Errorf("MAAS should advertise the Tagging capability")
	}
}

func TestListTags_ClassifiesManualAndAutomaticTags(t *testing.T) {
	fake := newFakeMAAS(t)
	// A tag with a definition is computed by MAAS: automatic, and not removable because MAAS would
	// apply the definition again. `virtual` has no definition but MAAS's commissioning maintains it:
	// automatic, yet removable. Any other tag without a definition is a manual tag swallow owns, even
	// one MAAS's switch hooks could also create (`mellanox`).
	fake.respond("GET "+apiPrefix+"/tags/{$}", http.StatusOK,
		`[{"name":"amd-gpu","definition":"//node"},{"name":"rack-a","definition":""},`+
			`{"name":"virtual","definition":""},{"name":"mellanox","definition":""}]`)
	provider := newTestProvider(t, fake)

	tags, err := provider.ListTags(context.Background())
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	got := map[string]provisioningdomain.MachineTag{}
	for _, tag := range tags {
		got[tag.Name] = tag
	}
	want := map[string]provisioningdomain.MachineTag{
		"amd-gpu":  {Name: "amd-gpu", Editable: false, Removable: false},
		"rack-a":   {Name: "rack-a", Editable: true, Removable: true},
		"virtual":  {Name: "virtual", Editable: false, Removable: true},
		"mellanox": {Name: "mellanox", Editable: true, Removable: true},
	}
	if len(got) != len(want) {
		t.Fatalf("ListTags() = %+v, want %d tags", tags, len(want))
	}
	for name, wantTag := range want {
		if got[name] != wantTag {
			t.Errorf("ListTags() %s = %+v, want %+v", name, got[name], wantTag)
		}
	}
}

func TestRemoveTag_SatisfiedWhenTheTagIsGone(t *testing.T) {
	fake := newFakeMAAS(t)
	// The server-tags API may delete or rename the tag while the Server controller still removes it
	// from a machine; MAAS then answers 404, which means nothing carries the tag any more.
	fake.respond("POST "+apiPrefix+"/tags/{name}/{$}", http.StatusNotFound, "")
	provider := newTestProvider(t, fake)

	if err := provider.RemoveTag(context.Background(), "gone", []string{"abc123"}); err != nil {
		t.Errorf("RemoveTag(gone) = %v, want nil", err)
	}
}

func TestDeleteTag(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		wantKind provisioningdomain.ProviderErrorKind
	}{
		{name: "deletes a present tag", status: http.StatusNoContent},
		{name: "an absent tag is satisfied", status: http.StatusNotFound},
		{name: "a refusal is a rejection", status: http.StatusBadRequest, wantKind: provisioningdomain.ProviderErrorRejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeMAAS(t)
			var deleted string
			fake.mux.HandleFunc("DELETE "+apiPrefix+"/tags/{name}/{$}", func(w http.ResponseWriter, r *http.Request) {
				deleted = r.PathValue("name")
				w.WriteHeader(tc.status)
			})
			provider := newTestProvider(t, fake)

			err := provider.DeleteTag(context.Background(), "rack-a")
			if deleted != "rack-a" {
				t.Errorf("DeleteTag(rack-a) deleted %q, want rack-a", deleted)
			}
			if tc.wantKind == "" {
				if err != nil {
					t.Errorf("DeleteTag(rack-a) = %v, want nil", err)
				}
				return
			}
			var provErr *provisioningdomain.ProviderError
			if !errors.As(err, &provErr) || provErr.Kind != tc.wantKind {
				t.Errorf("DeleteTag(rack-a) = %v, want a %s ProviderError", err, tc.wantKind)
			}
		})
	}
}

func TestRenameTag(t *testing.T) {
	cases := []struct {
		name        string
		existing    []string
		wantRenamed string // the new name sent in a PUT to /tags/old/
		wantDeleted string
	}{
		{name: "renames in place", existing: []string{"old"}, wantRenamed: "new"},
		{name: "merges onto an existing tag", existing: []string{"old", "new"}, wantDeleted: "old"},
		{name: "a missing tag is satisfied", existing: []string{"new"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeMAAS(t)
			fake.mux.HandleFunc("GET "+apiPrefix+"/tags/{name}/{$}", func(w http.ResponseWriter, r *http.Request) {
				for _, name := range tc.existing {
					if r.PathValue("name") == name {
						_, _ = w.Write([]byte(`{"name":"` + name + `","definition":""}`))
						return
					}
				}
				w.WriteHeader(http.StatusNotFound)
			})
			var renamed, deleted string
			fake.mux.HandleFunc("PUT "+apiPrefix+"/tags/{name}/{$}", func(w http.ResponseWriter, r *http.Request) {
				if r.PathValue("name") == "old" && r.ParseMultipartForm(1<<20) == nil {
					renamed = r.MultipartForm.Value["name"][0]
				}
				_, _ = w.Write([]byte(`{}`))
			})
			fake.mux.HandleFunc("DELETE "+apiPrefix+"/tags/{name}/{$}", func(w http.ResponseWriter, r *http.Request) {
				deleted = r.PathValue("name")
				w.WriteHeader(http.StatusNoContent)
			})
			provider := newTestProvider(t, fake)

			if err := provider.RenameTag(context.Background(), "old", "new"); err != nil {
				t.Fatalf("RenameTag(old, new) = %v, want nil", err)
			}
			if renamed != tc.wantRenamed {
				t.Errorf("RenameTag(old, new) renamed to %q, want %q", renamed, tc.wantRenamed)
			}
			if deleted != tc.wantDeleted {
				t.Errorf("RenameTag(old, new) deleted %q, want %q", deleted, tc.wantDeleted)
			}
		})
	}
}

func TestEnsureTag_CreatesWhenAbsent(t *testing.T) {
	fake := newFakeMAAS(t)
	// A missing tag reads as 404; the adapter then creates a manual tag.
	fake.respond("GET "+apiPrefix+"/tags/{name}/{$}", http.StatusNotFound, "")
	var created bool
	fake.mux.HandleFunc("POST "+apiPrefix+"/tags/{$}", func(w http.ResponseWriter, _ *http.Request) {
		created = true
		w.WriteHeader(http.StatusOK)
	})
	provider := newTestProvider(t, fake)

	if err := provider.EnsureTag(context.Background(), "rack-a"); err != nil {
		t.Fatalf("EnsureTag: %v", err)
	}
	if !created {
		t.Fatalf("EnsureTag did not POST a create for an absent tag")
	}
	if got := fake.lastForm["name"]; got != "rack-a" {
		t.Errorf("create name: got %q, want rack-a", got)
	}
}

func TestEnsureTag_NoCreateWhenPresent(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.respond("GET "+apiPrefix+"/tags/{name}/{$}", http.StatusOK, `{"name":"rack-a","definition":""}`)
	var created bool
	fake.mux.HandleFunc("POST "+apiPrefix+"/tags/{$}", func(w http.ResponseWriter, _ *http.Request) {
		created = true
		w.WriteHeader(http.StatusOK)
	})
	provider := newTestProvider(t, fake)

	if err := provider.EnsureTag(context.Background(), "rack-a"); err != nil {
		t.Fatalf("EnsureTag: %v", err)
	}
	if created {
		t.Errorf("EnsureTag created a tag that already exists")
	}
}

func TestAddTag_SendsUpdateNodesWithRepeatedSystemIDs(t *testing.T) {
	values := captureTagUpdateNodes(t, "amd-gpu", func(p *Provider) error {
		return p.AddTag(context.Background(), "amd-gpu", []string{"abc123", "def456"})
	})
	assertStringSet(t, "add", values["add"], []string{"abc123", "def456"})
	if _, present := values["remove"]; present {
		t.Errorf("AddTag must not send a remove field, got %v", values["remove"])
	}
}

func TestRemoveTag_SendsUpdateNodesWithRepeatedSystemIDs(t *testing.T) {
	values := captureTagUpdateNodes(t, "amd-gpu", func(p *Provider) error {
		return p.RemoveTag(context.Background(), "amd-gpu", []string{"abc123", "def456"})
	})
	assertStringSet(t, "remove", values["remove"], []string{"abc123", "def456"})
	if _, present := values["add"]; present {
		t.Errorf("RemoveTag must not send an add field, got %v", values["add"])
	}
}

func TestAddTag_EmptyMachineListIsNoOp(t *testing.T) {
	fake := newFakeMAAS(t)
	provider := newTestProvider(t, fake)

	if err := provider.AddTag(context.Background(), "amd-gpu", nil); err != nil {
		t.Fatalf("AddTag empty: %v", err)
	}
	if fake.lastMethod != "" {
		t.Errorf("AddTag with no machines must issue no request, got %s", fake.lastMethod)
	}
}

func TestAddTag_AutoTagRefusalSurfacesAsRejected(t *testing.T) {
	fake := newFakeMAAS(t)
	// MAAS refuses update_nodes on a tag with a definition with a validation error (HTTP 400).
	fake.respond("POST "+apiPrefix+"/tags/{name}/{$}", http.StatusBadRequest,
		"Cannot add nodes to tag amd-gpu as it has a definition.")
	provider := newTestProvider(t, fake)

	err := provider.AddTag(context.Background(), "amd-gpu", []string{"abc123"})
	var provErr *provisioningdomain.ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("AddTag auto-tag error = %v, want a ProviderError", err)
	}
	if provErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Errorf("kind: got %q, want %q", provErr.Kind, provisioningdomain.ProviderErrorRejected)
	}
}

// captureTagUpdateNodes registers a POST handler for one tag that records the repeated multipart
// fields of an update_nodes call, runs call, and returns the captured values. It reads the repeated
// values from the already-parsed multipart form because the shared fake only records the first value
// per field.
func captureTagUpdateNodes(t *testing.T, name string, call func(*Provider) error) map[string][]string {
	t.Helper()
	fake := newFakeMAAS(t)
	var (
		mu     sync.Mutex
		values = map[string][]string{}
	)
	fake.mux.HandleFunc("POST "+apiPrefix+"/tags/{name}/{$}", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err == nil && r.MultipartForm != nil {
			mu.Lock()
			for field, vals := range r.MultipartForm.Value {
				values[field] = append(values[field], vals...)
			}
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"` + name + `"}`))
	})
	provider := newTestProvider(t, fake)

	if err := call(provider); err != nil {
		t.Fatalf("tag update_nodes: %v", err)
	}
	if fake.lastMethod != http.MethodPost {
		t.Errorf("method: got %q, want POST", fake.lastMethod)
	}
	if fake.lastOperation != "update_nodes" {
		t.Errorf("operation: got %q, want update_nodes", fake.lastOperation)
	}
	return values
}

// assertStringSet checks that got contains exactly the want values, order-independent.
func assertStringSet(t *testing.T, field string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want %v", field, got, want)
	}
	seen := make(map[string]int, len(got))
	for _, v := range got {
		seen[v]++
	}
	for _, v := range want {
		if seen[v] == 0 {
			t.Errorf("%s: missing %q, got %v", field, v, got)
			continue
		}
		seen[v]--
	}
}
