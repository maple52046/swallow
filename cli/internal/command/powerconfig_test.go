package command

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestPowerConfigurationCommandsFollowTheContract checks method, path, and body against
// server-detail-actions.md "Power Configuration": get reads, set sends only the given parameters
// and no password key unless a password flag was used.
func TestPowerConfigurationCommandsFollowTheContract(t *testing.T) {
	type seen struct {
		method, path string
		body         map[string]any
	}
	var requests []seen
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		request := seen{method: r.Method, path: r.URL.Path}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&request.body)
		}
		requests = append(requests, request)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"serverId":"srv-1","driver":"virsh"}`))
	})

	run := func(args ...string) {
		t.Helper()
		cmd := serversPowerConfigurationCmd()
		cmd.SetArgs(args)
		cmd.SetContext(context.Background())
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	run("get", "srv-1")
	run("set", "srv-1", "--driver", "virsh", "--address", "qemu+ssh://maas@tainan-ci/system", "--power-id", "simple-pig")
	run("set", "srv-2", "--driver", "ipmi", "--address", "10.0.0.5", "--username", "maas", "--clear-password")

	want := []seen{
		{method: "GET", path: "/api/v1/servers/srv-1/power-configuration"},
		{method: "PUT", path: "/api/v1/servers/srv-1/power-configuration", body: map[string]any{
			"driver": "virsh", "address": "qemu+ssh://maas@tainan-ci/system", "powerId": "simple-pig",
		}},
		{method: "PUT", path: "/api/v1/servers/srv-2/power-configuration", body: map[string]any{
			"driver": "ipmi", "address": "10.0.0.5", "username": "maas", "password": "",
		}},
	}
	if len(requests) != len(want) {
		t.Fatalf("requests = %+v, want %d", requests, len(want))
	}
	for i := range want {
		got := requests[i]
		if got.method != want[i].method || got.path != want[i].path || len(got.body) != len(want[i].body) {
			t.Errorf("request %d = %+v, want %+v", i, got, want[i])
			continue
		}
		for key, value := range want[i].body {
			if got.body[key] != value {
				t.Errorf("request %d %s = %v, want %v", i, key, got.body[key], value)
			}
		}
	}
}

func TestPowerConfigurationBodyFromFlags(t *testing.T) {
	parse := func(t *testing.T, stdin string, args ...string) (map[string]any, error) {
		t.Helper()
		cmd := serversPowerConfigurationCmd()
		set, _, err := cmd.Find([]string{"set"})
		if err != nil {
			t.Fatal(err)
		}
		if err := set.ParseFlags(args); err != nil {
			t.Fatal(err)
		}
		return powerConfigurationBodyFromFlags(set, strings.NewReader(stdin))
	}

	body, err := parse(t, "s3cret\n", "--driver", "ipmi", "--address", "10.0.0.5", "--password-stdin")
	if err != nil || body["password"] != "s3cret" {
		t.Errorf("--password-stdin body = %v, %v; want the password without its newline", body, err)
	}
	if body, err := parse(t, "", "--driver", "virsh", "--address", "qemu+ssh://h/system", "--power-id", "vm"); err != nil {
		t.Errorf("body without a password flag error = %v", err)
	} else if _, sent := body["password"]; sent {
		t.Error("sent a password key without a password flag; the stored password would be cleared")
	}

	for name, args := range map[string][]string{
		"no driver":            {"--address", "10.0.0.5"},
		"two password sources": {"--driver", "ipmi", "--password", "x", "--clear-password"},
	} {
		if _, err := parse(t, "", args...); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := parse(t, "\n", "--driver", "ipmi", "--password-stdin"); err == nil {
		t.Error("an empty password from stdin was accepted; want --clear-password to be explicit")
	}
}
