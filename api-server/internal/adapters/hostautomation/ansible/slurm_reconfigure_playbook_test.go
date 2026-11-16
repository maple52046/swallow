package ansible

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reconfigure-slurm renders and applies the controllers' configuration and nothing else: it detects
// only the rescanned nodes, reads the others' records, starts slurmd only on the rescanned nodes,
// and installs no package, mount, or service. deploy-slurm classifies and detects through the same
// roles, so both see the cluster the same way.
func TestReconfigureSlurmOnlyRendersTheConfiguration(t *testing.T) {
	reconfigure := slurmPlaybookFile(t, "reconfigure-slurm.yml")
	for _, want := range []string{
		"role: slurm_facts",
		"name: slurm_node_hardware",
		`tasks_from: "{{ 'main' if inventory_hostname in (swallow_slurm_rescan_ids | default([])) else 'recorded' }}"`,
		"hosts: slurm_primary", "role: slurm_config", "role: slurm_controller",
		"hosts: slurm_controller:!slurm_primary",
		"name: slurm_compute", "when: inventory_hostname in (swallow_slurm_rescan_ids | default([]))",
		"role: slurm_verify",
	} {
		if !strings.Contains(reconfigure, want) {
			t.Errorf("reconfigure-slurm.yml does not contain %q", want)
		}
	}
	for _, unwanted := range []string{
		"artifact_sources", "slurm_munge", "slurm_packages_verify", "slurm_preflight", "slurm_state_server",
		"slurm_controller_state", "slurm_job_hooks", "slurm_oci", "slurm_pmix", "slurm_login",
		"slurm_workload_storage", "slurm_slurmrestd", "slurm_cluster_credential",
	} {
		if strings.Contains(reconfigure, unwanted) {
			t.Errorf("reconfigure-slurm.yml runs %s; it must only render and apply the configuration", unwanted)
		}
	}
	if strings.Index(reconfigure, "Reconfigure the primary controller") > strings.Index(reconfigure, "Reconfigure the backup controllers") {
		t.Error("reconfigure-slurm.yml must reconfigure the primary before the backups")
	}
	deploy := slurmPlaybookFile(t, "deploy-slurm.yml")
	for _, want := range []string{"role: slurm_facts", "role: slurm_node_hardware"} {
		if !strings.Contains(deploy, want) {
			t.Errorf("deploy-slurm.yml does not use %s", want)
		}
	}
	if !strings.Contains(slurmPlaybookFile(t, "roles", "slurm_node_cleanup", "tasks", "main.yml"), "- /var/lib/swallow/slurm\n") {
		t.Error("slurm_node_cleanup leaves the hardware record")
	}
}

