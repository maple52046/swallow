package slurm

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// threeNodeSpec is a Platform with a controller-and-login node and two compute nodes, and no node
// groups or partitions: what a Platform deployed before groups existed stores.
func threeNodeSpec() platformdomain.PlatformSpec {
	return platformdomain.PlatformSpec{
		Members: []platformdomain.PlatformMember{
			{ServerID: "c1", Roles: []string{RoleController, RoleLogin}},
			{ServerID: "g1", Roles: []string{RoleCompute}},
			{ServerID: "g2", Roles: []string{RoleCompute}},
		},
		Settings: map[string]any{"slurm": map[string]any{"clusterName": "lab"}},
	}
}

// grouped is spec with explicit groups and partitions.
func grouped(spec platformdomain.PlatformSpec, groups []any, partitions []any) platformdomain.PlatformSpec {
	out := spec.Clone()
	settings := slurmSettings(out)
	settings["nodeGroups"], settings["partitions"] = groups, partitions
	out.Settings["slurm"] = settings
	return out
}

func group(name, role string, expected int, members ...string) map[string]any {
	return map[string]any{"name": name, "role": role, "expectedGpusPerNode": expected, "members": anyStrings(members)}
}

func part(name string, isDefault bool, groups ...string) map[string]any {
	return map[string]any{"name": name, "default": isDefault, "groups": anyStrings(groups)}
}

// A spec without groups reads as one group per role and the one default partition over every
// compute node; UpgradeSpec stores that form once, and reports nothing to do afterwards.
func TestALegacySpecUpgradesToTheGroupsItsRolesImply(t *testing.T) {
	legacy := threeNodeSpec()
	groups := Platform{}.NodeGroups(legacy)
	want := []platformdomain.NodeGroup{
		{Name: RoleController, Role: RoleController, Members: []string{"c1"}},
		{Name: RoleLogin, Role: RoleLogin, Members: []string{"c1"}},
		{Name: RoleCompute, Role: RoleCompute, Members: []string{"g1", "g2"}},
	}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("NodeGroups(legacy) = %+v, want %+v", groups, want)
	}
	upgraded, changed := Platform{}.UpgradeSpec(legacy)
	if !changed {
		t.Fatal("UpgradeSpec(legacy) reports no change")
	}
	partitions, _ := slurmSettings(upgraded)["partitions"].([]any)
	if len(partitions) != 1 || partitions[0].(map[string]any)["name"] != defaultPartitionName ||
		partitions[0].(map[string]any)["default"] != true || partitions[0].(map[string]any)["maxTime"] != defaultMaxTime {
		t.Errorf("upgraded partitions = %+v", partitions)
	}
	if _, again := (Platform{}).UpgradeSpec(upgraded); again {
		t.Error("UpgradeSpec(upgraded) reports a change")
	}
	if slurmSettings(legacy)["nodeGroups"] != nil {
		t.Error("UpgradeSpec changed its argument")
	}
}

