package slurm

import (
	"fmt"
	"maps"
	"strings"

	"github.com/maple52046/swallow/internal/core/convergence"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// The Slurm member roles: a Server may run slurmctld, slurmd, or both, or be a login host.
const (
	RoleController = "controller"
	RoleCompute    = "compute"
	RoleLogin      = "login"
)

// settingKeys are the Slurm fields of the deploy request that a spec keeps as settings; "slurm"
// is the slurm object without its nodeAssignments.
var settingKeys = []string{
	"gpuStackOwner", "omnistat", "deviceMetricsExporter", "containerRuntime", "gpuDriver", "timeSyncCheck", "slurm",
}

// Platform is the Slurm module's platform hook.
type Platform struct{}

var _ platformdomain.PlatformModule = Platform{}

// PlatformType is slurm.
func (Platform) PlatformType() platformdomain.PlatformType { return platformdomain.PlatformTypeSlurm }

// DeployRequest renders the members as the slurm object's nodeAssignments.
func (Platform) DeployRequest(spec platformdomain.PlatformSpec) map[string]any {
	body := maps.Clone(spec.Settings)
	if body == nil {
		body = map[string]any{}
	}
	slurm, _ := body["slurm"].(map[string]any)
	slurm = maps.Clone(slurm)
	if slurm == nil {
		slurm = map[string]any{}
	}
	assignments := make([]any, 0, len(spec.Members))
	for _, member := range spec.Members {
		assignment := map[string]any{"serverId": member.ServerID, "controller": false, "compute": false, "login": false}
		for _, role := range member.Roles {
			assignment[role] = true
		}
		assignments = append(assignments, assignment)
	}
	slurm["nodeAssignments"] = assignments
	body["slurm"] = slurm
	return body
}

// SpecFromDeployRequest keeps the Slurm fields as settings and the nodeAssignments as members.
func (Platform) SpecFromDeployRequest(body map[string]any) (platformdomain.PlatformSpec, error) {
	spec := platformdomain.PlatformSpec{Desired: platformdomain.PlatformPresent, Settings: map[string]any{}}
	for _, key := range settingKeys {
		if value, ok := body[key]; ok && value != nil {
			spec.Settings[key] = value
		}
	}
	slurm, _ := body["slurm"].(map[string]any)
	slurm = maps.Clone(slurm)
	raw, _ := slurm["nodeAssignments"].([]any)
	delete(slurm, "nodeAssignments")
	if slurm != nil {
		spec.Settings["slurm"] = slurm
	}
	for _, item := range raw {
		assignment, ok := item.(map[string]any)
		serverID, _ := assignment["serverId"].(string)
		if !ok || serverID == "" {
			return platformdomain.PlatformSpec{}, fmt.Errorf("%w: each nodeAssignment needs a serverId", platformdomain.ErrInvalidSpec)
		}
		var roles []string
		for _, role := range []string{RoleController, RoleCompute, RoleLogin} {
			if flag, _ := assignment[role].(bool); flag {
				roles = append(roles, role)
			}
		}
		spec.Members = append(spec.Members, platformdomain.PlatformMember{ServerID: serverID, Roles: roles})
	}
	return spec, nil
}

// Changes classifies a Slurm change: creating and uninstalling are destructive; adding a member,
// enabling a monitoring feature, and first enabling the workload storage, a container runtime,
// native OCI containers, or PMIx converge directly by rerunning the convergent deploy; removing a
// member, changing roles, changing or disabling the storage or a runtime, and changing the state
// save location, the GRES type, the GPU stack owner, or the driver options are destructive.
// Swallow cannot yet remove members, change roles, disable features, or reconfigure them.
func (Platform) Changes(applied *platformdomain.PlatformSpec, desired platformdomain.PlatformSpec) (platformdomain.PlatformChanges, error) {
	platform := convergence.ResourceRef{Kind: "platform"}
	if desired.Desired == platformdomain.PlatformAbsent {
		return platformdomain.PlatformChanges{Actions: []convergence.PlannedAction{{
			Target: platform, Action: "uninstall", From: "installed", To: "uninstalled", Destructive: true,
			Reason: "the Platform is marked absent",
		}}}, nil
	}
	if applied == nil {
		actions := []convergence.PlannedAction{{Target: platform, Action: "install", To: "installed", Destructive: true, Reason: "the Platform is created on its members"}}
		for _, member := range desired.Members {
			actions = append(actions, convergence.PlannedAction{
				Target: serverRef(member.ServerID), Action: "join", To: strings.Join(member.Roles, ", "), Destructive: true,
				Reason: "the Server becomes a member of the new Platform",
			})
		}
		return platformdomain.PlatformChanges{Actions: actions}, nil
	}
	before, after := slurmSettings(*applied), slurmSettings(desired)
	if fmt.Sprint(before["clusterName"]) != fmt.Sprint(after["clusterName"]) {
		return platformdomain.PlatformChanges{}, fmt.Errorf("%w: slurm.clusterName", platformdomain.ErrImmutableSpecField)
	}
	var changes platformdomain.PlatformChanges
	add := func(action convergence.PlannedAction, supported bool) {
		changes.Actions = append(changes.Actions, action)
		if !supported {
			changes.Unsupported = append(changes.Unsupported, action.Action)
		}
	}
	added, removed, changed := platformdomain.MemberChanges(*applied, desired)
	for _, member := range added {
		add(convergence.PlannedAction{Target: serverRef(member.ServerID), Action: "add-member", To: strings.Join(member.Roles, ", "), Reason: "the Server joins the Platform"}, true)
	}
	for _, member := range removed {
		add(convergence.PlannedAction{Target: serverRef(member.ServerID), Action: "remove-member", From: strings.Join(member.Roles, ", "), Destructive: true, Reason: "the Server leaves the Platform"}, false)
	}
	for _, member := range changed {
		previous, _ := applied.Member(member.ServerID)
		add(convergence.PlannedAction{Target: serverRef(member.ServerID), Action: "change-node-role", From: strings.Join(previous.Roles, ", "), To: strings.Join(member.Roles, ", "), Destructive: true, Reason: "the Server's Slurm roles change"}, false)
	}
	for _, feature := range []string{"omnistat", "deviceMetricsExporter"} {
		was, is := platformdomain.SettingEnabled(*applied, feature), platformdomain.SettingEnabled(desired, feature)
		switch {
		case !was && is:
			add(convergence.PlannedAction{Target: platform, Action: "enable-" + feature, To: "enabled", Reason: "monitoring components are added"}, true)
		case was && !is:
			add(convergence.PlannedAction{Target: platform, Action: "disable-" + feature, From: "enabled", To: "disabled", Reason: "monitoring components are removed"}, false)
		case was && is && platformdomain.SettingChanged(*applied, desired, feature):
			add(convergence.PlannedAction{Target: platform, Action: "change-" + feature, Reason: "monitoring components are reconfigured"}, false)
		}
	}
	firstEnable := func(name string, was, is bool, wasValue, isValue any) {
		switch {
		case !was && is:
			add(convergence.PlannedAction{Target: platform, Action: "enable-" + name, To: "enabled", Reason: "a new capability is added; running jobs are unaffected"}, true)
		case was && !is:
			add(convergence.PlannedAction{Target: platform, Action: "disable-" + name, From: "enabled", To: "disabled", Destructive: true, Reason: "jobs lose the environment they run in"}, false)
		case was && is && fmt.Sprint(wasValue) != fmt.Sprint(isValue):
			add(convergence.PlannedAction{Target: platform, Action: "change-" + name, Destructive: true, Reason: "jobs' environment is remounted or replaced"}, false)
		}
	}
	runtime := func(spec platformdomain.PlatformSpec) string {
		object, _ := spec.Settings["containerRuntime"].(map[string]any)
		kind, _ := object["kind"].(string)
		return kind
	}
	firstEnable("container-runtime", runtime(*applied) != "", runtime(desired) != "", runtime(*applied), runtime(desired))
	storage := func(settings map[string]any) (bool, any) {
		object, _ := settings["workloadStorage"].(map[string]any)
		enabled, _ := object["enabled"].(bool)
		return enabled, object
	}
	storageWas, storageBefore := storage(before)
	storageIs, storageAfter := storage(after)
	firstEnable("workload-storage", storageWas, storageIs, storageBefore, storageAfter)
	for _, flag := range []string{"ociContainers", "pmix"} {
		was, _ := before[flag].(bool)
		is, _ := after[flag].(bool)
		firstEnable(flag, was, is, was, is)
	}
	for _, key := range []string{"stateSaveLocation", "gpuGresType"} {
		if fmt.Sprint(before[key]) != fmt.Sprint(after[key]) {
			add(convergence.PlannedAction{Target: platform, Action: "change-" + key, Destructive: true, Reason: "slurmctld or slurmd restart"}, false)
		}
	}
	if fmt.Sprint(before["apiVersion"]) != fmt.Sprint(after["apiVersion"]) {
		add(convergence.PlannedAction{Target: platform, Action: "change-apiVersion", Reason: "swallow reads slurmrestd at another version"}, true)
	}
	for _, key := range []string{"gpuStackOwner", "gpuDriver"} {
		if platformdomain.SettingChanged(*applied, desired, key) {
			add(convergence.PlannedAction{Target: platform, Action: "change-" + key, Destructive: true, Reason: "the GPU stack on the members changes"}, false)
		}
	}
	return changes, nil
}

func slurmSettings(spec platformdomain.PlatformSpec) map[string]any {
	object, _ := spec.Settings["slurm"].(map[string]any)
	if object == nil {
		return map[string]any{}
	}
	return object
}

func serverRef(id string) convergence.ResourceRef {
	return convergence.ResourceRef{Kind: "server", ID: id}
}
