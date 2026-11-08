package ansible

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
)

// countersModule is the module file every extension asset carries.
const countersModule = "rocprofiler_sdk_extension.cpython-312-x86_64-linux-gnu.so"

// countersAssetPath is the declared path the tests serve an extension asset under.
const countersAssetPath = "omnistat-rocprof/test/omnistat-rocprof-test-rocm10.0-cp312-x86_64.tar.gz"

// countersReport matches the line the test play prints after the decision: whether the exporter
// collects the counters and the node's result as JSON.
var countersReport = regexp.MustCompile(`"msg": "counters (True|False) (.*)"`)

// countersResult is a node's hardware counters result as the role records it.
type countersResult struct {
	ServerID string `json:"serverId"`
	State    string `json:"state"`
	Reason   string `json:"reason"`
}

// countersRun prepares a play that runs only the role's hardware counters decision, which needs no
// root, followed by a task that prints its outcome.
func countersRun(t *testing.T) *roleRun {
	t.Helper()
	run := newRoleRun(t, "omnistat", false)
	writeScratch(t, run.dir, "play.yml", "- hosts: all\n  gather_facts: false\n  tasks:\n"+
		"    - name: Decide the hardware counters\n      ansible.builtin.include_role:\n        name: omnistat\n        tasks_from: hardware_counters\n"+
		"    - name: Report the decision\n      ansible.builtin.debug:\n"+
		"        msg: \"counters {{ omnistat_rocprofiler_enabled }} {{ omnistat_hardware_counters_result | to_json }}\"\n")
	return run
}

// countersOutcome returns whether a run collects the counters and its node result (empty when the
// deployment asked for none).
func countersOutcome(t *testing.T, out string) (bool, countersResult) {
	t.Helper()
	match := countersReport.FindStringSubmatch(out)
	if match == nil {
		t.Fatalf("the run reported no hardware counters decision:\n%s", out)
	}
	var result countersResult
	raw := strings.ReplaceAll(match[2], `\"`, `"`)
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode the node result %q: %v", raw, err)
	}
	return match[1] == "True", result
}

// countersNode is a scratch node for the counters decision: a ROCm installation whose
// librocprofiler-sdk.so.1 resolves into core-<version>, an Omnistat environment's site-packages,
// an image's extension root, and a fake probe whose exit status is the content of probe-exit.
type countersNode struct {
	root, rocm, sitePackages, prebaked, probeExit string
}

// newCountersNode lays out a node whose ROCm reports rocmVersion; an empty version leaves no ROCm.
func newCountersNode(t *testing.T, rocmVersion string) countersNode {
	t.Helper()
	root := filepath.Join(t.TempDir(), "root")
	node := countersNode{
		root: root, rocm: filepath.Join(root, "opt", "rocm"),
		sitePackages: filepath.Join(root, "venv", "lib", "python3.12", "site-packages"),
		prebaked:     filepath.Join(root, "opt", "swallow", "omnistat-rocprof"),
		probeExit:    filepath.Join(root, "probe-exit"),
	}
	if rocmVersion != "" {
		core := filepath.Join(node.rocm, "core-"+rocmVersion[:strings.LastIndex(rocmVersion, ".")])
		writeScratch(t, core, "lib/librocprofiler-sdk.so.1.0.0", "library")
		writeScratch(t, core, ".info/version", rocmVersion+"\n")
		if err := os.MkdirAll(filepath.Join(node.rocm, "lib"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(core, "lib", "librocprofiler-sdk.so.1.0.0"), filepath.Join(node.rocm, "lib", "librocprofiler-sdk.so.1")); err != nil {
			t.Fatal(err)
		}
	}
	writeScratch(t, node.sitePackages, "omnistat/__init__.py", "")
	// The version directory converge.yml creates before the counters are decided.
	if err := os.MkdirAll(filepath.Join(root, "opt", "omnistat", convergeVersion), 0o755); err != nil {
		t.Fatal(err)
	}
	writeScratch(t, root, "probe-exit", "0\n")
	// The fake probe passes only when the extension is in the omnistat package, as the real one
	// only loads it from there.
	writeExecutable(t, filepath.Join(root, "probe"), "#!/bin/sh\n"+
		"[ -f '"+filepath.Join(node.sitePackages, "omnistat", countersModule)+"' ] || exit 3\n"+
		"exit \"$(cat '"+node.probeExit+"')\"\n")
	return node
}

// countersAsset returns an extension asset whose build_info.json names rocmVersion.
func countersAsset(t *testing.T, rocmVersion string) []byte {
	t.Helper()
	return tarGz(t, map[string]string{
		countersModule:    "module built for " + rocmVersion,
		"build_info.json": "{\n  \"rocm_path\": \"/opt/rocm/core\",\n  \"rocm_version\": \"" + rocmVersion + "\"\n}",
	})
}

// countersSource serves asset at countersAssetPath and counts the requests for it; a nil asset
// answers 404.
type countersSource struct {
	url      string
	asset    []byte
	requests atomic.Int32
}

func newCountersSource(t *testing.T, asset []byte) *countersSource {
	t.Helper()
	source := &countersSource{asset: asset}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files/"+countersAssetPath || source.asset == nil {
			http.NotFound(w, r)
			return
		}
		source.requests.Add(1)
		_, _ = w.Write(source.asset)
	}))
	t.Cleanup(server.Close)
	source.url = server.URL
	return source
}

