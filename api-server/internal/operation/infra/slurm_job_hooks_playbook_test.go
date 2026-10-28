package infra

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func slurmPlaybookFile(t *testing.T, parts ...string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(append([]string{"..", "..", "..", "automation", "playbooks"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

// knownJobHookFiles returns the file names of the job hooks Swallow owns, from the role defaults.
func knownJobHookFiles(t *testing.T) []string {
	t.Helper()
	known, _ := readRoleDefaults(t, "slurm_job_hooks")["slurm_job_hooks_known"].([]any)
	if len(known) == 0 {
		t.Fatal("slurm_job_hooks_known is empty")
	}
	files := make([]string, 0, len(known))
	for _, item := range known {
		hook, _ := item.(map[string]any)
		file, _ := hook["file"].(string)
		if file == "" {
			t.Fatalf("job hook %v has no file", hook)
		}
		files = append(files, file)
	}
	return files
}

// slurm.conf names exactly one Prolog and one Epilog, Swallow's dispatchers, so no integration can
// overwrite another's job attribution by claiming Prolog= for itself.
func TestSlurmConfNamesOnlyTheJobHookDispatchers(t *testing.T) {
	conf := slurmPlaybookFile(t, "roles", "slurm_config", "templates", "slurm.conf.j2")
	want := map[string]string{
		"Prolog=": "Prolog={{ slurm_conf_dir }}/swallow-prolog",
		"Epilog=": "Epilog={{ slurm_conf_dir }}/swallow-epilog",
	}
	for prefix, line := range want {
		var found []string
		for _, l := range strings.Split(conf, "\n") {
			if strings.HasPrefix(l, prefix) {
				found = append(found, l)
			}
		}
		if len(found) != 1 || found[0] != line {
			t.Errorf("slurm.conf %s lines = %q, want exactly %q", prefix, found, line)
		}
	}
}

// Every hook Swallow owns has a Prolog and an Epilog half, and uninstall removes both.
func TestEveryKnownJobHookHasBothPhasesAndIsUninstalled(t *testing.T) {
	uninstall := slurmPlaybookFile(t, "uninstall-slurm.yml")
	for _, file := range knownJobHookFiles(t) {
		for _, phase := range []string{"prolog.d", "epilog.d"} {
			script := slurmPlaybookFile(t, "roles", "slurm_job_hooks", "files", phase, file)
			if !strings.HasPrefix(script, "#!/bin/bash\n") || !strings.Contains(script, "Managed by Swallow") {
				t.Errorf("%s/%s is not a Swallow-managed bash script", phase, file)
			}
			if !strings.Contains(uninstall, "/etc/slurm/"+phase+"/"+file) {
				t.Errorf("uninstall-slurm.yml does not remove /etc/slurm/%s/%s", phase, file)
			}
		}
	}
	for _, dispatcher := range []string{"/etc/slurm/swallow-prolog", "/etc/slurm/swallow-epilog"} {
		if !strings.Contains(uninstall, dispatcher) {
			t.Errorf("uninstall-slurm.yml does not remove %s", dispatcher)
		}
	}
}

// renderDispatcher renders the dispatcher template for one phase over hookDir.
func renderDispatcher(t *testing.T, hookDir string, timeoutSeconds string) string {
	t.Helper()
	script := slurmPlaybookFile(t, "roles", "slurm_job_hooks", "templates", "dispatcher.j2")
	script = strings.NewReplacer(
		"{{ slurm_job_hooks_phase | capitalize }}", "Prolog",
		"{{ slurm_job_hooks_phase }}", "prolog",
		"{{ slurm_job_hooks_dir }}", hookDir,
		"{{ slurm_job_hooks_timeout_seconds }}", timeoutSeconds,
	).Replace(script)
	if strings.Contains(script, "{{") {
		t.Fatalf("dispatcher template has an unrendered variable:\n%s", script)
	}
	path := filepath.Join(t.TempDir(), "swallow-prolog")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeHook(t *testing.T, dir, name, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/bash\n"+body+"\n"), mode); err != nil {
		t.Fatal(err)
	}
}

// The dispatcher runs every executable hook in name order with the job's environment, and a hook
// that fails or hangs never fails the Prolog (which would drain the node) or stops later hooks.
func TestJobHookDispatcherIsolatesFailingHooks(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not available")
	}
	if _, err := exec.LookPath("timeout"); err != nil {
		t.Skip("timeout is not available")
	}
	hooks, out := t.TempDir(), t.TempDir()
	trace := filepath.Join(out, "trace")
	writeHook(t, hooks, "10-fails", "echo boom >&2; exit 3", 0o755)
	writeHook(t, hooks, "20-hangs", "sleep 30", 0o755)
	writeHook(t, hooks, "30-records", `echo "records $SLURM_JOB_ID" >> `+trace, 0o755)
	writeHook(t, hooks, "40-not-executable", "echo never >> "+trace, 0o644)
	writeHook(t, hooks, "05-first", "echo first >> "+trace, 0o755)

	cmd := exec.Command("bash", renderDispatcher(t, hooks, "1"))
	cmd.Env = append(os.Environ(), "SLURM_JOB_ID=42")
	start := time.Now()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dispatcher failed: %v\n%s", err, output)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Errorf("dispatcher took %s; the hanging hook was not cut off", elapsed)
	}
	got, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first\nrecords 42\n" {
		t.Errorf("hooks ran %q, want the executable hooks in name order with the job environment", got)
	}
}

// hookCopy writes a copy of a job hook with its state directory pointed at dir.
func hookCopy(t *testing.T, phase, dir string) string {
	t.Helper()
	script := slurmPlaybookFile(t, "roles", "slurm_job_hooks", "files", phase, "20-device-metrics-exporter")
	if !strings.Contains(script, "readonly state_dir=/run/exporter") {
		t.Fatalf("%s hook no longer names /run/exporter", phase)
	}
	script = strings.Replace(script, "readonly state_dir=/run/exporter", "readonly state_dir="+dir, 1)
	path := filepath.Join(t.TempDir(), phase+"-hook")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func runHook(t *testing.T, hook string, env ...string) error {
	t.Helper()
	cmd := exec.Command("bash", hook)
	cmd.Env = append(os.Environ(), env...)
	return cmd.Run()
}

// The Device Metrics Exporter hook records a job on each of its GPUs in a file naming only that GPU
// (so removing it detaches exactly that GPU), refuses a GPU list that is not numeric, and its Epilog
// removes only files that still name the ending job, so a late Epilog never detaches a later job.
func TestDeviceMetricsExporterJobHookAttributesEachGPU(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not available")
	}
	dir := t.TempDir()
	prolog, epilog := hookCopy(t, "prolog.d", dir), hookCopy(t, "epilog.d", dir)
	job := []string{"SLURM_JOB_ID=41", "SLURM_JOB_USER=alice", "SLURM_JOB_PARTITION=main", "SLURM_CLUSTER_NAME=lab", "SLURM_JOB_GPUS=1,3"}
	if err := runHook(t, prolog, job...); err != nil {
		t.Fatalf("prolog: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if strings.Join(names, ",") != "1,3" {
		t.Fatalf("state files = %v, want exactly the GPUs 1 and 3", names)
	}
	var record map[string]string
	raw, _ := os.ReadFile(filepath.Join(dir, "3"))
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("GPU 3 file %q: %v", raw, err)
	}
	if record["SLURM_JOB_ID"] != "41" || record["CUDA_VISIBLE_DEVICES"] != "3" || record["SLURM_JOB_USER"] != "alice" {
		t.Errorf("GPU 3 file = %v, want job 41 on GPU 3 only", record)
	}

	if err := runHook(t, prolog, "SLURM_JOB_ID=42", "SLURM_JOB_GPUS=1;touch /tmp/x"); err == nil {
		t.Error("a GPU list that is not numeric must fail the hook")
	}

	// Job 43 takes GPU 3 before job 41's Epilog runs.
	if err := runHook(t, prolog, "SLURM_JOB_ID=43", "SLURM_JOB_GPUS=3"); err != nil {
		t.Fatalf("prolog of job 43: %v", err)
	}
	if err := runHook(t, epilog, job...); err != nil {
		t.Fatalf("epilog: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "1")); !os.IsNotExist(err) {
		t.Error("job 41's Epilog must remove its file on GPU 1")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "3"))
	if err != nil || !strings.Contains(string(raw), `"SLURM_JOB_ID":"43"`) {
		t.Errorf("job 41's Epilog removed job 43's file on GPU 3: %q, %v", raw, err)
	}
}
