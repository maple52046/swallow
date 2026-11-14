package command

import (
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/cli/internal/client"
)

// The Slurm scaling commands (platforms.md "Node groups and partitions", "Remove members", "Rescan A
// Slurm Node"; decision 083) edit a Slurm Platform's spec: they read the Platform, change its
// members, the slurm setting's nodeGroups and partitions, or memberRemovals, and write the spec back
// at the generation they read, with every other part as read. The API validates and classifies each
// write; removing members and removing a partition are destructive and answer with a Change Plan,
// which --approve approves with the write.

// platformsNodesCmd groups adding, removing, and rescanning a Slurm Platform's compute nodes.
func platformsNodesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "nodes",
		Short: "Add, remove, or rescan a Slurm platform's compute nodes",
	}
	add := &cobra.Command{
		Use:   "add <platformId>",
		Short: "Add Servers to a compute node group; they install from the group's Deployment Template",
		Long: "Add Servers to a compute node group of a Slurm platform. Each new node is installed from the\n" +
			"group's Deployment Template (a Server already deployed keeps its OS), joins the cluster, and is\n" +
			"validated on its own; the nodes already in the cluster keep running their jobs. A Server below\n" +
			"its template's minimum hardware is refused, naming what it lacks. Adding a controller or login\n" +
			"node is refused: Swallow scales only compute nodes.\n\n" +
			"A new node may override only its network, on a platform whose members Swallow installs:\n" +
			"--interface <serverId>=<interfaceId> picks the interface it deploys on, and, when the\n" +
			"group's template deploys static addresses, --ip <serverId>=<address> its address. Every\n" +
			"other setting comes from the template.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			group, _ := cmd.Flags().GetString("group")
			servers, _ := cmd.Flags().GetStringSlice("server")
			if group == "" || len(servers) == 0 {
				return fmt.Errorf("--group and at least one --server are required")
			}
			networks, err := nodeNetworks(cmd, servers)
			if err != nil {
				return err
			}
			return updatePlatformSpec(cmd, args[0], func(spec platformSpec) error {
				if err := spec.addNodes(group, servers); err != nil {
					return err
				}
				return spec.setNodeNetworks(networks)
			})
		},
	}
	add.Flags().String("group", "", "the compute node group the Servers join (required)")
	add.Flags().StringSlice("server", nil, "a Server id to add; repeat or comma-separate for several (required)")
	add.Flags().StringArray("interface", nil, "<serverId>=<interfaceId>: the interface a new node deploys on; repeat per node")
	add.Flags().StringArray("ip", nil, "<serverId>=<address>: a new node's static address (static templates only); repeat per node")
	addApproveFlag(add)

	remove := &cobra.Command{
		Use:   "remove <platformId>",
		Short: "Remove compute nodes: drain, reconfigure the cluster, clean up, and optionally release them",
		Long: "Remove compute nodes from a Slurm platform (answers with a Change Plan). Swallow drains each\n" +
			"node and waits for its running jobs up to --drain-timeout; at the timeout --force cancels them,\n" +
			"and otherwise the removal stops with the node drained, so retrying waits again. The cluster is\n" +
			"reconfigured without the nodes, Swallow's Slurm configuration and the shared filesystem mount\n" +
			"are removed from them, and with --release the Servers are released to their provisioner.\n" +
			"Removing the last compute node, a controller, or a login node is refused. Removing a node\n" +
			"whose removal has not finished (a drain that timed out) writes its removal again with these\n" +
			"flags, for example --force, and answers with a new Change Plan that removes it with them.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			servers, _ := cmd.Flags().GetStringSlice("server")
			if len(servers) == 0 {
				return fmt.Errorf("at least one --server is required")
			}
			timeout, _ := cmd.Flags().GetDuration("drain-timeout")
			if timeout < 0 || timeout > 24*time.Hour || timeout%time.Second != 0 {
				return fmt.Errorf("--drain-timeout must be whole seconds from 0 to 24h; 0 uses the API's default of 10 minutes")
			}
			removal := memberRemoval(cmd, int(timeout/time.Second))
			return updatePlatform(cmd, args[0], func(platform platformRead, spec platformSpec) error {
				return spec.removeNodes(servers, removal, platform.leaving())
			})
		},
	}
	remove.Flags().StringSlice("server", nil, "a Server id to remove; repeat or comma-separate for several (required)")
	remove.Flags().Duration("drain-timeout", 0, "how long running jobs are waited for, for example 30m (at most 24h; 0 is 10m)")
	remove.Flags().Bool("force", false, "cancel jobs still running at the drain timeout instead of stopping the removal")
	remove.Flags().Bool("release", false, "release the Servers to their provisioner afterwards (their OS is removed)")
	remove.Flags().Bool("erase", false, "with --release, erase the disks")
	remove.Flags().Bool("secure-erase", false, "with --release and --erase, prefer hardware secure erase")
	remove.Flags().Bool("quick-erase", false, "with --release and --erase, use quick erase")
	remove.Flags().Bool("unbind-static-ips", false, "with --release, remove the Servers' static IP bindings")
	addApproveFlag(remove)

	rescan := &cobra.Command{
		Use:   "rescan <platformId> <serverId>",
		Short: "Re-detect a compute node's GPUs and reconfigure the cluster",
		Long: "Re-detect a compute node's GPUs after deployment (for example after a GPU failed) and reconfigure\n" +
			"the cluster so Slurm hands out the GPUs the node has now. The Slurm deploy workflow of the\n" +
			"applied spec runs again; nodes whose hardware did not change keep their jobs, and only this node\n" +
			"is validated. Prints the workflow id.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "POST", fmt.Sprintf("platforms/%s/slurm/nodes/%s/rescan", args[0], args[1]), nil, nil)
		},
	}
	cmd.AddCommand(add, remove, rescan)
	return cmd
}

