package ansible

import (
	"encoding/json"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// validationPlaybooks are the playbooks that run Validation Suites (docs/development/validation.md
// §4.2): every playbook of a validation module, and the validate-*.yml of other modules.
func validationPlaybooks(t *testing.T) []string {
	t.Helper()
	var paths []string
	for _, module := range moduleDirs(t) {
		matches, err := filepath.Glob(filepath.Join(module, "playbooks", "*.yml"))
		if err != nil {
			t.Fatal(err)
		}
		validationModule := filepath.Base(filepath.Dir(module)) == "validation"
		for _, path := range matches {
			if validationModule || strings.HasPrefix(filepath.Base(path), "validate-") {
				paths = append(paths, path)
			}
		}
	}
	if len(paths) == 0 {
		t.Fatal("no validation playbooks found")
	}
	return paths
}

// Every validation playbook writes its results through the shared validation_report role as the
// last task of its last play, so the run's result file holds every host's results.
func TestValidationPlaybooksReportThroughTheSharedRole(t *testing.T) {
	for _, path := range validationPlaybooks(t) {
		var plays []map[string]any
		readYAML(t, path, &plays)
		if len(plays) == 0 {
			t.Errorf("%s has no play", path)
			continue
		}
		tasks, _ := plays[len(plays)-1]["tasks"].([]any)
		if len(tasks) == 0 {
			t.Errorf("%s: the last play has no tasks", path)
			continue
		}
		last, _ := tasks[len(tasks)-1].(map[string]any)
		include, _ := last["ansible.builtin.include_role"].(map[string]any)
		if include["name"] != "validation_report" || include["tasks_from"] != nil {
			t.Errorf("%s: the last task is %v, want include_role validation_report (its main tasks)", path, last["name"])
		}
	}
}

// validationResults reads the result file a validation play wrote.
func validationResults(t *testing.T, path string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the result file: %v", err)
	}
	var result struct {
		Checks []map[string]any `json:"checks"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode the result file %q: %v", raw, err)
	}
	return result.Checks
}

func verdicts(results []map[string]any) map[string]string {
	byCheck := map[string]string{}
	for _, result := range results {
		check, _ := result["check"].(string)
		byCheck[check], _ = result["verdict"].(string)
	}
	return byCheck
}

// record.yml adds one bounded result for this host or another target; main.yml writes every
// recorded result to the result file; a verdict Swallow cannot read stops the play.
func TestValidationReportWritesTheRecordedResults(t *testing.T) {
	play := `- hosts: all
  gather_facts: false
  tasks:
    - ansible.builtin.include_role:
        name: validation_report
        tasks_from: record
      vars:
        validation_report_check: clock
        validation_report_verdict: failed
        validation_report_summary: "{{ 'x' * 2000 }}"
        validation_report_evidence: "{{ 'head' ~ 'y' * 9000 ~ 'tail' }}"
        validation_report_measurements: [{name: offset, value: 3, unit: ms, expected: "<= 1"}]
    - ansible.builtin.include_role:
        name: validation_report
        tasks_from: record
      vars:
        validation_report_check: cluster
        validation_report_verdict: "{{ verdict }}"
        validation_report_target_kind: platform
        validation_report_target_id: p-1
    - ansible.builtin.include_role:
        name: validation_report
`
	run := newPlayRun(t, "validation_report", play)
	resultPath := filepath.Join(run.dir, "result.json")
	output, _, failed := run.run(map[string]any{"swallow_result_path": resultPath, "verdict": "skipped"})
	if failed != 0 {
		t.Fatalf("the play failed:\n%s", output)
	}
	results := validationResults(t, resultPath)
	if len(results) != 2 {
		t.Fatalf("results = %v, want two", results)
	}
	clock, cluster := results[0], results[1]
	target, _ := clock["target"].(map[string]any)
	summary, _ := clock["summary"].(string)
	evidence, _ := clock["evidence"].(string)
	if target["kind"] != "server" || target["id"] != "localhost" || clock["verdict"] != "failed" ||
		len(summary) != 1000 || len(evidence) != 8000 || !strings.HasSuffix(evidence, "tail") {
		t.Errorf("clock result = target %v verdict %v, %d summary and %d evidence bytes; want this host's, bounded, keeping the evidence's tail",
			target, clock["verdict"], len(summary), len(evidence))
	}
	if target, _ := cluster["target"].(map[string]any); target["kind"] != "platform" || target["id"] != "p-1" || cluster["verdict"] != "skipped" {
		t.Errorf("cluster result = %v, want the Platform's skipped verdict", cluster)
	}

	output, _, failed = run.run(map[string]any{"swallow_result_path": resultPath, "verdict": "warning"})
	if failed == 0 || !strings.Contains(output, "[validation_result_invalid]") {
		t.Errorf("an unknown verdict did not stop the play with validation_result_invalid:\n%s", output)
	}
}

// host-baseline judges the CPUs and memory against the inventory's hardware inspection, or the
// run's parameters, and skips a check nothing gives an expectation for.
func TestHostBaselineJudgesAgainstTheInspection(t *testing.T) {
	raw, err := os.ReadFile(shippedFile(t, "validate-host-baseline.yml"))
	if err != nil {
		t.Fatal(err)
	}
	run := newPlayRun(t, "validation_report", string(raw))
	resultPath := filepath.Join(run.dir, "result.json")
	vars := func(extra map[string]any) map[string]any {
		base := map[string]any{"swallow_result_path": resultPath, "swallow_ntp_servers": []string{},
			"swallow_validation_parameters": map[string]any{"minFreeRootPercent": 1}}
		for key, value := range extra {
			base[key] = value
		}
		return base
	}

	output, _, failed := run.run(vars(map[string]any{"cpu_cores": 1, "memory_mib": 64}))
	if failed != 0 {
		t.Fatalf("the play failed:\n%s", output)
	}
	got := verdicts(validationResults(t, resultPath))
	if got["cpu-count"] != "passed" || got["memory"] != "passed" || got["time-sync"] == "" || got["root-filesystem"] == "" {
		t.Errorf("verdicts = %v, want cpu-count and memory passed and every check judged", got)
	}

	output, _, failed = run.run(vars(map[string]any{"cpu_cores": 1 << 20, "memory_mib": 1 << 40}))
	if got := verdicts(validationResults(t, resultPath)); failed != 0 || got["cpu-count"] != "failed" || got["memory"] != "failed" {
		t.Errorf("an inspection the host falls short of: verdicts %v (failed tasks %d), want cpu-count and memory failed:\n%s", got, failed, output)
	}

	output, _, failed = run.run(vars(nil))
	if got := verdicts(validationResults(t, resultPath)); failed != 0 || got["cpu-count"] != "skipped" || got["memory"] != "skipped" {
		t.Errorf("no inspection and no parameters: verdicts %v (failed tasks %d), want cpu-count and memory skipped:\n%s", got, failed, output)
	}
}

// fakeSudo stands in for sudo on localhost, both as the become executable (Ansible calls it with
// "-u <user>") and as the sudo the requester runs ("-n --"): it drops sudo's options and runs the
// command as the calling user. FAKE_BECOME_DENY refuses a become and FAKE_SUDO_DENY the requester's
// sudo, the way sudo -n refuses without a password.
const fakeSudo = `#!/bin/sh
become=
while [ $# -gt 0 ]; do
  case "$1" in
    -u|-p) become=1; shift 2 ;;
    --) shift; break ;;
    -*) shift ;;
    *) break ;;
  esac
