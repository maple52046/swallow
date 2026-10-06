package domain

import "testing"

// Inspection starts only from states it cannot destroy or race (decision 053).
func TestEvaluateInspection(t *testing.T) {
	allowed := map[MachineStatus]bool{
		MachineStatusNew: true, MachineStatusReady: true, MachineStatusFailed: true,
		MachineStatusBroken: true, MachineStatusUnknown: true,
	}
	for _, state := range []MachineStatus{
		MachineStatusNew, MachineStatusInspecting, MachineStatusReady, MachineStatusAllocated,
		MachineStatusDeploying, MachineStatusDeployed, MachineStatusReleasing, MachineStatusTesting,
		MachineStatusRescue, MachineStatusBroken, MachineStatusFailed, MachineStatusRetired, MachineStatusUnknown,
	} {
		decision := EvaluateInspection(state)
		if decision.Allowed != allowed[state] {
			t.Errorf("EvaluateInspection(%s).Allowed = %v, want %v", state, decision.Allowed, allowed[state])
		}
		if !decision.Allowed && decision.Reason == "" {
			t.Errorf("EvaluateInspection(%s) refused without a reason", state)
		}
	}
}
