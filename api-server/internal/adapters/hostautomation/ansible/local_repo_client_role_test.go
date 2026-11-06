package ansible

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// roleRun runs one shipped role on localhost through ansible-playbook, as the run would get it from
// its roles path, without become: a test points the role's paths at a scratch directory first.
type roleRun struct {
	t        *testing.T
	playbook string
	env      []string
	dir      string
}

// newRoleRun prepares a play of the shipped role, gathering facts when the role needs them, and
// skips the test where ansible-playbook is not installed.
func newRoleRun(t *testing.T, role string, gatherFacts bool) *roleRun {
	t.Helper()
	playbook, err := exec.LookPath("ansible-playbook")
	if err != nil {
		t.Skip("ansible-playbook is not available")
	}
	dir := t.TempDir()
	roles := []string{filepath.Dir(roleDir(t, role))}
	if shared := filepath.Join(modulesRoot, sharedModule, "roles"); roles[0] != shared {
		roles = append(roles, shared)
	}
	for i, path := range roles {
		abs, err := filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		roles[i] = abs
	}
	play := "- hosts: all\n  gather_facts: " + strconv.FormatBool(gatherFacts) + "\n  roles:\n    - role: " + role + "\n"
	if err := os.WriteFile(filepath.Join(dir, "play.yml"), []byte(play), 0o644); err != nil {
		t.Fatal(err)
	}
	return &roleRun{
		t:        t,
		playbook: playbook,
		dir:      dir,
		env: append(os.Environ(),
			"ANSIBLE_ROLES_PATH="+strings.Join(roles, string(os.PathListSeparator)),
			"ANSIBLE_HOME="+filepath.Join(dir, "home"),
			"ANSIBLE_LOCAL_TEMP="+filepath.Join(dir, "tmp"),
			"ANSIBLE_REMOTE_TEMP="+filepath.Join(dir, "tmp"),
			"ANSIBLE_NOCOLOR=1",
			"ANSIBLE_RETRY_FILES_ENABLED=0",
			"ANSIBLE_PYTHON_INTERPRETER=auto_silent",
		),
	}
}

// recapCounts reads the changed and failed counts of the play recap's localhost line.
var recapCounts = regexp.MustCompile(`localhost\s+:\s+ok=\d+\s+changed=(\d+)\s+unreachable=\d+\s+failed=(\d+)`)

// run executes the play with vars as extra vars and returns its output and the recap's changed and
// failed task counts.
func (r *roleRun) run(vars map[string]any) (output string, changed, failed int) {
	r.t.Helper()
	vars["ansible_become"] = false
	raw, err := json.Marshal(vars)
	if err != nil {
		r.t.Fatal(err)
	}
	varsFile := filepath.Join(r.dir, "vars.json")
	if err := os.WriteFile(varsFile, raw, 0o600); err != nil {
		r.t.Fatal(err)
	}
	cmd := exec.Command(r.playbook, "-i", "localhost,", "-c", "local", "-e", "@"+varsFile, "play.yml")
	cmd.Dir, cmd.Env = r.dir, r.env
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	_ = cmd.Run() // a failed play is reported by the recap, which the caller checks
	match := recapCounts.FindStringSubmatch(out.String())
	if match == nil {
		r.t.Fatalf("ansible-playbook printed no play recap:\n%s", out.String())
	}
	changed, _ = strconv.Atoi(match[1]) // recapCounts matches digits only
	failed, _ = strconv.Atoi(match[2])
	return out.String(), changed, failed
}

// writeScratch writes content at rel under root, creating its directories.
func writeScratch(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readScratch returns the file at rel under root, reporting a missing file as a test error and ""
// so the remaining checks still run.
func readScratch(t *testing.T, root, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Errorf("read %s: %v", rel, err)
	}
	return string(raw)
}

// testLocalRepository is a swallow_local_repository as the api-server sends it.
func testLocalRepository() map[string]any {
	return map[string]any{
		"deb": []any{
			map[string]any{
				"name": "ubuntu", "uri": "http://10.0.0.20:8080/pulp/content/deb/ubuntu/",
				"suites": []any{"noble", "noble-updates", "noble-security"}, "components": []any{"main", "universe"},
			},
			map[string]any{
				"name": "docker-ce", "uri": "http://10.0.0.20:8080/pulp/content/deb/docker-ce/",
				"suites": []any{"noble"}, "components": []any{"stable"},
			},
		},
		"deb_signing_key": "-----BEGIN PGP PUBLIC KEY BLOCK-----\n\nmQINBGc\n-----END PGP PUBLIC KEY BLOCK-----\n",
		"registry":        "10.0.0.20:8080",
		"registry_mirrors": map[string]any{
			"docker.io": "http://10.0.0.20:8080/v2/docker.io",
			"quay.io":   "http://10.0.0.20:8080/v2/quay.io",
		},
		"python_index": "http://10.0.0.20:8080/pypi/pypi/simple/",
		"files":        "http://10.0.0.20:8080/pulp/content/files/",
	}
}