done
deny="$FAKE_SUDO_DENY"
if [ -n "$become" ]; then
  deny="$FAKE_BECOME_DENY"
fi
if [ -n "$deny" ]; then
  echo "sudo: a password is required" >&2
  exit 1
fi
exec "$@"
`

// A user suite's script runs as the requester's account and its check follows the script's exit
// code; a script past its timeout is an error; a host where the account cannot run it is an error
// whose summary starts with its code; and the script does not stay on the host.
//
// The test runs without root: it is its own requester, root's become is off
// (script_validation_become_root), and a stand-in for sudo takes the place of both become and the
// requester's sudo. What only real hosts show (switching to another account without ACL support,
// the Directory's sudo rules, a home owned by another uid, a module's temporary files in a writable
// home) is verified in the lab (validation.md §3.1).
func TestUserScriptRunsAsTheRequester(t *testing.T) {
	raw, err := os.ReadFile(shippedFile(t, "run-validation-script.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("getent"); err != nil {
		t.Skip("getent is not available")
	}
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	uid, err := strconv.Atoi(me.Uid)
	if err != nil {
		t.Fatal(err)
	}
	run := newPlayRun(t, "validation_report", string(raw))
	run.keepBecome = true
	sudo := filepath.Join(run.dir, "fake-sudo")
	if err := os.WriteFile(sudo, []byte(fakeSudo), 0o755); err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(run.dir, "host-tmp")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	// The test is its own requester; Ansible skips a become to the login user unless told otherwise,
	// and every case must go through the stand-in become.
	baseEnv := append(run.env, "TMPDIR="+scratch, "ANSIBLE_BECOME_ALLOW_SAME_USER=1")
	resultPath := filepath.Join(run.dir, "result.json")
	for _, tc := range []struct {
		name, script, verdict, summary, evidence string
		timeout                                  int
		username                                 string
		uidNumber                                int
		sudo                                     bool
		env                                      string
	}{
		{name: "exit 0", script: "#!/bin/sh\necho mounted\n", verdict: "passed", summary: "the script exited 0 as " + me.Username, evidence: "mounted"},
		{name: "exit 3", script: "#!/bin/sh\necho missing >&2\nexit 3\n", verdict: "failed", summary: "the script exited 3", evidence: "missing"},
		{name: "no interpreter line", script: "test -d /\n", verdict: "passed"},
		{name: "past its timeout", script: "#!/bin/sh\nsleep 30\n", verdict: "error", summary: "past its timeout of 1 s", timeout: 1},
		{name: "through sudo", script: "#!/bin/sh\necho elevated\n", verdict: "passed", summary: "via sudo", evidence: "elevated", sudo: true},
		{name: "sudo refused", script: "#!/bin/sh\necho ran\n", verdict: "error", sudo: true, env: "FAKE_SUDO_DENY=1",
			summary: "[validation_sudo_unavailable] " + me.Username + " has no passwordless sudo here: sudo: a password is required"},
		{name: "no account", script: "#!/bin/sh\necho ran\n", verdict: "error", username: "swallow-no-such-user",
			summary: "[validation_account_missing] swallow-no-such-user has no account on this host"},
		{name: "another uid", script: "#!/bin/sh\necho ran\n", verdict: "error", uidNumber: uid + 7919,
			summary: "[validation_account_mismatch] " + me.Username + " is uid " + me.Uid + " here"},
		{name: "become refused", script: "#!/bin/sh\necho ran\n", verdict: "error", env: "FAKE_BECOME_DENY=1",
			summary: "[validation_become_failed] the automation user cannot run commands as " + me.Username},
		{name: "become refused with sudo", script: "#!/bin/sh\necho ran\n", verdict: "error", sudo: true, env: "FAKE_BECOME_DENY=1",
			summary: "[validation_become_failed] the automation user cannot run commands as " + me.Username},
	} {
		username, uidNumber, timeout := me.Username, uid, 30
		if tc.username != "" {
			username = tc.username
		}
		if tc.uidNumber != 0 {
			uidNumber = tc.uidNumber
		}
		if tc.timeout != 0 {
			timeout = tc.timeout
		}
		run.env = baseEnv
		if tc.env != "" {
			run.env = append(slices.Clone(baseEnv), tc.env)
		}
		output, _, failed := run.run(map[string]any{
			"swallow_result_path": resultPath, "swallow_validation_script": tc.script, "swallow_validation_timeout_seconds": timeout,
			"swallow_validation_requester": map[string]any{"userId": "u-1", "username": username, "uidNumber": uidNumber},
			"swallow_validation_sudo":      tc.sudo, "script_validation_become_root": false,
			"script_validation_sudo_command": sudo, "ansible_become_exe": sudo,
		})
		if failed != 0 {
			t.Errorf("%s: the play failed:\n%s", tc.name, output)
			continue
		}
		results := validationResults(t, resultPath)
		if len(results) != 1 || results[0]["check"] != "script" || results[0]["verdict"] != tc.verdict {
			t.Errorf("%s: results = %v, want one script result %s", tc.name, results, tc.verdict)
			continue
		}
		if summary, _ := results[0]["summary"].(string); !strings.Contains(summary, tc.summary) {
			t.Errorf("%s: summary %q, want it to hold %q", tc.name, summary, tc.summary)
		}
		if evidence, _ := results[0]["evidence"].(string); !strings.Contains(evidence, tc.evidence) {
			t.Errorf("%s: evidence %q does not hold the script's output %q", tc.name, evidence, tc.evidence)
		}
		if left, _ := os.ReadDir(scratch); len(left) != 0 {
			t.Errorf("%s: the script's directory stayed on the host: %v", tc.name, left)
		}
	}
}

// A canceled run stops the play while the script runs: the script runs on (validation.md §4.4), and
// its private directory goes away when it ends although the play's `always` never runs. The test
// starts the play in its own process group, waits for the script to start, then hangs up the group
// (what a closing session sends) and kills ansible-playbook, the way a Workflow cancel stops it. The
// script makes a read-only subdirectory, which the requester's cleanup must still remove; a sudo
// suite's root-owned files need a real host and are verified in the lab.
func TestUserScriptDirectoryGoesAwayAfterACancel(t *testing.T) {
	raw, err := os.ReadFile(shippedFile(t, "run-validation-script.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("getent"); err != nil {
		t.Skip("getent is not available")
	}
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	uid, err := strconv.Atoi(me.Uid)
	if err != nil {
		t.Fatal(err)
	}
	for _, sudo := range []bool{false, true} {
		run := newPlayRun(t, "validation_report", string(raw))
		run.keepBecome = true
		fake := filepath.Join(run.dir, "fake-sudo")
		if err := os.WriteFile(fake, []byte(fakeSudo), 0o755); err != nil {
			t.Fatal(err)
		}
		scratch := filepath.Join(run.dir, "host-tmp")
		if err := os.MkdirAll(scratch, 0o755); err != nil {
			t.Fatal(err)
		}
		run.env = append(run.env, "TMPDIR="+scratch, "ANSIBLE_BECOME_ALLOW_SAME_USER=1")
		started, finished := filepath.Join(run.dir, "started"), filepath.Join(run.dir, "finished")
		script := "#!/bin/sh\nmkdir -p kept/inner && touch kept/inner/file && chmod 0500 kept/inner kept\n" +
			"touch " + started + "\nsleep 3\ntouch " + finished + "\n"
		cmd, out := run.command(map[string]any{
			"swallow_result_path": filepath.Join(run.dir, "result.json"), "swallow_validation_script": script,
			"swallow_validation_timeout_seconds": 60, "swallow_validation_sudo": sudo,
			"swallow_validation_requester":  map[string]any{"userId": "u-1", "username": me.Username, "uidNumber": uid},
			"script_validation_become_root": false, "script_validation_sudo_command": fake, "ansible_become_exe": fake,
		})
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		if !waitFor(60*time.Second, func() bool { _, err := os.Stat(started); return err == nil }) {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_ = cmd.Wait()
			t.Fatalf("sudo=%v: the script never started:\n%s", sudo, out.String())
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		// The hangup also stops Ansible's own module process here, which then leaves its payload
		// directory behind; only the private directory is the playbook's.
		private := filepath.Join(scratch, "*.swallow-validation")
		gone := waitFor(30*time.Second, func() bool {
			_, err := os.Stat(finished)
			left, _ := filepath.Glob(private)
			return err == nil && len(left) == 0
		})
		if !gone {
			left, _ := filepath.Glob(private)
			_, ranOn := os.Stat(finished)
			t.Errorf("sudo=%v: after the cancel the script finished (%v) and left %v on the host", sudo, ranOn == nil, left)
		}
	}
}

// waitFor polls done every 100 ms until it holds or limit passes, and reports whether it held.
func waitFor(limit time.Duration, done func() bool) bool {
	for deadline := time.Now().Add(limit); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if done() {
			return true
		}
	}
	return done()
}

// The script's HOME is the requester's home when it is a directory the requester owns, and the
// private directory otherwise; the playbook never creates one.
func TestUserScriptHomeIsTheRequestersOwn(t *testing.T) {
	raw, err := os.ReadFile(shippedFile(t, "run-validation-script.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("getent"); err != nil {
		t.Skip("getent is not available")
	}
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	uid, err := strconv.Atoi(me.Uid)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := exec.Command("getent", "passwd", me.Username).Output()
	if err != nil {
		t.Skipf("getent passwd %s: %v", me.Username, err)
	}
	fields := strings.Split(strings.TrimSpace(string(entry)), ":")
	if len(fields) < 6 {
		t.Fatalf("getent passwd %s = %q", me.Username, entry)
	}
	home := fields[5]
	ownHome := false
	if info, err := os.Stat(home); err == nil && info.IsDir() {
		if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) == uid {
			ownHome = true
		}
	}
	run := newPlayRun(t, "validation_report", string(raw))
	run.keepBecome = true
	sudo := filepath.Join(run.dir, "fake-sudo")
	if err := os.WriteFile(sudo, []byte(fakeSudo), 0o755); err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(run.dir, "host-tmp")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	run.env = append(run.env, "TMPDIR="+scratch, "ANSIBLE_BECOME_ALLOW_SAME_USER=1")
	resultPath := filepath.Join(run.dir, "result.json")
	output, _, failed := run.run(map[string]any{
		"swallow_result_path": resultPath, "swallow_validation_script": "#!/bin/sh\necho \"HOME=$HOME\"\n",
		"swallow_validation_timeout_seconds": 30, "swallow_validation_sudo": false,
		"swallow_validation_requester":  map[string]any{"userId": "u-1", "username": me.Username, "uidNumber": uid},
		"script_validation_become_root": false, "script_validation_sudo_command": sudo, "ansible_become_exe": sudo,
	})
	if failed != 0 {
		t.Fatalf("the play failed:\n%s", output)
	}
	results := validationResults(t, resultPath)
	if len(results) != 1 {
		t.Fatalf("results = %v, want one", results)
	}
	evidence, _ := results[0]["evidence"].(string)
	got := strings.TrimPrefix(strings.TrimSpace(evidence), "HOME=")
	switch {
	case ownHome && got != home:
		t.Errorf("HOME = %q, want the requester's own home %q", got, home)
	case !ownHome && (!strings.HasPrefix(got, scratch) || !strings.HasSuffix(got, ".swallow-validation")):
		t.Errorf("HOME = %q, want the private directory under %s (the requester owns no home at %s)", got, scratch, home)
	}
}

// fakeAMDSysfs writes a sysfs tree under root: AMD GPU functions on PCI (passthrough ones bound to
// vfio-pci), an AMD audio function that is not a GPU, a CPU KFD node, and kfdGPUs GPU KFD nodes.
func fakeAMDSysfs(t *testing.T, root string, gpus, passthrough, kfdGPUs int) {
	t.Helper()
	device := func(name, class, driver string) {
		dir := filepath.Join("sys", "bus", "pci", "devices", name)
		writeScratch(t, root, filepath.Join(dir, "vendor"), "0x1002\n")
		writeScratch(t, root, filepath.Join(dir, "class"), class+"\n")
		if driver != "" {
			if err := os.Symlink(filepath.Join("..", "..", "drivers", driver), filepath.Join(root, dir, "driver")); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := range gpus {
		device("0000:0"+string(rune('a'+i))+":00.0", "0x038000", "amdgpu")
	}
	for i := range passthrough {
		device("0000:1"+string(rune('a'+i))+":00.0", "0x120000", "vfio-pci")
	}
	device("0000:0a:00.1", "0x040300", "snd_hda_intel")
	nodes := filepath.Join("sys", "class", "kfd", "kfd", "topology", "nodes")
	writeScratch(t, root, filepath.Join(nodes, "0", "properties"), "cpu_cores_count 64\nsimd_count 0\n")
	for i := range kfdGPUs {
		writeScratch(t, root, filepath.Join(nodes, string(rune('1'+i)), "properties"), "cpu_cores_count 0\nsimd_count 416\n")
	}
}

// amd-gpu-basic expects every AMD GPU on PCI, except those passed through to VMs, in the KFD
// topology, and skips the HIP probe where the host has no HIP runtime.
func TestAMDGPUBasicCountsTheGPUsTheDriverBroughtUp(t *testing.T) {
	raw, err := os.ReadFile(shippedFile(t, "validate-amd-gpu-basic.yml"))
	if err != nil {
		t.Fatal(err)
	}
	run := newPlayRun(t, "amd_gpu_census", string(raw))
	run.env = append(run.env, "HIP_PROBE_RUNTIME="+filepath.Join(run.dir, "no-libamdhip64.so"))
	resultPath := filepath.Join(run.dir, "result.json")
	for _, tc := range []struct {
		name                        string
		gpus, passthrough, kfd      int
		kfdVerdict, hipVerdict, why string
	}{
		{name: "all up", gpus: 2, passthrough: 1, kfd: 2, kfdVerdict: "passed", hipVerdict: "skipped", why: "HIP runtime is not installed"},
		{name: "one missing", gpus: 2, kfd: 1, kfdVerdict: "failed", hipVerdict: "skipped"},
		{name: "no AMD GPU", kfdVerdict: "skipped", hipVerdict: "skipped", why: "no AMD GPU"},
	} {
		root := filepath.Join(run.dir, strings.ReplaceAll(tc.name, " ", "-"))
		fakeAMDSysfs(t, root, tc.gpus, tc.passthrough, tc.kfd)
		output, _, failed := run.run(map[string]any{"swallow_result_path": resultPath, "amd_gpu_census_root": root})
		if failed != 0 {
			t.Errorf("%s: the play failed:\n%s", tc.name, output)
			continue
		}
		results := validationResults(t, resultPath)
		got := verdicts(results)
		if got["kfd-gpus"] != tc.kfdVerdict || got["hip-probe"] != tc.hipVerdict {
			t.Errorf("%s: verdicts = %v, want kfd-gpus %s and hip-probe %s", tc.name, got, tc.kfdVerdict, tc.hipVerdict)
		}
		for _, result := range results {
			if summary, _ := result["summary"].(string); result["check"] == "hip-probe" && !strings.Contains(summary, tc.why) {
				t.Errorf("%s: hip-probe summary %q does not say %q", tc.name, summary, tc.why)
			}
		}
	}
}

// fakeKubectl answers kube_smoke.py like a cluster of three nodes: alpha takes workloads, beta is
// tainted NoSchedule, gamma is not Ready. Pods it is given succeed (the server pod runs and is
// ready); with fail set the pod pinned to alpha cannot pull its image. It records every delete.
const fakeKubectl = `#!/usr/bin/env python3
import json, os, sys
state_path = os.path.join(os.path.dirname(__file__), "cluster.json")
state = json.load(open(state_path)) if os.path.exists(state_path) else {"pods": {}, "deleted": []}
args = [a for a in sys.argv[1:] if not a.startswith("--namespace") and a != "swallow-validation"]
def save():
    json.dump(state, open(state_path, "w"))
