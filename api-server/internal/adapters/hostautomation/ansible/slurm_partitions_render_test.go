package ansible

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// renderSlurmConf runs slurm_config's partition derivation and its slurm.conf template for a
// controller (localhost) and two compute nodes, g1 (64 CPUs, 512000 MiB) and g2 (16 CPUs, 64000
// MiB), and returns the rendered file.
func renderSlurmConf(t *testing.T, vars map[string]any, features map[string][]string, weights map[string]int) string {
	t.Helper()
	role := roleDir(t, "slurm_config")
	derive := namedTask(t, filepath.Join(role, "tasks", "main.yml"), "Derive each partition's nodes and default memory per CPU")
	template, err := filepath.Abs(filepath.Join(role, "templates", "slurm.conf.j2"))
	if err != nil {
		t.Fatal(err)
	}
	run := newPlayRun(t, "slurm_config", "")
	dest := filepath.Join(run.dir, "slurm.conf")
	play := []map[string]any{{
		"hosts": "localhost", "gather_facts": false,
		"tasks": []any{derive, map[string]any{
			"name": "Render", "ansible.builtin.template": map[string]any{"src": template, "dest": dest},
		}},
	}}
	raw, err := yaml.Marshal(play)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run.dir, "play.yml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	node := func(name string, cpus, memory int) map[string]any {
		host := map[string]any{
			"ansible_host": "10.0.0." + name[1:], "slurm_node_name": name, "slurm_node_cpus": cpus,
			"slurm_node_schedulable_memory": memory,
			"slurm_node_line":               "NodeName=" + name + " CPUs=" + strconv.Itoa(cpus) + " RealMemory=999999 Feature=old",
		}
		if list := features[name]; len(list) > 0 {
			host["slurm_node_features"] = list
		}
		if weight, ok := weights[name]; ok {
			host["slurm_node_weight"] = weight
		}
		return host
	}
	inventory := map[string]any{"all": map[string]any{"hosts": map[string]any{
		"localhost": map[string]any{"ansible_host": "10.0.0.1", "slurm_node_name": "c1"},
		"g1":        node("g1", 64, 512000),
		"g2":        node("g2", 16, 64000),
	}}}
	raw, err = yaml.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	run.inventory = filepath.Join(run.dir, "inventory.yml")
	if err := os.WriteFile(run.inventory, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	all := map[string]any{
		"swallow_slurm_cluster_name": "lab", "swallow_slurm_controller_ids": []string{"localhost"},
		"swallow_slurm_compute_ids": []string{"g1", "g2"}, "slurm_jwt_key_path": "/etc/slurm/jwt.key",
		"slurm_state_save_location": "/var/spool/slurmctld", "slurm_def_mem_per_cpu": 4000,
		"slurm_conf_dir": "/etc/slurm", "slurm_log_dir": "/var/log/slurm", "slurm_omnistat_enabled": false,
	}
	for key, value := range vars {
		all[key] = value
	}
	output, _, failed := run.run(all)
	if failed != 0 {
		t.Fatalf("the render failed:\n%s", output)
	}
	conf, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	return string(conf)
}

// partitionLines returns the PartitionName lines of a rendered slurm.conf.
func partitionLines(conf string) []string {
	var lines []string
	for _, line := range strings.Split(conf, "\n") {
		if strings.HasPrefix(line, "PartitionName=") {
			lines = append(lines, line)
		}
	}
	return lines
}

// A Platform from before partitions renders the partition it always had, and the same file from
// the partition its upgraded spec stores: one default partition over every node, no time limit,
// jobs sharing nodes, and the global DefMemPerCPU.
func TestSlurmConfRendersTheLegacyPartitionUnchanged(t *testing.T) {
	legacy := renderSlurmConf(t, map[string]any{}, nil, nil)
	upgraded := renderSlurmConf(t, map[string]any{"swallow_slurm_partitions": []any{map[string]any{
		"name": "main", "default": true, "nodes": []string{"g1", "g2"}, "max_time": "INFINITE",
		"def_mem_per_cpu": 0, "over_subscribe": "", "allow_groups": []string{},
	}}}, nil, nil)
	want := "PartitionName=main Nodes=ALL Default=YES MaxTime=INFINITE State=UP OverSubscribe=NO"
	if lines := partitionLines(legacy); len(lines) != 1 || lines[0] != want {
		t.Errorf("legacy partitions = %q, want %q", lines, want)
	}
	if lines := partitionLines(upgraded); len(lines) != 1 || lines[0] != want {
		t.Errorf("upgraded partitions = %q, want %q", lines, want)
	}
	strip := func(conf string) string {
		var kept []string
		for _, line := range strings.Split(conf, "\n") {
			if !strings.HasPrefix(line, "#") {
				kept = append(kept, line)
			}
		}
		return strings.Join(kept, "\n")
	}
	if strip(legacy) != strip(upgraded) {
		t.Errorf("the upgraded spec renders another slurm.conf:\n--- legacy\n%s\n--- upgraded\n%s", legacy, upgraded)
	}
}

// Each partition lists its own nodes and limits; one without defMemPerCpu takes the smallest
// per-CPU share of its own nodes when that differs from the global default; nodes carry their
// Features and Weight in place of the ones slurmd -C reported.
func TestSlurmConfRendersEachPartitionAndNodeFeatures(t *testing.T) {
	conf := renderSlurmConf(t, map[string]any{"swallow_slurm_partitions": []any{
		map[string]any{"name": "main", "default": true, "nodes": []string{"g1", "g2"}, "max_time": "INFINITE"},
		map[string]any{"name": "big", "default": false, "nodes": []string{"g1"}, "max_time": "2-00:00:00",
			"over_subscribe": "EXCLUSIVE", "allow_groups": []string{"hpc"}},
		map[string]any{"name": "small", "default": false, "nodes": []string{"g2"}, "max_time": "60", "def_mem_per_cpu": 2048},
	}}, map[string][]string{"g1": {"mi300x", "big"}, "g2": {"mi250x", "gpu_degraded"}}, map[string]int{"g2": 100})
	lines := partitionLines(conf)
	want := []string{
		"PartitionName=main Nodes=ALL Default=YES MaxTime=INFINITE State=UP OverSubscribe=NO",
		"PartitionName=big Nodes=g1 Default=NO MaxTime=2-00:00:00 State=UP OverSubscribe=EXCLUSIVE DefMemPerCPU=8000 AllowGroups=hpc",
		"PartitionName=small Nodes=g2 Default=NO MaxTime=60 State=UP OverSubscribe=NO DefMemPerCPU=2048",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("partitions =\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	for _, line := range []string{
		"NodeName=g1 CPUs=64 RealMemory=512000 Features=mi300x,big NodeAddr=10.0.0.1 State=UNKNOWN",
		"NodeName=g2 CPUs=16 RealMemory=64000 Features=mi250x,gpu_degraded Weight=100 NodeAddr=10.0.0.2 State=UNKNOWN",
	} {
		if !strings.Contains(conf, line+"\n") {
			t.Errorf("slurm.conf has no line %q:\n%s", line, conf)
		}
	}
	if strings.Contains(conf, "Feature=old") {
		t.Error("slurm.conf keeps the Feature slurmd -C reported")
	}
}
