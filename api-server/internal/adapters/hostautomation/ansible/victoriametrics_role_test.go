package ansible

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// victoriaMetricsArchive is a release archive shaped like upstream's: one executable,
// victoria-metrics-prod, at the archive root, owned by 1000:1000, reporting version on -version.
func victoriaMetricsArchive(t *testing.T, version string) []byte {
	t.Helper()
	binary := "#!/bin/sh\necho victoria-metrics-20261023-161400-tags-v" + version + "-0-g3fec2f7241\n"
	var archive bytes.Buffer
	zipped := gzip.NewWriter(&archive)
	writer := tar.NewWriter(zipped)
	header := &tar.Header{Name: "victoria-metrics-prod", Mode: 0o755, Size: int64(len(binary)), Uid: 1000, Gid: 1000}
	if err := writer.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(binary)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipped.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

// The release step installs the pinned VictoriaMetrics from the URL the run's artifact sources give
// its declared path, verified against the declared SHA-256, and treats a binary as installed only when
// it reports the pinned version: a rerun changes nothing, a binary an interrupted run truncated is
// replaced, an older release is removed, and a version swallow does not declare fails before any
// download.
func TestVictoriaMetricsReleaseInstallsTheVersionItVerifies(t *testing.T) {
	t.Parallel()
	const version = "1.0.0"
	archive := victoriaMetricsArchive(t, version)
	sum := sha256.Sum256(archive)
	path := "victoriametrics/" + version + "/victoria-metrics-linux-amd64-v" + version + ".tar.gz"
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files/"+path {
			http.NotFound(w, r)
			return
		}
		downloads.Add(1)
		_, _ = w.Write(archive)
	}))
	t.Cleanup(server.Close)

	run := newRoleTasksRun(t, "victoriametrics", "release", true)
	root := t.TempDir()
	release := map[string]any{"path": path, "sha256": hex.EncodeToString(sum[:])}
	vars := func(pinned string) map[string]any {
		return map[string]any{
			"swallow_victoriametrics_version": pinned,
			"swallow_artifacts":               map[string]any{"files": map[string]any{path: server.URL + "/files/" + path}},
			"victoriametrics_root":            root,
			"victoriametrics_releases":        map[string]any{version: map[string]any{"amd64": release, "arm64": release}},
		}
	}
	binary := filepath.Join(root, version, "victoria-metrics-prod")

	if out, changed, failed := run.run(vars(version)); failed != 0 || changed == 0 {
		t.Fatalf("first run changed %d and failed %d tasks, want the release installed:\n%s", changed, failed, out)
	}
	if info, err := os.Stat(binary); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("installed binary = %v (stat error %v), want an executable", info, err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 1 {
		t.Errorf("release root holds %v (error %v), want only the %s release (the archive removed)", entries, err, version)
	}
	if out, changed, failed := run.run(vars(version)); failed != 0 || changed != 0 {
		t.Errorf("rerun at the pinned version changed %d and failed %d tasks, want nothing:\n%s", changed, failed, out)
	}

	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeScratch(t, root, filepath.Join("0.9.0", "victoria-metrics-prod"), "older release")
	if out, changed, failed := run.run(vars(version)); failed != 0 || changed == 0 {
		t.Errorf("run over a truncated binary changed %d and failed %d tasks, want it replaced:\n%s", changed, failed, out)
	}
	if got := readScratch(t, root, filepath.Join(version, "victoria-metrics-prod")); !strings.Contains(got, "-tags-v"+version+"-") {
		t.Errorf("binary after replacing a truncated one = %q, want the verified release", got)
	}
	if _, err := os.Stat(filepath.Join(root, "0.9.0")); !os.IsNotExist(err) {
		t.Errorf("older release still present (stat error %v), want it removed", err)
	}

	before := downloads.Load()
	if out, _, failed := run.run(vars("9.9.9")); failed == 0 || !strings.Contains(out, "[victoriametrics_release_unavailable]") {
		t.Errorf("a version swallow does not declare = %d failed tasks, want [victoriametrics_release_unavailable]:\n%s", failed, out)
	}
	if got := downloads.Load(); got != before {
		t.Errorf("an undeclared version downloaded %d times, want no download", got-before)
	}
}

// The preflight step refuses a listen address or retention the api-server would not accept, before
// anything changes on the host, and passes the accepted ones.
func TestVictoriaMetricsPreflightRefusesUnacceptedIntent(t *testing.T) {
	t.Parallel()
	run := newRoleTasksRun(t, "victoriametrics", "preflight", false)
	tests := []struct {
		name      string
		listen    string
		retention string
		want      string
	}{
		{name: "defaults", want: ""},
		{name: "management and years", listen: "management", retention: "2y", want: ""},
		{name: "every interface", listen: "all", retention: "1M", want: "[victoriametrics_listen_invalid]"},
		{name: "hours", listen: "loopback", retention: "12h", want: "[victoriametrics_retention_invalid]"},
		{name: "minutes", listen: "loopback", retention: "1m", want: "[victoriametrics_retention_invalid]"},
		{name: "bare months", listen: "loopback", retention: "3", want: "[victoriametrics_retention_invalid]"},
	}
	for _, tc := range tests {
		out, changed, failed := run.run(map[string]any{
			"swallow_victoriametrics_listen":    tc.listen,
			"swallow_victoriametrics_retention": tc.retention,
		})
		if changed != 0 {
			t.Errorf("%s: preflight changed %d tasks, want none", tc.name, changed)
		}
		if tc.want == "" && failed != 0 {
			t.Errorf("%s: preflight failed %d tasks, want accepted:\n%s", tc.name, failed, out)
		}
		if tc.want != "" && (failed == 0 || !strings.Contains(out, tc.want)) {
			t.Errorf("%s: preflight failed %d tasks, want %s:\n%s", tc.name, failed, tc.want, out)
		}
	}
}

