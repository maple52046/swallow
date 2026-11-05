package ansible

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// k0sPreflightTasks is the k0s_host_preflight task file, which deploy-kubernetes runs on every node
// before any node bootstraps or joins the cluster.
var k0sPreflightTasks = filepath.Join("roles", "k0s_host_preflight", "tasks", "main.yml")

// A node the deploy provisions with automatic or DHCP addressing gets its address only then, so the
// deploy API cannot check it at acceptance. k0s_host_preflight refuses, on every node and before the
// cluster forms, an execution-time address (ansible_host) inside the pod or service CIDR, and stops
// the whole run for it.
func TestK0sPreflightRefusesANodeAddressInsideTheClusterNetworks(t *testing.T) {
	path := shippedFile(t, k0sPreflightTasks)
	task := namedTask(t, path, "Refuse a node address inside the pod or service CIDR")
	if task["any_errors_fatal"] != true {
		t.Errorf("the CIDR check any_errors_fatal = %v, want true so one node stops every node", task["any_errors_fatal"])
	}
	args, _ := task["ansible.builtin.fail"].(map[string]any)
	if msg, _ := args["msg"].(string); !strings.HasPrefix(msg, "[node_address_in_cluster_network] ") {
		t.Errorf("the CIDR check message = %q, want it tagged [node_address_in_cluster_network]", msg)
	}
	if want := "k0s_pf_covering_networks | length > 0"; task["when"] != want {
		t.Errorf("the CIDR check when = %v, want %q", task["when"], want)
	}

	python := requireJinja(t)
	vars, _ := task["vars"].(map[string]any)
	template, _ := vars["k0s_pf_covering_networks"].(string)
	tests := []struct {
		name                 string
		node, pod, service   string
		wantCoveringNetworks string
	}{
		{name: "an address outside both networks", node: "10.170.169.143", pod: "10.244.0.0/16", service: "10.96.0.0/12", wantCoveringNetworks: ""},
		{name: "an address inside the pod CIDR", node: "10.244.3.7", pod: "10.244.0.0/16", service: "10.96.0.0/12", wantCoveringNetworks: "the pod CIDR 10.244.0.0/16"},
		{name: "an address inside the service CIDR", node: "10.111.0.9", pod: "10.244.0.0/16", service: "10.96.0.0/12", wantCoveringNetworks: "the service CIDR 10.96.0.0/12"},
		{name: "the last address of the pod CIDR", node: "10.244.255.255", pod: "10.244.0.0/16", service: "10.96.0.0/12", wantCoveringNetworks: "the pod CIDR 10.244.0.0/16"},
		{name: "the first address after the pod CIDR", node: "10.245.0.0", pod: "10.244.0.0/16", service: "10.96.0.0/12", wantCoveringNetworks: ""},
		{name: "a wide pod CIDR over the node network", node: "10.170.169.143", pod: "10.0.0.0/8", service: "10.96.0.0/12", wantCoveringNetworks: "the pod CIDR 10.0.0.0/8"},
		{name: "both networks over the address", node: "10.170.169.143", pod: "10.0.0.0/8", service: "10.170.0.0/16", wantCoveringNetworks: "the pod CIDR 10.0.0.0/8 and the service CIDR 10.170.0.0/16"},
		{name: "a host-length CIDR equal to the address", node: "10.170.169.143", pod: "10.170.169.143/32", service: "10.96.0.0/12", wantCoveringNetworks: "the pod CIDR 10.170.169.143/32"},
		{name: "a host name is not compared", node: "mi308-1.maas", pod: "10.244.0.0/16", service: "10.96.0.0/12", wantCoveringNetworks: ""},
		{name: "IPv6 networks are not compared", node: "10.170.169.143", pod: "fd00::/8", service: "fd01::/108", wantCoveringNetworks: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := renderJinja(t, python, template, map[string]any{
				"ansible_host": tc.node, "swallow_k0s_pod_cidr": tc.pod, "swallow_k0s_service_cidr": tc.service,
			})
			if got != tc.wantCoveringNetworks {
				t.Errorf("k0s_pf_covering_networks for %s in pod %s, service %s = %q, want %q",
					tc.node, tc.pod, tc.service, got, tc.wantCoveringNetworks)
			}
		})
	}
}

