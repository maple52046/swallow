package command

import (
	"encoding/json"
	"net/http"
	"testing"
)

// set-pool sends the Pool id, or an explicit null for --clear-pool, to the Platform's pool route,
// and refuses before any request unless exactly one of the two flags is given.
func TestPlatformsSetPoolSendsThePool(t *testing.T) {
	type request struct {
		method, path string
		body         map[string]any
	}
	var requests []request
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		requests = append(requests, request{r.Method, r.URL.Path, body})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"platformId":"platform-a","poolId":null,"servers":[]}`))
	})

	if err := runCommand(t, platformsSetPoolCmd(), "platform-a", "--pool", "pool-1"); err != nil {
		t.Fatalf("set-pool --pool: %v", err)
	}
	if err := runCommand(t, platformsSetPoolCmd(), "platform-a", "--clear-pool"); err != nil {
		t.Fatalf("set-pool --clear-pool: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %+v, want two", requests)
	}
	for i, want := range []any{"pool-1", nil} {
		got := requests[i]
		if got.method != http.MethodPut || got.path != "/api/v1/platforms/platform-a/pool" {
			t.Errorf("request %d = %s %s, want PUT /api/v1/platforms/platform-a/pool", i, got.method, got.path)
		}
		if value, ok := got.body["poolId"]; !ok || value != want {
			t.Errorf("request %d body = %v, want poolId %v", i, got.body, want)
		}
	}

	for name, args := range map[string][]string{
		"neither flag": {"platform-a"},
		"both flags":   {"platform-a", "--pool", "pool-1", "--clear-pool"},
	} {
		if err := runCommand(t, platformsSetPoolCmd(), args...); err == nil {
			t.Errorf("%s: err = nil, want a refusal", name)
		}
	}
	if len(requests) != 2 {
		t.Errorf("a refused set-pool sent a request: %+v", requests[2:])
	}
}