// countersVars are the role's variables for node: the counters asked for on a GPU node, the
// declared extension for ROCm 10.0 (asset's checksum, builder glibc builderGlibc), and the fake
// probe run as the test's own user.
func countersVars(node countersNode, source *countersSource, asset []byte, builderGlibc string) map[string]any {
	sum := sha256.Sum256(asset)
	return map[string]any{
		"swallow_omnistat_hardware_counters": true,
		"swallow_slurm_gpu_compute_ids":      []string{"localhost"},
		"omnistat_home":                      filepath.Join(node.root, "opt", "omnistat"),
		"omnistat_rocm_path":                 node.rocm,
		"omnistat_probe_report":              map[string]any{"site_packages": node.sitePackages, "problems": []any{}},
		"omnistat_rocprof_extensions": map[string]any{convergeVersion: map[string]any{"10.0": map[string]any{
			"path": countersAssetPath, "sha256": hex.EncodeToString(sum[:]), "rocprofiler_sdk": "1.3.5", "builder_glibc": builderGlibc,
		}}},
		"omnistat_rocprof_prebaked_root":    node.prebaked,
		"omnistat_rocprof_download_retries": 0,
		"omnistat_counter_probe":            []string{filepath.Join(node.root, "probe")},
		"omnistat_counter_probe_runner":     []string{},
		"swallow_artifacts":                 map[string]any{"files": map[string]any{countersAssetPath: source.url + "/files/" + countersAssetPath}},
	}
}

