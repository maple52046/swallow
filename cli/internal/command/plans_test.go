package command

import (
	"context"
	"net/http"
	"testing"
)

// The plan commands call the change-plans contract: list passes its filters (state repeats), and
// approve and reject post to the plan.
func TestPlansCommandsCallTheContract(t *testing.T) {
	var got []string
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	})
	for _, args := range [][]string{
		{"list", "--resource-kind", "platform", "--state", "pending_approval,approved"},
		{"approve", "p1"},
		{"reject", "p1"},
	} {
		cmd := newPlansCommand()
		cmd.SetArgs(args)
		cmd.SetContext(context.Background())
		if err := cmd.Execute(); err != nil {
			t.Fatalf("plans %v: %v", args, err)
		}
	}
	want := []string{
		"GET /api/v1/change-plans?resourceKind=platform&state=pending_approval&state=approved",
		"POST /api/v1/change-plans/p1/approve?",
		"POST /api/v1/change-plans/p1/reject?",
	}
	if len(got) != len(want) {
		t.Fatalf("requests = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d = %q, want %q", i, got[i], want[i])
		}
	}
}
