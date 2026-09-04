package app

import "testing"

func TestSSHReadinessTimeoutMessageSeparatesMissingAddresses(t *testing.T) {
	message := sshReadinessTimeoutMessage(
		[]string{"lab-compute-2", "lab-control-1"},
		[]string{"lab-control-3"},
		22,
	)
	want := "No provider address was observed after OS deployment for: lab-compute-2, lab-control-1. SSH port 22 did not become reachable for: lab-control-3."
	if message != want {
		t.Fatalf("ssh readiness message = %q, want %q", message, want)
	}
}
