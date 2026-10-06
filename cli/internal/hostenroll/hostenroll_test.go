package hostenroll

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordedRun struct {
	name string
	args []string
}

func testEnroller(t *testing.T, euid int, registerOutput string, runs *[]recordedRun) enroller {
	t.Helper()
	return enroller{
		httpClient: http.DefaultClient,
		geteuid:    func() int { return euid },
		lookPath:   func(string) (string, error) { return "/usr/bin/python3", nil },
		hostname:   func() (string, error) { return "db-03.lab.example", nil },
		run: func(_ context.Context, dir string, stdout, _ io.Writer, name string, args ...string) error {
			*runs = append(*runs, recordedRun{name: name, args: append([]string(nil), args...)})
			if args[0] == "register-machine" {
				_, _ = io.WriteString(stdout, registerOutput)
			}
			if args[0] == "report-results" {
				if _, err := os.Stat(args[2]); err != nil {
					return errors.New("credentials missing")
				}
			}
			return nil
		},
	}
}

func maasRegion(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/MAAS/maas-run-scripts" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, "#!/usr/bin/env python3\n")
	}))
	t.Cleanup(server.Close)
	return server
}

// Enroll downloads maas-run-scripts from the region, registers the host under its short name with
// the token, then reports results with the credentials register-machine printed, and leaves nothing
// behind.
func TestEnrollMAAS(t *testing.T) {
	region := maasRegion(t)
	var runs []recordedRun
	var stdout bytes.Buffer
	e := testEnroller(t, 0, "reporting:\n  maas:\n    token_key: k\n", &runs)
	err := e.enroll(context.Background(), Options{
		Provisioner: "maas", Endpoint: region.URL + "/MAAS/api/2.0/", Token: "a:b:c", Stdout: &stdout,
	})
	if err != nil {
		t.Fatalf("enroll error = %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs = %+v, want register-machine then report-results", runs)
	}
	register := strings.Join(runs[0].args, " ")
	if want := "register-machine --hostname db-03 " + region.URL + "/MAAS a:b:c"; register != want {
		t.Errorf("register-machine args = %q, want %q", register, want)
	}
	if runs[1].args[0] != "report-results" || runs[1].args[1] != "--config" {
		t.Errorf("report-results args = %v", runs[1].args)
	}
	if _, err := os.Stat(filepath.Dir(runs[0].name)); !os.IsNotExist(err) {
		t.Errorf("working directory %s was left behind", filepath.Dir(runs[0].name))
	}
	if strings.Contains(stdout.String(), "a:b:c") {
		t.Error("progress output must not print the token")
	}
}

func TestEnrollRefuses(t *testing.T) {
	region := maasRegion(t)
	cases := []struct {
		name     string
		euid     int
		opts     Options
		register string
		want     string
	}{
		{"unknown provisioner", 0, Options{Provisioner: "ironic", Endpoint: region.URL + "/MAAS", Token: "t"}, "", "no existing-host enrollment"},
		{"missing token", 0, Options{Provisioner: "maas", Endpoint: region.URL + "/MAAS"}, "", "--token is required"},
		{"not root", 1000, Options{Provisioner: "maas", Endpoint: region.URL + "/MAAS", Token: "t"}, "", "run as root"},
		{"no credentials returned", 0, Options{Provisioner: "maas", Endpoint: region.URL + "/MAAS", Token: "t"}, "error: hostname exists", "no machine credentials"},
		{"region unreachable", 0, Options{Provisioner: "maas", Endpoint: region.URL + "/wrong", Token: "t"}, "", "HTTP 404"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var runs []recordedRun
			err := testEnroller(t, tc.euid, tc.register, &runs).enroll(context.Background(), tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("enroll error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}
