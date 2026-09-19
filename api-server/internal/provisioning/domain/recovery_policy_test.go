package domain

import "testing"

// TestEvaluateRecovery_AllowedSources locks the recovery matrix (decision 033): which
// intents each normalized state permits, so a future adapter or refactor cannot silently
// re-open the passthrough behaviour the policy replaced.
func TestEvaluateRecovery_AllowedSources(t *testing.T) {
	cases := []struct {
		intent  RecoveryIntent
		state   MachineStatus
		allowed bool
		noop    bool
	}{
		{RecoveryIntentRelease, MachineStatusDeployed, true, false},
		{RecoveryIntentRelease, MachineStatusFailed, true, false},
		{RecoveryIntentRelease, MachineStatusBroken, true, false},
		{RecoveryIntentRelease, MachineStatusRescue, true, false},
		{RecoveryIntentRelease, MachineStatusReady, false, false},
		{RecoveryIntentRelease, MachineStatusReleasing, false, false},

		{RecoveryIntentRecover, MachineStatusFailed, true, false},
		{RecoveryIntentRecover, MachineStatusBroken, true, false},
		{RecoveryIntentRecover, MachineStatusRescue, true, false},
		{RecoveryIntentRecover, MachineStatusDeployed, true, false},
		{RecoveryIntentRecover, MachineStatusReady, true, true},
		{RecoveryIntentRecover, MachineStatusReleasing, false, false},

		{RecoveryIntentMarkFixed, MachineStatusBroken, true, false},
		{RecoveryIntentMarkFixed, MachineStatusFailed, false, false},
		{RecoveryIntentMarkFixed, MachineStatusReady, false, false},

		{RecoveryIntentMarkBroken, MachineStatusReady, true, false},
		{RecoveryIntentMarkBroken, MachineStatusFailed, true, false},
		{RecoveryIntentMarkBroken, MachineStatusBroken, false, false},
		{RecoveryIntentMarkBroken, MachineStatusDeploying, false, false},

		{RecoveryIntentRescueEnter, MachineStatusDeployed, true, false},
		{RecoveryIntentRescueEnter, MachineStatusBroken, true, false},
		{RecoveryIntentRescueEnter, MachineStatusFailed, true, false},
		{RecoveryIntentRescueEnter, MachineStatusReady, false, false},
		{RecoveryIntentRescueEnter, MachineStatusRescue, false, false},

		{RecoveryIntentRescueExit, MachineStatusRescue, true, false},
		{RecoveryIntentRescueExit, MachineStatusDeployed, false, false},
	}
	for _, tc := range cases {
		decision := EvaluateRecovery(tc.intent, tc.state)
		if decision.Allowed != tc.allowed {
			t.Errorf("%s from %s: allowed=%v, want %v (reason %q)", tc.intent, tc.state, decision.Allowed, tc.allowed, decision.Reason)
		}
		if decision.Noop != tc.noop {
			t.Errorf("%s from %s: noop=%v, want %v", tc.intent, tc.state, decision.Noop, tc.noop)
		}
		if !decision.Allowed && decision.Reason == "" {
			t.Errorf("%s from %s: a refusal must carry a Swallow reason", tc.intent, tc.state)
		}
	}
}

// TestRecoverPlan_PrimitivesByState pins the ordered primitive sequence the Recover Step
// runs, including that rescue exits before releasing and broken uses Mark fixed only.
func TestRecoverPlan_PrimitivesByState(t *testing.T) {
	cases := []struct {
		state MachineStatus
		want  []RecoverPrimitive
	}{
		{MachineStatusBroken, []RecoverPrimitive{RecoverPrimitiveMarkFixed}},
		{MachineStatusRescue, []RecoverPrimitive{RecoverPrimitiveExitRescue, RecoverPrimitiveRelease}},
		{MachineStatusFailed, []RecoverPrimitive{RecoverPrimitiveRelease}},
		{MachineStatusDeployed, []RecoverPrimitive{RecoverPrimitiveRelease}},
		{MachineStatusReady, nil},
	}
	for _, tc := range cases {
		got := RecoverPlan(tc.state)
		if len(got) != len(tc.want) {
			t.Fatalf("RecoverPlan(%s) = %v, want %v", tc.state, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("RecoverPlan(%s)[%d] = %s, want %s", tc.state, i, got[i], tc.want[i])
			}
		}
	}
}
