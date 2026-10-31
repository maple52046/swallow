package domain

import "errors"

// This file defines the Swallow-owned policy for hardware inspection (decision 053): from which
// normalized states an inspection may start, and who asked for it. The durable inspect-hardware
// Workflow and the automatic sweep both consult it, so an operator meets a Swallow-authored
// reason before the provisioner's own rejection text.

// ErrInspectionNotAllowed means a hardware inspection was requested for a Server whose
// provisioning state does not allow one (in-progress work, or an installed OS that inspection
// would destroy). Callers wrap it with a message naming the Server.
var ErrInspectionNotAllowed = errors.New("hardware inspection is not allowed in this state")

// InspectionOrigin records who started an inspect-hardware Workflow. It is part of the Workflow's
// intent snapshot and decides whether the Workflow waits for the provider's enrollment to finish.
type InspectionOrigin string

const (
	// InspectionOriginAutomatic is the sweep that inspects a newly enrolled Server. It waits for
	// the provider's enrollment to finish, because the Server it acts on was just discovered.
	InspectionOriginAutomatic InspectionOrigin = "automatic"
	// InspectionOriginRequested is an operator's Inspect. The operator asserts the Server may boot
	// now, so the enrollment wait is skipped.
	InspectionOriginRequested InspectionOrigin = "requested"
)

// InspectionDecision is the outcome of evaluating a state against the inspection policy. Reason
// is the Swallow-authored explanation when Allowed is false, phrased to follow the Server's name.
type InspectionDecision struct {
	Allowed bool
	Reason  string
}

// EvaluateInspection decides whether a hardware inspection may start from state.
//
// Inspection boots the Server into the provider's inspection environment, so it is refused while
// other provider work runs (it would race it) and from a state that holds an installed or reserved
// OS (the operator must Release first). `unknown` is allowed so a provider state this version does
// not map stays inspectable; the provider remains the final authority.
func EvaluateInspection(state MachineStatus) InspectionDecision {
	switch state {
	case MachineStatusNew, MachineStatusReady, MachineStatusFailed, MachineStatusBroken, MachineStatusUnknown:
		return InspectionDecision{Allowed: true}
	case MachineStatusInspecting:
		return InspectionDecision{Reason: "is already being inspected."}
	case MachineStatusDeploying, MachineStatusReleasing, MachineStatusTesting:
		return InspectionDecision{Reason: "is " + string(state) + ". Wait for the provider lifecycle action to finish before inspecting it."}
	case MachineStatusDeployed, MachineStatusAllocated:
		return InspectionDecision{Reason: "is " + string(state) + ". Release it before inspecting its hardware; inspection reboots the Server."}
	default:
		return InspectionDecision{Reason: "is " + string(state) + ". Inspect is available from new, ready, failed, or broken."}
	}
}
