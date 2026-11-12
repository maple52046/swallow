package command

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// The validation commands call the validation contract: list passes its filters, a script suite is
// created from flags with the script file's content, start sends the run body with typed
// parameters, and cancel posts to the run.
func TestValidationCommandsCallTheContract(t *testing.T) {
	type call struct {
		line string
		body map[string]any
	}
	var mu sync.Mutex
	var got []call
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		got = append(got, call{line: r.Method + " " + r.URL.Path + "?" + r.URL.RawQuery, body: body})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r1","status":"pending","items":[]}`))
	})
	script := filepath.Join(t.TempDir(), "check.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"runs", "list", "--server", "s1", "--verdict", "failed", "--active"},
		{"suites", "create", "--name", "mounts", "--script-file", script, "--timeout", "120"},
		{"runs", "start", "--suite", "host-baseline", "--server", "s1,s2", "--param", "minCpus=64", "--param", "label=gpu"},
		{"runs", "cancel", "r1"},
	} {
		cmd := newValidationCommand()
		cmd.SetArgs(args)
		cmd.SetContext(context.Background())
		if err := cmd.Execute(); err != nil {
			t.Fatalf("validation %v: %v", args, err)
		}
	}
	want := []call{
		{line: "GET /api/v1/validation/runs?active=true&serverId=s1&verdict=failed"},
		{line: "POST /api/v1/validation/suites?", body: map[string]any{"name": "mounts", "script": "#!/bin/sh\nexit 0\n", "timeoutSeconds": float64(120)}},
		{line: "POST /api/v1/validation/runs?", body: map[string]any{
			"suiteId": "host-baseline", "serverIds": []any{"s1", "s2"}, "parameters": map[string]any{"minCpus": float64(64), "label": "gpu"},
		}},
		{line: "POST /api/v1/validation/runs/r1/cancel?"},
	}
	if len(got) != len(want) {
		t.Fatalf("requests = %v, want %v", got, want)
	}
	for i := range want {
		if got[i].line != want[i].line || !reflect.DeepEqual(got[i].body, want[i].body) {
			t.Errorf("request %d = %s %v, want %s %v", i, got[i].line, got[i].body, want[i].line, want[i].body)
		}
	}
}

// start --wait follows the run until it ends, reporting each change of status or counts, and
// fails (a non-zero exit) when the run's verdict is not passed.
func TestValidationRunsStartWaitFollowsTheRun(t *testing.T) {
	reads := 0
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"r1","status":"pending","counts":{}}`))
			return
		}
		reads++
		switch reads {
		case 1:
			_, _ = w.Write([]byte(`{"id":"r1","status":"running","counts":{"passed":1}}`))
		default:
			_, _ = w.Write([]byte(`{"id":"r1","status":"completed","verdict":"failed","counts":{"passed":3,"failed":1}}`))
		}
	})
	var progress strings.Builder
	cmd := newValidationCommand()
	cmd.SetArgs([]string{"runs", "start", "--suite", "host-baseline", "--server", "s1", "--wait", "--interval", "1ms"})
	cmd.SetContext(context.Background())
	cmd.SetErr(&progress)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "ended failed") {
		t.Fatalf("start --wait error = %v, want the failed verdict", err)
	}
	for _, line := range []string{"run r1 running: 1 passed", "run r1 completed: 3 passed, 1 failed"} {
		if !strings.Contains(progress.String(), line) {
			t.Errorf("progress %q lacks %q", progress.String(), line)
		}
	}
}

// --param values are read as YAML so numbers, booleans, and lists reach the API typed; a pair
// without "=" is refused before any request.
func TestParseRunParameters(t *testing.T) {
	got, err := parseRunParameters([]string{"n=64", "on=true", "nodes=[a, b]", "name=gpu node", "empty="})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"n": 64, "on": true, "nodes": []any{"a", "b"}, "name": "gpu node", "empty": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseRunParameters() = %#v, want %#v", got, want)
	}
	if _, err := parseRunParameters([]string{"nokey"}); err == nil {
		t.Error("a pair without = was accepted")
	}
}
