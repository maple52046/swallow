package ansible

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeRVS stands in for RVS's rvs binary. `rvs -g` lists the GPUs of fake-rvs.json; `rvs -c <conf>`
// prints, for each action of the conf, what the real one prints for its module (gst, iet, mem,
// babel, pebb, pbqt) and its closing summary row. fake-rvs.json can make an action fail (fail) and
// raise a GPU's uncorrectable error count while the stress runs (raiseUE: the err_count file to
// rewrite). Every call is appended to rvs-calls.log beside fake-rvs.json.
const fakeRVS = `#!/usr/bin/env python3
import json, os, re, sys
config = json.load(open(os.environ["FAKE_RVS_CONFIG"]))
here = os.path.dirname(os.environ["FAKE_RVS_CONFIG"])
with open(os.path.join(here, "rvs-calls.log"), "a") as log:
    log.write(" ".join(sys.argv[1:]) + "\n")
gpus = config["gpus"]
if sys.argv[1] == "-g":
    print("ROCm Validation Suite (version 1.7.112)\n\nSupported GPUs available:")
    for i, (bdf, gid) in enumerate(gpus):
        print("%s - GPU[ %d - %s] AMD Instinct MI250X / MI250 (Device 29708)" % (bdf, i + 2, gid))
    sys.exit(0)
conf = open(sys.argv[sys.argv.index("-c") + 1]).read()
module = re.search(r"module:\s*(\w+)", conf).group(1)
if config.get("raiseUE"):
    with open(config["raiseUE"], "w") as count:
        count.write("ue: 1\nce: 0\n")
rows = []
for action in re.findall(r"^\s*-\s*name\s*:\s*(\S+)", conf, re.M):
    ok = action not in (config.get("fail") or [])
    stamp = "[RESULT] [ 100.000000] [%s]" % action
    for i, (bdf, gid) in enumerate(gpus):
        if module == "gst":
            print("%s [GPU:: %s] GFLOPS %d Target GFLOPS: 30000 met: %s" % (stamp, gid, 35000 if ok else 1000, "TRUE" if ok else "FALSE"))
        elif module == "iet":
            print("%s [GPU:: %s] Power(W) %d.000000" % (stamp, gid, 560 if i % 2 == 0 else 0))
            print("%s [GPU:: %s] pass: %s" % (stamp, gid, "TRUE" if ok else "FALSE"))
        elif module == "pebb":
            print("%s pcie-bandwidth [ %d/8] [CPU:: 0] [GPU:: %d - %s - %s] h2d::true d2h::true %s GBps duration: 0.02 secs"
                  % (stamp, i + 1, i + 2, gid, bdf, config.get("pcie", "25.000")))
        elif module == "pbqt":
            peer = gpus[(i + 1) % len(gpus)]
            print("%s p2p-bandwidth[ %d/8] [GPU:: %d - %s - %s] [GPU:: %d - %s - %s] bidirectional: true 90.5 GBps duration: 0.01 secs"
                  % (stamp, i + 1, i + 2, gid, bdf, i + 3, peer[1], peer[0]))
    if module == "mem":
        print("%s mem Test 1 : %s" % (stamp, "PASS" if ok else "FAIL"))
    if module == "babel":
        for bdf, gid in gpus:
            print("[RESULT] [ 100.000000] \nGPU Id      Function    MiBytes/sec    Max MiB/s      Min MiB/s      Avg MiB/s")
            for function, average in (("Copy", 700000.0), ("Triad", 900000.0)):
                print("%s        %s        %.3f     %.3f     %.3f     %.3f" % (gid, function, average, average, average / 2, average))
    rows.append("| %-32s | %-14s | \x1b[32m%-14s\x1b[0m  |" % (action, module.upper(), "PASS" if ok else "FAIL"))
print("+=====+\n| Action Name | Module | Result |\n+=====+")
print("\n".join(rows))
`