def node(name, ready=True, taints=()):
    return {"metadata": {"name": name, "labels": {"kubernetes.io/hostname": name}},
            "spec": {"taints": [{"key": k, "effect": "NoSchedule"} for k in taints]},
            "status": {"conditions": [{"type": "Ready", "status": "True" if ready else "False", "message": "kubelet stopped"}]}}
if args[:2] == ["get", "nodes"]:
    print(json.dumps({"items": [node("alpha"), node("beta", taints=["dedicated"]), node("gamma", ready=False)]}))
elif args[:2] == ["get", "namespace"] or args[:1] == ["label"]:
    pass
elif args[:1] == ["apply"]:
    for item in json.load(sys.stdin)["items"]:
        if item["kind"] != "Pod":
            continue
        pinned = item["spec"].get("nodeSelector", {}).get("kubernetes.io/hostname")
        role = item["metadata"]["labels"]["swallow.io/validation-role"]
        status = {"phase": "Succeeded", "conditions": []}
        if role == "server":
            status = {"phase": "Running", "conditions": [{"type": "Ready", "status": "True"}]}
        elif role == "node" and pinned == "alpha" and os.environ.get("FAKE_FAIL"):
            status = {"phase": "Pending", "conditions": [], "containerStatuses": [
                {"state": {"waiting": {"reason": "ImagePullBackOff", "message": "busybox not found"}}}]}
        state["pods"][item["metadata"]["name"]] = {"metadata": item["metadata"], "status": status}
    save()
