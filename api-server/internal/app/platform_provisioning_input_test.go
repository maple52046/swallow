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
	gateway := true
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
			preparation: platformdomain.MachinePreparation{DefaultGateway: &gateway},
			want:        &provisioningapp.DeploymentNetworkInput{DefaultGateway: &gateway, Assignments: []provisioningapp.DeploymentNetworkAssignmentInput{}},
		},
		{
			name: "an explicit mode is forwarded with every field",
			preparation: platformdomain.MachinePreparation{
				NetworkMode: "static", SubnetID: "subnet-a", DefaultGateway: &gateway,
				Assignments: []platformdomain.MachineNetworkAssignment{assignment},
			},
			want: &provisioningapp.DeploymentNetworkInput{
				Mode: "static", SubnetID: "subnet-a", DefaultGateway: &gateway,
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

// A member installed from its node group's template takes everything from the template except its
// own network assignment (decision 083): the request names the template, inherits its cloud-init,
// and leaves the network mode, subnet, and gateway to it; members are batched by template.
func TestPlatformProvisioningInputInstallsAMemberFromItsTemplate(t *testing.T) {
	ram := true
	preparation := platformdomain.MachinePreparation{
		Mode: platformdomain.MachinePreparationProvisionOS, ImageID: "inline-image", Ephemeral: &ram, NetworkMode: "dhcp",
		Assignments: []platformdomain.MachineNetworkAssignment{
			{ServerID: "a", InterfaceID: "nic-a", TemplateID: "tpl-compute"},
			{ServerID: "b", TemplateID: "tpl-compute"},
			{ServerID: "c"},
		},
	}
	batches := byMemberTemplate(preparation, []string{"a", "c", "b"})
	if !reflect.DeepEqual(batches, [][]string{{"a", "b"}, {"c"}}) {
		t.Fatalf("batches = %v, want the template's members together and the inline one apart", batches)
	}
	input := platformProvisioningInput(preparation, batches[0])
	if input.TemplateID != "tpl-compute" || input.Settings.ImageID != nil || input.Settings.Ephemeral != nil || input.UserData.Mode != "inherit" {
		t.Fatalf("template batch = %+v, want the template alone to decide the image, mode, and cloud-init", input)
	}
	want := &provisioningapp.DeploymentNetworkInput{Assignments: []provisioningapp.DeploymentNetworkAssignmentInput{{ServerID: "a", InterfaceID: "nic-a"}}}
	if !reflect.DeepEqual(input.Network, want) {
		t.Errorf("template batch network = %+v, want only a's interface", input.Network)
	}
	inline := platformProvisioningInput(preparation, batches[1])
	if inline.TemplateID != "" || inline.Settings.ImageID == nil || *inline.Settings.ImageID != "inline-image" {
		t.Errorf("inline batch = %+v, want the machine preparation's image", inline)
	}
}
