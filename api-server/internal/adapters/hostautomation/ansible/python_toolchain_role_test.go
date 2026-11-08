package ansible

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// pythonToolchainRole is the shared role that finds any CPython 3.12 and any uv on a host and
// supplies only what is missing (decision 080).
const pythonToolchainRole = "python_toolchain"

// toolchainReport matches the line the test play prints after the role: the interpreter and the uv
// it chose.
var toolchainReport = regexp.MustCompile(`"msg": "toolchain (\S*) (\S*)"`)

// newToolchainRun prepares a play of python_toolchain followed by a task that prints what it chose.
func newToolchainRun(t *testing.T) *roleRun {
	t.Helper()
	run := newRoleRun(t, pythonToolchainRole, false)
	writeScratch(t, run.dir, "play.yml", "- hosts: all\n  gather_facts: false\n  roles:\n    - role: "+pythonToolchainRole+"\n"+
		"  tasks:\n    - name: Report the toolchain\n      ansible.builtin.debug:\n"+
		"        msg: \"toolchain {{ python_toolchain_python }} {{ python_toolchain_uv }}\"\n")
	return run
}

// chosenToolchain returns the interpreter and uv a run reported, failing the test when it printed none.
func chosenToolchain(t *testing.T, out string) (python, uv string) {
	t.Helper()
	match := toolchainReport.FindStringSubmatch(out)
	if match == nil {
		t.Fatalf("the run reported no toolchain:\n%s", out)
	}
	return match[1], match[2]
}