elif args[:2] == ["get", "pods"]:
    print(json.dumps({"items": list(state["pods"].values())}))
elif args[:1] == ["logs"]:
    print("swallow-smoke" if args[1].endswith("-client") else "Name: kubernetes.default.svc.cluster.local")
elif args[:1] == ["delete"]:
    state["deleted"].append(args[1:])
    save()
else:
    sys.exit("unexpected: " + " ".join(sys.argv[1:]))
`

// kubernetes-smoke judges every node on its Server (a member that never became a node fails
// nodes-ready), runs a pod only on nodes that take workloads, judges DNS and Service routing on the
// Platform, and deletes what it created even when a pod never runs.
func TestKubernetesSmokeJudgesTheNodesAndTheCluster(t *testing.T) {
	raw, err := os.ReadFile(shippedFile(t, "validate-kubernetes-smoke.yml"))
	if err != nil {
		t.Fatal(err)
	}
	run := newPlayRun(t, "k0s_smoke", string(raw))
	kubectl := filepath.Join(run.dir, "kubectl")
	if err := os.WriteFile(kubectl, []byte(fakeKubectl), 0o755); err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(run.dir, "result.json")
	vars := map[string]any{
		"swallow_result_path": resultPath, "swallow_validation_run_id": "0123456789abcdef", "swallow_validation_platform_id": "p-1",
		"k0s_smoke_kubectl": kubectl, "k0s_smoke_timeout": 5,
		// localhost stands in for the control-plane member; the members map through platform_node_name.
		"swallow_validation_members": []map[string]any{
			{"serverId": "localhost", "roles": []string{"control-plane"}},
		},
		"platform_node_name": "alpha",
	}
	for _, fail := range []bool{false, true} {
		if err := os.Remove(filepath.Join(run.dir, "cluster.json")); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		env := run.env
		if fail {
			run.env = append(run.env, "FAKE_FAIL=1")
		}
		output, _, failed := run.run(vars)
		run.env = env
		if failed != 0 {
			t.Fatalf("fail=%v: the play failed:\n%s", fail, output)
		}
		byCheckTarget := map[string]string{}
		for _, result := range validationResults(t, resultPath) {
			target, _ := result["target"].(map[string]any)
			byCheckTarget[result["check"].(string)+"@"+target["id"].(string)] = result["verdict"].(string)
		}
		wantAlphaPod := "passed"
		if fail {
			wantAlphaPod = "failed"
		}
		for key, want := range map[string]string{
			"nodes-ready@localhost": "passed", "pod-scheduling@localhost": wantAlphaPod,
			"cluster-dns@p-1": "passed", "service-routing@p-1": "passed",
		} {
			if byCheckTarget[key] != want {
				t.Errorf("fail=%v: %s = %q, want %s (results %v)", fail, key, byCheckTarget[key], want, byCheckTarget)
			}
		}
		state, err := os.ReadFile(filepath.Join(run.dir, "cluster.json"))
		if err != nil || !strings.Contains(string(state), "swallow.io/validation-run=0123456789abcdef") {
			t.Errorf("fail=%v: the run's objects were not deleted by their label: %s", fail, state)
		}
	}

	// beta (tainted) and gamma (not Ready) map to no member here, so they are judged on the Platform;
	// a worker member that is not a node fails nodes-ready on its Server.
	vars["swallow_validation_members"] = []map[string]any{
		{"serverId": "localhost", "roles": []string{"control-plane"}}, {"serverId": "w-9", "roles": []string{"worker"}},
	}
	output, _, failed := run.run(vars)
	if failed != 0 {
		t.Fatalf("the play failed:\n%s", output)
	}
	var missing, gammaNamed bool
	for _, result := range validationResults(t, resultPath) {
		target, _ := result["target"].(map[string]any)
		summary, _ := result["summary"].(string)
		if result["check"] == "nodes-ready" && target["id"] == "w-9" && result["verdict"] == "failed" {
			missing = true
		}
		if target["id"] == "p-1" && strings.Contains(summary, "node gamma") || strings.Contains(summary, "node beta") {
			gammaNamed = true
		}
	}
	if !missing || !gammaNamed {
		t.Errorf("missing worker judged %v, unmapped nodes named on the Platform %v:\n%s", missing, gammaNamed, output)
	}

	// The runner's inventory holds every member, and --limit narrows the play to the Task's target:
	// the play runs on its target even when another member comes first.
	if err := os.Remove(resultPath); err != nil {
		t.Fatal(err)
	}
	run.inventory, run.limit = "w-9,localhost,", "localhost"
	output, _, failed = run.run(vars)
	if _, err := os.Stat(resultPath); failed != 0 || err != nil {
		t.Errorf("limited to its target: the play wrote no results (%v):\n%s", err, output)
	}
}

// network-mesh needs two target Servers; with one, every check is skipped instead of judged.
func TestNetworkMeshSkipsASingleServer(t *testing.T) {
	raw, err := os.ReadFile(shippedFile(t, "validate-network-mesh.yml"))
	if err != nil {
		t.Fatal(err)
	}
	run := newPlayRun(t, "network_mesh", string(raw))
	resultPath := filepath.Join(run.dir, "result.json")
	output, _, failed := run.run(map[string]any{"swallow_result_path": resultPath, "swallow_validation_targets": []string{"localhost"}})
	if failed != 0 {
		t.Fatalf("the play failed:\n%s", output)
	}
	got := verdicts(validationResults(t, resultPath))
	for _, check := range []string{"ping-mesh", "mtu", "ssh-port"} {
		if got[check] != "skipped" {
			t.Errorf("%s = %q, want skipped (verdicts %v)", check, got[check], got)
		}
	}
}
