package command

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Every Platform spec write sends approve=true only with --approve (change-plans.md "Approval On
// Submit"): create, set-spec, delete, and the GPU Operator's set and uninstall.
func TestSpecWritesSendApproveOnlyWithTheFlag(t *testing.T) {
	var queries []string
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"generation":2,"spec":{"members":[],"settings":{}}}`))
			return
		}
		queries = append(queries, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"platform":{"id":"plat-1"},"plan":null}`))
	})
	body := filepath.Join(t.TempDir(), "body.yaml")
	if err := os.WriteFile(body, []byte("generation: 2\nspec: {}\n"), 0o600); err != nil {
		t.Fatalf("write body: %v", err)
	}
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"create", "-f", body}, want: "POST /api/v1/platforms/?"},
		{args: []string{"create", "-f", body, "--approve"}, want: "POST /api/v1/platforms/?approve=true"},
		{args: []string{"set-spec", "plat-1", "-f", body, "--approve"}, want: "PUT /api/v1/platforms/plat-1/spec?approve=true"},
		{args: []string{"delete", "plat-1", "--approve"}, want: "DELETE /api/v1/platforms/plat-1?approve=true"},
		{args: []string{"delete", "plat-1"}, want: "DELETE /api/v1/platforms/plat-1?"},
		{args: []string{"gpu-operator", "uninstall", "plat-1", "--approve"}, want: "PUT /api/v1/platforms/plat-1/spec?approve=true"},
	}
	for _, tc := range tests {
		queries = nil
		cmd := newPlatformsCommand()
		cmd.SetArgs(tc.args)
		cmd.SetContext(context.Background())
		if err := cmd.Execute(); err != nil {
			t.Fatalf("platforms %v: %v", tc.args, err)
		}
		if len(queries) != 1 || queries[0] != tc.want {
			t.Errorf("platforms %v sent %v, want [%s]", tc.args, queries, tc.want)
		}
	}
}

// After a spec write the CLI says on stderr what happens to its Change Plan: it started, it waits
// for `plans approve` with the API's reason, or it is blocked; a change without a plan says nothing.
func TestSpecWriteNotesWhatHappensToThePlan(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     string
	}{
		{
			name:     "approved with the write",
			response: `{"platform":{"id":"plat-1"},"plan":{"id":"plan-1","state":"approved","stateReason":"approved by admin with the change","blockers":[]}}`,
			want:     "Change Plan plan-1 was approved with this change; its controller carries it out now.\n",
		},
		{
			name:     "held by the safety switch",
			response: `{"platform":{"id":"plat-1"},"plan":{"id":"plan-1","state":"pending_approval","stateReason":"the Change Plan policy requires a separate approval","blockers":[]}}`,
			want:     "Change Plan plan-1 waits for approval: the Change Plan policy requires a separate approval. Approve it with `swallow plans approve plan-1`.\n",
		},
		{
			name:     "blocked",
			response: `{"platform":{"id":"plat-1"},"plan":{"id":"plan-1","state":"pending_approval","stateReason":"it has blockers and cannot be approved","blockers":["swallow cannot carry out remove-member yet"]}}`,
			want:     "Change Plan plan-1 waits: it has blockers and cannot be approved. See its blockers with `swallow plans get plan-1`.\n",
		},
		{name: "no plan", response: `{"platform":{"id":"plat-1"},"plan":null}`, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			useTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.response))
			})
			var stderr bytes.Buffer
			cmd := newPlatformsCommand()
			cmd.SetArgs([]string{"delete", "plat-1", "--approve"})
			cmd.SetContext(context.Background())
			cmd.SetErr(&stderr)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("platforms delete: %v", err)
			}
			if got := stderr.String(); got != tc.want {
				t.Errorf("stderr = %q, want %q", got, tc.want)
			}
		})
	}
}

// `plans policy` reads the policy, and `plans policy set` writes the boolean it is given and
// refuses to guess when the flag is missing.
func TestPlansPolicyCommandsCallTheContract(t *testing.T) {
	var got []string
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body bytes.Buffer
		_, _ = body.ReadFrom(r.Body)
		got = append(got, strings.TrimSpace(r.Method+" "+r.URL.Path+" "+body.String()))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"approveOnSubmit":false,"updatedAt":null,"updatedBy":""}`))
	})
	run := func(args ...string) error {
		cmd := newPlansCommand()
		cmd.SetArgs(args)
		cmd.SetContext(context.Background())
		silence(cmd)
		return cmd.Execute()
	}
	for _, args := range [][]string{
		{"policy"},
		{"policy", "set", "--approve-on-submit=false"},
		{"policy", "set", "--approve-on-submit=true"},
	} {
		if err := run(args...); err != nil {
			t.Fatalf("plans %v: %v", args, err)
		}
	}
	if err := run("policy", "set"); err == nil {
		t.Error("plans policy set without --approve-on-submit succeeded")
	}
	want := []string{
		"GET /api/v1/change-plan-policy",
		`PUT /api/v1/change-plan-policy {"approveOnSubmit":false}`,
		`PUT /api/v1/change-plan-policy {"approveOnSubmit":true}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests = %q, want %q", got, want)
	}
}

// silence keeps a command that is expected to fail from printing usage into the test output.
func silence(cmd *cobra.Command) {
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
}