// The deploy playbook runs the role after the run's artifact sources, the role reads every trusted
// variable the module passes, refuses bad intent before it installs, installs before it starts the
// service, and waits for the service where it listens. The uninstall playbook runs the removal branch.
func TestVictoriaMetricsPlaybooksDriveTheRole(t *testing.T) {
	deploy := slurmPlaybookFile(t, "deploy-victoriametrics.yml")
	sources, role := strings.Index(deploy, "role: artifact_sources"), strings.Index(deploy, "role: victoriametrics")
	if sources < 0 || role < 0 || sources > role {
		t.Error("deploy-victoriametrics.yml must point the host at its artifact sources before the role runs")
	}
	defaults := slurmPlaybookFile(t, "roles", "victoriametrics", "defaults", "main.yml")
	for _, trusted := range []string{
		"swallow_victoriametrics_version", "swallow_victoriametrics_listen",
		"swallow_victoriametrics_retention", "swallow_victoriametrics_port",
	} {
		if !strings.Contains(defaults, "{{ "+trusted+" ") {
			t.Errorf("the victoriametrics role does not read %s", trusted)
		}
	}
	uninstall := slurmPlaybookFile(t, "uninstall-victoriametrics.yml")
	if !strings.Contains(uninstall, "role: victoriametrics") || !strings.Contains(uninstall, "victoriametrics_state: absent") {
		t.Error("uninstall-victoriametrics.yml must run the role with victoriametrics_state: absent")
	}

	main := slurmPlaybookFile(t, "roles", "victoriametrics", "tasks", "main.yml")
	preflight, release, service := strings.Index(main, "preflight.yml"), strings.Index(main, "release.yml"), strings.Index(main, "service.yml")
	if preflight < 0 || release < 0 || service < 0 || preflight > release || release > service {
		t.Error("the role must run preflight, then release, then service")
	}
	if !strings.Contains(main, "remove.yml") {
		t.Error("the role's removal branch must include remove.yml")
	}
	if tasks := slurmPlaybookFile(t, "roles", "victoriametrics", "tasks", "service.yml"); !strings.Contains(tasks,
		"http://{{ victoriametrics_listen_address }}:{{ victoriametrics_port }}/health") {
		t.Error("service.yml must wait for /health on the address VictoriaMetrics listens on")
	}
}

// VictoriaMetrics has no authentication, so its unit binds the loopback address or the management
// address the spec chose, never every interface; it runs as its own user that can write only its data
// directory; and the binary is unpacked as root's, not as the archive's uid 1000.
func TestVictoriaMetricsUnitBindsOnlyTheChosenAddress(t *testing.T) {
	unit := slurmPlaybookFile(t, "roles", "victoriametrics", "templates", "swallow-victoriametrics.service.j2")
	for _, want := range []string{
		"-httpListenAddr={{ victoriametrics_listen_address }}:{{ victoriametrics_port }}",
		"-retentionPeriod={{ victoriametrics_retention }}",
		"-storageDataPath={{ victoriametrics_data_dir }}",
		"User={{ victoriametrics_user }}",
		"ProtectSystem=strict",
		"ReadWritePaths={{ victoriametrics_data_dir }}",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("swallow-victoriametrics.service.j2 missing %q", want)
		}
	}
	if strings.Contains(unit, "0.0.0.0") {
		t.Error("swallow-victoriametrics.service.j2 must not bind every interface")
	}
	defaults := slurmPlaybookFile(t, "roles", "victoriametrics", "defaults", "main.yml")
	if !strings.Contains(defaults, "victoriametrics_listen_address: \"{{ '127.0.0.1' if victoriametrics_listen == 'loopback' else ansible_host }}\"") {
		t.Error("the listen address must be 127.0.0.1 for loopback and the management address otherwise")
	}
	release := slurmPlaybookFile(t, "roles", "victoriametrics", "tasks", "release.yml")
	if !strings.Contains(release, "--no-same-owner") {
		t.Error("release.yml must unpack the binary without the archive's owner")
	}
}

// The file module gives the directories it creates the task's owner and mode, so the service user's
// 0750 data directory would make /var/lib/swallow its own on a fresh host and lock Grafana and
// Prometheus out of their data. The role keeps the shared parent root-owned and 0755 before it
// creates the data directory.
func TestVictoriaMetricsKeepsTheSharedDataDirectoryRootOwned(t *testing.T) {
	var tasks []map[string]any
	readYAML(t, shippedFile(t, filepath.Join("roles", "victoriametrics", "tasks", "service.yml")), &tasks)
	parent, data := -1, -1
	for i, task := range tasks {
		args, _ := task["ansible.builtin.file"].(map[string]any)
		switch args["path"] {
		case "{{ victoriametrics_data_dir | dirname }}":
			parent = i
			if args["owner"] != "root" || args["group"] != "root" || args["mode"] != "0755" {
				t.Errorf("the shared data directory task sets %v:%v %v, want root:root 0755", args["owner"], args["group"], args["mode"])
			}
		case "{{ victoriametrics_data_dir }}":
			data = i
		}
	}
	if parent < 0 || data < 0 || parent > data {
		t.Errorf("service.yml must make the shared data directory root-owned (task %d) before it creates the data directory (task %d)", parent, data)
	}
}
