package ansible

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSlurm answers slurm_acceptance.py as a cluster of a CPU node n1 and a node n2 with two GPUs,
// with native OCI containers and MPI on and every probe installed. FAKE_GPU_FAIL makes the HIP probe
// fail in a one-GPU job; FAKE_NO_FEATURES turns OCI and MPI off; FAKE_EXCLUSIVE makes the partition
// hand out whole nodes (as Omnistat does). Drains go to state.json.
const fakeSlurm = `#!/usr/bin/env python3
import json, os, sys
here = os.path.dirname(os.path.realpath(__file__))
state_path = os.path.join(os.path.dirname(os.path.abspath(sys.argv[0])), "state.json")
def save(key, value):
    state = json.load(open(state_path)) if os.path.exists(state_path) else {}
    state.setdefault(key, []).append(value)
    json.dump(state, open(state_path, "w"))
features = "0" if os.environ.get("FAKE_NO_FEATURES") else "1"
def srun(args):
    opts = {}
    i = 0
    while i < len(args) and args[i].startswith("-"):
        key, _, value = args[i].partition("=")
        opts[key] = value
        i += 1
    command = args[i:]
    nodes = opts.get("--nodelist", "n1").split(",")
    if "--mpi" in opts and opts["--mpi"] == "list":
        print("MPI plugin types are...\n\tnone\n\tpmix_v5")
        return 0
    if command[:2] == ["sh", "-c"]:
        for node in nodes:
            hip = "1" if node == "n2" else "0"
            print(f"{node} oci={features} bundle={features} mpi={features} hip={hip}")
        return 0
    if command == ["/bin/cat", "/proc/self/uid_map"]:
        print("         0      65534          1")
        return 0
    if command[0].endswith("hip-probe"):
        expect = next(a.split("=")[1] for a in command if a.startswith("--expect="))
        if os.environ.get("FAKE_GPU_FAIL") and opts.get("--gres") == "gpu:1":
            print(json.dumps({"ok": False, "error": "hipMalloc failed on device 0"}))
            return 1
        visible = "0" if opts.get("--gres") == "gpu:1" else None
        print(json.dumps({"ok": True, "runs": 1, "devices": int(expect), "visible": visible}))
        return 0
    if command[0].endswith("mpi-probe"):
        if "--fail-rank=1" in command:
            return 3
        ranks = int(opts.get("--ntasks") or len(nodes) * int(opts.get("--ntasks-per-node", "1")))
        for rank in range(ranks):
            print(json.dumps({"barrier": True, "size": ranks, "rank": rank, "pmixNamespace": "slurm.pmix.7.0",
                              "node": nodes[rank % len(nodes)]}))
        return 0
    sys.stderr.write("unexpected srun " + " ".join(args) + "\n")
    return 2
def main(argv):
    name = os.path.basename(argv[0])
    args = argv[1:]
    if name == "runuser":
        return main(args[args.index("--") + 1:])
    if name == "srun":
        return srun(args)
    if name == "sinfo":
        print("n1|4|(null)|idle\nn2|8|gpu:gfx90a:2(S:0)|idle")
    elif name == "scontrol" and args[:1] == ["ping"]:
        print("Slurmctld(primary) at c1 is UP")
    elif name == "scontrol" and args[:2] == ["show", "partition"]:
        # Slurm 26.05 reads an OverSubscribe=EXCLUSIVE partition back as Exclusive=NODE.
        exclusive = "NODE" if os.environ.get("FAKE_EXCLUSIVE") else "NO"
        print(f"PartitionName=main AllowGroups=ALL Default=YES Exclusive={exclusive} OverSubscribe=NO State=UP")
    elif name == "scontrol" and args[:1] == ["update"]:
        save("drained", args[1])
    elif name == "sbatch":
        save("submitted", " ".join(args))
        print("1" + str(len(json.load(open(state_path))["submitted"])))
    elif name == "squeue":
        print("RUNNING\nRUNNING")
    elif name == "scancel":
        save("canceled", args[0])
    else:
        sys.stderr.write("unexpected " + " ".join(argv) + "\n")
        return 2
    return 0
sys.exit(main([sys.argv[0]] + sys.argv[1:]))
`

