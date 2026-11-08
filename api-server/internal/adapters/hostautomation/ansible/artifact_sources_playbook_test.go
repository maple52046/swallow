package ansible

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// artifactSourcesRole is the shared role that points a host's apt, pip, and container runtimes at
// where its run's artifacts come from (the Site's Local Repository or the declared upstreams);
// every playbook marked fetchesArtifacts runs it first.
const artifactSourcesRole = "artifact_sources"

// artifactsEntry is a manifest entry with the flag that makes swallow hand the playbook the trusted
// variable swallow_artifacts, and refuse the run while the Site serves deployments from a Local
// Repository that is not ready. The tests read the flag from the shipped JSON so they hold whatever
// Go type the catalog gives it.
type artifactsEntry struct {
	Name             string `json:"name"`
	Path             string `json:"path"`
	FetchesArtifacts bool   `json:"fetchesArtifacts"`
}

// readArtifactsEntries parses the shipped manifest's entries, aliases included.
func readArtifactsEntries(t *testing.T) []artifactsEntry {
	t.Helper()
	raw, err := os.ReadFile(shippedManifest)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Playbooks []artifactsEntry `json:"playbooks"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest.Playbooks
}

// fetchModules are the task modules that download onto the host.
var fetchModules = map[string]bool{
	"get_url": true, "ansible.builtin.get_url": true,
	"pip": true, "ansible.builtin.pip": true,
}

// fetchCommand matches a command line that downloads: pip installing, or a container runtime
// pulling or starting an image.
var fetchCommand = regexp.MustCompile(
	`(^|[/\s])pip3?\s+install\b|\b(docker|podman)\s+(pull|run)\b|\bdocker\s+compose\b.*\bup\b`)

// isFetchTask reports whether a task takes something from outside the host: a package task (it
// installs or refreshes the index), a download, an archive unpacked from a URL, or a command that
// installs Python packages or pulls an image.
func isFetchTask(task map[string]any) bool {
	if isPackageTask(task) {
		return true
	}
	for key, value := range task {
		if fetchModules[key] {
			return true
		}
		if key == "unarchive" || key == "ansible.builtin.unarchive" {
			args, _ := value.(map[string]any)
			if src, _ := args["src"].(string); strings.Contains(src, "://") {
				return true
			}
		}
	}
	for _, key := range commandModules {
		if value, ok := task[key]; ok && fetchCommand.MatchString(commandLine(value)) {
			return true
		}
	}
	return false
}

// includedRole returns the role a task includes or imports, or "" for any other task.
func includedRole(task map[string]any) string {
	for _, key := range roleTaskKeys {
		switch value := task[key].(type) {
		case string:
			return value
		case map[string]any:
			name, _ := value["name"].(string)
			return name
		}
	}
	return ""
}

// fetchWalk follows a playbook in execution order (per play: pre_tasks, roles, tasks, post_tasks)
// and records each unit that fetches, and where the artifact sources role runs among them.
type fetchWalk struct {
	t      *testing.T
	search []string        // the run's roles path (rolesPath)
	cache  map[string]bool // role name: whether it, or a role it pulls in, fetches
	// fetches are the units that fetch, in execution order; artifact_sources is not one of them.
	fetches []string
	// pointedAt is how many fetches ran before the first artifact_sources, or -1 while it has not
	// run; pointedHosts is the hosts: of the play that ran it.
	pointedAt    int
	pointedHosts any
}

// walkPlaybookFetches walks the shipped playbook at path (relative to modulesRoot), resolving roles
// in the roles path its run gets. Handlers are not walked: they run after the tasks that notify them.
func walkPlaybookFetches(t *testing.T, path string) *fetchWalk {
	t.Helper()
	rolesSearch, err := rolesPath(modulesRoot, path)
	if err != nil {
		t.Fatal(err)
	}
	walk := &fetchWalk{t: t, search: filepath.SplitList(rolesSearch), cache: map[string]bool{}, pointedAt: -1}
	var plays []map[string]any
	readYAML(t, filepath.Join(modulesRoot, path), &plays)
	for _, play := range plays {
		if tasks, ok := play["pre_tasks"].([]any); ok {
			walk.tasks(tasks, play["hosts"])
		}
		if entries, ok := play["roles"].([]any); ok {
			for _, entry := range entries {
				walk.role(roleEntryName(entry), play["hosts"])
			}
		}
		for _, key := range []string{"tasks", "post_tasks"} {
			if tasks, ok := play[key].([]any); ok {
				walk.tasks(tasks, play["hosts"])
			}
		}
	}
	return walk
}

// role records a role a play lists or a task includes: artifact_sources marks where the host is
// pointed at its artifact sources, and any other role is a fetch when it, or a role it pulls in, is.
func (w *fetchWalk) role(name string, hosts any) {
	if name == artifactSourcesRole {
		if w.pointedAt < 0 {
			w.pointedAt, w.pointedHosts = len(w.fetches), hosts
		}
		return
	}
	if w.roleFetches(name, map[string]bool{}) {
		w.fetches = append(w.fetches, "role "+name)
	}
}

// tasks records a play's task list in order, descending into blocks.
func (w *fetchWalk) tasks(items []any, hosts any) {
	for _, item := range items {
		task, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name := includedRole(task); name != "" {
			w.role(name, hosts)
			continue
		}
		for _, key := range []string{"block", "rescue", "always"} {
			if nested, ok := task[key].([]any); ok {
				w.tasks(nested, hosts)
			}
		}
		if isFetchTask(task) {
			w.fetches = append(w.fetches, fmt.Sprintf("task %q", task["name"]))
		}
	}
}

// roleFetches reports whether the named role, or a role it pulls in, has a fetching task. A role
// the roles path cannot resolve is left to TestShippedPlaybooksResolveRolesWithinTheirModule.
func (w *fetchWalk) roleFetches(name string, visiting map[string]bool) bool {
	if fetches, ok := w.cache[name]; ok {
		return fetches
	}
	if visiting[name] {
		return false
	}
	visiting[name] = true
	var dir string
	for _, candidate := range w.search {
		if info, err := os.Stat(filepath.Join(candidate, name)); err == nil && info.IsDir() {
			dir = filepath.Join(candidate, name)
			break
		}
	}
	if dir == "" {
		return false
	}
	fetches := false
	for _, file := range roleTaskFiles(w.t, dir) {
		var tasks []any
		readYAML(w.t, file, &tasks)
		fetches = fetches || tasksFetch(tasks)
	}
	for nested := range nestedRoles(w.t, dir) {
		fetches = fetches || (nested != artifactSourcesRole && w.roleFetches(nested, visiting))
	}
	w.cache[name] = fetches
	return fetches
}

// tasksFetch reports whether a task list, blocks included, has a fetching task.
func tasksFetch(items []any) bool {
	for _, item := range items {
		task, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if isFetchTask(task) {
			return true
		}
		for _, key := range []string{"block", "rescue", "always"} {
			if nested, ok := task[key].([]any); ok && tasksFetch(nested) {
				return true
			}
		}
	}
	return false
}

// A playbook that fetches artifacts points every target at their sources before it installs,
// downloads, or pulls anything: otherwise that first fetch still goes where the host pointed before
// (the distribution's apt sources while the Site serves from its Local Repository, a registry, PyPI)
// or finds no package at all.
func TestArtifactPlaybooksPointAtTheirSourcesBeforeFetching(t *testing.T) {
	flagged := 0
	for _, entry := range readArtifactsEntries(t) {
		if !entry.FetchesArtifacts {
			continue
		}
		flagged++
		t.Run(entry.Name, func(t *testing.T) {
			walk := walkPlaybookFetches(t, entry.Path)
			switch {
			case walk.pointedAt < 0:
				t.Errorf("%s fetches artifacts but never runs %s", entry.Path, artifactSourcesRole)
			case walk.pointedAt > 0:
				t.Errorf("%s runs %v before %s", entry.Path, walk.fetches[:walk.pointedAt], artifactSourcesRole)
			case walk.pointedHosts != "all":
				t.Errorf("%s runs %s on hosts %v, want all so every target is pointed at its artifact sources",
					entry.Path, artifactSourcesRole, walk.pointedHosts)
			}
		})
	}
	if flagged == 0 {
		t.Fatal("no manifest playbook is marked fetchesArtifacts; is the flag's JSON name right?")
	}
}

// fetchesArtifactsNotYet are the manifest playbooks that fetch but are not marked fetchesArtifacts
// yet, with the reason. Removing the reason is the only way to remove an entry, so the list only
// shrinks deliberately.
var fetchesArtifactsNotYet = map[string]string{
	"deploy-prometheus": "the Prometheus Software module declares no artifacts yet; " +
		"its native method still downloads the release from GitHub",
}

// Every playbook that installs, downloads, or pulls is marked fetchesArtifacts, aliases included
// (a Workflow accepted under an old name retries under it), so a new playbook cannot quietly fetch
// something swallow does not declare. An uninstall playbook only removes what an install put there;
// roles it shares with the install path make its static content look like a fetch, so it is not
// checked.
func TestFetchingPlaybooksAreMarkedFetchesArtifacts(t *testing.T) {
	names := map[string]bool{}
	for _, entry := range readArtifactsEntries(t) {
		names[entry.Name] = true
		if strings.HasPrefix(entry.Name, "uninstall-") {
			continue
		}
		fetches := walkPlaybookFetches(t, entry.Path).fetches
		if reason, ok := fetchesArtifactsNotYet[entry.Name]; ok {
			if entry.FetchesArtifacts {
				t.Errorf("%s is marked fetchesArtifacts; drop it from fetchesArtifactsNotYet (%s)", entry.Name, reason)
			}
			continue
		}
		if len(fetches) > 0 && !entry.FetchesArtifacts {
			t.Errorf("%s (%s) fetches (%v) but is not marked fetchesArtifacts", entry.Name, entry.Path, fetches)
		}
		if len(fetches) == 0 && entry.FetchesArtifacts {
			t.Errorf("%s (%s) is marked fetchesArtifacts but fetches nothing", entry.Name, entry.Path)
		}
	}
	for name := range fetchesArtifactsNotYet {
		if !names[name] {
			t.Errorf("fetchesArtifactsNotYet names %s, which the manifest no longer has", name)
		}
	}
}

// grafanaDefaultVersion reads the release the grafana role installs when the run names none.
var grafanaDefaultVersion = regexp.MustCompile(`default\('([^']+)'`)