// In HA, keepalived claims the API VIP on a control-plane node, so a VIP that is already a node's
// execution-time address is refused on that node, which stops every node.
func TestK0sPreflightRefusesAnHAVIPThatIsANodeAddress(t *testing.T) {
	path := shippedFile(t, k0sPreflightTasks)
	task := namedTask(t, path, "Refuse an HA API VIP that is a node's address")
	if task["any_errors_fatal"] != true {
		t.Errorf("the VIP check any_errors_fatal = %v, want true so one node stops every node", task["any_errors_fatal"])
	}
	args, _ := task["ansible.builtin.fail"].(map[string]any)
	if msg, _ := args["msg"].(string); !strings.HasPrefix(msg, "[api_vip_is_node_address] ") {
		t.Errorf("the VIP check message = %q, want it tagged [api_vip_is_node_address]", msg)
	}
	conditions, _ := task["when"].([]any)
	if len(conditions) != 2 || conditions[0] != "swallow_k0s_high_availability | default(false) | bool" {
		t.Fatalf("the VIP check when = %v, want the HA gate and the address comparison", task["when"])
	}

	python := requireJinja(t)
	comparison, _ := conditions[1].(string)
	tests := []struct {
		name, vip, node string
		wantRefused     string
	}{
		{name: "the VIP is the node's address", vip: "192.168.100.200", node: "192.168.100.200", wantRefused: "True"},
		{name: "the VIP is padded with spaces", vip: " 192.168.100.200 ", node: "192.168.100.200", wantRefused: "True"},
		{name: "the VIP is another address", vip: "192.168.100.200", node: "192.168.100.2", wantRefused: "False"},
		{name: "no VIP", vip: "", node: "192.168.100.2", wantRefused: "False"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := renderJinja(t, python, "{{ "+comparison+" }}", map[string]any{
				"swallow_k0s_api_vip": tc.vip, "ansible_host": tc.node,
			})
			if got != tc.wantRefused {
				t.Errorf("VIP check for VIP %q on node %q = %s, want %s", tc.vip, tc.node, got, tc.wantRefused)
			}
		})
	}
}

// The address checks protect the cluster only if every node passes them before the initial
// controller bootstraps it. Their messages cannot name a node by k0s_node_name: the play that runs
// k0s_host_preflight sets it in its tasks, which Ansible runs after the play's roles, so a failure
// message using it would itself fail on an undefined variable.
func TestK0sPreflightRunsBeforeTheClusterBootstraps(t *testing.T) {
	content, err := os.ReadFile(shippedFile(t, "deploy-kubernetes.yml"))
	if err != nil {
		t.Fatal(err)
	}
	preflight := strings.Index(string(content), "role: k0s_host_preflight")
	bootstrap := strings.Index(string(content), "role: k0s_controller_bootstrap")
	if preflight < 0 || bootstrap < 0 || preflight > bootstrap {
		t.Errorf("deploy-kubernetes.yml runs k0s_host_preflight at %d and k0s_controller_bootstrap at %d, want the preflight first", preflight, bootstrap)
	}

	tasks, err := os.ReadFile(shippedFile(t, k0sPreflightTasks))
	if err != nil {
		t.Fatal(err)
	}
	for number, line := range strings.Split(string(tasks), "\n") {
		if code, _, _ := strings.Cut(line, "#"); strings.Contains(code, "k0s_node_name") {
			t.Errorf("%s:%d reads k0s_node_name, which is undefined while the role runs", k0sPreflightTasks, number+1)
		}
	}
}
