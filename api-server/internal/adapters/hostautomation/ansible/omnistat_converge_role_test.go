package ansible

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// convergeVersion is the Omnistat release the role's defaults install; the tests use its real patches
// and hash lock.
const convergeVersion = "1.15.0"

// convergeSourcePath is the declared path the tests serve a synthetic Omnistat source under.
const convergeSourcePath = "omnistat/test/omnistat-test.tar.gz"

// requireHostPython312 returns the host's CPython 3.12 and skips the test where there is none: the
// environments these tests build run the role's probe with a real interpreter.
func requireHostPython312(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("python3.12")
	if err != nil {
		t.Skip("python3.12 is not available")
	}
	return path
}

// omnistatPatches returns the patches the role's defaults list for convergeVersion.
func omnistatPatches(t *testing.T) []string {
	t.Helper()
	sources, _ := readRoleDefaults(t, "omnistat")["omnistat_sources"].(map[string]any)
	source, _ := sources[convergeVersion].(map[string]any)
	raw, _ := source["patches"].([]any)
	patches := make([]string, len(raw))
	for i, item := range raw {
		patches[i], _ = item.(string)
	}
	if len(patches) == 0 {
		t.Fatalf("omnistat_sources[%s] lists no patches", convergeVersion)
	}
	return patches
}

// hunkHeader reads the line counts of a unified diff hunk; a count left out is 1.
var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,(\d+))? \+\d+(?:,(\d+))? @@`)

// patchSides returns, for each file swallow's Omnistat patches change, the lines of the hunks' old
// side (context and removed lines) and new side (context and added lines). A file made of one side
// is a file the patches apply to forward (old) or show as applied (new), which is all patch checks.
func patchSides(t *testing.T) (old, patched map[string]string) {
	t.Helper()
	old, patched = map[string]string{}, map[string]string{}
	for _, name := range omnistatPatches(t) {
		raw, err := os.ReadFile(filepath.Join(roleDir(t, "omnistat"), "files", name))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(raw), "\n")
		var file string
		for i := 0; i < len(lines); i++ {
			line := lines[i]
			if strings.HasPrefix(line, "+++ ") {
				file = strings.TrimPrefix(strings.Fields(line)[1], "b/")
				continue
			}
			counts := hunkHeader.FindStringSubmatch(line)
			if counts == nil {
				continue
			}
			oldLeft, newLeft := hunkCount(counts[1]), hunkCount(counts[2])
			for i++; i < len(lines) && (oldLeft > 0 || newLeft > 0); i++ {
				body := lines[i]
				kind, text := byte(' '), ""
				if body != "" {
					kind, text = body[0], body[1:]
				}
				switch kind {
				case ' ':
					old[file] += text + "\n"
					patched[file] += text + "\n"
					oldLeft, newLeft = oldLeft-1, newLeft-1
				case '-':
					old[file] += text + "\n"
					oldLeft--
				case '+':
					patched[file] += text + "\n"
					newLeft--
				}
			}
			i--
		}
	}
	if len(old) == 0 {
		t.Fatal("the Omnistat patches change no file")
	}
	return old, patched
}

// hunkCount reads a hunk header's line count.
func hunkCount(raw string) int {
	if raw == "" {
		return 1
	}
	count, _ := strconv.Atoi(raw) // hunkHeader matches digits only
	return count
}

// lockedPin is one name==version line of Omnistat's hash lock.
type lockedPin struct{ name, version string }

// omnistatLock returns the pins of the role's hash lock for convergeVersion.
func omnistatLock(t *testing.T) []lockedPin {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(roleDir(t, "omnistat"), "files", "omnistat-"+convergeVersion+"-requirements.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var pins []lockedPin
	for _, match := range regexp.MustCompile(`(?m)^([A-Za-z0-9._-]+)==([^\s\\]+)`).FindAllStringSubmatch(string(raw), -1) {
		pins = append(pins, lockedPin{name: match[1], version: match[2]})
	}
	if len(pins) == 0 {
		t.Fatal("the Omnistat lock has no pins")
	}
	return pins
}

// installedEnvironment describes an environment for writeEnvironment: the locked pins installed,
// the Omnistat version recorded (none when ""), and Omnistat's files by path below site-packages.
type installedEnvironment struct {
	pins     []lockedPin
	omnistat string
	files    map[string]string
}

// writeEnvironment builds a Python environment at venv the way a venv looks to its interpreter
// (pyvenv.cfg, bin/python linked to the real one) with installed distributions recorded only by their
// metadata, which is all the role's probe reads.
func writeEnvironment(t *testing.T, venv, python string, env installedEnvironment) {
	t.Helper()
	writeScratch(t, venv, "pyvenv.cfg", "home = "+filepath.Dir(python)+"\ninclude-system-site-packages = false\n")
	if err := os.MkdirAll(filepath.Join(venv, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(python, filepath.Join(venv, "bin", "python")); err != nil {
		t.Fatal(err)
	}
	site := filepath.Join(venv, "lib", "python3.12", "site-packages")
	record := func(name, version string) {
		writeScratch(t, site, name+"-"+version+".dist-info/METADATA", "Metadata-Version: 2.1\nName: "+name+"\nVersion: "+version+"\n")
	}
	for _, pin := range env.pins {
		record(pin.name, pin.version)
	}
	if env.omnistat != "" {
		record("omnistat", env.omnistat)
	}
	for path, content := range env.files {
		writeScratch(t, site, path, content)
	}
}

// fakeOmnistatUV is a uv stand-in in Python, formatted with the interpreter that runs it and the
// path of its log. It logs each call as JSON and does what the role relies on: venv makes an
// environment that must not exist yet, pip install -r records every locked pin, and pip install of a
// source records Omnistat at the source's VERSION and copies its package.
const fakeOmnistatUV = `#!%[1]s
import json, os, re, shutil, sys
args = sys.argv[1:]
with open(%[2]q, "a") as log:
    log.write(json.dumps({"argv": args, "git_python_refresh": os.environ.get("GIT_PYTHON_REFRESH", "")}) + "\n")