// The playbooks that pull images say which container runtime artifact_sources configures: with the
// Local Repository, k0s's containerd must resolve every image through its mirrors before k0s
// starts, and a Docker or Podman must accept its plain-HTTP registry. An image a Docker or Podman
// runs is the declared reference as the run's artifact sources map it.
func TestArtifactPlaybooksConfigureTheirContainerRuntime(t *testing.T) {
	cases := []struct {
		playbook string
		required string
	}{
		{playbook: "deploy-kubernetes.yml", required: "artifact_sources_containerd: true"},
		{playbook: "configure-kubernetes-grafana.yml", required: "artifact_sources_containerd: true"},
		{playbook: "configure-amd-gpu-operator.yml", required: "artifact_sources_containerd: true"},
		{playbook: "deploy-docker-ce.yml", required: "artifact_sources_docker: true"},
		{playbook: "deploy-podman.yml", required: "artifact_sources_podman: true"},
		{playbook: "deploy-grafana.yml", required: "artifact_sources_docker: \"{{ grafana_container_runtime_used | trim == 'docker-ce' }}\""},
		{playbook: "deploy-grafana.yml", required: "artifact_sources_podman: \"{{ grafana_container_runtime_used | trim == 'podman' }}\""},
	}
	for _, tc := range cases {
		raw, err := os.ReadFile(shippedFile(t, tc.playbook))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), tc.required) {
			t.Errorf("%s does not pass %q to %s", tc.playbook, tc.required, artifactSourcesRole)
		}
	}
	// The k0s units start in the bootstrap and join roles, so the mirrors must be written before them.
	raw, err := os.ReadFile(shippedFile(t, "deploy-kubernetes.yml"))
	if err != nil {
		t.Fatal(err)
	}
	pointed := strings.Index(string(raw), "role: "+artifactSourcesRole)
	for _, starts := range []string{"role: k0s_controller_bootstrap", "role: k0s_controller_join", "role: k0s_worker_join"} {
		if at := strings.Index(string(raw), starts); pointed < 0 || at < 0 || at < pointed {
			t.Errorf("deploy-kubernetes.yml must run %s before %s", artifactSourcesRole, starts)
		}
	}

	defaults := readRoleDefaults(t, "grafana")
	if got := fmt.Sprint(defaults["grafana_image"]); !strings.HasPrefix(got, "{{ swallow_artifacts.images[grafana_image_reference]") {
		t.Errorf("grafana_image = %s, want the reference to pull from swallow_artifacts.images[grafana_image_reference]", got)
	}
	version := grafanaDefaultVersion.FindStringSubmatch(fmt.Sprint(defaults["grafana_version"]))
	if version == nil {
		t.Fatalf("grafana_version = %v, want a default release", defaults["grafana_version"])
	}
	reference := strings.ReplaceAll(fmt.Sprint(defaults["grafana_image_reference"]), "{{ grafana_version }}", version[1])
	if declared := readDeclaredImages(t, "grafana"); !declared[reference] {
		t.Errorf("the grafana role runs %s by default, which the grafana module does not declare (%v)", reference, declared)
	}
}

