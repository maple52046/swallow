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
