package application

import (
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// TestStaleDeploymentOnReady pins the recovery rule: a finished deployment outcome on a
// machine that is back in the provider's Ready pool is stale and should be cleared, while an
// active or operator-pending deployment (or a machine still deployed) must be left alone so
// reconcile never races an in-flight Operation.
func TestStaleDeploymentOnReady(t *testing.T) {
	ready := &provisioningdomain.Machine{Status: provisioningdomain.MachineStatusReady}
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
		{"ready + deploying is active", ready, &serverdomain.DeploymentStatus{State: serverdomain.DeploymentDeploying}, false},
		{"ready + verifying is active", ready, &serverdomain.DeploymentStatus{State: serverdomain.DeploymentVerifying}, false},
		{"ready + requires_attention is operator-pending", ready, &serverdomain.DeploymentStatus{State: serverdomain.DeploymentRequiresAttention}, false},
		{"deployed + failed is not on the ready pool", deployed, &serverdomain.DeploymentStatus{State: serverdomain.DeploymentFailed}, false},
		{"no deployment record", ready, nil, false},
		{"no machine observation", nil, &serverdomain.DeploymentStatus{State: serverdomain.DeploymentFailed}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := staleDeploymentOnReady(tc.machine, tc.deployment); got != tc.want {
				t.Fatalf("staleDeploymentOnReady = %v, want %v", got, tc.want)
			}
		})
	}
}