// nodeNetworks reads the --interface and --ip overrides as one assignment per Server, keyed by the
// Server id. An address needs an interface, and every Server named must be one being added.
func nodeNetworks(cmd *cobra.Command, servers []string) (map[string]map[string]any, error) {
	networks := map[string]map[string]any{}
	parse := func(flag, key string) error {
		values, _ := cmd.Flags().GetStringArray(flag)
		for _, value := range values {
			serverID, field, ok := strings.Cut(value, "=")
			if !ok || serverID == "" || field == "" {
				return fmt.Errorf("--%s %q must be <serverId>=<value>", flag, value)
			}
			if !slices.Contains(servers, serverID) {
				return fmt.Errorf("--%s names server %s, which is not one of the --server values", flag, serverID)
			}
			if networks[serverID] == nil {
				networks[serverID] = map[string]any{"serverId": serverID}
			}
			networks[serverID][key] = field
		}
		return nil
	}
	if err := parse("interface", "interfaceId"); err != nil {
		return nil, err
	}
	if err := parse("ip", "ipAddress"); err != nil {
		return nil, err
	}
	for serverID, network := range networks {
		if _, ok := network["interfaceId"]; !ok {
			return nil, fmt.Errorf("--ip for server %s needs its --interface too", serverID)
		}
		if address, ok := network["ipAddress"].(string); ok && net.ParseIP(address).To4() == nil {
			return nil, fmt.Errorf("--ip %s=%s is not an IPv4 address", serverID, address)
		}
	}
	return networks, nil
}

// memberRemoval is the memberRemovals entry the remove flags describe, without its serverId. The
// release options count only with --release, as the contract says.
func memberRemoval(cmd *cobra.Command, drainSeconds int) map[string]any {
	flag := func(name string) bool {
		on, _ := cmd.Flags().GetBool(name)
		return on
	}
	release := flag("release")
	return map[string]any{
		"drainTimeoutSeconds": drainSeconds,
		"force":               flag("force"),
		"releaseServer":       release,
		"erase":               release && flag("erase"),
		"secureErase":         release && flag("secure-erase"),
		"quickErase":          release && flag("quick-erase"),
		"unbindStaticIps":     release && flag("unbind-static-ips"),
	}
}

