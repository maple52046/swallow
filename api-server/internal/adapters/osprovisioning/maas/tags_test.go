package maas

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
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

func TestListTags_FlagsAutomaticAsNotEditable(t *testing.T) {
	fake := newFakeMAAS(t)
	// A tag with a definition is automatic (MAAS computes it) and must be reported read-only; a
	// tag with no definition is a manual tag swallow may assign.
	fake.respond("GET "+apiPrefix+"/tags/{$}", http.StatusOK,
		`[{"name":"amd-gpu","definition":"//node"},{"name":"rack-a","definition":""}]`)
	provider := newTestProvider(t, fake)

	tags, err := provider.ListTags(context.Background())
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d: %+v", len(tags), tags)
	}
	byName := map[string]provisioningdomain.MachineTag{}
	for _, tag := range tags {
		byName[tag.Name] = tag
	}
	if byName["amd-gpu"].Editable {
		t.Errorf("an automatic tag (with a definition) must be reported not editable")
	}
	if !byName["rack-a"].Editable {
		t.Errorf("a manual tag (no definition) must be reported editable")
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