// local_repo_client makes the Local Repository the host's only package, Python, and image source:
// every other apt source with an active line, the distribution's or the image's, is set aside
// (recoverably), apt bypasses the host's proxy for it,
// k0s's containerd resolves each mirrored registry through it and forgets one it no longer
// mirrors, and Docker gains the registry without losing its other settings. A second run changes
// nothing, and a run without the trusted variable stops before touching the host.
func TestLocalRepoClientPointsTheHostAtTheLocalRepository(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat("/etc/debian_version"); err != nil {
		t.Skip("the apt path runs on a Debian-family host only")
	}
	run := newRoleRun(t, localRepositoryRole, false)
	root := filepath.Join(t.TempDir(), "root")
	writeScratch(t, root, "etc/apt/sources.list.d/ubuntu.sources",
		"Types: deb\nURIs: http://archive.ubuntu.com/ubuntu/\nSuites: noble\nComponents: main\n")
	writeScratch(t, root, "etc/apt/sources.list", "# Ubuntu sources have moved to /etc/apt/sources.list.d/ubuntu.sources\n\n")
	writeScratch(t, root, "etc/apt/sources.list.d/amdgpu.list",
		"deb [arch=amd64 signed-by=/etc/apt/keyrings/rocm.gpg] https://repo.radeon.com/amdgpu/31.50/ubuntu noble main\n")
	writeScratch(t, root, "etc/apt/sources.list.d/retired.list", "# deb http://mirror.example/ubuntu noble main\n")
	writeScratch(t, root, "etc/apt/apt.conf.d/90curtin-aptproxy", "Acquire::http::Proxy \"http://maas:8000/\";\n")
	writeScratch(t, root, "etc/docker/daemon.json", `{"log-driver": "journald", "insecure-registries": ["10.0.0.5:5000"]}`+"\n")
	writeScratch(t, root, "etc/k0s/containerd.d/certs.d/ghcr.io/hosts.toml", "server = \"https://ghcr.io\"\n")
	vars := func() map[string]any {
		return map[string]any{
			"local_repo_client_root":       root,
			"local_repo_client_containerd": true,
			"local_repo_client_docker":     true,
			"local_repo_client_podman":     true,
			"swallow_local_repository":     testLocalRepository(),
		}
	}

	if out, _, failed := run.run(vars()); failed != 0 {
		t.Fatalf("first run failed:\n%s", out)
	}
	wantSources := "\nTypes: deb\nURIs: http://10.0.0.20:8080/pulp/content/deb/ubuntu/\nSuites: noble noble-updates noble-security\n" +
		"Components: main universe\nSigned-By: /etc/apt/keyrings/swallow-local-repository.asc\n" +
		"\nTypes: deb\nURIs: http://10.0.0.20:8080/pulp/content/deb/docker-ce/\nSuites: noble\n" +
		"Components: stable\nSigned-By: /etc/apt/keyrings/swallow-local-repository.asc\n"
	if got := readScratch(t, root, "etc/apt/sources.list.d/swallow-local-repository.sources"); !strings.HasSuffix(got, wantSources) {
		t.Errorf("swallow-local-repository.sources =\n%s\nwant one deb822 stanza per source ending in\n%s", got, wantSources)
	}
	if got := readScratch(t, root, "etc/apt/keyrings/swallow-local-repository.asc"); got != "-----BEGIN PGP PUBLIC KEY BLOCK-----\n\nmQINBGc\n-----END PGP PUBLIC KEY BLOCK-----\n" {
		t.Errorf("signing key = %q, want the trusted variable's key", got)
	}
	if _, err := os.Stat(filepath.Join(root, "etc/apt/sources.list.d/ubuntu.sources")); !os.IsNotExist(err) {
		t.Errorf("ubuntu.sources is still active (stat error %v), want it set aside", err)
	}
	if got := readScratch(t, root, "etc/apt/sources.list.d/ubuntu.sources.disabled-by-swallow"); !strings.Contains(got, "archive.ubuntu.com") {
		t.Errorf("ubuntu.sources.disabled-by-swallow = %q, want the distribution's sources kept for recovery", got)
	}
	if got := readScratch(t, root, "etc/apt/sources.list"); !strings.HasPrefix(got, "# Ubuntu sources have moved") {
		t.Errorf("sources.list = %q, want a file with only comments left in place", got)
	}
	if _, err := os.Stat(filepath.Join(root, "etc/apt/sources.list.d/amdgpu.list")); !os.IsNotExist(err) {
		t.Errorf("the image's amdgpu.list is still active (stat error %v), want it set aside like the distribution's", err)
	}
	if got := readScratch(t, root, "etc/apt/sources.list.d/amdgpu.list.disabled-by-swallow"); !strings.Contains(got, "repo.radeon.com") {
		t.Errorf("amdgpu.list.disabled-by-swallow = %q, want the image's source kept for recovery", got)
	}
	if got := readScratch(t, root, "etc/apt/sources.list.d/retired.list"); !strings.HasPrefix(got, "# deb ") {
		t.Errorf("retired.list = %q, want a file with only comments left in place", got)
	}
	if got := readScratch(t, root, "etc/apt/apt.conf.d/90swallow-local-repository"); !strings.HasSuffix(got, "\nAcquire::http::Proxy::10.0.0.20 \"DIRECT\";\n") ||
		strings.Count(got, "DIRECT") != 1 {
		t.Errorf("apt proxy bypass =\n%s\nwant one DIRECT line for the repository host", got)
	}
	if got := readScratch(t, root, "etc/pip.conf"); !strings.Contains(got, "[global]\nindex-url = http://10.0.0.20:8080/pypi/pypi/simple/\ntrusted-host = 10.0.0.20\n") {
		t.Errorf("pip.conf =\n%s\nwant the Python index and its trusted host", got)
	}
	wantDropin := "version = 3\n\n[plugins.\"io.containerd.cri.v1.images\".registry]\n  config_path = \"/etc/k0s/containerd.d/certs.d\"\n"
	if got := readScratch(t, root, "etc/k0s/containerd.d/swallow-registry-mirrors.toml"); !strings.HasSuffix(got, wantDropin) {
		t.Errorf("containerd drop-in =\n%s\nwant it to end in\n%s", got, wantDropin)
	}
	for upstream, mirror := range map[string]string{"docker.io": "http://10.0.0.20:8080/v2/docker.io", "quay.io": "http://10.0.0.20:8080/v2/quay.io"} {
		want := "server = \"https://" + upstream + "\"\n\n[host.\"" + mirror + "\"]\n  capabilities = [\"pull\", \"resolve\"]\n  override_path = true\n"
		if got := readScratch(t, root, "etc/k0s/containerd.d/certs.d/"+upstream+"/hosts.toml"); !strings.HasSuffix(got, want) {
			t.Errorf("%s hosts.toml =\n%s\nwant it to end in\n%s", upstream, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "etc/k0s/containerd.d/certs.d/ghcr.io")); !os.IsNotExist(err) {
		t.Errorf("certs.d/ghcr.io remains (stat error %v), want a registry the Local Repository does not mirror removed", err)
	}
	var daemon map[string]any
	if err := json.Unmarshal([]byte(readScratch(t, root, "etc/docker/daemon.json")), &daemon); err != nil {
		t.Errorf("daemon.json: %v", err)
	}
	if got := daemon["insecure-registries"]; strings.Join(stringsOf(got), ",") != "10.0.0.5:5000,10.0.0.20:8080" || daemon["log-driver"] != "journald" {
		t.Errorf("daemon.json = %v, want the registry added to insecure-registries and the other keys kept", daemon)
	}
	if got := readScratch(t, root, "etc/containers/registries.conf.d/50-swallow-local-repository.conf"); !strings.Contains(got, "location = \"10.0.0.20:8080\"\ninsecure = true\n") {
		t.Errorf("Podman registries drop-in =\n%s\nwant the registry marked insecure", got)
	}

	if out, changed, failed := run.run(vars()); failed != 0 || changed != 0 {
		t.Errorf("second run changed %d and failed %d tasks, want a converged host unchanged:\n%s", changed, failed, out)
	}

	missing := vars()
	delete(missing, "swallow_local_repository")
	if out, _, failed := run.run(missing); failed == 0 || !strings.Contains(out, "[local_repository_missing]") {
		t.Errorf("a run without swallow_local_repository = %d failed tasks, want [local_repository_missing]:\n%s", failed, out)
	}
}

// stringsOf returns the strings of a decoded JSON array.
func stringsOf(value any) []string {
	items, _ := value.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