// fakeStressAMDSMI stands in for amd-smi as the stress helper and the RAS helper call it: list,
// static, metric (telemetry and XGMI errors), and xgmi. fake-amd-smi.json sets the GPUs, the
// temperature every sample reports, and the XGMI error status after the stress.
const fakeStressAMDSMI = `#!/usr/bin/env python3
import json, os, sys
config = json.load(open(os.environ["FAKE_AMD_SMI_CONFIG"]))
args = sys.argv[1:]
gpus = config["gpus"]
if args[:1] == ["list"]:
    print(json.dumps([{"gpu": i, "bdf": bdf, "kfd_id": int(gid)} for i, (bdf, gid) in enumerate(gpus)]))
elif args[:1] == ["static"]:
    print(json.dumps([{"gpu": i, "asic": {"market_name": config.get("market", "AMD Instinct MI250X / MI250")},
                       "vbios": {"name": "Mi200 A1 D65210"}} for i in range(len(gpus))]))
elif args[:1] == ["metric"] and "-x" in args:
    print(json.dumps({"gpu_data": [{"gpu": i, "xgmi_err": config.get("xgmi", "AMDSMI_XGMI_STATUS_NO_ERRORS")} for i in range(len(gpus))]}))
elif args[:1] == ["metric"]:
    print(json.dumps({"gpu_data": [{"gpu": i, "temperature": {"edge": {"value": 50, "unit": "C"},
                                                                "hotspot": {"value": config.get("temperature", 80), "unit": "C"}},
                                     "power": {"socket_power": {"value": 300, "unit": "W"}, "throttle_status": "UNTHROTTLED"},
                                     "throttle": {"prochot_accumulated": "N/A"}} for i in range(len(gpus))]}))
elif args[:1] == ["xgmi"]:
    print(json.dumps({"link_status": [{"gpu": i, "bdf": bdf, "link_status": ["SELF" if j == i else "N/A" for j in range(len(gpus))]}
                                      for i, (bdf, gid) in enumerate(gpus)]}))
else:
    sys.exit("unexpected: " + " ".join(args))
`

// fakeLdd says a library is missing when fake-ldd.txt names one.
const fakeLdd = `#!/bin/sh
cat "$(dirname "$FAKE_RVS_CONFIG")/fake-ldd.txt" 2>/dev/null
`

// fakeRCCLMakefile builds a stand-in all_reduce_perf that prints what the real one prints at the end.
const fakeRCCLMakefile = "all:\n\tmkdir -p build && cp all_reduce_perf.sh build/all_reduce_perf && chmod +x build/all_reduce_perf\n"

const fakeAllReduce = `#!/bin/sh
echo "# nThread 1 nGpus $6"
echo "# Out of bounds values : 0 OK"
echo "# Avg bus bandwidth    : 124.3"
`

