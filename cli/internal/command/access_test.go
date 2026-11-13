package command

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// recordedRequest is what a test server saw: the method, the path, and the decoded JSON body.
type recordedRequest struct {
	method string
	path   string
	body   map[string]any
}

// recordRequests answers every request with status and a JSON object, and records them.
func recordRequests(t *testing.T, status int) *[]recordedRequest {
	t.Helper()
	var seen []recordedRequest
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		seen = append(seen, recordedRequest{method: r.Method, path: r.URL.Path, body: body})
		if status == http.StatusNoContent {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{}`))
	})
	return &seen
}

// TestUsersUpdateSendsOnlyTheGivenFields guards the partial update: an omitted
// flag must not reset the role or the password.
func TestUsersUpdateSendsOnlyTheGivenFields(t *testing.T) {
	seen := recordRequests(t, http.StatusOK)
	cmd := usersUpdateCmd()
	cmd.SetArgs([]string{"user-1", "--role", "admin"})
	cmd.SetContext(context.Background())
	if err := cmd.Execute(); err != nil {
		t.Fatalf("update: %v", err)
	}
	want := []recordedRequest{{method: "PATCH", path: "/api/v1/users/user-1", body: map[string]any{"role": "admin"}}}
	if !reflect.DeepEqual(*seen, want) {
		t.Errorf("requests = %+v, want %+v", *seen, want)
	}

	cmd = usersUpdateCmd()
	cmd.SetArgs([]string{"user-1"})
	cmd.SetContext(context.Background())
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "nothing to change") {
		t.Errorf("update without flags: err = %v, want a nothing-to-change error before any request", err)
	}
	if len(*seen) != 1 {
		t.Errorf("requests = %d, want 1", len(*seen))
	}
}

// TestPoolMembersAddChecksOverrides guards the closed set of sudo overrides and
// the request a valid add sends.
func TestPoolMembersAddChecksOverrides(t *testing.T) {
	seen := recordRequests(t, http.StatusCreated)
	run := func(args ...string) error {
		cmd := poolMembersCmd()
		cmd.SetArgs(append([]string{"add"}, args...))
		cmd.SetContext(context.Background())
		return cmd.Execute()
	}

	if err := run("pool-1", "--username", "bob", "--sudo", "yes"); err == nil || !strings.Contains(err.Error(), "inherit, on, off") {
		t.Errorf("add with --sudo yes: err = %v, want the allowed values", err)
	}
	if err := run("pool-1", "--username", "bob", "--sudo-passwordless", "on"); err != nil {
		t.Fatalf("add: %v", err)
	}
	want := []recordedRequest{{
		method: "POST", path: "/api/v1/infrastructure/pools/pool-1/members",
		body: map[string]any{"username": "bob", "role": "member", "sudoPasswordless": "on"},
	}}
	if !reflect.DeepEqual(*seen, want) {
		t.Errorf("requests = %+v, want %+v", *seen, want)
	}
}

func TestReadPasswordPair(t *testing.T) {
	tests := []struct {
		in            string
		current, next string
		wantErr       bool
	}{
		{in: "old-secret\nnew-secret\n", current: "old-secret", next: "new-secret"},
		{in: "old-secret\r\nnew-secret", current: "old-secret", next: "new-secret"},
		{in: "old-secret\n", wantErr: true},
		{in: "\nnew-secret\n", wantErr: true},
	}
	for _, tc := range tests {
		current, next, err := readPasswordPair(strings.NewReader(tc.in))
		if (err != nil) != tc.wantErr || current != tc.current || next != tc.next {
			t.Errorf("readPasswordPair(%q) = %q, %q, %v; want %q, %q, error %v", tc.in, current, next, err, tc.current, tc.next, tc.wantErr)
		}
	}
}