// platformsGroupsCmd lists and changes a Slurm Platform's node groups.
func platformsGroupsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "groups",
		Short: "List, add, or change a Slurm platform's node groups",
	}
	list := &cobra.Command{
		Use:   "list <platformId>",
		Short: "List the node groups of a Slurm platform's spec",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := readPlatformSpec(cmd, args[0])
			if err != nil {
				return err
			}
			return printResult(spec.slurmList("nodeGroups"))
		},
	}
	add := &cobra.Command{
		Use:   "add <platformId>",
		Short: "Add an empty compute node group, placed in the named partitions",
		Long: "Add a compute node group. It installs its nodes from --template, or from the platform's\n" +
			"default template when omitted; --expected-gpus marks a node with fewer GPUs gpu_degraded and\n" +
			"schedules it last instead of refusing it. Add nodes to it with `platforms nodes add`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			template, _ := cmd.Flags().GetString("template")
			expected, _ := cmd.Flags().GetInt("expected-gpus")
			partitions, _ := cmd.Flags().GetStringSlice("partition")
			return updatePlatformSpec(cmd, args[0], func(spec platformSpec) error {
				return spec.addGroup(name, template, expected, partitions)
			})
		},
	}
	add.Flags().String("name", "", "the group name: 1 to 32 lower-case letters, digits, or hyphens (required)")
	add.Flags().String("template", "", "the Deployment Template id its new nodes install from")
	add.Flags().Int("expected-gpus", 0, "the GPUs each node is expected to have (0 for none)")
	add.Flags().StringSlice("partition", nil, "a partition the group joins; repeat for several")
	addApproveFlag(add)

	set := &cobra.Command{
		Use:   "set <platformId> <group>",
		Short: "Change a node group's Deployment Template or expected GPUs per node",
		Long: "Change a node group's Deployment Template (--template, or --clear-template for the platform's\n" +
			"default) or its expected GPUs per node (--expected-gpus, 0 for none). The nodes already in the\n" +
			"group are not reinstalled; the change applies to nodes added later and to the degraded mark.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			changes := map[string]any{}
			if cmd.Flags().Changed("template") {
				template, _ := cmd.Flags().GetString("template")
				changes["templateId"] = template
			}
			if clear, _ := cmd.Flags().GetBool("clear-template"); clear {
				changes["templateId"] = ""
			}
			if cmd.Flags().Changed("expected-gpus") {
				expected, _ := cmd.Flags().GetInt("expected-gpus")
				changes["expectedGpusPerNode"] = expected
			}
			if len(changes) == 0 {
				return fmt.Errorf("nothing to change: give --template, --clear-template, or --expected-gpus")
			}
			return updatePlatformSpec(cmd, args[0], func(spec platformSpec) error {
				return spec.setGroup(args[1], changes)
			})
		},
	}
	set.Flags().String("template", "", "the Deployment Template id the group's new nodes install from")
	set.Flags().Bool("clear-template", false, "install the group's new nodes from the platform's default template")
	set.Flags().Int("expected-gpus", 0, "the GPUs each node is expected to have (0 for none)")
	addApproveFlag(set)
	cmd.AddCommand(list, add, set)
	return cmd
}

