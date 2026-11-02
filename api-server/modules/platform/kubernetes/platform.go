package kubernetes

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/maple52046/swallow/internal/core/convergence"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// The Kubernetes member roles. A control-plane member that also runs workloads carries
// RoleWorkloads too; a worker always runs workloads.
const (
	RoleControlPlane = "control-plane"
	RoleWorker       = "worker"
	RoleWorkloads    = "workloads"
)

// settingKeys are the Kubernetes fields of the deploy request that a spec keeps as settings.
var settingKeys = []string{
	"gpuStackOwner", "gpuOperator", "gpuDriver", "k0sVersion", "podCidr", "serviceCidr", "apiVip", "apiVipPrefix", "timeSyncCheck",
}

// immutableSettings change the cluster's network identity, so they are fixed at creation
// (desired-state-model §5.2); the defaults stand in for an omitted value.
var immutableSettings = map[string]any{
	"podCidr": "10.244.0.0/16", "serviceCidr": "10.96.0.0/12", "apiVip": "", "apiVipPrefix": nil,
}

// Platform is the Kubernetes module's platform hook.
type Platform struct{}

var _ platformdomain.PlatformModule = Platform{}

// PlatformType is kubernetes.
func (Platform) PlatformType() platformdomain.PlatformType {
	return platformdomain.PlatformTypeKubernetes
}

// DeployRequest renders the members as roleAssignments next to the settings.
func (Platform) DeployRequest(spec platformdomain.PlatformSpec) map[string]any {
	body := maps.Clone(spec.Settings)
	if body == nil {
		body = map[string]any{}
	}
	assignments := make([]any, 0, len(spec.Members))
	for _, member := range spec.Members {
		role, workloads := RoleWorker, false
		for _, r := range member.Roles {
			switch r {
			case RoleControlPlane:
				role = RoleControlPlane
			case RoleWorkloads:
				workloads = true
			}
		}
		assignments = append(assignments, map[string]any{"serverId": member.ServerID, "role": role, "runWorkloads": workloads})
	}
	body["roleAssignments"] = assignments
	return body
}

// SpecFromDeployRequest keeps the Kubernetes fields as settings and the roleAssignments as members.
func (Platform) SpecFromDeployRequest(body map[string]any) (platformdomain.PlatformSpec, error) {
	spec := platformdomain.PlatformSpec{Desired: platformdomain.PlatformPresent, Settings: map[string]any{}}
	for _, key := range settingKeys {
		if value, ok := body[key]; ok && value != nil {
			spec.Settings[key] = value
		}
	}
	raw, _ := body["roleAssignments"].([]any)
	for _, item := range raw {
		assignment, ok := item.(map[string]any)
		serverID, _ := assignment["serverId"].(string)
		if !ok || serverID == "" {
			return platformdomain.PlatformSpec{}, fmt.Errorf("%w: each roleAssignment needs a serverId", platformdomain.ErrInvalidSpec)
		}
		role, _ := assignment["role"].(string)
		roles := []string{RoleWorker}
		if role == RoleControlPlane {
			roles = []string{RoleControlPlane}
			if workloads, _ := assignment["runWorkloads"].(bool); workloads {
				roles = append(roles, RoleWorkloads)
			}
		} else if role != RoleWorker {
			return platformdomain.PlatformSpec{}, fmt.Errorf("%w: role %q is not control-plane or worker", platformdomain.ErrInvalidSpec, role)
		}
		spec.Members = append(spec.Members, platformdomain.PlatformMember{ServerID: serverID, Roles: roles})
	}
	return spec, nil
}