// readDeclaredImages reads the image artifacts the module id declares in its module.json.
func readDeclaredImages(t *testing.T, id string) map[string]bool {
	t.Helper()
	for _, module := range moduleDirs(t) {
		if filepath.Base(module) != id {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(module, "module.json"))
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Artifacts struct {
				Images []string `json:"images"`
			} `json:"artifacts"`
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatalf("%s: %v", filepath.Join(module, "module.json"), err)
		}
		images := map[string]bool{}
		for _, image := range manifest.Artifacts.Images {
			images[image] = true
		}
		return images
	}
	t.Fatalf("no module %s", id)
	return nil
}

// contentURL matches the literal part of an http(s) URL in shipped content, up to the first
// template expression; the group is the host and port, so a templated host reads as "{{".
var contentURL = regexp.MustCompile(`https?://([^\s"'<>/]*)[^\s"'<>{}]*`)

// internetURLAllowlist are the literal internet URLs the shipped roles and playbooks may still
// write, keyed by the file as the bundle addresses it (shippedFile), with the reason each remains.
var internetURLAllowlist = map[string]map[string]string{
	filepath.Join("roles", "docker_ce", "tasks", "main.yml"): {
		"https://download.docker.com/linux/centos/$releasever/$basearch/stable": "RHEL family: Docker CE's yum repository until the Local Repository serves rpm",
		"https://download.docker.com/linux/centos/gpg":                          "RHEL family: the key of Docker CE's yum repository",
	},
	filepath.Join("roles", "grafana", "defaults", "main.yml"): {
		"https://rpm.grafana.com": "RHEL family: Grafana's yum repository (native method) until the Local Repository serves rpm",
	},
	filepath.Join("roles", "prometheus", "defaults", "main.yml"): {
		"https://github.com/prometheus/prometheus/releases/download/v": fetchesArtifactsNotYet["deploy-prometheus"],
	},
	filepath.Join("roles", "omnistat", "templates", "omnistat.service.j2"): {
		"https://rocm.github.io/omnistat/": "the unit's Documentation= link, which nothing fetches",
	},
}