// platformsPartitionsCmd lists and changes a Slurm Platform's partitions.
func platformsPartitionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "partitions",
		Short: "List, add, change, or remove a Slurm platform's partitions",
	}
	list := &cobra.Command{
		Use:   "list <platformId>",
		Short: "List the partitions of a Slurm platform's spec (live state: `platforms slurm`)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := readPlatformSpec(cmd, args[0])
			if err != nil {
				return err
			}
			return printResult(spec.slurmList("partitions"))
		},
	}
	add := &cobra.Command{
		Use:   "add <platformId>",
		Short: "Add a partition over compute node groups",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			partition := map[string]any{"name": name, "default": false}
			if err := partitionFlags(cmd, partition); err != nil {
				return err
			}
			return updatePlatformSpec(cmd, args[0], func(spec platformSpec) error {
				return spec.putPartition("", partition)
			})
		},
	}
	add.Flags().String("name", "", "the partition name (required)")
	addPartitionFlags(add)
	addApproveFlag(add)

	set := &cobra.Command{
		Use:   "set <platformId> <partition>",
		Short: "Change a partition's groups, default, limits, sharing, or access",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return updatePlatformSpec(cmd, args[0], func(spec platformSpec) error {
				partition, err := spec.partition(args[1])
				if err != nil {
					return err
				}
				if name, _ := cmd.Flags().GetString("name"); name != "" {
					partition["name"] = name
				}
				if err := partitionFlags(cmd, partition); err != nil {
					return err
				}
				return spec.putPartition(args[1], partition)
			})
		},
	}
	set.Flags().String("name", "", "rename the partition")
	addPartitionFlags(set)
	addApproveFlag(set)

	remove := &cobra.Command{
		Use:   "remove <platformId> <partition>",
		Short: "Remove a partition (answers with a Change Plan; refused while it has jobs)",
		Long: "Remove a partition. Its nodes stay in the cluster and in their other partitions. The change\n" +
			"is destructive and answers with a Change Plan; it is blocked while the partition still has\n" +
			"queued or running jobs, and the blocker names how many.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return updatePlatformSpec(cmd, args[0], func(spec platformSpec) error {
				return spec.removePartition(args[1])
			})
		},
	}
	addApproveFlag(remove)
	cmd.AddCommand(list, add, set, remove)
	return cmd
}

// addPartitionFlags registers the partition fields `partitions add` and `set` share.
func addPartitionFlags(cmd *cobra.Command) {
	cmd.Flags().StringSlice("group", nil, "a compute node group in the partition; repeat for several (replaces the list)")
	cmd.Flags().Bool("default", false, "make it the default partition (the current default stops being one)")
	cmd.Flags().String("max-time", "", "the time limit: minutes, days-hours:minutes:seconds, or INFINITE")
	cmd.Flags().Int("def-mem-per-cpu", 0, "the default memory per CPU in MiB (0: the smallest share of its nodes)")
	cmd.Flags().String("over-subscribe", "", "node sharing: NO, YES, FORCE, or EXCLUSIVE")
	cmd.Flags().StringSlice("allow-group", nil, "a Unix group allowed to submit; repeat for several (replaces the list)")
}

// partitionFlags writes the partition flags the user gave into partition; flags not given keep the
// stored values.
func partitionFlags(cmd *cobra.Command, partition map[string]any) error {
	flags := cmd.Flags()
	if flags.Changed("group") {
		groups, _ := flags.GetStringSlice("group")
		partition["groups"] = groups
	}
	if flags.Changed("default") {
		isDefault, _ := flags.GetBool("default")
		partition["default"] = isDefault
	}
	if flags.Changed("max-time") {
		maxTime, _ := flags.GetString("max-time")
		partition["maxTime"] = maxTime
	}
	if flags.Changed("def-mem-per-cpu") {
		mem, _ := flags.GetInt("def-mem-per-cpu")
		if mem == 0 {
			delete(partition, "defMemPerCpu")
		} else {
			partition["defMemPerCpu"] = mem
		}
	}
	if flags.Changed("over-subscribe") {
		value, _ := flags.GetString("over-subscribe")
		partition["overSubscribe"] = value
	}
	if flags.Changed("allow-group") {
		groups, _ := flags.GetStringSlice("allow-group")
		partition["allowGroups"] = groups
	}
	empty := partition["groups"] == nil
	switch groups := partition["groups"].(type) {
	case []string:
		empty = len(groups) == 0
	case []any:
		empty = len(groups) == 0
	}
	if empty {
		return fmt.Errorf("a partition needs at least one --group")
	}
	return nil
}

// platformSpec is a Platform's spec as read, edited in place by the scaling commands. Its slurm
// setting's nodeGroups and partitions are materialized on first use: a spec from before node groups
// gets the groups and the partition the API derives for it (one group per role, partition main), so
// the write stores the same thing the API would.
type platformSpec map[string]any

func (s platformSpec) settings() map[string]any {
	settings, _ := s["settings"].(map[string]any)
	if settings == nil {
		settings = map[string]any{}
		s["settings"] = settings
	}
	return settings
}

func (s platformSpec) slurm() map[string]any {
	settings := s.settings()
	slurm, _ := settings["slurm"].(map[string]any)
	if slurm == nil {
		slurm = map[string]any{}
		settings["slurm"] = slurm
	}
	return slurm
}