// writeTarGz writes a gzipped tar of files (name: content; a name ending in "/" is a directory) and
// returns its SHA-256.
func writeTarGz(t *testing.T, path string, files map[string]string) string {
	t.Helper()
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := gzip.NewWriter(out)
	tw := tar.NewWriter(zw)
	for name, content := range files {
		header := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if strings.HasSuffix(name, "/") {
			header = &tar.Header{Name: name, Mode: 0o755, Typeflag: tar.TypeDir}
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil && header.Typeflag == tar.TypeReg {
			t.Fatal(err)
		}
	}
	for _, closer := range []interface{ Close() error }{tw, zw, out} {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// rvsConfs are the MI250X confs of the fake RVS release: two GEMM actions, one of each other module.
var rvsConfs = map[string]string{
	"gst_single.conf":  "actions:\n- name: gst-dgemm\n  module: gst\n  duration: 15000\n- name: gst-fp16\n  module: gst\n  duration: 15000\n",
	"iet_stress.conf":  "actions:\n- name: iet-560W\n  module: iet\n  duration: 600000\n  ramp_interval: 10000\n",
	"mem.conf":         "actions:\n- name: mem-1\n  module: mem\n  num_passes: 500\n  num_iter: 50000\n",
	"babel.conf":       "actions:\n- name: babel-256MiB\n  module: babel\n  num_iter: 5000\n",
	"pebb_single.conf": "actions:\n- name: h2d\n  module: pebb\n  duration: 50000\n",
	"pbqt_single.conf": "actions:\n- name: p2p\n  module: pbqt\n  duration: 10000\n",
}

// amd-gpu-stress runs RVS, rccl-tests, and AMD SMI from what the run declares and judges each check
// on its own: an RVS action that fails, a new uncorrectable error, a GPU too hot, or a bandwidth below
// the minimum the run gives fails its check only; a ROCm without RVS's libraries, a ROCm RVS is not
// declared for, or a release whose checksum differs make the RVS checks errors with their code; a
// Server without an AMD GPU skips every check. The private directory never stays on the host, and
// level quick shortens the conf but keeps its actions and targets.
func TestAMDGPUStressJudgesEachCheck(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not available")
	}
	raw, err := os.ReadFile(shippedFile(t, "validate-amd-gpu-stress.yml"))
	if err != nil {
		t.Fatal(err)
	}
	run := newPlayRun(t, "amd_gpu_stress", string(raw))
	tools := map[string]string{"rvs.py": fakeRVS, "amd-smi": fakeStressAMDSMI, "ldd": fakeLdd, "dmesg": fakeDmesg}
	for name, script := range tools {
		if err := os.WriteFile(filepath.Join(run.dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{"bin/": "", "bin/rvs": "#!/bin/sh\nexec python3 \"$FAKE_RVS_PY\" \"$@\"\n", "lib/rvs/libgst.so.0": "",
		"share/rocm-validation-suite/conf/MI250X/": ""}
	for name, conf := range rvsConfs {
		files["share/rocm-validation-suite/conf/MI250X/"+name] = conf
		files["share/rocm-validation-suite/conf/"+name] = conf
	}
	rvsArchive := filepath.Join(run.dir, "rvs.tar.gz")
	rvsSum := writeTarGz(t, rvsArchive, files)
	rcclArchive := filepath.Join(run.dir, "rccl-tests.tar.gz")
	rcclSum := writeTarGz(t, rcclArchive, map[string]string{"rccl-tests-1/Makefile": fakeRCCLMakefile, "rccl-tests-1/all_reduce_perf.sh": fakeAllReduce})

	// The role's pinned paths and checksums, so the run hands the helper the test's archives.
	var defaults struct {
		Releases []struct{ Rocm, Path, SHA256 string } `yaml:"amd_gpu_stress_rvs_releases"`
		RCCL     struct{ Path, SHA256 string }         `yaml:"amd_gpu_stress_rccl_tests"`
	}
	readYAML(t, filepath.Join(roleDir(t, "amd_gpu_stress"), "defaults", "main.yml"), &defaults)
	gpus := [][2]string{{"0000:0a:00.0", "51627"}, {"0000:0b:00.0", "64489"}}

	for _, tc := range []struct {
		name        string
		gpus        int
		rocm        string // the version in the fake ROCm's .info/version; default 10.0.0
		noRCCL      bool
		missingLib  string
		failActions []string
		raiseUE     bool
		hot         bool
		xgmiErr     bool
		badSum      bool
		parameters  map[string]any
		want        map[string]string
		summaries   map[string]string
	}{
		{
			name: "healthy", gpus: 2,
			want: map[string]string{"gemm": "passed", "hbm-integrity": "passed", "hbm-bandwidth": "passed", "pcie-bandwidth": "passed",
				"xgmi-bandwidth": "passed", "xgmi-links": "passed", "power": "passed", "collectives": "passed",
				"thermal-throttle": "passed", "ras-after-stress": "passed"},
			summaries: map[string]string{"gemm": "RVS passed 2 actions on 2 GPUs", "hbm-bandwidth": "recorded, not judged",
				"collectives": "124.3 GB/s bus bandwidth", "thermal-throttle": "at most 80 C",
				"xgmi-links": "no XGMI error on 2 GPUs"},
		},
		{
			name: "failures stay in their checks", gpus: 2, failActions: []string{"gst-fp16", "mem-1"}, raiseUE: true, hot: true, xgmiErr: true,
			parameters: map[string]any{"minPcieGBps": 30},
			want: map[string]string{"gemm": "failed", "hbm-integrity": "failed", "pcie-bandwidth": "failed", "power": "passed",
				"thermal-throttle": "failed", "ras-after-stress": "failed", "xgmi-links": "failed", "collectives": "passed"},
			summaries: map[string]string{"gemm": "gst-fp16 on 0000:0a:00.0 (1000 of 30000 GFLOPS)", "hbm-integrity": "RVS failed mem-1",
				"pcie-bandwidth": "below 30", "thermal-throttle": "above 95 C", "ras-after-stress": "new uncorrectable errors on 0000:0a:00.0",
				"xgmi-links": "XGMI errors on"},
		},
		{
			name: "ROCm lacks the libraries RVS needs", gpus: 2, missingLib: "libhipblaslt.so.1",
			want:      map[string]string{"gemm": "error", "power": "error", "hbm-bandwidth": "error", "collectives": "passed", "ras-after-stress": "passed"},
			summaries: map[string]string{"gemm": "[rocm_libraries_missing]", "power": "libhipblaslt.so.1"},
		},
		{
			name: "no RVS for this ROCm", gpus: 2, rocm: "6.4.1",
			want:      map[string]string{"gemm": "error", "xgmi-bandwidth": "error", "xgmi-links": "passed"},
			summaries: map[string]string{"gemm": "[rvs_unsupported_rocm] no RVS release is declared for ROCm 6.4.1"},
		},
		{
			name: "checksum differs", gpus: 2, badSum: true,
			want:      map[string]string{"gemm": "error", "collectives": "error"},
			summaries: map[string]string{"gemm": "[artifact_download_failed]", "collectives": "[artifact_download_failed]"},
		},
		{
			name: "no RCCL to build against", gpus: 2, noRCCL: true,
			want:      map[string]string{"collectives": "error", "gemm": "passed"},
			summaries: map[string]string{"collectives": "[rccl_build_unavailable] rccl-tests cannot be built on this host: no rccl.h"},
		},
		{
			name: "unknown SKU", gpus: 2, parameters: map[string]any{"rvsProfile": "MI999X"},
			want:      map[string]string{"gemm": "error", "power": "error"},
			summaries: map[string]string{"gemm": "[rvs_profile_unknown] RVS has no conf directory MI999X"},
		},
		{
			name: "level quick", gpus: 2, parameters: map[string]any{"level": "quick"},
			want: map[string]string{"gemm": "passed", "power": "passed"},
		},
		{
			name:      "no AMD GPU",
			want:      map[string]string{"gemm": "skipped", "ras-after-stress": "skipped", "collectives": "skipped"},
			summaries: map[string]string{"gemm": "no AMD GPU on PCI"},
		},
	} {
		dir := filepath.Join(run.dir, strings.ReplaceAll(tc.name, " ", "-"))
		root, rocm, work := filepath.Join(dir, "root"), filepath.Join(dir, "rocm"), filepath.Join(dir, "work")
		fakeAMDSysfs(t, root, tc.gpus, 0, tc.gpus)
		for _, gpu := range gpus[:tc.gpus] {
			fakeAMDRAS(t, root, gpu[0], "0x7fff", map[string]string{"umc": "ue: 0\nce: 0\n"})
		}
		version := tc.rocm
		if version == "" {
			version = "10.0.0"
		}
		writeScratch(t, rocm, filepath.Join(".info", "version"), version+"\n")
		writeScratch(t, rocm, filepath.Join("bin", "hipcc"), "")
		writeScratch(t, rocm, filepath.Join("lib", "librccl.so"), "")
		if !tc.noRCCL {
			writeScratch(t, rocm, filepath.Join("include", "rccl", "rccl.h"), "")
		}
		if err := os.MkdirAll(work, 0o755); err != nil {
			t.Fatal(err)
		}
		config := map[string]any{"gpus": gpus[:tc.gpus], "fail": tc.failActions, "pcie": "25.000"}
		if tc.raiseUE {
			config["raiseUE"] = filepath.Join(root, "sys", "bus", "pci", "devices", gpus[0][0], "ras", "umc_err_count")
		}
		smi := map[string]any{"gpus": gpus[:tc.gpus]}
		if tc.hot {
			smi["temperature"] = 101
		}
		if tc.xgmiErr {
			smi["xgmi"] = "AMDSMI_XGMI_STATUS_ERROR"
		}
		writeScratch(t, dir, "fake-rvs.json", mustJSON(t, config))
		writeScratch(t, dir, "fake-amd-smi.json", mustJSON(t, smi))
		lddOut := ""
		if tc.missingLib != "" {
			lddOut = "\t" + tc.missingLib + " => not found\n"
		}
		writeScratch(t, dir, "fake-ldd.txt", lddOut)
		run.env = append(run.env, "FAKE_RVS_CONFIG="+filepath.Join(dir, "fake-rvs.json"), "FAKE_RVS_PY="+filepath.Join(run.dir, "rvs.py"),
			"FAKE_AMD_SMI_CONFIG="+filepath.Join(dir, "fake-amd-smi.json"))

		artifactFiles := map[string]string{defaults.RCCL.Path: "file://" + rcclArchive}
		releases := []map[string]string{}
		for _, release := range defaults.Releases {
			artifactFiles[release.Path] = "file://" + rvsArchive
			sum := rvsSum
			if tc.badSum {
				sum = strings.Repeat("0", 64)
			}
			releases = append(releases, map[string]string{"rocm": release.Rocm, "path": release.Path, "sha256": sum})
		}
		rcclPin := map[string]string{"path": defaults.RCCL.Path, "sha256": rcclSum}
		if tc.badSum {
			rcclPin["sha256"] = strings.Repeat("0", 64)
		}
		parameters := tc.parameters
		if parameters == nil {
			parameters = map[string]any{}
		}
		resultPath := filepath.Join(dir, "result.json")
		output, _, failed := run.run(map[string]any{
			"swallow_result_path": resultPath, "swallow_validation_parameters": parameters,
			"swallow_artifacts":   map[string]any{"files": artifactFiles},
			"amd_gpu_census_root": root, "amd_gpu_stress_root": root, "amd_gpu_stress_rocm": rocm,
			"amd_gpu_stress_amd_smi": filepath.Join(run.dir, "amd-smi"), "amd_gpu_stress_dmesg": filepath.Join(run.dir, "dmesg"),
			"amd_gpu_stress_ldd": filepath.Join(run.dir, "ldd"), "amd_gpu_stress_workdir": work,
			"amd_gpu_stress_rvs_releases": releases, "amd_gpu_stress_rccl_tests": rcclPin,
		})
		run.env = run.env[:len(run.env)-3]
		if failed != 0 {
			t.Errorf("%s: the play failed:\n%s", tc.name, output)
			continue
		}
		results := validationResults(t, resultPath)
		if got := verdicts(results); len(got) != 10 {
			t.Errorf("%s: verdicts = %v, want one for each of the ten checks", tc.name, got)
		}
		for _, result := range results {
			check, _ := result["check"].(string)
			summary, _ := result["summary"].(string)
			if want, ok := tc.want[check]; ok && result["verdict"] != want {
				t.Errorf("%s: %s = %v (%s), want %s", tc.name, check, result["verdict"], summary, want)
			}
			if want := tc.summaries[check]; !strings.Contains(summary, want) {
				t.Errorf("%s: %s summary %q does not say %q", tc.name, check, summary, want)
			}
		}
		if left, _ := filepath.Glob(filepath.Join(work, "swallow-amd-gpu-stress-*")); len(left) > 0 {
			t.Errorf("%s: the private directory stayed on the host: %v", tc.name, left)
		}
		if tc.name == "level quick" {
			calls := readScratch(t, dir, "rvs-calls.log")
			if !strings.Contains(calls, "power-iet_stress.conf") || !strings.Contains(calls, "gemm-gst_single.conf") {
				t.Errorf("%s: RVS did not run the shortened confs: %q", tc.name, calls)
			}
		}
	}
}

// Level quick caps how long each action runs but keeps every action and its targets, and power keeps
// the three minutes at load iet needs to hold its target.
func TestAMDGPUStressQuickConfKeepsActionsAndTargets(t *testing.T) {
	helper := filepath.Join(roleDir(t, "amd_gpu_stress"), "files", "amd_gpu_stress.py")
	dir := t.TempDir()
	source := filepath.Join(dir, "gst.conf")
	writeScratch(t, dir, "gst.conf", "actions:\n- name: a\n  duration: 15000\n  target_stress: 30000\n  count: 3\n- name: b\n  duration: 2000\n")
	writeScratch(t, dir, "iet.conf", "actions:\n- name: c\n  duration: 600000\n  ramp_interval: 10000\n  target_power: 560\n")
	script := `
import importlib.util, sys
spec = importlib.util.spec_from_file_location("stress", sys.argv[1])
stress = importlib.util.module_from_spec(spec)
spec.loader.exec_module(stress)
stress.quick_conf(sys.argv[2], sys.argv[2] + ".quick", stress.QUICK)
stress.quick_conf(sys.argv[3], sys.argv[3] + ".quick", stress.QUICK_BY_CHECK["power"])
`
	out, err := exec.Command("python3", "-c", script, helper, source, filepath.Join(dir, "iet.conf")).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	gst, iet := readScratch(t, dir, "gst.conf.quick"), readScratch(t, dir, "iet.conf.quick")
	for _, want := range []string{"- name: a\n  duration: 5000\n  target_stress: 30000\n  count: 1\n", "- name: b\n  duration: 2000\n"} {
		if !strings.Contains(gst, want) {
			t.Errorf("quick gst conf %q does not hold %q", gst, want)
		}
	}
	if want := "duration: 180000\n  ramp_interval: 10000\n  target_power: 560\n"; !strings.Contains(iet, want) {
		t.Errorf("quick iet conf %q does not hold %q", iet, want)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// sleepingRVS lists one GPU and, asked to run a conf, records its pid and sleeps: a tool still
// running when the helper is told to stop.
const sleepingRVS = `#!/bin/sh
if [ "$1" = "-g" ]; then
  echo "ROCm Validation Suite (version 1.7.112)"
  echo "0000:0a:00.0 - GPU[ 2 - 51627] AMD Instinct MI250X / MI250 (Device 29708)"
  exit 0
fi
echo $$ > "$RVS_PID_FILE"
exec sleep 120
`

// A run cancelled while RVS works stops RVS and still removes its private directory: the helper
// ignores SIGHUP, and on SIGTERM it stops the tool its main thread runs (never one of the sampler's
// amd-smi calls) before it removes the directory.
func TestAMDGPUStressStopsItsToolOnSIGTERM(t *testing.T) {
	dir := t.TempDir()
	root, rocm, work := filepath.Join(dir, "root"), filepath.Join(dir, "rocm"), filepath.Join(dir, "work")
	fakeAMDSysfs(t, root, 1, 0, 1)
	fakeAMDRAS(t, root, "0000:0a:00.0", "0x7fff", map[string]string{"umc": "ue: 0\nce: 0\n"})
	writeScratch(t, rocm, filepath.Join(".info", "version"), "10.0.0\n")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, script := range map[string]string{"amd-smi": fakeStressAMDSMI, "ldd": "#!/bin/sh\n", "dmesg": fakeDmesg} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeScratch(t, dir, "fake-amd-smi.json", mustJSON(t, map[string]any{"gpus": [][2]string{{"0000:0a:00.0", "51627"}}}))
	archive := filepath.Join(dir, "rvs.tar.gz")
	sum := writeTarGz(t, archive, map[string]string{"bin/rvs": sleepingRVS,
		"share/rocm-validation-suite/conf/MI250X/gst_single.conf": rvsConfs["gst_single.conf"]})
	ras, err := os.ReadFile(filepath.Join(roleDir(t, "amd_gpu_ras"), "files", "amd_gpu_ras.py"))
	if err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(dir, "rvs.pid")
	helper := exec.Command("python3", filepath.Join(roleDir(t, "amd_gpu_stress"), "files", "amd_gpu_stress.py"),
		"--target=srv", "--gpu=0000:0a:00.0", "--rvs=10|file://"+archive+"|"+sum,
		"--ras-helper="+base64.StdEncoding.EncodeToString(ras), "--rocm="+rocm, "--root="+root,
		"--amd-smi="+filepath.Join(dir, "amd-smi"), "--dmesg="+filepath.Join(dir, "dmesg"), "--ldd="+filepath.Join(dir, "ldd"),
		"--workdir="+work)
	helper.Env = append(os.Environ(), "FAKE_AMD_SMI_CONFIG="+filepath.Join(dir, "fake-amd-smi.json"), "RVS_PID_FILE="+pidFile)
	var out strings.Builder
	helper.Stdout, helper.Stderr = &out, &out
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	if !waitFor(30*time.Second, func() bool { _, err := os.Stat(pidFile); return err == nil }) {
		_ = helper.Process.Kill()
		t.Fatalf("RVS never started:\n%s", out.String())
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if err := helper.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	if err := helper.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("the helper did not outlive SIGHUP: %v\n%s", err, out.String())
	}
	done := make(chan error, 1)
	go func() { done <- helper.Wait() }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		_ = helper.Process.Kill()
		t.Fatalf("the helper did not stop on SIGTERM:\n%s", out.String())
	}
	if !waitFor(10*time.Second, func() bool { return syscall.Kill(pid, 0) != nil }) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("RVS (pid %d) still runs after the helper stopped", pid)
	}
	if left, _ := filepath.Glob(filepath.Join(work, "swallow-amd-gpu-stress-*")); len(left) > 0 {
		t.Errorf("the private directory stayed on the host: %v", left)
	}
}

// Results stay inside the API's bounds, counted in UTF-8 bytes, so a long multibyte summary or
// evidence is cut rather than making the API drop the result.
func TestAMDGPUStressBoundsResultsInUTF8Bytes(t *testing.T) {
	helper := filepath.Join(roleDir(t, "amd_gpu_stress"), "files", "amd_gpu_stress.py")
	script := `
import importlib.util, json, sys
spec = importlib.util.spec_from_file_location("stress", sys.argv[1])
stress = importlib.util.module_from_spec(spec)
spec.loader.exec_module(stress)
r = stress.result("srv", "gemm", "failed", "測" * 2000, "溫度" * 5000, [{}] * 30)
print(json.dumps([len(r["summary"].encode()), len(r["evidence"].encode()), len(r["measurements"])]))
`
	out, err := exec.Command("python3", "-c", script, helper).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var sizes [3]int
	if err := json.Unmarshal(out, &sizes); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if sizes[0] > 1024 || sizes[0] == 0 || sizes[1] > 8192 || sizes[1] == 0 || sizes[2] != 20 {
		t.Errorf("summary %d bytes, evidence %d bytes, %d measurements; want at most 1024, 8192, and 20", sizes[0], sizes[1], sizes[2])
	}
}