// The rendered topology lists each partition's nodes, gives each compute node its GPU-model and
// group features, and warns about a node with fewer GPUs than its group expects.
func TestDeploymentTopologyRendersPartitionsFeaturesAndExpectedGPUs(t *testing.T) {
	spec := grouped(threeNodeSpec(),
		[]any{group("ctl", RoleController, 0, "c1"), group("login", RoleLogin, 0, "c1"),
			group("mi300x", RoleCompute, 8, "g1"), group("mi250", RoleCompute, 0, "g2")},
		[]any{part("main", true, "mi300x", "mi250"), map[string]any{"name": "big", "groups": []any{"mi300x"}, "maxTime": "2-00:00:00", "defMemPerCpu": 4096}})
	top, err := Platform{}.DeploymentTopology(spec, []platformdomain.TopologyTarget{
		{ServerID: "g1", Name: "gpu-node-1", ComputeGPUs: 7, GPUModels: []string{"Aqua Vanjaram [Instinct MI300X]"}},
		{ServerID: "g2", ComputeGPUs: 2, GPUModels: []string{"Aldebaran/MI200 [Instinct MI250X/MI250]"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	partitions := top.Vars[varPartitions].([]any)
	main, big := partitions[0].(map[string]any), partitions[1].(map[string]any)
	if !reflect.DeepEqual(main["nodes"], []any{"g1", "g2"}) || main["default"] != true || main["max_time"] != defaultMaxTime {
		t.Errorf("main = %+v", main)
	}
	if !reflect.DeepEqual(big["nodes"], []any{"g1"}) || big["default"] != false || big["max_time"] != "2-00:00:00" || big["def_mem_per_cpu"] != int64(4096) {
		t.Errorf("big = %+v", big)
	}
	features := top.Vars[varNodeFeatures].(map[string]any)
	if !reflect.DeepEqual(features["g1"], []any{"mi300x"}) || !reflect.DeepEqual(features["g2"], []any{"mi250x", "mi250"}) {
		t.Errorf("features = %+v", features)
	}
	if features["c1"] != nil {
		t.Errorf("a control-plane node has features %v", features["c1"])
	}
	if expected := top.Vars[varExpectedGPUs].(map[string]any); !reflect.DeepEqual(expected, map[string]any{"g1": 8}) {
		t.Errorf("expected GPUs = %+v", expected)
	}
	if len(top.Warnings) != 1 || !strings.HasPrefix(top.Warnings[0], "server gpu-node-1 in node group mi300x has 7 of the 8 GPUs") ||
		!strings.Contains(top.Warnings[0], degradedFeature) {
		t.Errorf("warnings = %v", top.Warnings)
	}
}

// Each rule the groups and partitions must follow is refused with ErrInvalidDeployment.
func TestDeploymentTopologyRefusesAnInvalidTopology(t *testing.T) {
	base := []any{group("controller", RoleController, 0, "c1"), group("login", RoleLogin, 0, "c1")}
	for name, spec := range map[string]platformdomain.PlatformSpec{
		"a member in no group": grouped(threeNodeSpec(), append(slices.Clone(base), group("gpu", RoleCompute, 0, "g1")),
			[]any{part("main", true, "gpu")}),
		"a group naming a non-member": grouped(threeNodeSpec(), append(slices.Clone(base), group("gpu", RoleCompute, 0, "g1", "g2", "x9")),
			[]any{part("main", true, "gpu")}),
		"no default partition": grouped(threeNodeSpec(), append(slices.Clone(base), group("gpu", RoleCompute, 0, "g1", "g2")),
			[]any{part("main", false, "gpu")}),
		"two default partitions": grouped(threeNodeSpec(), append(slices.Clone(base), group("a", RoleCompute, 0, "g1"), group("b", RoleCompute, 0, "g2")),
			[]any{part("pa", true, "a"), part("pb", true, "b")}),
		"a partition over a login group": grouped(threeNodeSpec(), append(slices.Clone(base), group("gpu", RoleCompute, 0, "g1", "g2")),
			[]any{part("main", true, "gpu", "login")}),
		"a compute group in no partition": grouped(threeNodeSpec(), append(slices.Clone(base), group("a", RoleCompute, 0, "g1"), group("b", RoleCompute, 0, "g2")),
			[]any{part("main", true, "a")}),
		"a bad group name": grouped(threeNodeSpec(), append(slices.Clone(base), group("GPU_Nodes", RoleCompute, 0, "g1", "g2")),
			[]any{part("main", true, "GPU_Nodes")}),
		"a bad max time": grouped(threeNodeSpec(), append(slices.Clone(base), group("gpu", RoleCompute, 0, "g1", "g2")),
			[]any{map[string]any{"name": "main", "default": true, "groups": []any{"gpu"}, "maxTime": "forever"}}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := (Platform{}).DeploymentTopology(spec, nil); !errors.Is(err, platformdomain.ErrInvalidDeployment) {
				t.Errorf("DeploymentTopology() error = %v, want ErrInvalidDeployment", err)
			}
		})
	}
}

// A spec without compute nodes says so, before its partitions would report only that they have no
// nodes left.
func TestDeploymentTopologyNeedsAComputeNode(t *testing.T) {
	applied, _ := Platform{}.UpgradeSpec(threeNodeSpec())
	none := withoutMember(withoutMember(applied.Clone(), "g1"), "g2")
	if _, err := (Platform{}).DeploymentTopology(none, nil); !errors.Is(err, platformdomain.ErrInvalidDeployment) ||
		!strings.Contains(err.Error(), "needs at least one compute node; to remove the last one, uninstall the Platform instead") {
		t.Errorf("DeploymentTopology() error = %v", err)
	}
}

// A legacy spec renders the partition it was deployed with: one default partition over every
// compute node with no time limit.
func TestALegacySpecRendersTheSinglePartition(t *testing.T) {
	top, err := Platform{}.DeploymentTopology(threeNodeSpec(), nil)
	if err != nil {
		t.Fatal(err)
	}
	partitions := top.Vars[varPartitions].([]any)
	if len(partitions) != 1 || !reflect.DeepEqual(partitions[0].(map[string]any)["nodes"], []any{"g1", "g2"}) {
		t.Errorf("partitions = %+v", partitions)
	}
}

// Removing compute nodes is a destructive scale-in; adding a compute node converges by deploying;
// the control plane, removing the last compute node, and mixing adds with removals are blocked.
func TestMemberChangesScaleComputeOnly(t *testing.T) {
	applied, _ := Platform{}.UpgradeSpec(threeNodeSpec())

	removed := applied.Clone()
	removed.Members = removed.Members[:2]
	removed = withoutMember(removed, "g2")
	changes, err := Platform{}.Changes(&applied, removed)
	if err != nil || !changes.Destructive() || changes.Realization != platformdomain.RealizeScaleIn || len(changes.Blockers) != 0 {
		t.Errorf("remove a compute node = %+v, %v; want a destructive scale-in", changes, err)
	}

	added := applied.Clone()
	added.Members = append(added.Members, platformdomain.PlatformMember{ServerID: "g3", Roles: []string{RoleCompute}})
	added = withMember(added, "compute", "g3")
	changes, err = Platform{}.Changes(&applied, added)
	if err != nil || changes.Destructive() || changes.Realization != platformdomain.RealizeDeploy || len(changes.Blockers) != 0 {
		t.Errorf("add a compute node = %+v, %v; want a direct deploy", changes, err)
	}

	controller := applied.Clone()
	controller.Members = append(controller.Members, platformdomain.PlatformMember{ServerID: "c2", Roles: []string{RoleController}})
	controller = withMember(controller, "controller", "c2")
	if changes, _ = (Platform{}).Changes(&applied, controller); len(changes.Blockers) == 0 {
		t.Errorf("add a controller = %+v; want it blocked", changes)
	}

	mixed := added.Clone()
	mixed = withoutMember(mixed, "g1")
	if changes, _ = (Platform{}).Changes(&applied, mixed); !containsText(changes.Blockers, "separate changes") {
		t.Errorf("add and remove together = %+v; want it blocked", changes)
	}

	last := withoutMember(withoutMember(applied.Clone(), "g1"), "g2")
	if changes, _ = (Platform{}).Changes(&applied, last); !containsText(changes.Blockers, "last compute node") {
		t.Errorf("remove every compute node = %+v; want it blocked", changes)
	}
}

// A new partition converges in place; removing one is destructive.
func TestPartitionChanges(t *testing.T) {
	applied, _ := Platform{}.UpgradeSpec(threeNodeSpec())
	top := readTopology(applied)
	top.partitions = append(slices.Clone(top.partitions), partition{Name: "debug", Groups: []string{"compute"}})
	added := withTopology(applied, top)
	changes, err := Platform{}.Changes(&applied, added)
	if err != nil || changes.Destructive() || len(changes.Actions) != 1 || changes.Actions[0].Action != "add-partition" {
		t.Errorf("add a partition = %+v, %v", changes, err)
	}
	changes, _ = Platform{}.Changes(&added, applied)
	if !changes.Destructive() || len(changes.Actions) != 1 || changes.Actions[0].Action != "remove-partition" {
		t.Errorf("remove a partition = %+v", changes)
	}
}

func TestModelFeature(t *testing.T) {
	for model, want := range map[string]string{
		"Aqua Vanjaram [Instinct MI300X]":         "mi300x",
		"Aldebaran/MI200 [Instinct MI250X/MI250]": "mi250x",
		"AMD Instinct MI308X":                     "mi308x",
		"":                                        "",
	} {
		if got := modelFeature(model); got != want {
			t.Errorf("modelFeature(%q) = %q, want %q", model, got, want)
		}
	}
}

func withoutMember(spec platformdomain.PlatformSpec, id string) platformdomain.PlatformSpec {
	out := spec.Clone()
	out.Members = slices.DeleteFunc(out.Members, func(m platformdomain.PlatformMember) bool { return m.ServerID == id })
	top := readTopology(out)
	for i := range top.groups {
		top.groups[i].Members = slices.DeleteFunc(top.groups[i].Members, func(m string) bool { return m == id })
	}
	return withTopology(out, top)
}

func withMember(spec platformdomain.PlatformSpec, groupName, id string) platformdomain.PlatformSpec {
	top := readTopology(spec)
	for i := range top.groups {
		if top.groups[i].Name == groupName {
			top.groups[i].Members = append(top.groups[i].Members, id)
		}
	}
	return withTopology(spec, top)
}

func containsText(values []string, text string) bool {
	for _, value := range values {
		if strings.Contains(value, text) {
			return true
		}
	}
	return false
}