// A node's recorded hardware gives the controller the same node line, schedulable memory, GRES, and
// Features a detection would, derived with the deployment's variables of the day: a group that now
// expects more GPUs than the node recorded marks it gpu_degraded. A node without a record, or with a
// record of another version, is detected instead.
func TestSlurmNodeHardwareRecordIsReadWithoutDetecting(t *testing.T) {
	run := newPlayRun(t, "slurm_node_hardware", `
- hosts: localhost
  gather_facts: false
  tasks:
    - ansible.builtin.include_role:
        name: slurm_node_hardware
        tasks_from: recorded
    - ansible.builtin.copy:
        dest: "{{ report_path }}"
        content: "{{ {'line': slurm_node_line, 'cpus': slurm_node_cpus | int, 'memory': slurm_node_schedulable_memory | int,
                      'gres': slurm_node_gres, 'features': slurm_node_features, 'weight': slurm_node_weight | string} | to_json }}"
`)
	record := filepath.Join(run.dir, "node-hardware.json")
	reportPath := filepath.Join(run.dir, "report.json")
	gpus := make([]map[string]any, 0, 15)
	for i := 0; i < 15; i++ {
		gpus = append(gpus, map[string]any{"minor": 128 + i, "type": "gfx942"})
	}
	writeRecord := func(version int) {
		raw, _ := json.Marshal(map[string]any{
			"version": version, "nodeLine": "NodeName=n1 CPUs=92 Boards=1 SocketsPerBoard=1 CoresPerSocket=92 ThreadsPerCore=1 RealMemory=458000",
			"gpus": gpus, "modelFeatures": []string{"mi308x"},
		})
		if err := os.WriteFile(record, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	vars := func() map[string]any {
		return map[string]any{
			"report_path": reportPath, "slurm_node_hardware_record": record, "slurm_node_name": "n1",
			"slurm_memory_reserve_percent": 5, "slurm_memory_reserve_min_mib": 2048, "slurm_memory_reserve_max_mib": 16384,
			"swallow_slurm_gpu_compute_ids": []string{"localhost"},
			"swallow_slurm_node_features":   map[string]any{"localhost": []string{"solo"}},
			"swallow_slurm_expected_gpus":   map[string]any{"localhost": 16},
		}
	}

	writeRecord(1)
	output, _, failed := run.run(vars())
	if failed != 0 {
		t.Fatalf("reading the record failed:\n%s", output)
	}
	raw, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Line     string   `json:"line"`
		CPUs     int      `json:"cpus"`
		Memory   int      `json:"memory"`
		GRES     string   `json:"gres"`
		Features []string `json:"features"`
		Weight   string   `json:"weight"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(report.Line, "NodeName=n1 CPUs=92") || report.CPUs != 92 || report.Memory != 458000-16384 ||
		report.GRES != "gpu:gfx942:15" || report.Weight != "100" ||
		fmt.Sprint(report.Features) != "[solo mi308x gfx942 gpu_degraded]" {
		t.Errorf("recorded node = %+v; want its line, 92 CPUs, %d MiB, gpu:gfx942:15, and gpu_degraded with Weight 100", report, 458000-16384)
	}
	if strings.Contains(output, "slurmd -C") || strings.Contains(output, "Load amdgpu") {
		t.Errorf("a node with a record was detected again:\n%s", output)
	}

	for name, prepare := range map[string]func(){
		"no record":             func() { _ = os.Remove(record) },
		"a record of version 0": func() { writeRecord(0) },
	} {
		prepare()
		output, _, failed := run.run(vars())
		if failed == 0 || !strings.Contains(output, "Read the slurmd-detected node line") {
			t.Errorf("%s: the node was not detected (failed %d):\n%s", name, failed, output)
		}
	}
}

// scale-out-slurm installs only the nodes it adds: every install role runs under the new-node guard,
// the other compute nodes only read their records, the controllers render and apply the configuration
// before the new nodes start slurmd, and the cluster's own services are not touched. slurm_verify waits
// only for the new nodes.
func TestScaleOutSlurmInstallsOnlyTheNewNodes(t *testing.T) {
	scaleOut := slurmPlaybookFile(t, "scale-out-slurm.yml")
	guard := "inventory_hostname in swallow_slurm_scale_out_ids"
	for _, want := range []string{
		"role: package_maintenance_hold", "name: artifact_sources", "role: slurm_facts",
		"name: time_sync_check", "name: slurm_preflight", "name: slurm_packages_verify", "name: slurm_munge",
		`tasks_from: "{{ 'main' if inventory_hostname in swallow_slurm_scale_out_ids else 'recorded' }}"`,
		"hosts: slurm_primary", "role: slurm_config", "role: slurm_controller",
		"hosts: slurm_controller:!slurm_primary",
		"name: slurm_job_hooks", "name: slurm_oci", "name: slurm_pmix", "name: slurm_compute",
		"role: slurm_workload_storage_server", "name: slurm_workload_storage_client",
		"role: slurm_verify", "slurm_verify_nodes:",
	} {
		if !strings.Contains(scaleOut, want) {
			t.Errorf("scale-out-slurm.yml does not contain %q", want)
		}
	}
	for _, unwanted := range []string{
		"slurm_state_server", "slurm_controller_state", "slurm_login", "slurm_slurmrestd", "slurm_cluster_credential",
	} {
		if strings.Contains(scaleOut, unwanted) {
			t.Errorf("scale-out-slurm.yml runs %s; the cluster's services stay as they are", unwanted)
		}
	}
	// Every host-local step that installs or starts something on a node is limited to the new nodes:
	// the artifact sources, three checks, MUNGE, the hardware detection, four compute roles, and the mount.
	if got := strings.Count(scaleOut, guard); got < 11 {
		t.Errorf("scale-out-slurm.yml guards %d steps with %q; every install step on a node must be guarded", got, guard)
	}
	order := []string{"Give the new nodes the cluster's MUNGE key", "Read the compute nodes' hardware",
		"Reconfigure the primary controller", "Reconfigure the backup controllers",
		"Configure the new compute nodes and start slurmd", "Export the shared workload filesystem to the new nodes",
		"Mount the shared workload filesystem on the new nodes", "Verify the new nodes joined"}
	for i := 1; i < len(order); i++ {
		if strings.Index(scaleOut, order[i-1]) > strings.Index(scaleOut, order[i]) {
			t.Errorf("scale-out-slurm.yml runs %q after %q", order[i-1], order[i])
		}
	}
	verify := slurmPlaybookFile(t, "roles", "slurm_verify", "tasks", "main.yml")
	if !strings.Contains(verify, "--nodes=") || !strings.Contains(verify, "slurm_verify_nodes | default(swallow_slurm_compute_ids, true)") {
		t.Error("slurm_verify does not narrow its wait to slurm_verify_nodes")
	}
}
