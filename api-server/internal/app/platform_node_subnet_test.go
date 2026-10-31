package app

import (
	"net/netip"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// TestNodeSubnetReadsTheAssignedOrBootSubnet pins where an HA deploy reads a control-plane
// Server's subnet: the subnet its network assignment names when the deploy provisions it, else
// the boot interface's first IPv4 link. Host bits in a provider CIDR are masked.
func TestNodeSubnetReadsTheAssignedOrBootSubnet(t *testing.T) {
	interfaces := []provisioningdomain.NetworkInterface{
		{ID: "130", Boot: false, Links: []provisioningdomain.NetworkLink{{SubnetID: "9", CIDR: "10.9.0.0/16"}}},
		{
			ID: "131", Boot: true,
			Links: []provisioningdomain.NetworkLink{
				{SubnetID: "6", CIDR: "fd00::/64"},
				{SubnetID: "1", CIDR: "10.170.169.126/23"},
			},
			AvailableSubnets: []provisioningdomain.NetworkSubnet{{ID: "2", CIDR: "10.170.172.0/22"}},
		},
	}
	tests := []struct {
		name       string
		assignment platformdomain.MachineNetworkAssignment
		want       string
	}{
		{name: "boot interface link", want: "10.170.168.0/23"},
		{name: "assigned available subnet", assignment: platformdomain.MachineNetworkAssignment{InterfaceID: "131", SubnetID: "2"}, want: "10.170.172.0/22"},
		{name: "assigned linked subnet", assignment: platformdomain.MachineNetworkAssignment{InterfaceID: "130", SubnetID: "9"}, want: "10.9.0.0/16"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := nodeSubnet(interfaces, tc.assignment)
			if !ok || got != netip.MustParsePrefix(tc.want) {
				t.Errorf("nodeSubnet = %v, %v; want %s", got, ok, tc.want)
			}
		})
	}
	if _, ok := nodeSubnet(interfaces, platformdomain.MachineNetworkAssignment{InterfaceID: "131", SubnetID: "7"}); ok {
		t.Error("nodeSubnet found a subnet the assigned interface does not offer")
	}
}