// A node collects the counters only when every check passes, and a node that cannot reports why
// and leaves them off, without failing the run: a deployment that asks for none records nothing,
// a node without GPUs, without a ROCm minor swallow builds for (or without ROCm), with a glibc older
// than the builder's, whose source cannot serve the extension or serves one for another ROCm, or
// whose probe fails is unavailable with that reason.
func TestOmnistatHardwareCountersDegradePerNode(t *testing.T) {
	t.Parallel()
	asset := countersAsset(t, "10.0.0")
	cases := []struct {
		name        string
		rocm        string
		setup       func(vars map[string]any, node countersNode, source *countersSource)
		wantResult  countersResult
		wantFetches int32
	}{
		{
			name: "counters not asked for",
			rocm: "10.0.0",
			setup: func(vars map[string]any, _ countersNode, _ *countersSource) {
				vars["swallow_omnistat_hardware_counters"] = false
			},
			wantResult: countersResult{},
		},
		{
			name: "a node without GPUs",
			rocm: "10.0.0",
			setup: func(vars map[string]any, _ countersNode, _ *countersSource) {
				vars["swallow_slurm_gpu_compute_ids"] = []string{}
			},
			wantResult: countersResult{ServerID: "localhost", State: "unavailable", Reason: "no_gpu"},
		},
		{
			name:       "a ROCm minor swallow builds no extension for",
			rocm:       "10.9.0",
			wantResult: countersResult{ServerID: "localhost", State: "unavailable", Reason: "rocm_unsupported"},
		},
		{
			name:       "a node without ROCm",
			wantResult: countersResult{ServerID: "localhost", State: "unavailable", Reason: "rocm_unsupported"},
		},
		{
			name: "a glibc older than the builder's",
			rocm: "10.0.0",
			setup: func(vars map[string]any, _ countersNode, _ *countersSource) {
				extension := vars["omnistat_rocprof_extensions"].(map[string]any)[convergeVersion].(map[string]any)["10.0"].(map[string]any)
				extension["builder_glibc"] = "99.0"
			},
			wantResult: countersResult{ServerID: "localhost", State: "unavailable", Reason: "glibc_too_old"},
		},
		{
			name:       "a source that cannot serve the extension",
			rocm:       "10.0.0",
			setup:      func(_ map[string]any, _ countersNode, source *countersSource) { source.asset = nil },
			wantResult: countersResult{ServerID: "localhost", State: "unavailable", Reason: "artifact_unavailable"},
		},
		{
			name: "an extension built for another ROCm minor",
			rocm: "10.0.0",
			setup: func(vars map[string]any, _ countersNode, source *countersSource) {
				source.asset = countersAsset(t, "10.1.0")
				sum := sha256.Sum256(source.asset)
				extension := vars["omnistat_rocprof_extensions"].(map[string]any)[convergeVersion].(map[string]any)["10.0"].(map[string]any)
				extension["sha256"] = hex.EncodeToString(sum[:])
			},
			wantResult:  countersResult{ServerID: "localhost", State: "unavailable", Reason: "artifact_unavailable"},
			wantFetches: 1,
		},
		{
			name: "a probe that fails",
			rocm: "10.0.0",
			setup: func(_ map[string]any, node countersNode, _ *countersSource) {
				writeScratch(t, node.root, "probe-exit", "1\n")
			},
			wantResult:  countersResult{ServerID: "localhost", State: "unavailable", Reason: "probe_failed"},
			wantFetches: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			run := countersRun(t)
			node := newCountersNode(t, tc.rocm)
			source := newCountersSource(t, asset)
			vars := countersVars(node, source, asset, "2.28")
			if tc.setup != nil {
				tc.setup(vars, node, source)
			}
			out, _, failed := run.run(vars)
			if failed != 0 {
				t.Fatalf("run failed %d tasks, want a node without counters to keep the run going:\n%s", failed, out)
			}
			enabled, result := countersOutcome(t, out)
			if enabled || result != tc.wantResult {
				t.Errorf("counters enabled %v with %+v, want off with %+v:\n%s", enabled, result, tc.wantResult, out)
			}
			if got := source.requests.Load(); got != tc.wantFetches {
				t.Errorf("the source served the extension %d times, want %d", got, tc.wantFetches)
			}
		})
	}
}