// writeExecutable writes a shell script at path and makes it executable.
func writeExecutable(t *testing.T, path, script string) {
	t.Helper()
	writeScratch(t, filepath.Dir(path), filepath.Base(path), script)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// Stand-ins for the candidates the role tries: an interpreter that is CPython 3.12 prints its own
// path as sys.executable would, one that is another version exits non-zero; a uv reports itself as
// uv, and another program that happens to be called uv does not.
const (
	fakePython312   = "#!/bin/sh\necho \"$0\"\n"
	fakeOtherPython = "#!/bin/sh\nexit 1\n"
	fakeUV          = "#!/bin/sh\necho 'uv 0.0.1'\n"
	fakeOtherUV     = "#!/bin/sh\necho 'unrelated 1.0'\n"
)

// A host that already has a CPython 3.12 and a uv keeps them: the role takes the first candidate of
// each that works, in its order, skipping a missing path, an interpreter of another version, and a
// program that is not uv, and it changes nothing. The host's own python3.12, where it has one, is
// found by the real probe.
func TestPythonToolchainUsesTheFirstCandidatesThatWork(t *testing.T) {
	t.Parallel()
	run := newToolchainRun(t)
	dir := t.TempDir()
	for name, script := range map[string]string{
		"python-other": fakeOtherPython, "python-first": fakePython312, "python-second": fakePython312,
		"uv-other": fakeOtherUV, "uv-first": fakeUV, "uv-second": fakeUV,
	} {
		writeExecutable(t, filepath.Join(dir, name), script)
	}
	vars := map[string]any{
		"python_toolchain_python_candidates": []string{
			filepath.Join(dir, "python-missing"), filepath.Join(dir, "python-other"),
			filepath.Join(dir, "python-first"), filepath.Join(dir, "python-second"),
		},
		"python_toolchain_uv_candidates": []string{
			filepath.Join(dir, "uv-missing"), filepath.Join(dir, "uv-other"),
			filepath.Join(dir, "uv-first"), filepath.Join(dir, "uv-second"),
		},
	}
	out, changed, failed := run.run(vars)
	if failed != 0 || changed != 0 {
		t.Fatalf("run changed %d and failed %d tasks, want a host with both used as it is:\n%s", changed, failed, out)
	}
	if python, uv := chosenToolchain(t, out); python != filepath.Join(dir, "python-first") || uv != filepath.Join(dir, "uv-first") {
		t.Errorf("chose %s and %s, want the first working candidates %s and %s",
			python, uv, filepath.Join(dir, "python-first"), filepath.Join(dir, "uv-first"))
	}

	const host = "/usr/bin/python3.12"
	if _, err := os.Stat(host); err != nil {
		t.Logf("%s is not on this host; the real interpreter probe is not exercised", host)
		return
	}
	vars["python_toolchain_python_candidates"] = []string{filepath.Join(dir, "python-other"), host}
	out, _, failed = run.run(vars)
	if python, _ := chosenToolchain(t, out); failed != 0 || python != host {
		t.Errorf("with the host's interpreter as a candidate the run failed %d tasks and chose %q, want %s:\n%s", failed, python, host, out)
	}
}

// uvArchive returns a gzipped tar like uv's release: the binary under a top-level directory.
func uvArchive(t *testing.T, dir, binary string) []byte {
	t.Helper()
	return tarGz(t, map[string]string{dir + "/uv": binary}, dir+"/uv")
}

// A host without uv gets the declared release from the URL the run's artifact sources give its
// path, verified by its SHA-256 and linked where the role looks next, so the next run finds it and
// changes nothing. An architecture swallow declares no uv for stops before any download.
func TestPythonToolchainSuppliesTheDeclaredUV(t *testing.T) {
	t.Parallel()
	const path, unpacked = "uv/9.9.9/uv-test.tar.gz", "uv-test"
	archive := uvArchive(t, unpacked, fakeUV)
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files/"+path {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(archive)
	}))
	t.Cleanup(server.Close)
	run := newToolchainRun(t)
	tools := t.TempDir()
	writeExecutable(t, filepath.Join(tools, "python"), fakePython312)
	vars := func(root, checksum string, architectures ...string) map[string]any {
		releases := map[string]any{}
		for _, architecture := range architectures {
			releases[architecture] = map[string]any{"path": path, "sha256": checksum, "dir": unpacked}
		}
		link := filepath.Join(root, "opt", "swallow", "bin", "uv")
		return map[string]any{
			"python_toolchain_python_candidates": []string{filepath.Join(tools, "python")},
			"python_toolchain_uv_candidates":     []string{filepath.Join(root, "usr", "local", "bin", "uv"), link},
			"python_toolchain_uv_home":           filepath.Join(root, "opt", "swallow", "uv"),
			"python_toolchain_uv_link":           link,
			"python_toolchain_uv_releases":       releases,
			"swallow_artifacts":                  map[string]any{"files": map[string]any{path: server.URL + "/files/" + path}},
		}
	}
	declared := hex.EncodeToString(sum[:])

	root := filepath.Join(t.TempDir(), "root")
	if out, changed, failed := run.run(vars(root, declared, "x86_64", "aarch64")); failed != 0 || changed == 0 {
		t.Fatalf("first run changed %d and failed %d tasks, want the declared uv supplied:\n%s", changed, failed, out)
	} else if _, uv := chosenToolchain(t, out); uv != filepath.Join(root, "opt", "swallow", "bin", "uv") {
		t.Errorf("chose uv %s, want the supplied one linked at %s", uv, filepath.Join(root, "opt", "swallow", "bin", "uv"))
	}
	if target, err := os.Readlink(filepath.Join(root, "opt", "swallow", "bin", "uv")); err != nil ||
		target != filepath.Join(root, "opt", "swallow", "uv", unpacked, "uv") {
		t.Errorf("uv link = %q (error %v), want it to point into the unpacked release", target, err)
	}
	if out, changed, failed := run.run(vars(root, declared, "x86_64", "aarch64")); failed != 0 || changed != 0 {
		t.Errorf("second run changed %d and failed %d tasks, want the supplied uv found and nothing changed:\n%s", changed, failed, out)
	}

	fresh := filepath.Join(t.TempDir(), "root")
	if out, _, failed := run.run(vars(fresh, declared)); failed == 0 || !strings.Contains(out, "[uv_unavailable]") {
		t.Errorf("a run on an architecture swallow declares no uv for = %d failed tasks, want [uv_unavailable]:\n%s", failed, out)
	}
	assertAbsent(t, fresh, "opt/swallow/bin/uv", "opt/swallow/uv")
}

// A host without CPython 3.12 whose OS swallow installs no package for stops with
// [python312_unavailable] before anything else, so no role builds with another interpreter.
func TestPythonToolchainRefusesAHostWithoutCPython312(t *testing.T) {
	t.Parallel()
	run := newToolchainRun(t)
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "python-other"), fakeOtherPython)
	out, _, failed := run.run(map[string]any{
		"python_toolchain_python_candidates": []string{filepath.Join(dir, "python-missing"), filepath.Join(dir, "python-other")},
		"python_toolchain_python_packages":   map[string]any{},
	})
	if failed == 0 || !strings.Contains(out, "[python312_unavailable]") {
		t.Errorf("run without CPython 3.12 = %d failed tasks, want [python312_unavailable]:\n%s", failed, out)
	}
	if strings.Contains(out, "Look for uv") {
		t.Errorf("the run went on to look for uv after refusing the host:\n%s", out)
	}
}
