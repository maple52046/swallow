package domain

import (
	"errors"
	"reflect"
	"slices"

	"github.com/maple52046/swallow/internal/core/convergence"
)

// HookPlatform is the hook name a Platform module lists in its module.json for its PlatformModule.
const HookPlatform = "platform"

var (
	// ErrInvalidSpec means a Platform spec is malformed for its type.
	ErrInvalidSpec = errors.New("invalid platform spec")
	// ErrImmutableSpecField means a write changes a field a Platform cannot change after it was
	// created (desired-state-model §5.2); changing it means uninstalling and creating it again.
	ErrImmutableSpecField = errors.New("platform spec field cannot change after creation")
)

// PlatformChanges is what realizing a desired spec over the applied one does: each action
// classified as destructive or not (desired-state-model §5.2), and the actions swallow has no
// automation for yet. An unsupported destructive action blocks its Change Plan; an unsupported
// non-destructive one refuses the write.
type PlatformChanges struct {
	Actions     []convergence.PlannedAction
	Unsupported []string
	// Realization is the Workflow that carries out the supported actions.
	Realization PlatformRealization
}

// PlatformRealization names the Workflow that carries out a Platform spec change.
type PlatformRealization string

const (
	// RealizeNothing means the change needs no Workflow: the applied spec simply becomes the desired.
	RealizeNothing PlatformRealization = ""
	// RealizeDeploy runs the type's deploy Workflow over every member of the desired spec. It is
	// convergent, so a member already in the Platform is left as it is.
	RealizeDeploy PlatformRealization = "deploy"
	// RealizeUninstall runs the type's uninstall Workflow.
	RealizeUninstall PlatformRealization = "uninstall"
	// RealizeGPUOperator runs the GPU Operator change Workflow for the desired gpuOperator setting.
	RealizeGPUOperator PlatformRealization = "gpu-operator"
)

// UnsupportedDestructive reports whether an action swallow cannot carry out yet is destructive:
// such an action blocks the Change Plan, while an unsupported non-destructive one refuses the write.
func (c PlatformChanges) UnsupportedDestructive() (blocking, refusing []string) {
	for _, name := range c.Unsupported {
		index := slices.IndexFunc(c.Actions, func(action convergence.PlannedAction) bool { return action.Action == name })
		if index >= 0 && c.Actions[index].Destructive {
			blocking = append(blocking, name)
		} else {
			refusing = append(refusing, name)
		}
	}
	return blocking, refusing
}

// Destructive reports whether any action is destructive, which makes the write a Change Plan.
func (c PlatformChanges) Destructive() bool {
	return slices.ContainsFunc(c.Actions, func(action convergence.PlannedAction) bool { return action.Destructive })
}

// Empty reports whether realizing the spec changes nothing.
func (c PlatformChanges) Empty() bool {
	return len(c.Actions) == 0
}

// PlatformModule is the hook a Platform module implements for the Platform resource. Its spec's
// settings are the module's type-specific fields of the deploy request, and its members are the
// type's assignments with the type's role names; the hook converts between the two and classifies
// changes. It performs no I/O.
type PlatformModule interface {
	PlatformType() PlatformType
	// DeployRequest renders spec as the type-specific fields of a deploy request body: the
	// settings, with the members in the type's assignment form.
	DeployRequest(spec PlatformSpec) map[string]any
	// SpecFromDeployRequest reads the type-specific fields of a deploy request body as a present
	// spec; the machine preparation is set by the caller. A malformed body is ErrInvalidSpec.
	SpecFromDeployRequest(body map[string]any) (PlatformSpec, error)
	// Changes lists what realizing desired over applied does; applied nil is the first deploy and
	// desired absent an uninstall. A change to an immutable field is ErrImmutableSpecField.
	Changes(applied *PlatformSpec, desired PlatformSpec) (PlatformChanges, error)
	// ReportsMembership says whether the platform reports member as one of its nodes, so a member
	// that does not report membership has drifted.
	ReportsMembership(member PlatformMember) bool
}

// MemberChanges compares the members of two specs: the Servers only desired has, the ones only
// applied has, and the ones whose roles differ, each in the order of the spec that has them.
func MemberChanges(applied, desired PlatformSpec) (added, removed, changed []PlatformMember) {
	for _, member := range desired.Members {
		before, ok := applied.Member(member.ServerID)
		switch {
		case !ok:
			added = append(added, member)
		case !sameRoles(before.Roles, member.Roles):
			changed = append(changed, member)
		}
	}
	for _, member := range applied.Members {
		if _, ok := desired.Member(member.ServerID); !ok {
			removed = append(removed, member)
		}
	}
	return added, removed, changed
}

func sameRoles(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// SettingChanged reports whether a setting differs between two specs, treating an absent key and a
// JSON null alike.
func SettingChanged(applied, desired PlatformSpec, key string) bool {
	return !reflect.DeepEqual(applied.Settings[key], desired.Settings[key])
}

// SettingEnabled reports whether a feature setting is an object whose "enabled" is true.
func SettingEnabled(spec PlatformSpec, key string) bool {
	object, _ := spec.Settings[key].(map[string]any)
	enabled, _ := object["enabled"].(bool)
	return enabled
}

// SettingString returns a string setting, or "" when it is absent or not a string.
func SettingString(spec PlatformSpec, key string) string {
	value, _ := spec.Settings[key].(string)
	return value
}

// SettingMonitoringDashboards is the setting every Platform type keeps that gives the Platform its
// own monitoring dashboards (MonitoringDashboardsLaunch). It is desired state, on unless set to
// false.
const SettingMonitoringDashboards = "monitoringDashboards"

// MonitoringDashboardsWanted reports whether a spec gives the Platform its own monitoring
// dashboards: an absent or null setting is on, so only an explicit false turns them off.
func MonitoringDashboardsWanted(spec PlatformSpec) bool {
	enabled, ok := spec.Settings[SettingMonitoringDashboards].(bool)
	return !ok || enabled
}
