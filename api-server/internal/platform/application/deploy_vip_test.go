package application

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
)

// staticNodeSubnets answers NodeSubnets from a fixed map, as the provisioner would.
type staticNodeSubnets map[string]netip.Prefix

func (s staticNodeSubnets) NodeSubnets(_ context.Context, ids []string, _ platformdomain.MachinePreparation) (map[string]netip.Prefix, error) {
	found := map[string]netip.Prefix{}
	for _, id := range ids {
		if subnet, ok := s[id]; ok {
			found[id] = subnet
		}
	}
	return found, nil
}

func controlPlaneOn(cidr string) staticNodeSubnets {
	subnet := netip.MustParsePrefix(cidr)
	return staticNodeSubnets{"c1": subnet, "c2": subnet, "c3": subnet}
}

// TestDeployTakesTheVIPPrefixFromTheControlPlaneSubnet pins the lab failure: the HA VIP prefix
// follows the control plane's subnet, because a longer one (the old default 24 inside a /23)
// makes the VIP holder reach part of the subnet from the VIP, and etcd rejects it as a peer.
func TestDeployTakesTheVIPPrefixFromTheControlPlaneSubnet(t *testing.T) {
	service, launcher, _ := newDeployHarness(haServers()...)
	service.AttachNodeSubnetResolver(controlPlaneOn("192.168.100.0/23"))

	if _, err := service.Deploy(context.Background(), validDeployInput()); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if got := launcher.launched.TrustedVars[varK0sAPIVIPPrefix]; got != 23 {
		t.Errorf("VIP prefix = %v, want 23 from the control-plane subnet", got)
	}
}

func TestDeployRefusesAVIPThatDoesNotFitTheControlPlaneSubnet(t *testing.T) {
	tests := []struct {
		name    string
		subnets staticNodeSubnets
		modify  func(*DeployPlatformInput)
	}{
		{name: "a longer prefix", subnets: controlPlaneOn("192.168.100.0/23"), modify: func(input *DeployPlatformInput) {
			input.Spec.APIVIPPrefix = 24
		}},
		{name: "a VIP outside the subnet", subnets: controlPlaneOn("192.168.100.0/25"), modify: func(input *DeployPlatformInput) {}},
		{name: "controllers on different subnets", subnets: staticNodeSubnets{
			"c1": netip.MustParsePrefix("192.168.100.0/24"), "c2": netip.MustParsePrefix("192.168.101.0/24"),
		}, modify: func(input *DeployPlatformInput) {}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service, launcher, platforms := newDeployHarness(haServers()...)
			service.AttachNodeSubnetResolver(tc.subnets)
			input := validDeployInput()
			tc.modify(&input)

			_, err := service.Deploy(context.Background(), input)
			if !errors.Is(err, platformdomain.ErrInvalidDeployment) {
				t.Fatalf("Deploy error = %v, want ErrInvalidDeployment", err)
			}
			if launcher.launched != nil || len(platforms.platforms) != 0 {
				t.Errorf("Deploy created %d platforms and launched %v, want nothing", len(platforms.platforms), launcher.launched)
			}
		})
	}
}

func TestDeployKeepsTheRequestedVIPPrefixWithoutASubnet(t *testing.T) {
	service, launcher, _ := newDeployHarness(haServers()...)
	service.AttachNodeSubnetResolver(staticNodeSubnets{})
	input := validDeployInput()
	input.Spec.APIVIPPrefix = 22

	if _, err := service.Deploy(context.Background(), input); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if got := launcher.launched.TrustedVars[varK0sAPIVIPPrefix]; got != 22 {
		t.Errorf("VIP prefix = %v, want the requested 22 when no subnet is readable", got)
	}
}
