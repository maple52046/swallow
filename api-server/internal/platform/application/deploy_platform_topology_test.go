package application

import (
	"context"
	"errors"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
)

func standaloneDeployInput() DeployPlatformInput {
	return DeployPlatformInput{
		SiteID:        "site-1",
		Name:          "edge-k0s",
		GPUStackOwner: "provisioning",
		Spec: platformdomain.DeploymentSpec{
			K0sVersion: "v1.36.3+k0s.2",
			RoleAssignments: []platformdomain.RoleAssignment{
				{ServerID: "c1", Role: platformdomain.NodeRoleControlPlane, RunWorkloads: true},
			},
		},
		RequestedBy: "admin",
	}
}

func TestDeployStandaloneUsesControllerAddressWithoutVRRP(t *testing.T) {
	service, launcher, _ := newDeployHarness(
		deployedServer("c1", "edge-1", "site-1", "192.168.40.10"),
	)

	if _, err := service.Deploy(context.Background(), standaloneDeployInput()); err != nil {
		t.Fatalf("deploy standalone: %v", err)
	}
	if launcher.launched == nil {
		t.Fatal("launcher was not called")
	}

	vars := launcher.launched.TrustedVars
	if got := vars[varK0sHighAvailability]; got != false {
		t.Errorf("high availability = %v, want false", got)
	}
	if got := vars[varK0sAPIAddress]; got != "192.168.40.10" {
		t.Errorf("API address = %v, want initial controller address", got)
	}
	if got := vars[varK0sWorkloadIDs]; !equalStrings(got, []string{"c1"}) {
		t.Errorf("workload ids = %v, want [c1]", got)
	}
	if got := vars[varK0sWorkloadControllerIDs]; !equalStrings(got, []string{"c1"}) {
		t.Errorf("workload controller ids = %v, want [c1]", got)
	}
	if len(launcher.launched.SecretVars) != 0 {
		t.Errorf("standalone secret vars = %v, want none", launcher.launched.SecretVars)
	}
}

func TestDeployNonHAMultiNodeUsesOneControlPlane(t *testing.T) {
	service, launcher, _ := newDeployHarness(
		deployedServer("c1", "control-1", "site-1", "192.168.40.10"),
		deployedServer("w1", "worker-1", "site-1", "192.168.40.11"),
		deployedServer("w2", "worker-2", "site-1", "192.168.40.12"),
	)
	input := standaloneDeployInput()
	input.Spec.RoleAssignments = []platformdomain.RoleAssignment{
		{ServerID: "c1", Role: platformdomain.NodeRoleControlPlane},
		{ServerID: "w1", Role: platformdomain.NodeRoleWorker},
		{ServerID: "w2", Role: platformdomain.NodeRoleWorker},
	}

	if _, err := service.Deploy(context.Background(), input); err != nil {
		t.Fatalf("deploy non-HA multi-node: %v", err)
	}
	if got := launcher.launched.TrustedVars[varK0sWorkloadIDs]; !equalStrings(got, []string{"w1", "w2"}) {
		t.Errorf("workload ids = %v, want worker ids", got)
	}
	if len(launcher.launched.SecretVars) != 0 {
		t.Errorf("non-HA secret vars = %v, want none", launcher.launched.SecretVars)
	}
}

func TestDeployRejectsUnsupportedFlexibleTopology(t *testing.T) {
	cases := []struct {
		name  string
		input func() DeployPlatformInput
	}{
		{
			name: "single dedicated control plane has no workload capacity",
			input: func() DeployPlatformInput {
				input := standaloneDeployInput()
				input.Spec.RoleAssignments[0].RunWorkloads = false
				return input
			},
		},
		{
			name: "worker cannot set runWorkloads",
			input: func() DeployPlatformInput {
				input := standaloneDeployInput()
				input.Spec.RoleAssignments = []platformdomain.RoleAssignment{
					{ServerID: "c1", Role: platformdomain.NodeRoleControlPlane},
					{ServerID: "w1", Role: platformdomain.NodeRoleWorker, RunWorkloads: true},
				}
				return input
			},
		},
		{
			name: "non-HA cannot claim a VIP",
			input: func() DeployPlatformInput {
				input := standaloneDeployInput()
				input.Spec.APIVIP = "192.168.40.200"
				return input
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, _, _ := newDeployHarness(
				deployedServer("c1", "control-1", "site-1", "192.168.40.10"),
				deployedServer("w1", "worker-1", "site-1", "192.168.40.11"),
			)
			_, err := service.Deploy(context.Background(), tc.input())
			if !errors.Is(err, platformdomain.ErrInvalidDeployment) {
				t.Fatalf("error = %v, want invalid deployment", err)
			}
		})
	}
}

func TestDeployRejectsHAMissingVIP(t *testing.T) {
	service, _, _ := newDeployHarness(haServers()...)
	input := validDeployInput()
	input.Spec.APIVIP = ""

	_, err := service.Deploy(context.Background(), input)
	if !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Fatalf("error = %v, want invalid deployment", err)
	}
}

func TestDeployRejectsAddresslessNonHAControlPlane(t *testing.T) {
	service, _, _ := newDeployHarness(deployedServer("c1", "edge-1", "site-1"))

	_, err := service.Deploy(context.Background(), standaloneDeployInput())
	if !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Fatalf("error = %v, want invalid deployment", err)
	}
}

func equalStrings(value any, want []string) bool {
	got, ok := value.([]string)
	if !ok || len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
