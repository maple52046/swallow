package ansible

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeHIP is a stand-in libamdhip64 whose behavior the environment selects: FAKE_HIP_COUNT GPUs,
// FAKE_HIP_MEMSET_FAIL makes hipMemset fail, and FAKE_HIP_ABORT_AT_EXIT aborts the process at
// exit with glibc's heap-corruption message, as libhsakmt does on duplicate peer links.
const fakeHIP = `#include <stdio.h>
#include <stdlib.h>
static int count(void) { const char *c = getenv("FAKE_HIP_COUNT"); return c ? atoi(c) : 0; }
static void corrupt(void) { fprintf(stderr, "free(): invalid size\n"); abort(); }
__attribute__((constructor)) static void init(void) { if (getenv("FAKE_HIP_ABORT_AT_EXIT")) atexit(corrupt); }
int hipInit(unsigned flags) { return count() == 0 ? 100 : 0; }
int hipGetDeviceCount(int *n) { *n = count(); return count() == 0 ? 100 : 0; }
int hipSetDevice(int d) { return d < count() ? 0 : 101; }
int hipMalloc(void **p, size_t size) { *p = malloc(16); return 0; }
int hipMemset(void *p, int value, size_t size) { return getenv("FAKE_HIP_MEMSET_FAIL") ? 1 : 0; }
int hipDeviceSynchronize(void) { return 0; }
int hipFree(void *p) { free(p); return 0; }
`

type hipProbeReport struct {
	OK         bool   `json:"ok"`
	Devices    *int   `json:"devices"`
	Run        int    `json:"run"`
	Returncode int    `json:"returncode"`
	Error      string `json:"error"`
}

func buildFakeHIP(t *testing.T) string {
	t.Helper()
	compiler, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no C compiler")
	}
	rocm := t.TempDir()
	source := filepath.Join(rocm, "fake_hip.c")
	if err := os.WriteFile(source, []byte(fakeHIP), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rocm, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(compiler, "-shared", "-fPIC", "-o", filepath.Join(rocm, "lib", "libamdhip64.so"), source).CombinedOutput(); err != nil {
		t.Fatalf("build fake libamdhip64: %v\n%s", err, out)
	}
	return rocm
}

func runHIPProbe(t *testing.T, env []string, args ...string) (hipProbeReport, int) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not available")
	}
	probe := filepath.Join("..", "..", "..", "..", "automation", "playbooks", "roles", "gpu_probe", "files", "hip_probe.py")
	cmd := exec.Command(python, append([]string{probe}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.Output()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("hip-probe: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var report hipProbeReport
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &report); err != nil {
		t.Fatalf("parse %q: %v", out, err)
	}
	return report, code
}

// A Slurm GPU node joins only after its GPUs pass the probe directly and through Slurm (no GPU
// request, one GPU, every GPU); a failed node is drained before the run fails.
func TestSlurmGPUGatesUseTheHIPProbe(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "automation", "playbooks")
	cases := map[string][]string{
		filepath.Join("roles", "slurm_gpu", "tasks", "main.yml"): {
			"name: gpu_probe", "gpu_probe_expect: \"{{ slurm_gpus | length }}\"", "state=drain",
			"'hip_runtime_missing' if gpu_probe_run.rc == 2 else 'gpu_probe_failed'", "slurm_node_gres_full",
		},
		filepath.Join("roles", "slurm_verify", "tasks", "main.yml"): {
			"--expect=0", "--gres=gpu:1", "--expect=1", "--gres={{ hostvars[item].slurm_node_gres_full }}", "--runs=3",
			"state=drain", "[gpu_probe_failed]",
		},
		filepath.Join("roles", "gpu_probe", "tasks", "main.yml"): {"'hip_runtime_missing' if gpu_probe_run.rc == 2 else 'gpu_probe_failed'"},
		"uninstall-slurm.yml": {"/usr/local/libexec/swallow/hip-probe"},
	}
	for path, required := range cases {
		content, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, want := range required {
			if !strings.Contains(string(content), want) {
				t.Errorf("%s does not contain %q", path, want)
			}
		}
	}
}

// The probe passes only when every visible GPU allocates, fills, and frees memory and the process
// exits cleanly; a wrong count, a failing call, or a heap corruption at exit fails it.
func TestHIPProbeVerdicts(t *testing.T) {
	rocm := "ROCM_PATH=" + buildFakeHIP(t)

	if report, code := runHIPProbe(t, []string{rocm, "FAKE_HIP_COUNT=8"}, "--expect", "8"); code != 0 || !report.OK || report.Devices == nil || *report.Devices != 8 {
		t.Errorf("healthy 8 GPUs: report = %+v, exit %d; want ok over 8 devices", report, code)
	}
	if report, code := runHIPProbe(t, []string{rocm}, "--expect", "0", "--runs", "1"); code != 0 || !report.OK {
		t.Errorf("no GPU visible, none expected: report = %+v, exit %d; want ok", report, code)
	}
	if report, code := runHIPProbe(t, []string{rocm, "FAKE_HIP_COUNT=7"}, "--expect", "8"); code != 1 || report.OK || report.Run != 1 {
		t.Errorf("7 of 8 GPUs: report = %+v, exit %d; want a failed first run", report, code)
	}
	if report, code := runHIPProbe(t, []string{rocm, "FAKE_HIP_COUNT=8", "FAKE_HIP_MEMSET_FAIL=1"}, "--expect", "8"); code != 1 || !strings.Contains(report.Error, "hipMemset") {
		t.Errorf("failing hipMemset: report = %+v, exit %d; want a hipMemset failure", report, code)
	}
	report, code := runHIPProbe(t, []string{rocm, "FAKE_HIP_COUNT=8", "FAKE_HIP_ABORT_AT_EXIT=1"}, "--expect", "8")
	if code != 1 || report.OK || report.Returncode == 0 || !strings.Contains(report.Error, "free(): invalid") {
		t.Errorf("heap corruption at exit: report = %+v, exit %d; want the abort reported", report, code)
	}
	if report, code := runHIPProbe(t, []string{"HIP_PROBE_RUNTIME=/nonexistent/libamdhip64.so"}, "--expect", "8"); code != 2 || report.OK {
		t.Errorf("no HIP runtime: report = %+v, exit %d; want exit 2", report, code)
	}
}
