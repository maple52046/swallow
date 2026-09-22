package application

import (
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// TestStaleFinishedDeployment pins the recovery rule: a finished deployment outcome on a
// machine that is back in a not-deployed provider state — `ready` (released) or `allocated`
// (reserved but not running an OS) — is stale and should be cleared, while an active or
// operator-pending deployment (or a machine still deployed) must be left alone so reconcile
// never races an in-flight Operation.
func TestStaleFinishedDeployment(t *testing.T) {
	ready := &provisioningdomain.Machine{Status: provisioningdomain.MachineStatusReady}
	allocated := &provisioningdomain.Machine{Status: provisioningdomain.MachineStatusAllocated}
	deployed := &provisioningdomain.Machine{Status: provisioningdomain.MachineStatusDeployed}

	cases := []struct {
		name       string
		machine    *provisioningdomain.Machine
		deployment *serverdomain.DeploymentStatus
		want       bool
	}{
		{"ready + failed is stale", ready, &serverdomain.DeploymentStatus{State: serverdomain.DeploymentFailed}, true},
		{"ready + canceled is stale", ready, &serverdomain.DeploymentStatus{State: serverdomain.DeploymentCanceled}, true},
		{"ready + succeeded is stale", ready, &serverdomain.DeploymentStatus{State: serverdomain.DeploymentSucceeded}, true},
		{"allocated + succeeded is stale", allocated, &serverdomain.DeploymentStatus{State: serverdomain.DeploymentSucceeded}, true},
		{"allocated + failed is stale", allocated, &serverdomain.DeploymentStatus{State: serverdomain.DeploymentFailed}, true},
		{"ready + deploying is active", ready, &serverdomain.DeploymentStatus{State: serverdomain.DeploymentDeploying}, false},
		{"ready + verifying is active", ready, &serverdomain.DeploymentStatus{State: serverdomain.DeploymentVerifying}, false},
		{"ready + requires_attention is operator-pending", ready, &serverdomain.DeploymentStatus{State: serverdomain.DeploymentRequiresAttention}, false},
		{"allocated + deploying is active", allocated, &serverdomain.DeploymentStatus{State: serverdomain.DeploymentDeploying}, false},
		{"deployed + failed is not on the ready pool", deployed, &serverdomain.DeploymentStatus{State: serverdomain.DeploymentFailed}, false},
		{"no deployment record", ready, nil, false},
		{"no machine observation", nil, &serverdomain.DeploymentStatus{State: serverdomain.DeploymentFailed}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := staleFinishedDeployment(tc.machine, tc.deployment); got != tc.want {
				t.Fatalf("staleFinishedDeployment = %v, want %v", got, tc.want)
			}
		})
	}
}