if args == ["--version"]:
    print("uv 0.0.1")
    sys.exit(0)
def option(name):
    return args[args.index(name) + 1]
def site_packages(venv):
    return os.path.join(venv, "lib", "python3.12", "site-packages")
def record(site, name, version):
    info = os.path.join(site, name + "-" + version + ".dist-info")
    os.makedirs(info)
    with open(os.path.join(info, "METADATA"), "w") as metadata:
        metadata.write("Metadata-Version: 2.1\nName: %%s\nVersion: %%s\n" %% (name, version))
if args[0] == "venv":
    venv = args[-1]
    os.makedirs(os.path.join(venv, "bin"))
    os.makedirs(site_packages(venv))
    with open(os.path.join(venv, "pyvenv.cfg"), "w") as cfg:
        cfg.write("home = %%s\ninclude-system-site-packages = false\n" %% os.path.dirname(option("--python")))
    os.symlink(option("--python"), os.path.join(venv, "bin", "python"))
elif args[:2] == ["pip", "install"]:
    site = site_packages(os.path.dirname(os.path.dirname(option("--python"))))
    if "-r" in args:
        for name, version in re.findall(r"(?m)^([A-Za-z0-9._-]+)==([^\s\\]+)", open(option("-r")).read()):
            record(site, name, version)
    else:
        source = args[-1]
        record(site, "omnistat", open(os.path.join(source, "VERSION")).read().strip())
        shutil.copytree(os.path.join(source, "omnistat"), os.path.join(site, "omnistat"))
else:
    sys.exit(2)
