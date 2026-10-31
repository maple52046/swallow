package convergence

import (
	"slices"
	"time"
)

// ConditionType names one of the shared judgements in a resource's status. The set is closed: a
// feature may add its own type but must not change what these mean (glossary: Condition).
type ConditionType string

const (
	// ConditionRealized is true when observed state matches the current spec.
	ConditionRealized ConditionType = "realized"
	// ConditionDrifted is true when an observed field departed from the spec without swallow asking.
	ConditionDrifted ConditionType = "drifted"
	// ConditionRequiresAttention is true when the controller stopped trying and an operator must act.
	ConditionRequiresAttention ConditionType = "requires_attention"
	// ConditionHeld is true while convergence is paused (glossary: Hold).
	ConditionHeld ConditionType = "held"
)

// ConditionStatus is a Condition's truth value.
type ConditionStatus string

const (
	ConditionTrue    ConditionStatus = "true"
	ConditionFalse   ConditionStatus = "false"
	ConditionUnknown ConditionStatus = "unknown"
)

// Reasons shared by every resource. They are wire values listed in the glossary; a resource may
// add reasons of its own.
const (
	// ReasonConverged: realized is true for the current spec.
	ReasonConverged = "converged"
	// ReasonConverging: the controller has not yet realized the current spec.
	ReasonConverging = "converging"
	// ReasonCapabilityMissing: the provider cannot realize a field; the desired value is kept.
	ReasonCapabilityMissing = "capability_missing"
	// ReasonProviderUnreachable: a transient provider error; the controller keeps retrying.
	ReasonProviderUnreachable = "provider_unreachable"
	// ReasonProviderMissing: the provider no longer has the resource.
	ReasonProviderMissing = "provider_missing"
	// ReasonProviderRejected: the provider refused the change; the controller stopped trying.
	ReasonProviderRejected = "provider_rejected"
	// ReasonInSync: drifted is false; observed state matches the spec.
	ReasonInSync = "in_sync"
	// ReasonDriftDetected: drifted is true; the message lists the fields.
	ReasonDriftDetected = "drift_detected"
	// ReasonNotHeld: held is false.
	ReasonNotHeld = "not_held"
	// ReasonAttentionNotNeeded: requires_attention is false.
	ReasonAttentionNotNeeded = "none"
	// ReasonServerLocked, ReasonRescueMode, ReasonWorkflowLease: why a Server is held.
	ReasonServerLocked  = "server_locked"
	ReasonRescueMode    = "rescue_mode"
	ReasonWorkflowLease = "workflow_lease"
)

// Condition is one named judgement in a resource's status, written only by that resource's
// controller. ObservedGeneration is the spec generation the judgement was made for, so a client can
// tell a stale judgement (made for an older spec) from a current one.
type Condition struct {
	Type               ConditionType
	Status             ConditionStatus
	Reason             string
	Message            string
	LastTransitionTime time.Time
	ObservedGeneration int64
}

// SetCondition returns conditions with next applied and whether anything changed. A condition of
// the same type is replaced; its LastTransitionTime is kept when the status does not change, and set
// to now when it does or when the type is new. The input slice is not modified. Controllers write
// their status only when changed is true, so an unchanged judgement never produces a store write
// (and so no change-feed event that would wake the controller again).
func SetCondition(conditions []Condition, next Condition, now time.Time) ([]Condition, bool) {
	out := slices.Clone(conditions)
	for i, current := range out {
		if current.Type != next.Type {
			continue
		}
		next.LastTransitionTime = current.LastTransitionTime
		if current.Status != next.Status || next.LastTransitionTime.IsZero() {
			next.LastTransitionTime = now
		}
		if current == next {
			return out, false
		}
		out[i] = next
		return out, true
	}
	next.LastTransitionTime = now
	return append(out, next), true
}

// FindCondition returns the condition of type t, if present.
func FindCondition(conditions []Condition, t ConditionType) (Condition, bool) {
	for _, c := range conditions {
		if c.Type == t {
			return c, true
		}
	}
	return Condition{}, false
}

// IsTrue reports whether the condition of type t is present and true.
func IsTrue(conditions []Condition, t ConditionType) bool {
	c, ok := FindCondition(conditions, t)
	return ok && c.Status == ConditionTrue
}

// TargetStatus is a resource's realization on one target — for example one provisioner Integration,
// for a resource realized on every provisioner of its Site (an SSH Key, a Zone). ObservedAt is when
// the target was last observed, nil before the first observation.
type TargetStatus struct {
	TargetID           string
	ObservedGeneration int64
	ObservedAt         *time.Time
	Conditions         []Condition
}