func (s platformSpec) members() []any {
	members, _ := s["members"].([]any)
	return members
}

// groups returns the spec's node groups, deriving them from the members' roles when absent.
func (s platformSpec) groups() []any {
	slurm := s.slurm()
	if groups, ok := slurm["nodeGroups"].([]any); ok {
		return groups
	}
	var groups []any
	for _, role := range []string{"controller", "login", "compute"} {
		var members []any
		for _, member := range s.members() {
			object, _ := member.(map[string]any)
			roles, _ := object["roles"].([]any)
			if slices.Contains(roles, any(role)) {
				members = append(members, object["serverId"])
			}
		}
		if len(members) > 0 || role != "login" {
			if members == nil {
				members = []any{}
			}
			groups = append(groups, map[string]any{"name": role, "role": role, "members": members})
		}
	}
	slurm["nodeGroups"] = groups
	return groups
}

// partitions returns the spec's partitions, deriving the default partition main when absent.
func (s platformSpec) partitions() []any {
	slurm := s.slurm()
	if partitions, ok := slurm["partitions"].([]any); ok {
		return partitions
	}
	var compute []any
	for _, group := range s.groups() {
		object, _ := group.(map[string]any)
		if object["role"] == "compute" {
			compute = append(compute, object["name"])
		}
	}
	partitions := []any{map[string]any{"name": "main", "default": true, "groups": compute, "maxTime": "INFINITE"}}
	slurm["partitions"] = partitions
	return partitions
}

// slurmList is the groups or partitions to print, derived when the spec has none yet.
func (s platformSpec) slurmList(key string) []any {
	if key == "nodeGroups" {
		return s.groups()
	}
	return s.partitions()
}

func (s platformSpec) group(name string) (map[string]any, error) {
	for _, group := range s.groups() {
		object, _ := group.(map[string]any)
		if object["name"] == name {
			return object, nil
		}
	}
	return nil, fmt.Errorf("the platform has no node group %q (see `platforms groups list`)", name)
}

func (s platformSpec) addNodes(groupName string, servers []string) error {
	group, err := s.group(groupName)
	if err != nil {
		return err
	}
	if group["role"] != "compute" {
		return fmt.Errorf("node group %s is a %v group; Swallow adds only compute nodes", groupName, group["role"])
	}
	members := s.members()
	groupMembers, _ := group["members"].([]any)
	for _, id := range servers {
		if slices.ContainsFunc(members, func(member any) bool { object, _ := member.(map[string]any); return object["serverId"] == id }) {
			return fmt.Errorf("server %s is already a member of the platform", id)
		}
		members = append(members, map[string]any{"serverId": id, "roles": []any{"compute"}})
		groupMembers = append(groupMembers, id)
	}
	s["members"] = members
	group["members"] = groupMembers
	removals, _ := s["memberRemovals"].([]any)
	s["memberRemovals"] = slices.DeleteFunc(removals, func(removal any) bool {
		object, _ := removal.(map[string]any)
		return slices.Contains(servers, fmt.Sprint(object["serverId"]))
	})
	return nil
}

// setNodeNetworks writes each override as its Server's machinePreparation.network.assignments entry,
// replacing an earlier one for the same Server. A platform whose members keep their OS has no
// machine preparation network to override and is refused.
func (s platformSpec) setNodeNetworks(networks map[string]map[string]any) error {
	if len(networks) == 0 {
		return nil
	}
	preparation, _ := s["machinePreparation"].(map[string]any)
	if preparation == nil || preparation["mode"] != "provision_os" {
		return fmt.Errorf("--interface and --ip need a platform whose members Swallow installs (machinePreparation mode provision_os)")
	}
	network, _ := preparation["network"].(map[string]any)
	if network == nil {
		network = map[string]any{}
		preparation["network"] = network
	}
	assignments, _ := network["assignments"].([]any)
	ids := make([]string, 0, len(networks))
	for id := range networks {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		assignments = slices.DeleteFunc(assignments, func(entry any) bool {
			object, _ := entry.(map[string]any)
			return object["serverId"] == id
		})
		assignments = append(assignments, networks[id])
	}
	network["assignments"] = assignments
	return nil
}

