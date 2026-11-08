package ansible

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	return newPlayRun(t, role, "- hosts: all\n  gather_facts: "+strconv.FormatBool(gatherFacts)+
		"\n  roles:\n    - role: "+role+"\n")
}

// newRoleTasksRun prepares a play of one task file of the shipped role (include_role with tasks_from,
// which still loads the role's defaults), for a role whose other task files need root or systemd.
func newRoleTasksRun(t *testing.T, role, tasksFrom string, gatherFacts bool) *roleRun {
	t.Helper()
	return newPlayRun(t, role, "- hosts: all\n  gather_facts: "+strconv.FormatBool(gatherFacts)+
		"\n  tasks:\n    - ansible.builtin.include_role:\n        name: "+role+"\n        tasks_from: "+tasksFrom+"\n")
}

// newPlayRun prepares play under the roles path a run of the role's module gets (that module's roles
// and shared/roles), and skips the test where ansible-playbook is not installed.
func newPlayRun(t *testing.T, role, play string) *roleRun {
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

// snapshotScratch returns every file under root by its path relative to root, so two states of a
// scratch host compare as a whole.
func snapshotScratch(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		files[filepath.ToSlash(rel)] = string(raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// diffSnapshots lists the paths whose content differs between two snapshots, sorted.
func diffSnapshots(got, want map[string]string) []string {
	var diff []string
	for path, content := range got {
		if wanted, ok := want[path]; !ok || wanted != content {
			diff = append(diff, path)
		}
	}
	for path := range want {
		if _, ok := got[path]; !ok {
			diff = append(diff, path)
		}
	}
	sort.Strings(diff)
	return diff
}

// testSigningKey is a throwaway OpenPGP public key (ed25519; its secret was discarded) standing in
// for the signing key a module declares for an upstream deb source, so the tests need no Internet.
const testSigningKey = `-----BEGIN PGP PUBLIC KEY BLOCK-----

mDMEau1SZhYJKwYBBAHaRw8BAQdAMfvNc1Vh9K8JIreDlOvI4SSvFKX6v7RkPA0Q
AeTOKHC0M3N3YWxsb3cgY29udGVudCB0ZXN0IDxjb250ZW50LXRlc3RAc3dhbGxv
dy5pbnZhbGlkPoiTBBMWCgA7FiEErQ1TYTdi1j+7apllRKTygVA96s4FAmrtUmYC
GwMFCwkIBwICIgIGFQoJCAsCBBYCAwECHgcCF4AACgkQRKTygVA96s6GTAEAlgUB
/xguNKT/RrDS8jvstgT7biWi86QvmuW03dDy984BAKFJs8gow8K6FVH/mpwIZDau
4UiUw+7nqrDmHbpQQLcD
=rj1L
-----END PGP PUBLIC KEY BLOCK-----
`

// testSigningKeyFingerprint is testSigningKey's fingerprint, as a module declares it.
const testSigningKeyFingerprint = "AD0D53613762D63FBB6A996544A4F281503DEACE"

// dearmoredTestSigningKey returns testSigningKey's binary packets: the keyring apt reads for an
// upstream source.
func dearmoredTestSigningKey(t *testing.T) string {
	t.Helper()
	var body strings.Builder
	for _, line := range strings.Split(testSigningKey, "\n")[2:] {
		if strings.HasPrefix(line, "=") || strings.HasPrefix(line, "-----END") {
			break
		}
		body.WriteString(line)
	}
	raw, err := base64.StdEncoding.DecodeString(body.String())
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// keyServer stands in for the upstreams' key URLs: /keys/garbage.gpg serves no key, any other
// /keys/<name>.gpg serves testSigningKey. It counts the requests per path, so a test can tell which
// keys a run fetched.
type keyServer struct {
	url      string
	mu       sync.Mutex     // guards requests, which the server's handler goroutines write
	requests map[string]int // path: requests so far
}

// newKeyServer starts a keyServer for the test's lifetime.
func newKeyServer(t *testing.T) *keyServer {
	t.Helper()
	keys := &keyServer{requests: map[string]int{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys.mu.Lock()
		keys.requests[r.URL.Path]++
		keys.mu.Unlock()
		switch {
		case r.URL.Path == "/keys/garbage.gpg":
			_, _ = w.Write([]byte("<html>not a key</html>\n"))
		case strings.HasPrefix(r.URL.Path, "/keys/"):
			_, _ = w.Write([]byte(testSigningKey))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	keys.url = server.URL
	return keys
}

// requested reports how many times path was requested.
func (k *keyServer) requested(path string) int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.requests[path]
}

// testLocalRepository is swallow_artifacts with the Site's Local Repository serving every artifact,
// as the api-server sends it.
func testLocalRepository() map[string]any {
	return map[string]any{
		"source": "local_repository",
		"deb": []any{
			map[string]any{
				"name": "ubuntu", "uri": "http://10.0.0.20:8080/pulp/content/deb/ubuntu/",
				"suites": []any{"noble", "noble-updates", "noble-security"}, "components": []any{"main", "universe"},
				"signing_key_url": "", "signing_key_fingerprint": "",
			},
			map[string]any{
				"name": "docker-ce", "uri": "http://10.0.0.20:8080/pulp/content/deb/docker-ce/",
				"suites": []any{"noble"}, "components": []any{"stable"},
				"signing_key_url": "", "signing_key_fingerprint": "",
			},
		},
		"deb_signing_key": "-----BEGIN PGP PUBLIC KEY BLOCK-----\n\nmQINBGc\n-----END PGP PUBLIC KEY BLOCK-----\n",
		"registry":        "10.0.0.20:8080",
		"registry_mirrors": map[string]any{
			"docker.io": "http://10.0.0.20:8080/v2/docker.io",
			"quay.io":   "http://10.0.0.20:8080/v2/quay.io",
		},
		"python_index": "http://10.0.0.20:8080/pypi/pypi/simple/",
		"files": map[string]any{
			"helm/v3.22.1/helm-v3.22.1-linux-amd64.tar.gz": "http://10.0.0.20:8080/pulp/content/files/helm/v3.22.1/helm-v3.22.1-linux-amd64.tar.gz",
		},
		"images": map[string]any{
			"docker.io/grafana/grafana:12.4.12": "10.0.0.20:8080/docker.io/grafana/grafana:12.4.12",
		},
	}
}

// testUpstreams is swallow_artifacts from the upstreams: the docker-ce and grafana deb sources the
// modules declare, their signing keys at keys, declared with fingerprint.
func testUpstreams(keys, fingerprint string) map[string]any {
	return map[string]any{
		"source": "upstream",
		"deb": []any{
			map[string]any{
				"name": "docker-ce", "uri": "https://download.docker.com/linux/ubuntu",
				"suites": []any{"noble"}, "components": []any{"stable"},
				"signing_key_url": keys + "/keys/docker-ce.gpg", "signing_key_fingerprint": fingerprint,
			},
			map[string]any{
				"name": "grafana", "uri": "https://apt.grafana.com/",
				"suites": []any{"stable"}, "components": []any{"main"},
				"signing_key_url": keys + "/keys/grafana.gpg", "signing_key_fingerprint": fingerprint,
			},
		},
		"deb_signing_key":  "",
		"registry":         "",
		"registry_mirrors": map[string]any{},
		"python_index":     "",
		"files": map[string]any{
			"helm/v3.22.1/helm-v3.22.1-linux-amd64.tar.gz": "https://get.helm.sh/helm-v3.22.1-linux-amd64.tar.gz",
		},
		"images": map[string]any{
			"docker.io/grafana/grafana:12.4.12": "docker.io/grafana/grafana:12.4.12",
		},
	}
}

// artifactSourcesVars are the role's variables for a scratch host at root, pulling with every
// container runtime the role configures.
func artifactSourcesVars(root string, artifacts map[string]any) map[string]any {
	return map[string]any{
		"artifact_sources_root":       root,
		"artifact_sources_containerd": true,
		"artifact_sources_docker":     true,
		"artifact_sources_podman":     true,
		"swallow_artifacts":           artifacts,
	}
}

// requireDebianHost skips a test of the apt path on a host that is not of the Debian family.
func requireDebianHost(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/etc/debian_version"); err != nil {
		t.Skip("the apt path runs on a Debian-family host only")
	}
}

// requireGPG skips a test that verifies upstream signing keys where gpg is not installed.
func requireGPG(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg is not available")
	}
}

// Host files a scratch host starts with: the distribution's deb822 sources, a sources.list with
// only comments, a GPU vendor's archive from the image, the apt proxy MAAS configures, and Docker
// settings of the user's.
const (
	hostUbuntuSources = "Types: deb\nURIs: http://archive.ubuntu.com/ubuntu/\nSuites: noble\nComponents: main\n"
	hostSourcesList   = "# Ubuntu sources have moved to /etc/apt/sources.list.d/ubuntu.sources\n\n"
	hostAmdgpuList    = "deb [arch=amd64 signed-by=/etc/apt/keyrings/rocm.gpg] https://repo.radeon.com/amdgpu/31.50/ubuntu noble main\n"
	hostAptProxy      = "Acquire::http::Proxy \"http://maas:8000/\";\n"
	hostDockerDaemon  = `{"log-driver": "journald", "insecure-registries": ["10.0.0.5:5000"]}` + "\n"
)

// writeHost writes the host files every scratch host starts with under root.
func writeHost(t *testing.T, root string) {
	t.Helper()
	writeScratch(t, root, "etc/apt/sources.list.d/ubuntu.sources", hostUbuntuSources)
	writeScratch(t, root, "etc/apt/sources.list", hostSourcesList)
	writeScratch(t, root, "etc/apt/sources.list.d/amdgpu.list", hostAmdgpuList)
	writeScratch(t, root, "etc/apt/apt.conf.d/90curtin-aptproxy", hostAptProxy)
	writeScratch(t, root, "etc/docker/daemon.json", hostDockerDaemon)
}

// readDockerDaemon parses daemon.json under root.
func readDockerDaemon(t *testing.T, root string) map[string]any {
	t.Helper()
	var daemon map[string]any
	if err := json.Unmarshal([]byte(readScratch(t, root, "etc/docker/daemon.json")), &daemon); err != nil {
		t.Errorf("daemon.json: %v", err)
	}
	return daemon
}

// assertAbsent reports each path under root that exists.
func assertAbsent(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, rel := range paths {
		if _, err := os.Stat(filepath.Join(root, rel)); !os.IsNotExist(err) {
			t.Errorf("%s exists (stat error %v), want it absent", rel, err)
		}
	}
}

// With the Local Repository, artifact_sources makes it the host's only package, Python, and image
// source: every other apt source with an active line, the distribution's or the image's, is set
// aside (recoverably), apt bypasses the host's proxy for it, k0s's containerd resolves each
// mirrored registry through it and forgets one it no longer mirrors, and Docker gains the registry
// (recorded as the role's) without losing its other settings. A second run changes nothing, and a
// run without the trusted variable, or with a source it does not know, stops before touching the
// host.
func TestArtifactSourcesPointsTheHostAtTheLocalRepository(t *testing.T) {
	t.Parallel()
	requireDebianHost(t)
	run := newRoleRun(t, artifactSourcesRole, false)
	root := filepath.Join(t.TempDir(), "root")
	writeHost(t, root)
	writeScratch(t, root, "etc/apt/sources.list.d/retired.list", "# deb http://mirror.example/ubuntu noble main\n")
	writeScratch(t, root, "etc/k0s/containerd.d/certs.d/ghcr.io/hosts.toml", "server = \"https://ghcr.io\"\n")
	vars := func() map[string]any { return artifactSourcesVars(root, testLocalRepository()) }

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
	assertAbsent(t, root, "etc/apt/sources.list.d/ubuntu.sources", "etc/apt/sources.list.d/amdgpu.list")
	if got := readScratch(t, root, "etc/apt/sources.list.d/ubuntu.sources.disabled-by-swallow"); got != hostUbuntuSources {
		t.Errorf("ubuntu.sources.disabled-by-swallow = %q, want the distribution's sources kept for recovery", got)
	}
	if got := readScratch(t, root, "etc/apt/sources.list"); got != hostSourcesList {
		t.Errorf("sources.list = %q, want a file with only comments left in place", got)
	}
	if got := readScratch(t, root, "etc/apt/sources.list.d/amdgpu.list.disabled-by-swallow"); got != hostAmdgpuList {
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
	assertAbsent(t, root, "etc/k0s/containerd.d/certs.d/ghcr.io")
	daemon := readDockerDaemon(t, root)
	if got := daemon["insecure-registries"]; strings.Join(stringsOf(got), ",") != "10.0.0.5:5000,10.0.0.20:8080" || daemon["log-driver"] != "journald" {
		t.Errorf("daemon.json = %v, want the registry added to insecure-registries and the other keys kept", daemon)
	}
	if got := readScratch(t, root, "var/lib/swallow/artifact-sources/docker-insecure-registry"); got != "10.0.0.20:8080\n" {
		t.Errorf("Docker registry record = %q, want the registry the role added", got)
	}
	if got := readScratch(t, root, "etc/containers/registries.conf.d/50-swallow-local-repository.conf"); !strings.Contains(got, "location = \"10.0.0.20:8080\"\ninsecure = true\n") {
		t.Errorf("Podman registries drop-in =\n%s\nwant the registry marked insecure", got)
	}

	if out, changed, failed := run.run(vars()); failed != 0 || changed != 0 {
		t.Errorf("second run changed %d and failed %d tasks, want a converged host unchanged:\n%s", changed, failed, out)
	}

	converged := snapshotScratch(t, root)
	missing := vars()
	delete(missing, "swallow_artifacts")
	unknown := vars()
	unknown["swallow_artifacts"].(map[string]any)["source"] = "mirror"
	for name, vars := range map[string]map[string]any{"without swallow_artifacts": missing, "with an unknown source": unknown} {
		if out, _, failed := run.run(vars); failed == 0 || !strings.Contains(out, "[artifact_sources_missing]") {
			t.Errorf("a run %s = %d failed tasks, want [artifact_sources_missing]:\n%s", name, failed, out)
		}
	}
	if diff := diffSnapshots(snapshotScratch(t, root), converged); len(diff) > 0 {
		t.Errorf("refused runs changed %v, want the host untouched", diff)
	}
}

// From the upstreams, artifact_sources adds the declared deb sources next to the host's own, each
// signed by its declared key, which it downloads and verifies; it leaves the host's apt sources,
// pip, and Docker settings as they are, and a declared source the host already configures (with
// its own key, as earlier Docker CE and Grafana installs left it) stays the host's, since apt
// refuses one URI with two keys. A second run changes nothing. A key that is not the declared one,
// or that gpg cannot read, stops the run before apt trusts it.
func TestArtifactSourcesAddsTheDeclaredUpstreams(t *testing.T) {
	t.Parallel()
	requireDebianHost(t)
	requireGPG(t)
	run := newRoleRun(t, artifactSourcesRole, false)
	keys := newKeyServer(t)
	root := filepath.Join(t.TempDir(), "root")
	writeHost(t, root)
	hostGrafanaList := "deb [signed-by=/etc/apt/keyrings/grafana.asc] https://apt.grafana.com stable main\n"
	writeScratch(t, root, "etc/apt/sources.list.d/grafana.list", hostGrafanaList)
	writeScratch(t, root, "etc/pip.conf", "[global]\ntimeout = 60\n")
	host := snapshotScratch(t, root)
	vars := func(artifacts map[string]any) map[string]any { return artifactSourcesVars(root, artifacts) }

	if out, _, failed := run.run(vars(testUpstreams(keys.url, testSigningKeyFingerprint))); failed != 0 {
		t.Fatalf("first run failed:\n%s", out)
	}
	wantSources := "\nTypes: deb\nURIs: https://download.docker.com/linux/ubuntu\nSuites: noble\n" +
		"Components: stable\nSigned-By: /etc/apt/keyrings/swallow-upstream-docker-ce.gpg\n"
	if got := readScratch(t, root, "etc/apt/sources.list.d/swallow-local-repository.sources"); !strings.HasSuffix(got, wantSources) ||
		strings.Contains(got, "apt.grafana.com") {
		t.Errorf("the role's apt sources =\n%s\nwant only the docker-ce stanza, ending in\n%s", got, wantSources)
	}
	if got := readScratch(t, root, "etc/apt/keyrings/swallow-upstream-docker-ce.gpg"); got != dearmoredTestSigningKey(t) {
		t.Errorf("docker-ce keyring = %q, want the verified key as a binary keyring", got)
	}
	assertAbsent(t, root, "etc/apt/keyrings/swallow-upstream-grafana.gpg", "etc/apt/keyrings/swallow-local-repository.asc",
		"etc/apt/apt.conf.d/90swallow-local-repository", "var/lib/swallow/artifact-sources/docker-insecure-registry")
	if got, want := keys.requested("/keys/grafana.gpg"), 0; got != want {
		t.Errorf("grafana key requests = %d, want %d: the host's own grafana.list serves that source", got, want)
	}
	after := snapshotScratch(t, root)
	for path, content := range host {
		if after[path] != content {
			t.Errorf("%s = %q, want the host's %q kept", path, after[path], content)
		}
	}

	if out, changed, failed := run.run(vars(testUpstreams(keys.url, testSigningKeyFingerprint))); failed != 0 || changed != 0 {
		t.Errorf("second run changed %d and failed %d tasks, want a converged host unchanged:\n%s", changed, failed, out)
	}

	cases := []struct {
		name      string
		artifacts map[string]any
		want      string
	}{
		{
			name:      "a key with another fingerprint",
			artifacts: testUpstreams(keys.url, "9DC858229FC7DD38854AE2D88D81803C0EBFCD88"),
			want:      "[artifact_signing_key_mismatch]",
		},
		{
			name: "a URL that serves no key",
			artifacts: func() map[string]any {
				artifacts := testUpstreams(keys.url, testSigningKeyFingerprint)
				artifacts["deb"].([]any)[0].(map[string]any)["signing_key_url"] = keys.url + "/keys/garbage.gpg"
				return artifacts
			}(),
			want: "[artifact_signing_key_unverifiable]",
		},
	}
	for _, tc := range cases {
		fresh := filepath.Join(t.TempDir(), "root")
		writeHost(t, fresh)
		if out, _, failed := run.run(artifactSourcesVars(fresh, tc.artifacts)); failed == 0 || !strings.Contains(out, tc.want) {
			t.Errorf("a run with %s = %d failed tasks, want %s:\n%s", tc.name, failed, tc.want, out)
		}
		assertAbsent(t, fresh, "etc/apt/keyrings/swallow-upstream-docker-ce.gpg", "etc/apt/sources.list.d/swallow-local-repository.sources")
	}
}

// A Site switches between its Local Repository and the upstreams, and every host follows on its
// next run: from the upstreams the run restores the apt sources it set aside and removes only what
// it wrote for the Local Repository (the Site's key, the proxy bypass, its pip.conf, the containerd
// drop-in and hosts files, its Docker registry, the Podman drop-in); back on the Local Repository
// the host is exactly as the first run left it. Each mode is idempotent.
func TestArtifactSourcesSwitchesModesBothWays(t *testing.T) {
	t.Parallel()
	requireDebianHost(t)
	requireGPG(t)
	run := newRoleRun(t, artifactSourcesRole, false)
	keys := newKeyServer(t)
	root := filepath.Join(t.TempDir(), "root")
	writeHost(t, root)
	host := snapshotScratch(t, root)
	local := artifactSourcesVars(root, testLocalRepository())
	upstream := artifactSourcesVars(root, testUpstreams(keys.url, testSigningKeyFingerprint))

	if out, _, failed := run.run(local); failed != 0 {
		t.Fatalf("run with the Local Repository failed:\n%s", out)
	}
	fromLocal := snapshotScratch(t, root)

	if out, _, failed := run.run(upstream); failed != 0 {
		t.Fatalf("run from the upstreams failed:\n%s", out)
	}
	after := snapshotScratch(t, root)
	for _, path := range []string{"etc/apt/sources.list.d/ubuntu.sources", "etc/apt/sources.list.d/amdgpu.list", "etc/apt/sources.list"} {
		if after[path] != host[path] {
			t.Errorf("%s = %q, want the host's %q restored", path, after[path], host[path])
		}
	}
	for path := range after {
		if strings.HasSuffix(path, ".disabled-by-swallow") {
			t.Errorf("%s remains, want every source the role set aside restored", path)
		}
	}
	assertAbsent(t, root,
		"etc/apt/keyrings/swallow-local-repository.asc",
		"etc/apt/apt.conf.d/90swallow-local-repository",
		"etc/pip.conf",
		"etc/k0s/containerd.d/swallow-registry-mirrors.toml",
		"etc/k0s/containerd.d/certs.d",
		"etc/containers/registries.conf.d/50-swallow-local-repository.conf",
		"var/lib/swallow/artifact-sources/docker-insecure-registry",
	)
	daemon := readDockerDaemon(t, root)
	if got := daemon["insecure-registries"]; strings.Join(stringsOf(got), ",") != "10.0.0.5:5000" || daemon["log-driver"] != "journald" {
		t.Errorf("daemon.json = %v, want only the role's registry removed", daemon)
	}
	sources := readScratch(t, root, "etc/apt/sources.list.d/swallow-local-repository.sources")
	for _, name := range []string{"docker-ce", "grafana"} {
		if !strings.Contains(sources, "Signed-By: /etc/apt/keyrings/swallow-upstream-"+name+".gpg\n") {
			t.Errorf("the role's apt sources =\n%s\nwant a %s stanza signed by its own key", sources, name)
		}
		if got := after["etc/apt/keyrings/swallow-upstream-"+name+".gpg"]; got != dearmoredTestSigningKey(t) {
			t.Errorf("%s keyring = %q, want the verified key", name, got)
		}
	}
	if strings.Contains(sources, "10.0.0.20") {
		t.Errorf("the role's apt sources =\n%s\nwant no Local Repository source left", sources)
	}
	if out, changed, failed := run.run(upstream); failed != 0 || changed != 0 {
		t.Errorf("second run from the upstreams changed %d and failed %d tasks, want nothing:\n%s", changed, failed, out)
	}

	if out, _, failed := run.run(local); failed != 0 {
		t.Fatalf("run back on the Local Repository failed:\n%s", out)
	}
	if diff := diffSnapshots(snapshotScratch(t, root), fromLocal); len(diff) > 0 {
		t.Errorf("back on the Local Repository, %v differ from the first run's host, want none", diff)
	}
	if out, changed, failed := run.run(local); failed != 0 || changed != 0 {
		t.Errorf("second run back on the Local Repository changed %d and failed %d tasks, want nothing:\n%s", changed, failed, out)
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
