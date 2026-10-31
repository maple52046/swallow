package ansible

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// amdgpuNodeState is the JSON report of roles/amdgpu_loader/files/amdgpu_node_state.py.
type amdgpuNodeState struct {
	Loaded           bool              `json:"loaded"`
	Parameters       map[string]string `json:"parameters"`
	Mismatched       []string          `json:"mismatched"`
	Missing          []string          `json:"missing"`
	Unknown          []string          `json:"unknown"`
	Suggestions      map[string]string `json:"suggestions"`
	Unverifiable     []string          `json:"unverifiable"`
	Refcnt           *int              `json:"refcnt"`
	Holders          []string          `json:"holders"`
	RootFilesystem   *string           `json:"rootFilesystem"`
	MemoryBackedRoot bool              `json:"memoryBackedRoot"`
	GPUNodes         int               `json:"gpuNodes"`
	Rules            map[string]string `json:"rules"`
	RuleReasons      map[string]string `json:"ruleReasons"`
}

// kfdNode writes one KFD topology node under root: its properties and the peers of its io_links
// and p2p_links.
func kfdNode(t *testing.T, root, id string, gfx int, ioPeers, p2pPeers []string) {
	t.Helper()
	dir := filepath.Join(root, "sys", "class", "kfd", "kfd", "topology", "nodes", id)
	writeFile(t, filepath.Join(dir, "properties"), "gfx_target_version "+itoa(gfx)+"\nsimd_count 4\n")
	for i, peer := range ioPeers {
		writeFile(t, filepath.Join(dir, "io_links", itoa(i), "properties"), "type 11\nnode_from "+id+"\nnode_to "+peer+"\n")
	}
	for i, peer := range p2pPeers {
		writeFile(t, filepath.Join(dir, "p2p_links", itoa(i), "properties"), "type 2\nnode_from "+id+"\nnode_to "+peer+"\n")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func runNodeState(t *testing.T, root string, desired map[string]string, extra ...string) amdgpuNodeState {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not available")
	}
	script := filepath.Join("..", "..", "..", "..", "automation", "playbooks", "roles", "amdgpu_loader", "files", "amdgpu_node_state.py")
	encoded, _ := json.Marshal(desired)
	args := append([]string{script, "--root", root, "--desired", base64.StdEncoding.EncodeToString(encoded)}, extra...)
	out, err := exec.Command(python, args...).Output()
	if err != nil {
		t.Fatalf("amdgpu_node_state.py: %v", err)
	}
	var state amdgpuNodeState
	if err := json.Unmarshal(out, &state); err != nil {
		t.Fatalf("parse report %q: %v", out, err)
	}
	return state
}

// The pcie_p2p rule follows the libhsakmt overflow condition, never a GPU model: it fires only
// when a GPU node reaches the same peer through both an io_link and a p2p_link.
func TestAMDGPUNodeStateTopologyRule(t *testing.T) {
	duplicate := t.TempDir()
	kfdNode(t, duplicate, "0", 0, nil, nil) // the CPU node
	kfdNode(t, duplicate, "1", 90402, []string{"0", "2"}, []string{"2"})
	kfdNode(t, duplicate, "2", 90402, []string{"0", "1"}, []string{"1"})
	state := runNodeState(t, duplicate, nil)
	if state.GPUNodes != 2 || state.Rules["pcie_p2p"] != "0" || state.RuleReasons["pcie_p2p"] == "" {
		t.Errorf("duplicate links: report = %+v, want pcie_p2p=0 with a reason over 2 GPU nodes", state)
	}

	xgmiOnly := t.TempDir()
	kfdNode(t, xgmiOnly, "0", 0, nil, nil)
	kfdNode(t, xgmiOnly, "1", 90402, []string{"0", "2"}, nil)
	kfdNode(t, xgmiOnly, "2", 90402, []string{"0", "1"}, nil)
	pcieOnly := t.TempDir()
	kfdNode(t, pcieOnly, "1", 110000, []string{"0"}, []string{"2"})
	kfdNode(t, pcieOnly, "2", 110000, []string{"0"}, []string{"1"})
	for name, root := range map[string]string{"XGMI only": xgmiOnly, "PCIe peers only": pcieOnly} {
		if rules := runNodeState(t, root, nil).Rules; len(rules) != 0 {
			t.Errorf("%s: rules = %v, want none", name, rules)
		}
	}
}

// Desired options are compared with the loaded parameters: booleans by meaning (0 and N agree),
// other values exactly, and an option the driver does not expose is reported as missing.
func TestAMDGPUNodeStateComparesLoadedParameters(t *testing.T) {
	root := t.TempDir()
	if state := runNodeState(t, root, map[string]string{"pcie_p2p": "0"}); state.Loaded || len(state.Mismatched) != 0 {
		t.Errorf("unloaded driver: report = %+v, want not loaded and nothing compared", state)
	}

	params := filepath.Join(root, "sys", "module", "amdgpu", "parameters")
	writeFile(t, filepath.Join(params, "pcie_p2p"), "Y\n")
	writeFile(t, filepath.Join(params, "vm_fragment_size"), "-1\n")
	state := runNodeState(t, root, map[string]string{"pcie_p2p": "0", "vm_fragment_size": "-1", "no_such_option": "1"})
	if !state.Loaded || !slices.Equal(state.Mismatched, []string{"pcie_p2p"}) || !slices.Equal(state.Missing, []string{"no_such_option"}) {
		t.Errorf("loaded driver: report = %+v, want pcie_p2p mismatched and no_such_option missing", state)
	}

	writeFile(t, filepath.Join(params, "pcie_p2p"), "N\n")
	if state := runNodeState(t, root, map[string]string{"pcie_p2p": "0"}); len(state.Mismatched) != 0 || state.Parameters["pcie_p2p"] != "N" {
		t.Errorf("applied option: report = %+v, want pcie_p2p=N matching", state)
	}
}

// Option names are checked against the installed driver's own list before anything loads, so a
// misspelled option fails with the closest real name; an option the driver has but does not expose
// in sysfs is unverifiable rather than missing.
func TestAMDGPUNodeStateChecksOptionNames(t *testing.T) {
	root := t.TempDir()
	parm := filepath.Join(root, "modinfo-parm")
	writeFile(t, parm, "pcie_p2p:Enable PCIe P2P (requires large-BAR). (bool)\n"+
		"msi:MSI support (1 = enable, 0 = disable, -1 = auto) (int)\n"+
		"hidden_knob:Not readable in sysfs (int)\n")
	state := runNodeState(t, root, map[string]string{"pcei_p2p": "0", "msi": "1"}, "--parm-file", parm)
	if state.Loaded || !slices.Equal(state.Unknown, []string{"pcei_p2p"}) || state.Suggestions["pcei_p2p"] != "pcie_p2p" {
		t.Errorf("misspelled option: report = %+v, want pcei_p2p unknown with suggestion pcie_p2p", state)
	}

	params := filepath.Join(root, "sys", "module", "amdgpu", "parameters")
	writeFile(t, filepath.Join(params, "msi"), "1\n")
	state = runNodeState(t, root, map[string]string{"msi": "1", "hidden_knob": "3"}, "--parm-file", parm)
	if len(state.Unknown) != 0 || len(state.Missing) != 0 || !slices.Equal(state.Unverifiable, []string{"hidden_knob"}) || len(state.Mismatched) != 0 {
		t.Errorf("hidden option: report = %+v, want hidden_knob unverifiable and nothing missing", state)
	}

	if state := runNodeState(t, root, map[string]string{"pcei_p2p": "0"}); len(state.Unknown) != 0 {
		t.Errorf("no option list: unknown = %v, want the name check skipped", state.Unknown)
	}
}

// A memory-backed root (MAAS RAM deployments run on tmpfs or overlay) is where a reboot erases the
// operating system; the last mount on / is the one the system runs on.
func TestAMDGPUNodeStateRootFilesystem(t *testing.T) {
	for name, tc := range map[string]struct {
		mounts string
		want   bool
	}{
		"RAM deploy tmpfs":   {"tmpfs / tmpfs rw,relatime 0 0\nproc /proc proc rw 0 0\n", true},
		"overlay root":       {"overlay / overlay rw,lowerdir=/media/root-ro 0 0\n", true},
		"disk root":          {"/dev/sda2 / ext4 rw,relatime 0 0\n", false},
		"rootfs then disk":   {"rootfs / rootfs rw 0 0\n/dev/vda1 / xfs rw 0 0\n", false},
		"no mounts readable": {"", false},
	} {
		root := t.TempDir()
		if tc.mounts != "" {
			writeFile(t, filepath.Join(root, "proc", "self", "mounts"), tc.mounts)
		}
		if state := runNodeState(t, root, nil); state.MemoryBackedRoot != tc.want {
			t.Errorf("%s: memoryBackedRoot = %v (root %v), want %v", name, state.MemoryBackedRoot, state.RootFilesystem, tc.want)
		}
	}
}

// loaderTasks reads one task file of the amdgpu_loader role, flattening blocks so every task is
// visible with its own conditions.
func loaderTasks(t *testing.T, file string) []map[string]any {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "automation", "playbooks", "roles", "amdgpu_loader", "tasks", file)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var tasks []map[string]any
	if err := yaml.Unmarshal(content, &tasks); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	var flat []map[string]any
	var walk func([]map[string]any)
	walk = func(list []map[string]any) {
		for _, task := range list {
			flat = append(flat, task)
			if block, ok := task["block"].([]any); ok {
				var inner []map[string]any
				for _, item := range block {
					if m, ok := item.(map[string]any); ok {
						inner = append(inner, m)
					}
				}
				walk(inner)
			}
		}
	}
	walk(tasks)
	return flat
}

