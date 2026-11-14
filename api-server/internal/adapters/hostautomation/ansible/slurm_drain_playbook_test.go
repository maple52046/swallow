package ansible

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// runDrainWait runs slurm_drain_wait.py against fake squeue, scancel, and scontrol commands: squeue
// lists the job ids in jobs for a node until scancel (or, for a node in stuck, scontrol setting it
// DOWN) has run for it, and fails when squeueFails is set. It returns the helper's exit code and
// output, and the nodes scancel was called for.
func runDrainWait(t *testing.T, nodes string, jobs map[string]string, squeueFails bool, stuck ...string) (int, string, []string) {
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
[ -f "$STATE/down-$node" ] && exit 0
case " $STUCK " in *" $node "*) ;; *) [ -f "$STATE/canceled-$node" ] && exit 0;; esac
eval "printf '%s' \"\${JOBS_$node:-}\"" | tr ',' '\n'
`
	scancel := `#!/bin/sh
for arg in "$@"; do case "$arg" in --nodelist=*) touch "$STATE/canceled-${arg#--nodelist=}";; esac; done
`
	scontrol := `#!/bin/sh
node=""; down=""
for arg in "$@"; do case "$arg" in nodename=*) node="${arg#nodename=}";; state=down) down=1;; esac; done
[ -n "$down" ] && touch "$STATE/down-$node"
`
	for name, body := range map[string]string{"squeue": squeue, "scancel": scancel, "scontrol": scontrol} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script, err := filepath.Abs(filepath.Join(roleDir(t, "slurm_drain"), "files", "slurm_drain_wait.py"))
	if err != nil {
		t.Fatal(err)
	}
	grace := "60"
	if len(stuck) > 0 {
		grace = "0"
	}
	cmd := exec.Command(python, script, "--nodes", nodes, "--poll", "0", "--grace", grace)
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "STATE="+dir, "STUCK="+strings.Join(stuck, " "))
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

	// A forced node whose jobs do not end after they were canceled (its machine failed) is set DOWN
	// and no longer waited for.
	code, output, _ = runDrainWait(t, `[{"name":"g1","timeout":0,"force":true}]`, map[string]string{"g1": "41"}, false, "g1")
	if code != 0 || !strings.Contains(output, "set g1 DOWN") {
		t.Errorf("a forced node whose jobs do not end: exit %d, output %q", code, output)
	}

	if code, output, _ = runDrainWait(t, `[{"name":"g1","timeout":600}]`, nil, true); code != 2 ||
		!strings.Contains(output, "cannot be read") {
		t.Errorf("an unreadable queue: exit %d, output %q", code, output)
	}
}

// The drain fails with the module's declared failure codes, and nothing in it touches a node that
// stays or needs a leaving node to answer: it runs on the primary controller, gathers no facts, and
// names the leaving nodes from the run's variables.
func TestSlurmDrainPlaybookReportsItsFailures(t *testing.T) {
	tasks := slurmPlaybookFile(t, "roles", "slurm_drain", "tasks", "main.yml")
	for _, want := range []string{"[slurm_drain_timeout]", "[slurm_drain_failed]", "swallow_slurm_scale_in_nodes", "state=drain", "node.name"} {
		if !strings.Contains(tasks, want) {
			t.Errorf("slurm_drain does not contain %q", want)
		}
	}
	if strings.Contains(tasks, "hostvars") {
		t.Error("slurm_drain reads a leaving node's host facts; a node that cannot be reached has none")
	}
	if drain := slurmPlaybookFile(t, "drain-slurm-nodes.yml"); strings.Contains(drain, "gather_facts: true") {
		t.Error("drain-slurm-nodes gathers facts, which fails on a leaving node that cannot be reached")
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

// runCleanupReachability runs the reachability pre_tasks of uninstall-slurm-node.yml's first play
// (which find the leaving nodes that can be reached) against a host that cannot be reached, with its removal forced or not,
// and returns ansible-playbook's exit code and output.
func runCleanupReachability(t *testing.T, force bool) (int, string) {
	t.Helper()
	playbook, err := exec.LookPath("ansible-playbook")
	if err != nil {
		t.Skip("ansible-playbook is not available")
	}
	var plays []map[string]any
	readYAML(t, shippedFile(t, "uninstall-slurm-node.yml"), &plays)
	dir := t.TempDir()
	// The first play's pre_tasks decide reachability; its hold role needs root and a real host.
	first := plays[0]
	delete(first, "roles")
	raw, err := yaml.Marshal([]map[string]any{first})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "play.yml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	// A TEST-NET address no machine answers, with a short connection timeout.
	inventory := "dead ansible_host=192.0.2.1 ansible_connection=ssh ansible_ssh_common_args='-o ConnectTimeout=2 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null'\n"
	if err := os.WriteFile(filepath.Join(dir, "inventory"), []byte(inventory), 0o644); err != nil {
		t.Fatal(err)
	}
	vars, _ := json.Marshal(map[string]any{"swallow_slurm_scale_in_nodes": []any{map[string]any{"server_id": "dead", "name": "gpu-09", "force": force}}})
	if err := os.WriteFile(filepath.Join(dir, "vars.json"), vars, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(playbook, "-i", "inventory", "-e", "@vars.json", "play.yml")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ANSIBLE_HOME="+filepath.Join(dir, "home"), "ANSIBLE_LOCAL_TEMP="+filepath.Join(dir, "tmp"), "ANSIBLE_NOCOLOR=1", "ANSIBLE_RETRY_FILES_ENABLED=0")
	output, _ := cmd.CombinedOutput()
	return cmd.ProcessState.ExitCode(), string(output)
}

// A leaving node that cannot be reached is skipped with a warning when its removal is forced, so the
// run succeeds and the removal finishes; otherwise the run fails with [slurm_node_unreachable].
func TestSlurmNodeCleanupToleratesAnUnreachableForcedNode(t *testing.T) {
	code, output := runCleanupReachability(t, true)
	if code != 0 || !strings.Contains(output, "[slurm_node_unreachable] warning") {
		t.Errorf("forced: exit %d, output:\n%s", code, output)
	}
	code, output = runCleanupReachability(t, false)
	if code == 0 || !strings.Contains(output, "[slurm_node_unreachable] dead cannot be reached") {
		t.Errorf("not forced: exit %d, output:\n%s", code, output)
	}
	for _, play := range []string{"package_maintenance_hold", "slurm_node_cleanup"} {
		if !strings.Contains(slurmPlaybookFile(t, "uninstall-slurm-node.yml"), play) {
			t.Errorf("uninstall-slurm-node.yml no longer runs %s", play)
		}
	}
}

// A leaving node is out of the NFS server's exports by the time it is cleaned, so its mounted path
// answers "Permission denied" even to root: the cleanup finds the mount in the mount table and
// unmounts it, and removes the GPU records the Slurm deploy wrote.
func TestSlurmNodeCleanupUnmountsAShareThatNoLongerExportsToIt(t *testing.T) {
	remove := slurmPlaybookFile(t, "roles", "slurm_workload_storage_client", "tasks", "remove.yml")
	if !strings.Contains(remove, `findmnt, -n, --mountpoint, "{{ slurm_workload_mount_path }}"`) || strings.Contains(remove, "mountpoint, -q") {
		t.Error("the workload filesystem removal must find the mount in the mount table, not by the path")
	}
	if !strings.Contains(slurmPlaybookFile(t, "roles", "slurm_node_cleanup", "tasks", "main.yml"), "- /var/lib/swallow/gpu\n") {
		t.Error("slurm_node_cleanup leaves the GPU records slurm_gpu writes")
	}
}
