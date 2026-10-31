package maas

import (
	"context"
	"net/http"
	"sync"
	"testing"
)

// The grouping capability wraps MAAS zone and resource-pool endpoints. The tests assert the
// request the adapter actually sends (method, path, and form fields), because a wrong verb or
// field name would silently do nothing or the wrong thing against a live MAAS.

func TestGroupingCapability_Advertised(t *testing.T) {
	provider := &Provider{}
	if !provider.Capabilities().Grouping {
		t.Errorf("MAAS should advertise the Grouping capability")
	}
}

func TestEnsureZone_CreatesWhenAbsent(t *testing.T) {
	fake := newFakeMAAS(t)
	// A missing zone reads as 404; the adapter then creates it.
	fake.respond("GET "+apiPrefix+"/zones/{name}/{$}", http.StatusNotFound, "")
	var created bool
	fake.mux.HandleFunc("POST "+apiPrefix+"/zones/{$}", func(w http.ResponseWriter, _ *http.Request) {
		created = true
		w.WriteHeader(http.StatusOK)
	})
	provider := newTestProvider(t, fake)

	if err := provider.EnsureZone(context.Background(), "rack-a", "top of rack"); err != nil {
		t.Fatalf("EnsureZone: %v", err)
	}
	if !created {
		t.Fatalf("EnsureZone did not POST a create for an absent zone")
	}
	if got := fake.lastForm["name"]; got != "rack-a" {
		t.Errorf("create name: got %q, want rack-a", got)
	}
	if got := fake.lastForm["description"]; got != "top of rack" {
		t.Errorf("create description: got %q, want %q", got, "top of rack")
	}
}

func TestEnsureZone_NoCreateWhenPresent(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.respond("GET "+apiPrefix+"/zones/{name}/{$}", http.StatusOK, `{"name":"rack-a"}`)
	var created bool
	fake.mux.HandleFunc("POST "+apiPrefix+"/zones/{$}", func(w http.ResponseWriter, _ *http.Request) {
		created = true
		w.WriteHeader(http.StatusOK)
	})
	provider := newTestProvider(t, fake)

	if err := provider.EnsureZone(context.Background(), "rack-a", ""); err != nil {
		t.Fatalf("EnsureZone: %v", err)
	}
	if created {
		t.Errorf("EnsureZone created a zone that already exists")
	}
}

func TestDeleteZone_DeletesWhenPresent(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.respond("GET "+apiPrefix+"/zones/{name}/{$}", http.StatusOK, `{"name":"rack-a"}`)
	var deleted bool
	fake.mux.HandleFunc("DELETE "+apiPrefix+"/zones/{name}/{$}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("name") == "rack-a" {
			deleted = true
		}
		w.WriteHeader(http.StatusNoContent)
	})
	provider := newTestProvider(t, fake)

	if err := provider.DeleteZone(context.Background(), "rack-a"); err != nil {
		t.Fatalf("DeleteZone: %v", err)
	}
	if !deleted {
		t.Errorf("DeleteZone did not DELETE the present zone")
	}
}

func TestDeleteZone_SatisfiedWhenAbsent(t *testing.T) {
	fake := newFakeMAAS(t)
	fake.respond("GET "+apiPrefix+"/zones/{name}/{$}", http.StatusNotFound, "")
	var deleted bool
	fake.mux.HandleFunc("DELETE "+apiPrefix+"/zones/{name}/{$}", func(w http.ResponseWriter, _ *http.Request) {
		deleted = true
		w.WriteHeader(http.StatusNoContent)
	})
	provider := newTestProvider(t, fake)

	if err := provider.DeleteZone(context.Background(), "gone"); err != nil {
		t.Fatalf("DeleteZone absent: %v", err)
	}
	if deleted {
		t.Errorf("DeleteZone issued a DELETE for a zone MAAS does not have")
	}
}