`

// uvCall is one logged call of fakeOmnistatUV.
type uvCall struct {
	Argv             []string `json:"argv"`
	GitPythonRefresh string   `json:"git_python_refresh"`
}

// readUVCalls returns the calls fakeOmnistatUV logged at path, without its --version probes.
func readUVCalls(t *testing.T, path string) []uvCall {
	t.Helper()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var calls []uvCall
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		var call uvCall
		if err := json.Unmarshal(scanner.Bytes(), &call); err != nil {
			t.Fatalf("uv log line %q: %v", scanner.Text(), err)
		}
		if len(call.Argv) == 1 && call.Argv[0] == "--version" {
			continue
		}
		calls = append(calls, call)
	}
	return calls
}

// sourceArchive returns a gzipped tar laid out like Omnistat's tag archive: files under
// omnistat-<version>/.
func sourceArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	archive := tar.NewWriter(gz)
	top := "omnistat-" + convergeVersion + "/"
	dirs := map[string]bool{}
	for path, content := range files {
		for dir := filepath.Dir(path); dir != "."; dir = filepath.Dir(dir) {
			dirs[dir] = true
		}
		if err := archive.WriteHeader(&tar.Header{Name: top + path, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	for dir := range dirs {
		if err := archive.WriteHeader(&tar.Header{Name: top + dir + "/", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// rebuiltReport matches the line the test play prints after converging: whether it rebuilt.
var rebuiltReport = regexp.MustCompile(`"msg": "rebuilt (True|False)"`)

// convergeRun prepares a play that runs only the role's convergence (converge.yml), which needs no
// root, followed by a task that prints whether it rebuilt the environment.
func convergeRun(t *testing.T) *roleRun {
	t.Helper()
	run := newRoleRun(t, "omnistat", false)
	writeScratch(t, run.dir, "play.yml", "- hosts: all\n  gather_facts: false\n  tasks:\n"+
		"    - name: Converge the Omnistat environment\n      ansible.builtin.include_role:\n        name: omnistat\n        tasks_from: converge\n"+
		"    - name: Report the convergence\n      ansible.builtin.debug:\n        msg: \"rebuilt {{ omnistat_environment_rebuilt }}\"\n")
	return run
}

// rebuilt returns whether a converge run reported a rebuilt environment.
func rebuilt(t *testing.T, out string) bool {
	t.Helper()
	match := rebuiltReport.FindStringSubmatch(out)
	if match == nil {
		t.Fatalf("the run reported no convergence:\n%s", out)
	}
	return match[1] == "True"
}

// convergeVars are the role's variables for a scratch node at root: Omnistat under root, the
// candidates python_toolchain may use, and the synthetic source at files when the run fetches one.
func convergeVars(t *testing.T, root, python, uv, sum, files string) map[string]any {
	t.Helper()
	return map[string]any{
		"omnistat_home": filepath.Join(root, "opt", "omnistat"),
		"omnistat_sources": map[string]any{convergeVersion: map[string]any{
			"path": convergeSourcePath, "sha256": sum, "patches": omnistatPatches(t),
			"requirements": "omnistat-" + convergeVersion + "-requirements.txt",
		}},
		"python_toolchain_python_candidates": []string{python},
		"python_toolchain_uv_candidates":     []string{uv},
		"swallow_artifacts":                  map[string]any{"files": map[string]any{convergeSourcePath: files + "/files/" + convergeSourcePath}},
	}
}

// An environment that already is the expected Omnistat (CPython 3.12, every locked pin, Omnistat
// 1.15.0, swallow's patches applied), as an OS image or an earlier run leaves it, is used as it is:
// the role brings in no toolchain and fetches nothing (neither has a source here), points current at
// it, and a second run changes nothing.
func TestOmnistatUsesAUsableEnvironmentAsItIs(t *testing.T) {
	t.Parallel()
	python := requireHostPython312(t)
	_, patched := patchSides(t)
	run := convergeRun(t)
	root := filepath.Join(t.TempDir(), "root")
	prefix := filepath.Join(root, "opt", "omnistat", convergeVersion)
	writeEnvironment(t, filepath.Join(prefix, "venv"), python, installedEnvironment{pins: omnistatLock(t), omnistat: convergeVersion, files: patched})
	vars := convergeVars(t, root, python, filepath.Join(root, "no-uv"), strings.Repeat("0", 64), "http://127.0.0.1:9")

	out, _, failed := run.run(vars)
	if failed != 0 || rebuilt(t, out) {
		t.Fatalf("run over a usable environment failed %d tasks and rebuilt it, want it used as it is:\n%s", failed, out)
	}
	for _, task := range []string{"TASK [omnistat : Find or supply CPython 3.12 and uv]", "TASK [python_toolchain", "TASK [omnistat : Download the pinned Omnistat source]"} {
		if strings.Contains(out, task) {
			t.Errorf("the run reached %q over a usable environment:\n%s", task, out)
		}
	}
	if target, err := os.Readlink(filepath.Join(root, "opt", "omnistat", "current")); err != nil || target != prefix {
		t.Errorf("current = %q (error %v), want %s", target, err, prefix)
	}
	if out, changed, failed := run.run(vars); failed != 0 || changed != 0 {
		t.Errorf("second run changed %d and failed %d tasks, want nothing:\n%s", changed, failed, out)
	}
}

// An environment that is not the expected Omnistat is rebuilt from nothing: python_toolchain's
// interpreter and uv, the declared source with swallow's patches applied, a fresh environment, the
// hash lock as wheels only, then Omnistat without build isolation or dependencies, with no index
// named and GitPython quiet without git. The rebuilt environment passes the probe, so a second run
// changes nothing and calls uv no more.
func TestOmnistatRebuildsAnEnvironmentThatIsNotUsable(t *testing.T) {
	t.Parallel()
	python := requireHostPython312(t)
	old, patched := patchSides(t)
	pins := omnistatLock(t)
	source := map[string]string{"pyproject.toml": "[project]\nname = \"omnistat\"\n", "VERSION": convergeVersion + "\n", "omnistat/__init__.py": ""}
	for path, content := range old {
		source[path] = content
	}
	archive := sourceArchive(t, source)
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files/"+convergeSourcePath {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(archive)
	}))
	t.Cleanup(server.Close)

	var withoutBuildRequirements []lockedPin
	for _, pin := range pins {
		if pin.name != "cmake" && pin.name != "nanobind" {
			withoutBuildRequirements = append(withoutBuildRequirements, pin)
		}
	}
	cases := []struct {
		name string
		env  *installedEnvironment
	}{
		{name: "no environment"},
		{name: "another Omnistat version", env: &installedEnvironment{pins: pins, omnistat: "1.14.0", files: patched}},
		{name: "a patch not applied", env: &installedEnvironment{pins: pins, omnistat: convergeVersion, files: old}},
		{name: "built by pip before the lock", env: &installedEnvironment{pins: withoutBuildRequirements, omnistat: convergeVersion, files: patched}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			run := convergeRun(t)
			root := filepath.Join(t.TempDir(), "root")
			prefix := filepath.Join(root, "opt", "omnistat", convergeVersion)
			venv := filepath.Join(prefix, "venv")
			if tc.env != nil {
				writeEnvironment(t, venv, python, *tc.env)
			}
			uv, log := filepath.Join(root, "tools", "uv"), filepath.Join(root, "uv.log")
			writeExecutable(t, uv, fmt.Sprintf(fakeOmnistatUV, python, log))
			vars := convergeVars(t, root, python, uv, hex.EncodeToString(sum[:]), server.URL)

			out, _, failed := run.run(vars)
			if failed != 0 || !rebuilt(t, out) {
				t.Fatalf("run failed %d tasks and reported no rebuild, want the environment rebuilt:\n%s", failed, out)
			}
			calls := readUVCalls(t, log)
			lock := filepath.Join(prefix, "omnistat-"+convergeVersion+"-requirements.txt")
			want := [][]string{
				{"venv", "--python", python, venv},
				{"pip", "install", "--python", venv + "/bin/python", "--no-cache", "--require-hashes", "--no-build", "-r", lock},
				{"pip", "install", "--python", venv + "/bin/python", "--no-cache", "--no-deps", "--no-build-isolation", filepath.Join(prefix, "src")},
			}
			if len(calls) != len(want) {
				t.Fatalf("uv calls = %v, want %v", calls, want)
			}
			for i, call := range calls {
				if strings.Join(call.Argv, " ") != strings.Join(want[i], " ") {
					t.Errorf("uv call %d = %v, want %v", i, call.Argv, want[i])
				}
			}
			if got := calls[2].GitPythonRefresh; got != "quiet" {
				t.Errorf("Omnistat's install ran with GIT_PYTHON_REFRESH %q, want quiet", got)
			}
			if out, changed, failed := run.run(vars); failed != 0 || changed != 0 || len(readUVCalls(t, log)) != len(want) {
				t.Errorf("second run changed %d and failed %d tasks and called uv %d times in all, want nothing more:\n%s",
					changed, failed, len(readUVCalls(t, log)), out)
			}
		})
	}
}