// downloadNotYet are the roles whose get_url tasks may still download from elsewhere, with the reason.
var downloadNotYet = map[string]string{"prometheus": fetchesArtifactsNotYet["deploy-prometheus"]}

// declaredFileURL is how a role downloads a declared file: by its declared path in
// swallow_artifacts.files, which maps it to the Local Repository or the file's upstream URL.
const declaredFileURL = "{{ swallow_artifacts.files["

// signingKeyURL is the one download artifact_sources makes: the signing key a module declares for
// its upstream deb source, verified against the declared fingerprint before apt trusts it.
const signingKeyURL = "{{ item.signing_key_url }}"

// unscannedContent reports whether a shipped file is data a role copies or applies as it is
// (archives, dashboards and other JSON, patches, licenses) rather than content that runs or renders.
func unscannedContent(path string) bool {
	switch filepath.Ext(path) {
	case ".tgz", ".gz", ".json", ".patch", ".deb":
		return true
	}
	return filepath.Base(path) == "LICENSE"
}

// bundlePath names a shipped file the way shippedFile addresses it: a playbook by its file name,
// a role file as roles/<role>/<file>.
func bundlePath(module, path string) string {
	rel, _ := filepath.Rel(module, path)
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if parts[0] == "playbooks" {
		return filepath.Join(parts[1:]...)
	}
	return filepath.FromSlash(rel)
}