// Changes classifies a Kubernetes change: creating and uninstalling are destructive; adding a
// member and enabling the GPU Operator converge directly; removing a member, changing a Node Role,
// changing the k0s version, the GPU stack owner, or the driver options, and disabling or changing
// the GPU Operator are destructive. swallow cannot yet remove members, change roles, or upgrade.
func (Platform) Changes(applied *platformdomain.PlatformSpec, desired platformdomain.PlatformSpec) (platformdomain.PlatformChanges, error) {
	platform := convergence.ResourceRef{Kind: "platform"}
	if desired.Desired == platformdomain.PlatformAbsent {
		return platformdomain.PlatformChanges{Actions: []convergence.PlannedAction{{
			Target: platform, Action: "uninstall", From: "installed", To: "uninstalled", Destructive: true,
			Reason: "the Platform is marked absent",
		}}, Realization: platformdomain.RealizeUninstall}, nil
	}
	if applied == nil {
		actions := []convergence.PlannedAction{{Target: platform, Action: "install", To: "installed", Destructive: true, Reason: "the Platform is created on its members"}}
		for _, member := range desired.Members {
			actions = append(actions, convergence.PlannedAction{
				Target: serverRef(member.ServerID), Action: "join", To: roleList(member.Roles), Destructive: true,
				Reason: "the Server becomes a member of the new Platform",
			})
		}
		return platformdomain.PlatformChanges{Actions: actions, Realization: platformdomain.RealizeDeploy}, nil
	}
	for key, fallback := range immutableSettings {
		if settingOr(*applied, key, fallback) != settingOr(desired, key, fallback) {
			return platformdomain.PlatformChanges{}, fmt.Errorf("%w: %s", platformdomain.ErrImmutableSpecField, key)
		}
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
		add(convergence.PlannedAction{Target: serverRef(member.ServerID), Action: "add-member", To: roleList(member.Roles), Reason: "the Server joins the Platform"}, true)
	}
	for _, member := range removed {
		add(convergence.PlannedAction{Target: serverRef(member.ServerID), Action: "remove-member", From: roleList(member.Roles), Destructive: true, Reason: "the Server leaves the Platform"}, false)
	}
	for _, member := range changed {
		before, _ := applied.Member(member.ServerID)
		add(convergence.PlannedAction{Target: serverRef(member.ServerID), Action: "change-node-role", From: roleList(before.Roles), To: roleList(member.Roles), Destructive: true, Reason: "the Server's Node Role changes"}, false)
	}
	if platformdomain.SettingChanged(*applied, desired, "k0sVersion") {
		add(convergence.PlannedAction{Target: platform, Action: "upgrade", From: platformdomain.SettingString(*applied, "k0sVersion"), To: platformdomain.SettingString(desired, "k0sVersion"), Destructive: true, Reason: "k0s, kubelet, and containerd restart"}, false)
	}
	before, after := platformdomain.SettingEnabled(*applied, "gpuOperator"), platformdomain.SettingEnabled(desired, "gpuOperator")
	switch {
	case !before && after:
		add(convergence.PlannedAction{Target: platform, Action: "enable-gpu-operator", To: "enabled", Reason: "the GPU Operator is added"}, true)
	case before && !after:
		add(convergence.PlannedAction{Target: platform, Action: "disable-gpu-operator", From: "enabled", To: "disabled", Destructive: true, Reason: "GPU workloads lose the device plugin"}, true)
	case before && after && platformdomain.SettingChanged(*applied, desired, "gpuOperator"):
		add(convergence.PlannedAction{Target: platform, Action: "change-gpu-operator", Destructive: true, Reason: "pods that use GPUs may restart"}, true)
	}
	for _, key := range []string{"gpuStackOwner", "gpuDriver"} {
		if platformdomain.SettingChanged(*applied, desired, key) && !(key == "gpuStackOwner" && before != after) {
			add(convergence.PlannedAction{Target: platform, Action: "change-" + key, Destructive: true, Reason: "the GPU stack on the members changes"}, false)
		}
	}
	switch {
	case len(added) > 0:
		changes.Realization = platformdomain.RealizeDeploy
	case before || after:
		if platformdomain.SettingChanged(*applied, desired, "gpuOperator") {
			changes.Realization = platformdomain.RealizeGPUOperator
		}
	}
	return changes, nil
}

// ReportsMembership: a member that runs workloads is a Kubernetes node; a controller-only member is
// not.
func (Platform) ReportsMembership(member platformdomain.PlatformMember) bool {
	return slices.Contains(member.Roles, RoleWorker) || slices.Contains(member.Roles, RoleWorkloads)
}

func serverRef(id string) convergence.ResourceRef {
	return convergence.ResourceRef{Kind: "server", ID: id}
}

func roleList(roles []string) string {
	return strings.Join(roles, ", ")
}

func settingOr(spec platformdomain.PlatformSpec, key string, fallback any) any {
	if value, ok := spec.Settings[key]; ok && value != nil && value != "" {
		return value
	}
	return fallback
}
