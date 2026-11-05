package app

import (
	"reflect"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
)

// A Platform's machine preparation forwards its network intent whenever it carries any network
// field, even without a mode, so the provisioning preflight keeps a Deployment Template's mode and
// still receives the per-target assignments. Without any network field the request carries no
// network intent at all.
func TestPlatformProvisioningInputForwardsNetworkWithoutMode(t *testing.T) {
	assignment := platformdomain.MachineNetworkAssignment{ServerID: "a", InterfaceID: "nic-a", SubnetID: "subnet-a", IPAddress: "192.0.2.40"}
	forwarded := provisioningapp.DeploymentNetworkAssignmentInput{ServerID: "a", InterfaceID: "nic-a", SubnetID: "subnet-a", IPAddress: "192.0.2.40"}
	tests := []struct {
		name        string
		preparation platformdomain.MachinePreparation
		want        *provisioningapp.DeploymentNetworkInput
	}{
		{
			name:        "no network field sends no network intent",
			preparation: platformdomain.MachinePreparation{TemplateID: "template-a"},
		},
		{
			name: "assignments without a mode are forwarded with an empty mode",
			preparation: platformdomain.MachinePreparation{
				TemplateID: "template-a", Assignments: []platformdomain.MachineNetworkAssignment{assignment},
			},
			want: &provisioningapp.DeploymentNetworkInput{Assignments: []provisioningapp.DeploymentNetworkAssignmentInput{forwarded}},
		},
		{
			name:        "a subnet without a mode is forwarded",
			preparation: platformdomain.MachinePreparation{SubnetID: "subnet-a"},
			want:        &provisioningapp.DeploymentNetworkInput{SubnetID: "subnet-a", Assignments: []provisioningapp.DeploymentNetworkAssignmentInput{}},
		},
		{
			name:        "a default gateway without a mode is forwarded",
			preparation: platformdomain.MachinePreparation{DefaultGateway: true},
			want:        &provisioningapp.DeploymentNetworkInput{DefaultGateway: true, Assignments: []provisioningapp.DeploymentNetworkAssignmentInput{}},
		},
		{
			name: "an explicit mode is forwarded with every field",
			preparation: platformdomain.MachinePreparation{
				NetworkMode: "static", SubnetID: "subnet-a", DefaultGateway: true,
				Assignments: []platformdomain.MachineNetworkAssignment{assignment},
			},
			want: &provisioningapp.DeploymentNetworkInput{
				Mode: "static", SubnetID: "subnet-a", DefaultGateway: true,
				Assignments: []provisioningapp.DeploymentNetworkAssignmentInput{forwarded},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := platformProvisioningInput(tc.preparation, []string{"a"}).Network
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("platformProvisioningInput(%+v).Network = %+v, want %+v", tc.preparation, got, tc.want)
			}
		})
	}
}