// A host fetches only what swallow declares, from where its run's artifact sources say: no shipped
// role or playbook writes a literal internet URL outside a comment, and every download takes its
// URL from swallow_artifacts.files (artifact_sources downloads only the declared signing keys). The
// allowlist names what remains and why; an entry that no longer matches fails too, so it is
// removed when its URL goes.
func TestShippedContentFetchesOnlyDeclaredArtifacts(t *testing.T) {
	used := map[string]bool{}
	for _, module := range moduleDirs(t) {
		for _, sub := range []string{"playbooks", "roles"} {
			root := filepath.Join(module, sub)
			if _, err := os.Stat(root); os.IsNotExist(err) {
				continue
			}
			err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
				if err != nil || entry.IsDir() || unscannedContent(path) {
					return err
				}
				raw, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				file := bundlePath(module, path)
				for number, line := range strings.Split(string(raw), "\n") {
					trimmed := strings.TrimSpace(line)
					if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "{#") || strings.HasPrefix(trimmed, "//") {
						continue
					}
					for _, match := range contentURL.FindAllStringSubmatch(line, -1) {
						host := strings.Split(match[1], ":")[0]
						if host == "" || strings.HasPrefix(host, "{{") || host == "127.0.0.1" || host == "localhost" {
							continue
						}
						if reason := internetURLAllowlist[file][match[0]]; reason != "" {
							used[file+" "+match[0]] = true
							continue
						}
						t.Errorf("%s:%d writes the internet URL %s; declare the artifact and take it from swallow_artifacts instead",
							file, number+1, match[0])
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	for file, urls := range internetURLAllowlist {
		for url := range urls {
			if !used[file+" "+url] {
				t.Errorf("internetURLAllowlist keeps %s in %s, which the content no longer writes", url, file)
			}
		}
	}

	for _, module := range moduleDirs(t) {
		roles, err := filepath.Glob(filepath.Join(module, "roles", "*"))
		if err != nil {
			t.Fatal(err)
		}
		for _, dir := range roles {
			for _, file := range roleTaskFiles(t, dir) {
				var tasks []any
				readYAML(t, file, &tasks)
				for _, url := range downloadURLs(tasks) {
					switch role := filepath.Base(dir); {
					case role == artifactSourcesRole:
						if url != signingKeyURL {
							t.Errorf("%s downloads %s; %s downloads only %s", bundlePath(module, file), url, role, signingKeyURL)
						}
					case !strings.HasPrefix(url, declaredFileURL) && downloadNotYet[role] == "":
						t.Errorf("%s downloads %s; take it from %s<declared path>] }}", bundlePath(module, file), url, declaredFileURL)
					}
				}
			}
		}
	}
}

// downloadURLs returns the url of every get_url task in a task list, blocks included.
func downloadURLs(items []any) []string {
	var urls []string
	for _, item := range items {
		task, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"get_url", "ansible.builtin.get_url"} {
			if args, ok := task[key].(map[string]any); ok {
				urls = append(urls, fmt.Sprint(args["url"]))
			}
		}
		for _, key := range []string{"block", "rescue", "always"} {
			if nested, ok := task[key].([]any); ok {
				urls = append(urls, downloadURLs(nested)...)
			}
		}
	}
	return urls
}

// pinnedFiles collects every mapping with a string path and sha256 in a role's defaults: a file the
// role downloads through swallow_artifacts.files and verifies.
func pinnedFiles(value any, into map[string]string) {
	switch node := value.(type) {
	case map[string]any:
		path, hasPath := node["path"].(string)
		sum, hasSum := node["sha256"].(string)
		if hasPath && hasSum {
			into[path] = sum
		}
		for _, child := range node {
			pinnedFiles(child, into)
		}
	case []any:
		for _, child := range node {
			pinnedFiles(child, into)
		}
	}
}

// declaredFile is a file artifact a module declares in its module.json: a run fetches it by path,
// from the Local Repository or its upstream URL, and verifies it against sha256.
type declaredFile struct {
	module string
	sha256 string
}

// readDeclaredFiles reads every module.json's file artifacts, by path; modules may declare the same
// path (the registry requires the same URL and checksum), so a path can have several declarations.
func readDeclaredFiles(t *testing.T) map[string][]declaredFile {
	t.Helper()
	declared := map[string][]declaredFile{}
	for _, module := range moduleDirs(t) {
		raw, err := os.ReadFile(filepath.Join(module, "module.json"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Artifacts struct {
				Files []struct {
					Path   string `json:"path"`
					SHA256 string `json:"sha256"`
				} `json:"files"`
			} `json:"artifacts"`
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatalf("%s: %v", filepath.Join(module, "module.json"), err)
		}
		for _, file := range manifest.Artifacts.Files {
			declared[file.Path] = append(declared[file.Path], declaredFile{module: filepath.Base(module), sha256: file.SHA256})
		}
	}
	return declared
}

// A run fetches only the files modules declare, so every file a role pins (path and SHA-256 in its
// defaults) is a module's file artifact with the same checksum: a role and its declaration cannot
// drift apart, and a version bump updates both.
func TestPinnedFilesAreDeclaredModuleArtifacts(t *testing.T) {
	declared := readDeclaredFiles(t)
	found := map[string]bool{}
	for _, module := range moduleDirs(t) {
		roles, err := filepath.Glob(filepath.Join(module, "roles", "*", "defaults", "main.yml"))
		if err != nil {
			t.Fatal(err)
		}
		for _, defaultsFile := range roles {
			role := filepath.Base(filepath.Dir(filepath.Dir(defaultsFile)))
			var defaults map[string]any
			readYAML(t, defaultsFile, &defaults)
			pins := map[string]string{}
			pinnedFiles(defaults, pins)
			paths := make([]string, 0, len(pins))
			for path := range pins {
				paths = append(paths, path)
			}
			sort.Strings(paths)
			for _, path := range paths {
				found[role] = true
				declarations := declared[path]
				if len(declarations) == 0 {
					t.Errorf("role %s pins %s, which no module.json declares as a file artifact", role, path)
				}
				for _, declaration := range declarations {
					if declaration.sha256 != pins[path] {
						t.Errorf("module %s declares %s with sha256 %s, but role %s pins %s",
							declaration.module, path, declaration.sha256, role, pins[path])
					}
				}
			}
		}
	}
	for _, role := range []string{"k0s_binary", "helm", "device_metrics_exporter", "omnistat", "victoriametrics"} {
		if !found[role] {
			t.Errorf("role %s pins no file (path and sha256 in its defaults); the declaration check would miss it", role)
		}
	}
}