// removeNodes drops servers from the members and their node groups and writes each one's removal.
// A server in leaving is already out of the spec but not yet removed, so only its removal is
// written again.
func (s platformSpec) removeNodes(servers []string, removal map[string]any, leaving []string) error {
	members := s.members()
	for _, id := range servers {
		if !slices.ContainsFunc(members, func(member any) bool { object, _ := member.(map[string]any); return object["serverId"] == id }) &&
			!slices.Contains(leaving, id) {
			return fmt.Errorf("server %s is not a member of the platform", id)
		}
	}
	s["members"] = slices.DeleteFunc(slices.Clone(members), func(member any) bool {
		object, _ := member.(map[string]any)
		return slices.Contains(servers, fmt.Sprint(object["serverId"]))
	})
	for _, group := range s.groups() {
		object, _ := group.(map[string]any)
		groupMembers, _ := object["members"].([]any)
		object["members"] = slices.DeleteFunc(slices.Clone(groupMembers), func(id any) bool { return slices.Contains(servers, fmt.Sprint(id)) })
	}
	removals, _ := s["memberRemovals"].([]any)
	removals = slices.DeleteFunc(slices.Clone(removals), func(entry any) bool {
		object, _ := entry.(map[string]any)
		return slices.Contains(servers, fmt.Sprint(object["serverId"]))
	})
	for _, id := range servers {
		entry := map[string]any{"serverId": id}
		for key, value := range removal {
			entry[key] = value
		}
		removals = append(removals, entry)
	}
	s["memberRemovals"] = removals
	return nil
}

func (s platformSpec) addGroup(name, template string, expected int, partitions []string) error {
	if _, err := s.group(name); err == nil {
		return fmt.Errorf("the platform already has a node group %q", name)
	}
	group := map[string]any{"name": name, "role": "compute", "members": []any{}}
	if template != "" {
		group["templateId"] = template
	}
	if expected > 0 {
		group["expectedGpusPerNode"] = expected
	}
	s.slurm()["nodeGroups"] = append(s.groups(), group)
	for _, partitionName := range partitions {
		partition, err := s.partition(partitionName)
		if err != nil {
			return err
		}
		groups, _ := partition["groups"].([]any)
		partition["groups"] = append(groups, name)
	}
	return nil
}

func (s platformSpec) setGroup(name string, changes map[string]any) error {
	group, err := s.group(name)
	if err != nil {
		return err
	}
	if template, ok := changes["templateId"]; ok {
		if template == "" {
			delete(group, "templateId")
		} else {
			group["templateId"] = template
		}
	}
	if expected, ok := changes["expectedGpusPerNode"].(int); ok {
		if expected <= 0 {
			delete(group, "expectedGpusPerNode")
		} else {
			group["expectedGpusPerNode"] = expected
		}
	}
	return nil
}

func (s platformSpec) partition(name string) (map[string]any, error) {
	for _, partition := range s.partitions() {
		object, _ := partition.(map[string]any)
		if object["name"] == name {
			return object, nil
		}
	}
	return nil, fmt.Errorf("the platform has no partition %q (see `platforms partitions list`)", name)
}

// putPartition adds partition, or replaces the one named previous; making it the default clears the
// default of every other partition, because exactly one is the default.
func (s platformSpec) putPartition(previous string, partition map[string]any) error {
	partitions := s.partitions()
	index := -1
	for i, existing := range partitions {
		object, _ := existing.(map[string]any)
		if previous != "" && object["name"] == previous {
			index = i
		} else if object["name"] == partition["name"] {
			return fmt.Errorf("the platform already has a partition %q", partition["name"])
		}
	}
	if previous != "" && index < 0 {
		return fmt.Errorf("the platform has no partition %q", previous)
	}
	if isDefault, _ := partition["default"].(bool); isDefault {
		for _, existing := range partitions {
			object, _ := existing.(map[string]any)
			object["default"] = false
		}
	}
	if index >= 0 {
		partitions[index] = partition
	} else {
		partitions = append(partitions, partition)
	}
	s.slurm()["partitions"] = partitions
	return nil
}