func TestEnsurePool_TreatsIdZeroDefaultAsExisting(t *testing.T) {
	fake := newFakeMAAS(t)
	// MAAS's built-in default resource pool has id 0. A zero id is a real pool, so ensure must
	// not try to re-create it (which MAAS rejects as a duplicate).
	fake.respond("GET "+apiPrefix+"/resourcepools/{$}", http.StatusOK, `[{"id":0,"name":"default"}]`)
	var created bool
	fake.mux.HandleFunc("POST "+apiPrefix+"/resourcepools/{$}", func(w http.ResponseWriter, _ *http.Request) {
		created = true
		w.WriteHeader(http.StatusOK)
	})
	provider := newTestProvider(t, fake)

	if err := provider.EnsurePool(context.Background(), "default", ""); err != nil {
		t.Fatalf("EnsurePool: %v", err)
	}
	if created {
		t.Errorf("EnsurePool re-created the id-0 default pool; a zero id must count as existing")
	}
}

func TestDeletePool_ResolvesNameToIDThenDeletes(t *testing.T) {
	fake := newFakeMAAS(t)
	// MAAS keys resource pools by id, so the adapter lists then deletes by id.
	fake.respond("GET "+apiPrefix+"/resourcepools/{$}", http.StatusOK,
		`[{"id":1,"name":"default"},{"id":5,"name":"research"}]`)
	var deletedID string
	fake.mux.HandleFunc("DELETE "+apiPrefix+"/resourcepools/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		deletedID = r.PathValue("id")
		w.WriteHeader(http.StatusNoContent)
	})
	provider := newTestProvider(t, fake)

	if err := provider.DeletePool(context.Background(), "research"); err != nil {
		t.Fatalf("DeletePool: %v", err)
	}
	if deletedID != "5" {
		t.Errorf("deleted pool id: got %q, want 5", deletedID)
	}
}

func TestSetMachineZone_PutsZoneField(t *testing.T) {
	form := captureMachineUpdate(t, "zone", func(p *Provider) error {
		_, err := p.SetMachineZone(context.Background(), "abc123", "rack-a")
		return err
	})
	if got := form["zone"]; got != "rack-a" {
		t.Errorf("zone field: got %q, want rack-a", got)
	}
}

func TestSetMachinePool_PutsPoolField(t *testing.T) {
	form := captureMachineUpdate(t, "pool", func(p *Provider) error {
		_, err := p.SetMachinePool(context.Background(), "abc123", "research")
		return err
	})
	if got := form["pool"]; got != "research" {
		t.Errorf("pool field: got %q, want research", got)
	}
}

func TestSetMachineZone_RejectsEmptyName(t *testing.T) {
	provider := newTestProvider(t, newFakeMAAS(t))
	if _, err := provider.SetMachineZone(context.Background(), "abc123", ""); err == nil {
		t.Errorf("SetMachineZone with empty name should be refused")
	}
}

// captureMachineUpdate registers a PUT machine handler that records the multipart form and
// returns a ready machine, runs call, and returns the captured fields. It parses the form in the
// handler because the shared fake only records the form for POST, and a machine update is a PUT.
func captureMachineUpdate(t *testing.T, field string, call func(*Provider) error) map[string]string {
	t.Helper()
	fake := newFakeMAAS(t)
	var (
		mu   sync.Mutex
		form = map[string]string{}
	)
	fake.mux.HandleFunc("PUT "+apiPrefix+"/machines/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			mu.Lock()
			for name, values := range r.MultipartForm.Value {
				if len(values) > 0 {
					form[name] = values[0]
				}
			}
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(readyMachineJSON))
	})
	provider := newTestProvider(t, fake)

	if err := call(provider); err != nil {
		t.Fatalf("machine update (%s): %v", field, err)
	}
	if fake.lastMethod != http.MethodPut {
		t.Errorf("method: got %q, want PUT", fake.lastMethod)
	}
	return form
}
