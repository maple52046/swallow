package infra

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// amdgpuNodeState is the JSON report of roles/amdgpu_loader/files/amdgpu_node_state.py.
type amdgpuNodeState struct {
	Loaded      bool              `json:"loaded"`
	Parameters  map[string]string `json:"parameters"`
	Mismatched  []string          `json:"mismatched"`
	Missing     []string          `json:"missing"`
	GPUNodes    int               `json:"gpuNodes"`
	Rules       map[string]string `json:"rules"`
	RuleReasons map[string]string `json:"ruleReasons"`
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

func runNodeState(t *testing.T, root string, desired map[string]string) amdgpuNodeState {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not available")
	}
	script := filepath.Join("..", "..", "..", "automation", "playbooks", "roles", "amdgpu_loader", "files", "amdgpu_node_state.py")
	encoded, _ := json.Marshal(desired)
	out, err := exec.Command(python, script, "--root", root, "--desired", base64.StdEncoding.EncodeToString(encoded)).Output()
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