func (s platformSpec) removePartition(name string) error {
	partition, err := s.partition(name)
	if err != nil {
		return err
	}
	if isDefault, _ := partition["default"].(bool); isDefault {
		return fmt.Errorf("partition %s is the default; make another partition the default first", name)
	}
	s.slurm()["partitions"] = slices.DeleteFunc(slices.Clone(s.partitions()), func(entry any) bool {
		object, _ := entry.(map[string]any)
		return object["name"] == name
	})
	return nil
}

// platformRead is the part of a Platform the scaling commands read before writing its spec back.
type platformRead struct {
	Generation int64          `json:"generation"`
	Spec       map[string]any `json:"spec"`
	Status     struct {
		Applied *struct {
			Members []struct {
				ServerID string `json:"serverId"`
			} `json:"members"`
		} `json:"applied"`
	} `json:"status"`
}

// leaving lists the applied members the spec no longer has: their removal has not finished.
func (p platformRead) leaving() []string {
	if p.Status.Applied == nil {
		return nil
	}
	members, _ := p.Spec["members"].([]any)
	var ids []string
	for _, applied := range p.Status.Applied.Members {
		if !slices.ContainsFunc(members, func(member any) bool {
			object, _ := member.(map[string]any)
			return object["serverId"] == applied.ServerID
		}) {
			ids = append(ids, applied.ServerID)
		}
	}
	return ids
}

// readPlatform reads a Platform's generation and spec; a Platform not yet adopted has no spec and is
// refused, since there is nothing to write back.
func readPlatform(cmd *cobra.Command, platformID string) (platformRead, error) {
	c, err := newClient()
	if err != nil {
		return platformRead{}, err
	}
	var platform platformRead
	if err := c.JSON(ctx(cmd), client.Request{Method: "GET", Path: "platforms/" + platformID, Auth: client.AuthBearer}, &platform); err != nil {
		return platformRead{}, err
	}
	if platform.Spec == nil {
		return platformRead{}, fmt.Errorf("platform %s has no spec yet; wait for the platform controller to adopt it", platformID)
	}
	return platform, nil
}

func readPlatformSpec(cmd *cobra.Command, platformID string) (platformSpec, error) {
	platform, err := readPlatform(cmd, platformID)
	if err != nil {
		return nil, err
	}
	return platformSpec(platform.Spec), nil
}

// updatePlatformSpec reads a Platform, applies change to its spec, and writes the members, settings,
// memberRemovals, and machine preparation back at the generation it read (the machine preparation
// exactly as read, with any templates the Platform generates). An error from change refuses the
// write before anything is sent; a spec changed in between is the API's `conflict`.
func updatePlatformSpec(cmd *cobra.Command, platformID string, change func(spec platformSpec) error) error {
	return updatePlatform(cmd, platformID, func(_ platformRead, spec platformSpec) error { return change(spec) })
}

// updatePlatform is updatePlatformSpec for a change that also needs the Platform as read.
func updatePlatform(cmd *cobra.Command, platformID string, change func(platform platformRead, spec platformSpec) error) error {
	platform, err := readPlatform(cmd, platformID)
	if err != nil {
		return err
	}
	spec := platformSpec(platform.Spec)
	if _, slurm := spec.settings()["slurm"]; !slurm {
		return fmt.Errorf("platform %s is not a Slurm platform: it has no slurm setting", platformID)
	}
	// Both are materialized before the change, so a spec from before node groups is written with
	// the groups and the partition the API would derive, never with only one of them.
	spec.groups()
	spec.partitions()
	if err := change(platform, spec); err != nil {
		return err
	}
	written := map[string]any{"members": spec["members"], "settings": spec["settings"]}
	if removals, ok := spec["memberRemovals"]; ok && removals != nil {
		written["memberRemovals"] = removals
	}
	if preparation := spec["machinePreparation"]; preparation != nil {
		written["machinePreparation"] = preparation
	}
	return sendSpecWrite(cmd, "PUT", fmt.Sprintf("platforms/%s/spec", platformID), nil,
		map[string]any{"generation": platform.Generation, "spec": written})
}
