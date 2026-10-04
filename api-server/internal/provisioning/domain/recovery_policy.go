package domain

// This file defines the Swallow-owned provider recovery policy: the single source of
// truth for which recovery intent is allowed from each normalized MachineStatus and which
// provider primitive sequence a Recover Operation uses to reach ready. It exists so the
// durable Release, the Recover Operation, and the single-action operator-state use cases
// share one matrix and never drift, and so an operator meets a Swallow-authored reason
// before the provisioner's own rejection text. See docs/decisions/033.

// RecoveryIntent is one Swallow-owned command that can be directed at a Server whose
// provisioning axis is not usable, or at an advanced operator-state primitive.
type RecoveryIntent string

const (
	// RecoveryIntentRelease returns a Machine to the provider's ready pool.
	RecoveryIntentRelease RecoveryIntent = "release"
	// RecoveryIntentRecover is the state-driven "Return to Ready" orchestration.
	RecoveryIntentRecover RecoveryIntent = "recover"
	// RecoveryIntentMarkFixed clears a provider-marked broken Machine.
	RecoveryIntentMarkFixed RecoveryIntent = "mark-fixed"
	// RecoveryIntentMarkBroken flags a Machine as unusable.
	RecoveryIntentMarkBroken RecoveryIntent = "mark-broken"
	// RecoveryIntentRescueEnter boots the provider diagnostic environment.
	RecoveryIntentRescueEnter RecoveryIntent = "rescue-mode"
	// RecoveryIntentRescueExit leaves the diagnostic environment.
	RecoveryIntentRescueExit RecoveryIntent = "exit-rescue-mode"
)

// RecoverPrimitive is a single provider action the Recover Operation runs. A Recover Step
// executes the plan in order, observing provider state between primitives and stopping as
// soon as the Machine is ready.
type RecoverPrimitive string

const (
	// RecoverPrimitiveMarkFixed asks the provider to clear a broken Machine.
	RecoverPrimitiveMarkFixed RecoverPrimitive = "mark-fixed"
	// RecoverPrimitiveMarkBroken flags a Machine broken as a recovery escalation. It is used
	// only to unstick a Machine that cannot leave rescue: a provider that keeps failing to exit
	// rescue still accepts Mark broken from rescue, and a subsequent Mark fixed then returns it
	// to ready without the disk erase a Release would attempt.
	RecoverPrimitiveMarkBroken RecoverPrimitive = "mark-broken"
	// RecoverPrimitiveExitRescue leaves rescue before any further recovery.
	RecoverPrimitiveExitRescue RecoverPrimitive = "exit-rescue-mode"
	// RecoverPrimitiveRelease returns the Machine to the ready pool.
	RecoverPrimitiveRelease RecoverPrimitive = "release"
)

// RecoveryDecision is the outcome of evaluating an intent against a live MachineStatus.
// Allowed reports whether Swallow will call the provider; Reason carries the
// Swallow-authored explanation when Allowed is false; Noop reports that the requested end
// state already holds, so the caller may report success without touching the provider.
type RecoveryDecision struct {
	Allowed bool
	Reason  string
	Noop    bool
}

// activeLifecycle reports that the provider is mid-transition, where a recovery command
// would race in-flight work rather than converge it.
func activeLifecycle(state MachineStatus) bool {
	switch state {
	case MachineStatusInspecting,
		MachineStatusDeploying,
		MachineStatusReleasing,
		MachineStatusTesting:
		return true
	default:
		return false
	}
}

// EvaluateRecovery decides whether an intent may run against a Server currently in state,
// returning a Swallow-authored reason when it may not. It is the one place the recovery
// matrix is expressed; callers translate a refusal into an ErrServerMutationConflict that
// names the Server.
func EvaluateRecovery(intent RecoveryIntent, state MachineStatus) RecoveryDecision {
	switch intent {
	case RecoveryIntentRelease:
		return releaseDecision(state)
	case RecoveryIntentRecover:
		return recoverDecision(state)
	case RecoveryIntentMarkFixed:
		return markFixedDecision(state)
	case RecoveryIntentMarkBroken:
		return markBrokenDecision(state)
	case RecoveryIntentRescueEnter:
		return rescueEnterDecision(state)
	case RecoveryIntentRescueExit:
		return rescueExitDecision(state)
	default:
		return RecoveryDecision{Allowed: false, Reason: "is not a recognised recovery action."}
	}
}