// A node that can collect the counters gets the declared extension into its omnistat package,
// verified by its SHA-256, and collects them once the probe passes; the next run probes the
// installed extension, fetches nothing, and changes nothing. An image's extension directory is used
// instead of the download.
func TestOmnistatHardwareCountersCollectWhenTheProbePasses(t *testing.T) {
	t.Parallel()
	asset := countersAsset(t, "10.0.0")

	run := countersRun(t)
	node := newCountersNode(t, "10.0.0")
	source := newCountersSource(t, asset)
	vars := countersVars(node, source, asset, "2.28")
	out, changed, failed := run.run(vars)
	if failed != 0 || changed == 0 {
		t.Fatalf("first run changed %d and failed %d tasks, want the extension installed:\n%s", changed, failed, out)
	}
	want := countersResult{ServerID: "localhost", State: "collecting"}
	if enabled, result := countersOutcome(t, out); !enabled || result != want {
		t.Errorf("counters enabled %v with %+v, want on with %+v", enabled, result, want)
	}
	if got := readScratch(t, node.sitePackages, "omnistat/"+countersModule); got != "module built for 10.0.0" {
		t.Errorf("installed extension = %q, want the declared asset's module", got)
	}
	if out, changed, failed := run.run(vars); failed != 0 || changed != 0 || source.requests.Load() != 1 {
		t.Errorf("second run changed %d and failed %d tasks with %d downloads in all, want the installed extension used:\n%s",
			changed, failed, source.requests.Load(), out)
	}

	prebaked := newCountersNode(t, "10.0.0")
	writeScratch(t, filepath.Join(prebaked.prebaked, convergeVersion, "rocm10.0"), countersModule, "module from the image")
	writeScratch(t, filepath.Join(prebaked.prebaked, convergeVersion, "rocm10.0"), "build_info.json", "{\"rocm_version\": \"10.0.4\"}")
	imageSource := newCountersSource(t, asset)
	out, _, failed = countersRun(t).run(countersVars(prebaked, imageSource, asset, "2.28"))
	if enabled, result := countersOutcome(t, out); failed != 0 || !enabled || result != want {
		t.Errorf("with an image's extension the run failed %d tasks and enabled %v with %+v, want on with %+v:\n%s", failed, enabled, result, want, out)
	}
	if got := readScratch(t, prebaked.sitePackages, "omnistat/"+countersModule); got != "module from the image" || imageSource.requests.Load() != 0 {
		t.Errorf("installed extension = %q after %d downloads, want the image's, never downloaded", got, imageSource.requests.Load())
	}
}

// Turning the counters off takes a node back to plain Omnistat: the extension swallow installed, its
// download, and the probe are removed, and the next run changes nothing. Turning them on again
// installs the extension anew. An extension the environment came with, which swallow did not install,
// stays.
func TestOmnistatHardwareCountersOffRemovesWhatSwallowInstalled(t *testing.T) {
	t.Parallel()
	asset := countersAsset(t, "10.0.0")
	run := countersRun(t)
	node := newCountersNode(t, "10.0.0")
	source := newCountersSource(t, asset)
	vars := countersVars(node, source, asset, "2.28")
	prefix := filepath.Join(node.root, "opt", "omnistat", convergeVersion)
	installed := filepath.Join(node.sitePackages, "omnistat", countersModule)
	if out, _, failed := run.run(vars); failed != 0 {
		t.Fatalf("turning the counters on failed %d tasks:\n%s", failed, out)
	}

	vars["swallow_omnistat_hardware_counters"] = false
	out, changed, failed := run.run(vars)
	if enabled, result := countersOutcome(t, out); failed != 0 || changed == 0 || enabled || result != (countersResult{}) {
		t.Fatalf("turning the counters off changed %d and failed %d tasks, enabled %v with %+v:\n%s", changed, failed, enabled, result, out)
	}
	for _, path := range []string{installed, filepath.Join(prefix, "rocprof"), filepath.Join(prefix, "omnistat-counter-probe.py")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s is still there after the counters were turned off (stat error %v)", path, err)
		}
	}
	if out, changed, failed := run.run(vars); failed != 0 || changed != 0 {
		t.Errorf("a converged run without counters changed %d and failed %d tasks:\n%s", changed, failed, out)
	}

	vars["swallow_omnistat_hardware_counters"] = true
	out, _, failed = run.run(vars)
	if enabled, _ := countersOutcome(t, out); failed != 0 || !enabled || readScratch(t, node.sitePackages, "omnistat/"+countersModule) != "module built for 10.0.0" {
		t.Errorf("turning the counters on again failed %d tasks and enabled %v:\n%s", failed, enabled, out)
	}

	image := newCountersNode(t, "10.0.0")
	writeScratch(t, image.sitePackages, "omnistat/"+countersModule, "module the image installed")
	imageVars := countersVars(image, newCountersSource(t, asset), asset, "2.28")
	imageVars["swallow_omnistat_hardware_counters"] = false
	if out, _, failed := countersRun(t).run(imageVars); failed != 0 || readScratch(t, image.sitePackages, "omnistat/"+countersModule) != "module the image installed" {
		t.Errorf("an image's extension did not survive the counters off (failed %d):\n%s", failed, out)
	}
}