func taskConditions(task map[string]any) string {
	switch when := task["when"].(type) {
	case string:
		return when
	case []any:
		parts := make([]string, len(when))
		for i, part := range when {
			parts[i] = fmt.Sprint(part)
		}
		return strings.Join(parts, " and ")
	}
	return ""
}

// A RAM-deployed node loses its operating system on reboot, so the role's only reboot is guarded by
// the memory-backed-root fact; option names are refused before the options file is written; and an
// unload cannot hang the deployment.
func TestAMDGPULoaderNeverRebootsAMemoryBackedRoot(t *testing.T) {
	reboots := 0
	for _, file := range []string{"main.yml", "apply.yml", "reload.yml"} {
		for _, task := range loaderTasks(t, file) {
			if _, ok := task["ansible.builtin.reboot"]; !ok {
				continue
			}
			reboots++
			if when := taskConditions(task); file != "reload.yml" || !strings.Contains(when, "not (amdgpu_loader_release.stdout | from_json).memoryBackedRoot") {
				t.Errorf("%s: reboot task %q runs when %q, want only reload.yml guarded by not memoryBackedRoot", file, task["name"], when)
			}
		}
	}
	if reboots != 1 {
		t.Errorf("reboot tasks = %d, want exactly one", reboots)
	}

	order := map[string]int{}
	for i, task := range loaderTasks(t, "apply.yml") {
		order[fmt.Sprint(task["name"])] = i
	}
	refuse, okRefuse := order["Refuse an option the amdgpu driver does not have"]
	write, okWrite := order["Write the amdgpu module options"]
	if !okRefuse || !okWrite || refuse > write {
		t.Errorf("apply.yml order = %v, want the unknown-option refusal before the options file is written", order)
	}

	for _, task := range loaderTasks(t, "reload.yml") {
		if task["name"] != "Unload the amdgpu driver" {
			continue
		}
		command, _ := task["ansible.builtin.command"].(map[string]any)
		argv, _ := command["argv"].([]any)
		if len(argv) < 5 || argv[0] != "timeout" || argv[2] != "modprobe" || argv[3] != "-r" {
			t.Errorf("unload argv = %v, want timeout N modprobe -r amdgpu", argv)
		}
	}
}

