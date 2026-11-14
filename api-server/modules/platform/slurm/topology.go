package slurm

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// The node groups and partitions of a Slurm Platform (decision 083) are the slurm setting's nodeGroups and
// partitions. A spec from before groups existed has neither; it is read as the groups its members'
// roles imply and the one partition every deployment used to have.
const (
	defaultPartitionName = "main"
	defaultMaxTime       = "INFINITE"
	// degradedFeature marks a node that has fewer GPUs than its group expects; degradedWeight makes
	// the scheduler use such nodes last (Slurm prefers the lowest Weight).
	degradedFeature = "gpu_degraded"
	degradedWeight  = 100
)

// The trusted variables the module adds to deploy-slurm (platform-deployment §6.3).
const (
	varPartitions      = "swallow_slurm_partitions"
	varNodeFeatures    = "swallow_slurm_node_features"
	varExpectedGPUs    = "swallow_slurm_expected_gpus"
	varDegradedFeature = "swallow_slurm_degraded_feature"
	varDegradedWeight  = "swallow_slurm_degraded_weight"
)

var (
	groupNamePattern     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)
	partitionNamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]{0,31}$`)
	maxTimePattern       = regexp.MustCompile(`^(INFINITE|UNLIMITED|\d+|\d+:\d{2}(:\d{2})?|\d+-\d+(:\d{2}(:\d{2})?)?)$`)
	unixGroupPattern     = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	featureCleaner       = regexp.MustCompile(`[^a-z0-9]+`)
	overSubscribeValues  = []string{"NO", "YES", "FORCE", "EXCLUSIVE"}
)

// nodeGroup is one entry of slurm.nodeGroups.
type nodeGroup struct {
	Name                string
	Role                string
	TemplateID          string
	ExpectedGPUsPerNode int
	Members             []string
}

// partition is one entry of slurm.partitions.
type partition struct {
	Name          string
	Default       bool
	Groups        []string
	MaxTime       string
	DefMemPerCPU  int64
	OverSubscribe string
	AllowGroups   []string
}

// topology is a spec's groups and partitions as the module reads them, legacy specs included.
type topology struct {
	groups     []nodeGroup
	partitions []partition
	// derived reports that the spec had no groups and these follow from its members.
	derived bool
}

// readTopology reads the spec's groups and partitions; a spec without nodeGroups gets the groups
// its members' roles imply, and one without partitions the default partition over every compute
// group.
func readTopology(spec platformdomain.PlatformSpec) topology {
	settings := slurmSettings(spec)
	top := topology{}
	raw, hasGroups := settings["nodeGroups"].([]any)
	for _, item := range raw {
		object, _ := item.(map[string]any)
		top.groups = append(top.groups, nodeGroup{
			Name: stringOf(object["name"]), Role: stringOf(object["role"]), TemplateID: stringOf(object["templateId"]),
			ExpectedGPUsPerNode: intOf(object["expectedGpusPerNode"]), Members: stringsOf(object["members"]),
		})
	}
	if !hasGroups {
		top.groups, top.derived = derivedGroups(spec), true
	}
	rawPartitions, hasPartitions := settings["partitions"].([]any)
	for _, item := range rawPartitions {
		object, _ := item.(map[string]any)
		top.partitions = append(top.partitions, partition{
			Name: stringOf(object["name"]), Default: object["default"] == true, Groups: stringsOf(object["groups"]),
			MaxTime: stringOf(object["maxTime"]), DefMemPerCPU: int64(intOf(object["defMemPerCpu"])),
			OverSubscribe: strings.ToUpper(stringOf(object["overSubscribe"])), AllowGroups: stringsOf(object["allowGroups"]),
		})
	}
	if !hasPartitions {
		var compute []string
		for _, group := range top.groups {
			if group.Role == RoleCompute {
				compute = append(compute, group.Name)
			}
		}
		top.partitions = []partition{{Name: defaultPartitionName, Default: true, Groups: compute, MaxTime: defaultMaxTime}}
		top.derived = true
	}
	return top
}

// derivedGroups is one group per role, holding the members with that role in spec order: what a
// Platform deployed before groups existed is.
func derivedGroups(spec platformdomain.PlatformSpec) []nodeGroup {
	var groups []nodeGroup
	for _, role := range []string{RoleController, RoleLogin, RoleCompute} {
		group := nodeGroup{Name: role, Role: role}
		for _, member := range spec.Members {
			if slices.Contains(member.Roles, role) {
				group.Members = append(group.Members, member.ServerID)
			}
		}
		if len(group.Members) > 0 || role != RoleLogin {
			groups = append(groups, group)
		}
	}
	return groups
}

// withTopology writes top into a copy of spec's slurm setting.
func withTopology(spec platformdomain.PlatformSpec, top topology) platformdomain.PlatformSpec {
	out := spec.Clone()
	if out.Settings == nil {
		out.Settings = map[string]any{}
	}
	settings := maps.Clone(slurmSettings(spec))
	if settings == nil {
		settings = map[string]any{}
	}
	groups := make([]any, 0, len(top.groups))
	for _, group := range top.groups {
		object := map[string]any{"name": group.Name, "role": group.Role, "members": anyStrings(group.Members)}
		if group.TemplateID != "" {
			object["templateId"] = group.TemplateID
		}
		if group.ExpectedGPUsPerNode > 0 {
			object["expectedGpusPerNode"] = group.ExpectedGPUsPerNode
		}
		groups = append(groups, object)
	}
	partitions := make([]any, 0, len(top.partitions))
	for _, p := range top.partitions {
		object := map[string]any{"name": p.Name, "default": p.Default, "groups": anyStrings(p.Groups), "maxTime": p.MaxTime}
		if p.MaxTime == "" {
			object["maxTime"] = defaultMaxTime
		}
		if p.DefMemPerCPU > 0 {
			object["defMemPerCpu"] = p.DefMemPerCPU
		}
		if p.OverSubscribe != "" {
			object["overSubscribe"] = p.OverSubscribe
		}
		if len(p.AllowGroups) > 0 {
			object["allowGroups"] = anyStrings(p.AllowGroups)
		}
		partitions = append(partitions, object)
	}
	settings["nodeGroups"], settings["partitions"] = groups, partitions
	out.Settings["slurm"] = settings
	return out
}

// NodeGroups implements platformdomain.NodeGroupModule.
func (Platform) NodeGroups(spec platformdomain.PlatformSpec) []platformdomain.NodeGroup {
	top := readTopology(spec)
	groups := make([]platformdomain.NodeGroup, 0, len(top.groups))
	for _, group := range top.groups {
		groups = append(groups, platformdomain.NodeGroup{
			Name: group.Name, Role: group.Role, TemplateID: group.TemplateID,
			ExpectedGPUsPerNode: group.ExpectedGPUsPerNode, Members: slices.Clone(group.Members),
		})
	}
	return groups
}

// WithGroupTemplates implements platformdomain.NodeGroupModule.
func (Platform) WithGroupTemplates(spec platformdomain.PlatformSpec, templates map[string]string) platformdomain.PlatformSpec {
	top := readTopology(spec)
	for index, group := range top.groups {
		if id, ok := templates[group.Name]; ok {
			top.groups[index].TemplateID = id
		}
	}
	return withTopology(spec, top)
}

// UpgradeSpec implements platformdomain.NodeGroupModule: a spec without groups or partitions gains
// the ones readTopology derives, which render the same slurm.conf the Platform was deployed with.
func (Platform) UpgradeSpec(spec platformdomain.PlatformSpec) (platformdomain.PlatformSpec, bool) {
	top := readTopology(spec)
	if !top.derived {
		return spec, false
	}
	return withTopology(spec, top), true
}

// validate checks the groups and partitions against the spec's members (decision 083): unique group
// names, known roles, every member in a group, each member's roles the union of its groups' roles,
// partitions over existing compute groups with exactly one default, and every compute group that
// has members in a partition.
func (t topology) validate(spec platformdomain.PlatformSpec) error {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", platformdomain.ErrInvalidDeployment, fmt.Sprintf(format, args...))
	}
	byName := map[string]nodeGroup{}
	rolesOf := map[string][]string{}
	for _, group := range t.groups {
		if !groupNamePattern.MatchString(group.Name) {
			return invalid("node group name %q must be 1 to 32 lower-case letters, digits, or hyphens", group.Name)
		}
		if _, taken := byName[group.Name]; taken {
			return invalid("node group %q is declared twice", group.Name)
		}
		if !slices.Contains([]string{RoleController, RoleCompute, RoleLogin}, group.Role) {
			return invalid("node group %q role must be controller, compute, or login", group.Name)
		}
		if group.ExpectedGPUsPerNode < 0 {
			return invalid("node group %q expectedGpusPerNode cannot be negative", group.Name)
		}
		byName[group.Name] = group
		for _, id := range group.Members {
			if _, ok := spec.Member(id); !ok {
				return invalid("node group %q names server %s, which is not a member", group.Name, id)
			}
			if !slices.Contains(rolesOf[id], group.Role) {
				rolesOf[id] = append(rolesOf[id], group.Role)
			}
		}
	}
	for _, member := range spec.Members {
		groups := rolesOf[member.ServerID]
		if len(groups) == 0 {
			return invalid("server %s is in no node group", member.ServerID)
		}
		if !sameSet(groups, member.Roles) {
			return invalid("server %s has the roles %s, but its node groups give it %s", member.ServerID,
				strings.Join(member.Roles, ", "), strings.Join(groups, ", "))
		}
	}
	defaults := 0
	partitioned := map[string]bool{}
	names := map[string]bool{}
	for _, p := range t.partitions {
		if !partitionNamePattern.MatchString(p.Name) {
			return invalid("partition name %q must be up to 32 letters, digits, underscores, or hyphens", p.Name)
		}
		if names[p.Name] {
			return invalid("partition %q is declared twice", p.Name)
		}
		names[p.Name] = true
		if p.Default {
			defaults++
		}
		if len(p.Groups) == 0 {
			return invalid("partition %q must contain at least one compute node group", p.Name)
		}
		nodes := 0
		for _, name := range p.Groups {
			group, ok := byName[name]
			if !ok || group.Role != RoleCompute {
				return invalid("partition %q names %q, which is not a compute node group", p.Name, name)
			}
			partitioned[name] = true
			nodes += len(group.Members)
		}
		if nodes == 0 {
			return invalid("partition %q has no nodes: its node groups have no members", p.Name)
		}
		if p.MaxTime != "" && !maxTimePattern.MatchString(p.MaxTime) {
			return invalid("partition %q maxTime %q is not a Slurm time (minutes, [days-]hours:minutes:seconds, or INFINITE)", p.Name, p.MaxTime)
		}
		if p.DefMemPerCPU < 0 {
			return invalid("partition %q defMemPerCpu cannot be negative", p.Name)
		}
		if p.OverSubscribe != "" && !slices.Contains(overSubscribeValues, p.OverSubscribe) {
			return invalid("partition %q overSubscribe must be NO, YES, FORCE, or EXCLUSIVE", p.Name)
		}
		for _, group := range p.AllowGroups {
			if !unixGroupPattern.MatchString(group) {
				return invalid("partition %q allowGroups entry %q is not a group name", p.Name, group)
			}
		}
	}
	if defaults != 1 {
		return invalid("exactly one partition must be the default, got %d", defaults)
	}
	for _, group := range t.groups {
		if group.Role == RoleCompute && len(group.Members) > 0 && !partitioned[group.Name] {
			return invalid("compute node group %q has members but is in no partition", group.Name)
		}
	}
	return nil
}

// DeploymentTopology implements platformdomain.TopologyModule: it validates the groups and
// partitions and renders them as deploy-slurm's partitions, per-node Features, and expected GPU
// counts. A node with fewer compute GPUs in its inventory than its group expects is reported as a
// warning; the playbook, which counts the GPUs the node really has, marks it degraded.
func (Platform) DeploymentTopology(spec platformdomain.PlatformSpec, targets []platformdomain.TopologyTarget) (platformdomain.DeploymentTopology, error) {
	top := readTopology(spec)
	if err := top.validate(spec); err != nil {
		return platformdomain.DeploymentTopology{}, err
	}
	byServer := map[string]platformdomain.TopologyTarget{}
	for _, target := range targets {
		byServer[target.ServerID] = target
	}
	groups := map[string]nodeGroup{}
	features := map[string][]string{}
	expected := map[string]int{}
	var warnings []string
	for _, group := range top.groups {
		groups[group.Name] = group
		if group.Role != RoleCompute {
			continue
		}
		for _, id := range group.Members {
			target := byServer[id]
			node := features[id]
			for _, model := range target.GPUModels {
				if feature := modelFeature(model); feature != "" && !slices.Contains(node, feature) {
					node = append(node, feature)
				}
			}
			if feature := cleanFeature(group.Name); !slices.Contains(node, feature) {
				node = append(node, feature)
			}
			features[id] = node
			if group.ExpectedGPUsPerNode > expected[id] {
				expected[id] = group.ExpectedGPUsPerNode
			}
			if group.ExpectedGPUsPerNode > 0 && target.ComputeGPUs < group.ExpectedGPUsPerNode {
				warnings = append(warnings, fmt.Sprintf("server %s in node group %s has %d of the %d GPUs the group expects; "+
					"it is marked %s and scheduled last", id, group.Name, target.ComputeGPUs, group.ExpectedGPUsPerNode, degradedFeature))
			}
		}
	}
	partitions := make([]any, 0, len(top.partitions))
	for _, p := range top.partitions {
		var nodes []string
		for _, name := range p.Groups {
			for _, id := range groups[name].Members {
				if !slices.Contains(nodes, id) {
					nodes = append(nodes, id)
				}
			}
		}
		maxTime := p.MaxTime
		if maxTime == "" {
			maxTime = defaultMaxTime
		}
		partitions = append(partitions, map[string]any{
			"name": p.Name, "default": p.Default, "nodes": anyStrings(nodes), "max_time": maxTime,
			"def_mem_per_cpu": p.DefMemPerCPU, "over_subscribe": p.OverSubscribe, "allow_groups": anyStrings(p.AllowGroups),
		})
	}
	nodeFeatures := map[string]any{}
	for id, list := range features {
		nodeFeatures[id] = anyStrings(list)
	}
	expectedGPUs := map[string]any{}
	for id, count := range expected {
		if count > 0 {
			expectedGPUs[id] = count
		}
	}
	return platformdomain.DeploymentTopology{
		Vars: map[string]any{
			varPartitions: partitions, varNodeFeatures: nodeFeatures, varExpectedGPUs: expectedGPUs,
			varDegradedFeature: degradedFeature, varDegradedWeight: degradedWeight,
		},
		Warnings: warnings,
	}, nil
}

// modelFeature turns a GPU Inventory model name into a Slurm feature: the product inside brackets
// when the name has them ("Aldebaran/MI200 [Instinct MI250X/MI250]" is "mi250x"), without the
// "Instinct" brand, lower-cased with every other character an underscore. An empty model (a virtual
// function reports none) has no feature.
func modelFeature(model string) string {
	name := strings.TrimSpace(model)
	if open, close := strings.Index(name, "["), strings.LastIndex(name, "]"); open >= 0 && close > open {
		name = name[open+1 : close]
	}
	name, _, _ = strings.Cut(name, "/")
	name = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "Instinct"))
	return cleanFeature(name)
}

// cleanFeature makes value a Slurm feature name: lower case, letters, digits, and underscores, at
// most 32 characters.
func cleanFeature(value string) string {
	feature := strings.Trim(featureCleaner.ReplaceAllString(strings.ToLower(value), "_"), "_")
	if len(feature) > 32 {
		feature = feature[:32]
	}
	return feature
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func stringOf(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func intOf(value any) int {
	switch number := value.(type) {
	case float64:
		return int(number)
	case int:
		return number
	case int64:
		return int(number)
	}
	return 0
}

func stringsOf(value any) []string {
	items, _ := value.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text := stringOf(item); text != "" {
			out = append(out, text)
		}
	}
	if list, ok := value.([]string); ok {
		return slices.Clone(list)
	}
	return out
}

func anyStrings(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

var (
	_ platformdomain.NodeGroupModule = Platform{}
	_ platformdomain.TopologyModule  = Platform{}
)