// releaseAllowedStates and recoverAllowedStates are the states Release and Recover may
// start from. Recover additionally treats ready as a no-op success. `allocated` is a
// recovery source because MAAS holds a machine there when a deployment was reserved but
// never completed (a failed or canceled deploy, or a verification borrow that ended
// early); the provider allows Release from it, so leaving it out stranded such machines
// as an un-actionable "reserved" dead end.
func releaseDecision(state MachineStatus) RecoveryDecision {
	switch state {
	case MachineStatusDeployed, MachineStatusAllocated, MachineStatusFailed, MachineStatusBroken, MachineStatusRescue:
		return RecoveryDecision{Allowed: true}
	case MachineStatusReleasing:
		return RecoveryDecision{Allowed: false, Reason: "is already releasing."}
	case MachineStatusReady:
		return RecoveryDecision{Allowed: false, Reason: "is already in the ready pool; Release is not needed."}
	default:
		return RecoveryDecision{Allowed: false, Reason: "is " + string(state) + ". Release is available from deployed, allocated, failed, broken, or rescue."}
	}
}

func recoverDecision(state MachineStatus) RecoveryDecision {
	switch state {
	case MachineStatusReady:
		return RecoveryDecision{Allowed: true, Noop: true}
	case MachineStatusDeployed, MachineStatusAllocated, MachineStatusFailed, MachineStatusBroken, MachineStatusRescue:
		return RecoveryDecision{Allowed: true}
	default:
		return RecoveryDecision{Allowed: false, Reason: "is " + string(state) + ". Recover is available from deployed, allocated, failed, broken, or rescue."}
	}
}

func markFixedDecision(state MachineStatus) RecoveryDecision {
	if state == MachineStatusBroken {
		return RecoveryDecision{Allowed: true}
	}
	if state == MachineStatusFailed {
		return RecoveryDecision{Allowed: false, Reason: "is failed, not broken. Use Recover or Release to return it to Ready; Mark fixed only clears a Broken Machine."}
	}
	return RecoveryDecision{Allowed: false, Reason: "is " + string(state) + ". Mark fixed only clears a Broken Machine."}
}

func markBrokenDecision(state MachineStatus) RecoveryDecision {
	if state == MachineStatusBroken {
		return RecoveryDecision{Allowed: false, Reason: "is already broken."}
	}
	if activeLifecycle(state) {
		return RecoveryDecision{Allowed: false, Reason: "is " + string(state) + ". Wait for the provider lifecycle action to finish before marking it broken."}
	}
	return RecoveryDecision{Allowed: true}
}

func rescueEnterDecision(state MachineStatus) RecoveryDecision {
	switch state {
	case MachineStatusDeployed, MachineStatusBroken, MachineStatusFailed:
		return RecoveryDecision{Allowed: true}
	case MachineStatusRescue:
		return RecoveryDecision{Allowed: false, Reason: "is already in rescue mode."}
	default:
		return RecoveryDecision{Allowed: false, Reason: "is " + string(state) + ". Rescue mode is available from deployed, broken, or failed."}
	}
}

func rescueExitDecision(state MachineStatus) RecoveryDecision {
	if state == MachineStatusRescue {
		return RecoveryDecision{Allowed: true}
	}
	return RecoveryDecision{Allowed: false, Reason: "is not in rescue mode."}
}

// RecoverPlan returns the ordered provider primitives a Recover Step attempts for a
// Machine in state. The Step observes provider state after each primitive and stops once
// the Machine is ready, so the plan is a superset of what a given run may need: a rescue
// Machine exits rescue and then, only if it is still not ready, is released.
//
// An empty plan means Recover has nothing to do (the Machine is already ready) or the
// state is not recoverable; callers gate with EvaluateRecovery before planning.
func RecoverPlan(state MachineStatus) []RecoverPrimitive {
	switch state {
	case MachineStatusBroken:
		return []RecoverPrimitive{RecoverPrimitiveMarkFixed}
	case MachineStatusRescue:
		return []RecoverPrimitive{RecoverPrimitiveExitRescue, RecoverPrimitiveRelease}
	case MachineStatusFailed, MachineStatusDeployed, MachineStatusAllocated:
		return []RecoverPrimitive{RecoverPrimitiveRelease}
	default:
		return nil
	}
}
