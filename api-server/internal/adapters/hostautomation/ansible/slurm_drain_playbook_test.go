package ansible

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runDrainWait runs slurm_drain_wait.py against fake squeue and scancel commands: squeue lists the
// job ids in jobs for a node until scancel has run for it, and fails when squeueFails is set. It
// returns the helper's exit code and output, and the nodes scancel was called for.
func runDrainWait(t *testing.T, nodes string, jobs map[string]string, squeueFails bool) (int, string, []string) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not available")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	squeue := `#!/bin/sh
node=""
for arg in "$@"; do case "$arg" in --nodelist=*) node="${arg#--nodelist=}";; esac; done
[ -n "$SQUEUE_FAIL" ] && { echo "slurm_load_jobs error: Unable to contact slurm controller" >&2; exit 1; }
[ -f "$STATE/canceled-$node" ] && exit 0
eval "printf '%s' \"\${JOBS_$node:-}\"" | tr ',' '\n'
`
	scancel := `#!/bin/sh
for arg in "$@"; do case "$arg" in --nodelist=*) touch "$STATE/canceled-${arg#--nodelist=}";; esac; done
`
	for name, body := range map[string]string{"squeue": squeue, "scancel": scancel} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script, err := filepath.Abs(filepath.Join(roleDir(t, "slurm_drain"), "files", "slurm_drain_wait.py"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, script, "--nodes", nodes, "--poll", "0", "--grace", "60")
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "STATE="+dir)
	for node, ids := range jobs {
		cmd.Env = append(cmd.Env, "JOBS_"+node+"="+ids)
	}
	if squeueFails {
		cmd.Env = append(cmd.Env, "SQUEUE_FAIL=1")
	}
	output, _ := cmd.CombinedOutput()
	var canceled []string
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if name, ok := strings.CutPrefix(entry.Name(), "canceled-"); ok {
			canceled = append(canceled, name)
		}
	}
	return cmd.ProcessState.ExitCode(), string(output), canceled
}

// Empty nodes finish at once; a node still running jobs at its timeout fails the wait with its job
// count, unless the removal forces it, which cancels that node's jobs and waits for them to end; an
// unreadable queue is its own failure.
func TestSlurmDrainWaitIsBoundedPerNode(t *testing.T) {
	if code, output, _ := runDrainWait(t, `[{"name":"g1","timeout":600},{"name":"g2","timeout":600}]`, nil, false); code != 0 ||
		!strings.Contains(output, "g1 0 jobs") {
		t.Errorf("empty nodes: exit %d, output %q", code, output)
	}

	code, output, canceled := runDrainWait(t, `[{"name":"g1","timeout":0},{"name":"g2","timeout":0}]`, map[string]string{"g1": "41,42"}, false)
	if code != 3 || !strings.Contains(output, "g1 still runs 2 jobs (41,42)") || len(canceled) != 0 {
		t.Errorf("an unforced busy node past its timeout: exit %d, canceled %v, output %q", code, canceled, output)
	}

	code, output, canceled = runDrainWait(t, `[{"name":"g1","timeout":0,"force":true}]`, map[string]string{"g1": "41"}, false)
	if code != 0 || strings.Join(canceled, ",") != "g1" || !strings.Contains(output, "canceled jobs on g1") {
		t.Errorf("a forced busy node: exit %d, canceled %v, output %q", code, canceled, output)
	}

	if code, output, _ = runDrainWait(t, `[{"name":"g1","timeout":600}]`, nil, true); code != 2 ||
		!strings.Contains(output, "cannot be read") {
		t.Errorf("an unreadable queue: exit %d, output %q", code, output)
	}
}

// The drain fails with the module's declared failure codes, and nothing in it touches a node that
// stays: it runs on the primary controller and names only the leaving nodes.
func TestSlurmDrainPlaybookReportsItsFailures(t *testing.T) {
	tasks := slurmPlaybookFile(t, "roles", "slurm_drain", "tasks", "main.yml")
	for _, want := range []string{"[slurm_drain_timeout]", "[slurm_drain_failed]", "swallow_slurm_scale_in_nodes", "state=drain"} {
		if !strings.Contains(tasks, want) {
			t.Errorf("slurm_drain does not contain %q", want)
		}
	}
	cleanup := slurmPlaybookFile(t, "roles", "slurm_workload_storage_client", "tasks", "remove.yml")
	for _, want := range []string{"[slurm_unmount_busy]", "timeout"} {
		if !strings.Contains(cleanup, want) {
			t.Errorf("the workload storage removal does not contain %q", want)
		}
	}
	node := slurmPlaybookFile(t, "roles", "slurm_node_cleanup", "tasks", "main.yml")
	if !strings.Contains(node, "tasks_from: remove") {
		t.Error("the node cleanup does not remove the workload storage mount")
	}
	for _, playbook := range []string{"uninstall-slurm.yml", "uninstall-slurm-node.yml"} {
		if !strings.Contains(slurmPlaybookFile(t, playbook), "slurm_node_cleanup") {
			t.Errorf("%s does not use the shared node cleanup", playbook)
		}
	}
}