// The module reference count and the processes holding a GPU device decide whether the driver can
// be unloaded and loaded again in place.
func TestAMDGPUNodeStateReportsHolders(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "sys", "module", "amdgpu", "refcnt"), "2\n")
	holder := filepath.Join(root, "proc", "4242")
	writeFile(t, filepath.Join(holder, "comm"), "omnistat-monitor\n")
	bystander := filepath.Join(root, "proc", "77")
	writeFile(t, filepath.Join(bystander, "comm"), "sshd\n")
	for path, target := range map[string]string{
		filepath.Join(holder, "fd", "3"):    "/dev/null",
		filepath.Join(holder, "fd", "4"):    "/dev/kfd",
		filepath.Join(holder, "fd", "5"):    "/dev/dri/renderD129",
		filepath.Join(bystander, "fd", "0"): "/dev/dri/card0",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
	}
	state := runNodeState(t, root, nil)
	if state.Refcnt == nil || *state.Refcnt != 2 || !slices.Equal(state.Holders, []string{"4242 omnistat-monitor"}) {
		t.Errorf("held driver: refcnt = %v, holders = %v, want 2 and the process with /dev/kfd open", state.Refcnt, state.Holders)
	}

	free := t.TempDir()
	writeFile(t, filepath.Join(free, "sys", "module", "amdgpu", "refcnt"), "0\n")
	if state := runNodeState(t, free, nil); state.Refcnt == nil || *state.Refcnt != 0 || len(state.Holders) != 0 {
		t.Errorf("free driver: refcnt = %v, holders = %v, want 0 and none", state.Refcnt, state.Holders)
	}
}