// fakeSlurmBin installs fakeSlurm under every Slurm command slurm_acceptance.py runs and returns the
// directory, to put first on PATH.
func fakeSlurmBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-slurm")
	if err := os.WriteFile(script, []byte(fakeSlurm), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sinfo", "scontrol", "srun", "sbatch", "squeue", "scancel", "runuser"} {
		if err := os.Symlink(script, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// runSlurmAcceptance runs the shipped slurm_acceptance.py against the fake cluster in bin with env,
// mapping n2 to Server s-2, and returns its verdicts by check and target.
func runSlurmAcceptance(t *testing.T, bin string, env []string, args ...string) map[string]string {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not available")
	}
	script := shippedFile(t, filepath.Join("roles", "slurm_acceptance", "files", "slurm_acceptance.py"))
	cmd := exec.Command(python, append([]string{script, "--run-id=0123456789", "--platform-id=p-1",
		`--servers={"n2": "s-2"}`, `--expected-nodes=["s-2"]`}, args...)...)
	cmd.Env = append(append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH")), env...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("slurm_acceptance.py: %v\n%s", err, out)
	}
	var report struct {
		Checks []map[string]any `json:"checks"`
	}
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	verdicts := map[string]string{}
	for _, check := range report.Checks {
		target, _ := check["target"].(map[string]any)
		verdicts[check["check"].(string)+"@"+target["id"].(string)] = check["verdict"].(string) + ": " + check["summary"].(string)
	}
	return verdicts
}

func verdictOf(verdicts map[string]string, key string) string {
	verdict, _, _ := strings.Cut(verdicts[key], ":")
	return verdict
}

// slurm-acceptance judges a healthy cluster from what it reads, judges a GPU node on its Server,
// drains a failed GPU node only in gate mode, and skips the features that are off.
func TestSlurmAcceptanceJudgesTheCluster(t *testing.T) {
	bin := fakeSlurmBin(t)
	healthy := runSlurmAcceptance(t, bin, nil)
	for key, want := range map[string]string{
		"srun-all-nodes@p-1": "passed", "shared-node@p-1": "passed", "mpi-pmix@p-1": "passed",
		"gpu-gres@s-2": "passed", "oci-container@s-2": "passed",
	} {
		if got := verdictOf(healthy, key); got != want {
			t.Errorf("healthy %s = %q, want %s (all: %v)", key, healthy[key], want, healthy)
		}
	}
	if !strings.Contains(healthy["oci-container@p-1"], "node n1") {
		t.Errorf("the rootless container check of unmapped n1 is not named on the Platform: %v", healthy)
	}

	failing := runSlurmAcceptance(t, bin, []string{"FAKE_GPU_FAIL=1"})
	if verdictOf(failing, "gpu-gres@s-2") != "failed" || strings.Contains(failing["gpu-gres@s-2"], "drained") {
		t.Errorf("report mode: gpu-gres@s-2 = %q, want failed and not drained", failing["gpu-gres@s-2"])
	}
	if _, err := os.Stat(filepath.Join(bin, "state.json")); err == nil {
		state, _ := os.ReadFile(filepath.Join(bin, "state.json"))
		if strings.Contains(string(state), "drained") {
			t.Errorf("report mode drained a node: %s", state)
		}
	}
	gated := runSlurmAcceptance(t, bin, []string{"FAKE_GPU_FAIL=1"}, "--drain")
	state, _ := os.ReadFile(filepath.Join(bin, "state.json"))
	if verdictOf(gated, "gpu-gres@s-2") != "failed" || !strings.Contains(gated["gpu-gres@s-2"], "drained") ||
		!strings.Contains(string(state), "nodename=n2") {
		t.Errorf("gate mode: gpu-gres@s-2 = %q, state %s; want failed and n2 drained", gated["gpu-gres@s-2"], state)
	}

	// A partition that hands out whole nodes (Omnistat) skips shared-node, in the form Slurm 26.05 reports it.
	if exclusive := runSlurmAcceptance(t, bin, []string{"FAKE_EXCLUSIVE=1"}); verdictOf(exclusive, "shared-node@p-1") != "skipped" {
		t.Errorf("whole-node partition: shared-node = %q, want skipped", exclusive["shared-node@p-1"])
	}

	off := runSlurmAcceptance(t, bin, []string{"FAKE_NO_FEATURES=1"}, `--gpu-servers=["s-9"]`)
	if verdictOf(off, "oci-container@p-1") != "skipped" || verdictOf(off, "mpi-pmix@p-1") != "skipped" {
		t.Errorf("features off: %v, want oci-container and mpi-pmix skipped", off)
	}
	if verdictOf(off, "gpu-gres@s-9") != "failed" {
		t.Errorf("a GPU Server that is not a node: gpu-gres@s-9 = %q, want failed", off["gpu-gres@s-9"])
	}
}

// The playbook runs the acceptance once and writes what it judged through validation_report; only
// the gate mode asks it to drain.
func TestSlurmAcceptancePlaybookReportsTheResults(t *testing.T) {
	raw, err := os.ReadFile(shippedFile(t, "validate-slurm-acceptance.yml"))
	if err != nil {
		t.Fatal(err)
	}
	bin := fakeSlurmBin(t)
	run := newPlayRun(t, "slurm_acceptance", string(raw))
	run.env = append(run.env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_GPU_FAIL=1")
	resultPath := filepath.Join(run.dir, "result.json")
	for _, mode := range []string{"report", "gate"} {
		output, _, failed := run.run(map[string]any{
			"swallow_result_path": resultPath, "swallow_validation_run_id": "0123456789", "swallow_validation_platform_id": "p-1",
			"swallow_validation_mode": mode, "platform_node_name": "n2",
			"swallow_validation_members": []map[string]any{{"serverId": "localhost", "roles": []string{"controller", "compute"}}},
		})
		if failed != 0 {
			t.Fatalf("%s: the play failed:\n%s", mode, output)
		}
		got := verdicts(validationResults(t, resultPath))
		if got["gpu-gres"] != "failed" || got["srun-all-nodes"] != "passed" {
			t.Errorf("%s: verdicts = %v, want gpu-gres failed and srun-all-nodes passed", mode, got)
		}
		state, _ := os.ReadFile(filepath.Join(bin, "state.json"))
		if drained := strings.Contains(string(state), "drained"); drained != (mode == "gate") {
			t.Errorf("%s: drained %v, want %v", mode, drained, mode == "gate")
		}
	}

	// The runner's inventory holds every member, and --limit narrows the play to the Task's target:
	// the play runs on its target even when another member comes first.
	if err := os.Remove(resultPath); err != nil {
		t.Fatal(err)
	}
	run.inventory, run.limit = "s-0,localhost,", "localhost"
	output, _, failed := run.run(map[string]any{
		"swallow_result_path": resultPath, "swallow_validation_run_id": "0123456789", "swallow_validation_platform_id": "p-1",
		"platform_node_name":         "n2",
		"swallow_validation_members": []map[string]any{{"serverId": "localhost", "roles": []string{"controller", "compute"}}},
	})
	if _, err := os.Stat(resultPath); failed != 0 || err != nil {
		t.Errorf("limited to its target: the play wrote no results (%v):\n%s", err, output)
	}
}
