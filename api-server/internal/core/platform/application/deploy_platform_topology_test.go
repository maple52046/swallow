package application

import (
	"context"
	"errors"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
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

// readyServer is a Ready (not yet deployed) target observed with addresses, the addresses its
// provisioner still reports from before a provision_os deploy provisions it.
func readyServer(id, hostname string, addresses ...string) *serverdomain.Server {
	server := deployedServer(id, hostname, "site-1", addresses...)
	server.Provisioning.State = "ready"
	return server
}

// nonHAProvisionOSInput is a non-HA multi-node provision_os deploy: c1 is the initial control
// plane, w1 its worker, and the Servers it provisions get network mode mode.
func nonHAProvisionOSInput(mode string) DeployPlatformInput {
	input := standaloneDeployInput()
	input.Spec.RoleAssignments = []platformdomain.RoleAssignment{
		{ServerID: "c1", Role: platformdomain.NodeRoleControlPlane},
		{ServerID: "w1", Role: platformdomain.NodeRoleWorker},
	}
	input.MachinePreparation = platformdomain.MachinePreparation{
		Mode: platformdomain.MachinePreparationProvisionOS, ImageID: "ubuntu/noble", NetworkMode: mode,
	}
	return input
}

// staticAssignment is a static network assignment of address to serverID.
func staticAssignment(serverID, address string) platformdomain.MachineNetworkAssignment {
	return platformdomain.MachineNetworkAssignment{ServerID: serverID, InterfaceID: "eth0", SubnetID: "subnet-1", IPAddress: address}
}

// A non-HA endpoint is the initial control plane's address after the deploy. A Ready Server's
// observed address predates provisioning, so under automatic or dhcp addressing the endpoint is left
// to the run (the playbook reads the controller's inventory address there); a reused deployed
// controller keeps its observed address, and static addressing uses the assignment. The lab
// failure: a Ready controller observed at .126 was provisioned at .143, but the credential named .126.
func TestDeployProvisionOSResolvesTheNonHAAPIAddressAfterProvisioning(t *testing.T) {
	ram := true
	tests := []struct {
		name    string
		servers []*serverdomain.Server
		input   func() DeployPlatformInput
		want    string
	}{
		{
			name:    "automatic RAM provision of a Ready controller observed at an old address",
			servers: []*serverdomain.Server{readyServer("c1", "mi308-1", "10.170.169.126"), readyServer("w1", "mi308-2", "10.170.169.127")},
			input: func() DeployPlatformInput {
				input := nonHAProvisionOSInput("automatic")
				input.MachinePreparation.Ephemeral = &ram
				return input
			},
			want: "",
		},
		{
			name:    "dhcp provision of a Ready controller observed at an old address",
			servers: []*serverdomain.Server{readyServer("c1", "mi308-1", "10.170.169.126"), readyServer("w1", "mi308-2", "10.170.169.127")},
			input:   func() DeployPlatformInput { return nonHAProvisionOSInput("dhcp") },
			want:    "",
		},
		{
			name:    "an omitted mode is automatic",
			servers: []*serverdomain.Server{readyServer("c1", "mi308-1", "10.170.169.126"), readyServer("w1", "mi308-2")},
			input:   func() DeployPlatformInput { return nonHAProvisionOSInput("") },
			want:    "",
		},
		{
			name:    "a Ready controller without any address under dhcp",
			servers: []*serverdomain.Server{readyServer("c1", "mi308-1"), readyServer("w1", "mi308-2")},
			input:   func() DeployPlatformInput { return nonHAProvisionOSInput("DHCP") },
			want:    "",
		},
		{
			name:    "a deployed controller reused beside a Ready worker keeps its address",
			servers: []*serverdomain.Server{deployedServer("c1", "mi308-1", "site-1", "10.170.169.143"), readyServer("w1", "mi308-2", "10.170.169.127")},
			input:   func() DeployPlatformInput { return nonHAProvisionOSInput("automatic") },
			want:    "10.170.169.143",
		},
		{
			name:    "static provision uses the controller's assignment, not its old address",
			servers: []*serverdomain.Server{readyServer("c1", "mi308-1", "10.170.169.126"), readyServer("w1", "mi308-2")},
			input: func() DeployPlatformInput {
				input := nonHAProvisionOSInput(" Static ")
				input.MachinePreparation.Assignments = []platformdomain.MachineNetworkAssignment{
					staticAssignment("c1", "10.170.169.150"), workerStaticAssignment(),
				}
				return input
			},
			want: "10.170.169.150",
		},
		{
			name:    "static with a deployed controller and no assignment for it uses its address",
			servers: []*serverdomain.Server{deployedServer("c1", "mi308-1", "site-1", "10.170.169.143"), readyServer("w1", "mi308-2")},
			input: func() DeployPlatformInput {
				input := nonHAProvisionOSInput("static")
				input.MachinePreparation.Assignments = []platformdomain.MachineNetworkAssignment{workerStaticAssignment()}
				return input
			},
			want: "10.170.169.143",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service, launcher, _ := newDeployHarness(tc.servers...)

			if _, err := service.Deploy(context.Background(), tc.input()); err != nil {
				t.Fatalf("Deploy() error = %v", err)
			}
			got, ok := launcher.launched.TrustedVars[varK0sAPIAddress]
			if !ok || got != tc.want {
				t.Errorf("TrustedVars[%s] = %#v (present %v), want %q", varK0sAPIAddress, got, ok, tc.want)
			}
		})
	}
}

// workerStaticAssignment is w1's static assignment, which every static case needs because the
// worker is provisioned too.
func workerStaticAssignment() platformdomain.MachineNetworkAssignment {
	return staticAssignment("w1", "10.170.169.151")
}

// A non-HA provision_os deploy is refused before anything is created when its endpoint cannot be
// resolved, or when the request carries HA-only fields in any network mode: an automatic or dhcp
// endpoint resolved in the run must not let an apiVip through unused.
func TestDeployProvisionOSRefusesAnUnresolvableNonHAAPIAddress(t *testing.T) {
	tests := []struct {
		name    string
		servers []*serverdomain.Server
		input   func() DeployPlatformInput
	}{
		{
			name:    "static provision without the controller's assignment",
			servers: []*serverdomain.Server{readyServer("c1", "mi308-1", "10.170.169.126"), readyServer("w1", "mi308-2")},
			input: func() DeployPlatformInput {
				input := nonHAProvisionOSInput("static")
				input.MachinePreparation.Assignments = []platformdomain.MachineNetworkAssignment{workerStaticAssignment()}
				return input
			},
		},
		{
			name:    "static provision with an invalid controller address",
			servers: []*serverdomain.Server{readyServer("c1", "mi308-1"), readyServer("w1", "mi308-2")},
			input: func() DeployPlatformInput {
				input := nonHAProvisionOSInput("static")
				input.MachinePreparation.Assignments = []platformdomain.MachineNetworkAssignment{
					staticAssignment("c1", "not-an-ip"), workerStaticAssignment(),
				}
				return input
			},
		},
		{
			name:    "a reused controller without an address",
			servers: []*serverdomain.Server{deployedServer("c1", "mi308-1", "site-1"), readyServer("w1", "mi308-2")},
			input:   func() DeployPlatformInput { return nonHAProvisionOSInput("automatic") },
		},
		{
			name:    "an apiVip without high availability",
			servers: []*serverdomain.Server{readyServer("c1", "mi308-1"), readyServer("w1", "mi308-2")},
			input: func() DeployPlatformInput {
				input := nonHAProvisionOSInput("dhcp")
				input.Spec.APIVIP = "10.170.169.200"
				return input
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service, launcher, platforms := newDeployHarness(tc.servers...)

			_, err := service.Deploy(context.Background(), tc.input())
			if !errors.Is(err, platformdomain.ErrInvalidDeployment) {
				t.Fatalf("Deploy() error = %v, want ErrInvalidDeployment", err)
			}
			if launcher.launched != nil || len(platforms.platforms) != 0 {
				t.Errorf("a refused deploy left %d Platforms and launched %v, want neither", len(platforms.platforms), launcher.launched)
			}
		})
	}
}

// HA keeps the VIP as its endpoint in provision_os too, whatever the controllers' addresses.
func TestDeployProvisionOSKeepsTheHAVIPEndpoint(t *testing.T) {
	servers := haServers()
	servers[0].Provisioning.State = "ready"
	service, launcher, _ := newDeployHarness(servers...)
	input := provisionOSInput()
	input.MachinePreparation.NetworkMode = "automatic"

	if _, err := service.Deploy(context.Background(), input); err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if got := launcher.launched.TrustedVars[varK0sAPIAddress]; got != "192.168.100.200" {
		t.Errorf("TrustedVars[%s] = %#v, want the VIP 192.168.100.200", varK0sAPIAddress, got)
	}
}

// Acceptance checks the pod and service networks and the HA VIP against the addresses the nodes
// will have after the deploy: a reused Server's observed addresses and a static assignment, but not
// the old addresses of a Server provisioned with automatic or dhcp addressing, which predict nothing
// (the playbook checks those nodes in the run).
func TestDeployNetworkChecksReadTheAddressesNodesWillHave(t *testing.T) {
	tests := []struct {
		name    string
		servers []*serverdomain.Server
		input   func() DeployPlatformInput
		wantErr bool
	}{
		{
			name:    "an automatic provision's old address inside the pod CIDR",
			servers: []*serverdomain.Server{readyServer("c1", "mi308-1", "10.244.3.7"), readyServer("w1", "mi308-2")},
			input:   func() DeployPlatformInput { return nonHAProvisionOSInput("automatic") },
		},
		{
			name:    "a dhcp provision's old address inside the service CIDR",
			servers: []*serverdomain.Server{readyServer("c1", "mi308-1"), readyServer("w1", "mi308-2", "10.96.0.9")},
			input:   func() DeployPlatformInput { return nonHAProvisionOSInput("dhcp") },
		},
		{
			name:    "a reused Server's address inside the pod CIDR",
			servers: []*serverdomain.Server{readyServer("c1", "mi308-1"), deployedServer("w1", "mi308-2", "site-1", "10.244.3.7")},
			input:   func() DeployPlatformInput { return nonHAProvisionOSInput("automatic") },
			wantErr: true,
		},
		{
			name:    "a static provision's old address inside the pod CIDR but its assignment outside",
			servers: []*serverdomain.Server{readyServer("c1", "mi308-1", "10.244.3.7"), readyServer("w1", "mi308-2")},
			input: func() DeployPlatformInput {
				input := nonHAProvisionOSInput("static")
				input.MachinePreparation.Assignments = []platformdomain.MachineNetworkAssignment{
					staticAssignment("c1", "10.170.169.150"), workerStaticAssignment(),
				}
				return input
			},
		},
		{
			name:    "a static assignment inside the service CIDR",
			servers: []*serverdomain.Server{readyServer("c1", "mi308-1"), readyServer("w1", "mi308-2")},
			input: func() DeployPlatformInput {
				input := nonHAProvisionOSInput("static")
				input.MachinePreparation.Assignments = []platformdomain.MachineNetworkAssignment{
					staticAssignment("c1", "10.170.169.150"), staticAssignment("w1", "10.96.0.9"),
				}
				return input
			},
			wantErr: true,
		},
		{
			name: "an HA automatic provision's old address equal to the VIP",
			servers: func() []*serverdomain.Server {
				servers := haServers()
				servers[1] = readyServer("c2", "lab-control-2", "192.168.100.200")
				return servers
			}(),
			input: func() DeployPlatformInput {
				input := provisionOSInput()
				input.MachinePreparation.NetworkMode = "automatic"
				return input
			},
		},
		{
			name: "an HA reused Server's address equal to the VIP",
			servers: func() []*serverdomain.Server {
				servers := haServers()
				servers[1].Observed.Addresses = []string{"192.168.100.200"}
				servers[3].Provisioning.State = "ready"
				return servers
			}(),
			input: func() DeployPlatformInput {
				input := provisionOSInput()
				input.MachinePreparation.NetworkMode = "automatic"
				return input
			},
			wantErr: true,
		},
		{
			name: "an HA static assignment equal to the VIP",
			servers: func() []*serverdomain.Server {
				servers := haServers()
				servers[3] = readyServer("w1", "lab-compute-1")
				return servers
			}(),
			input: func() DeployPlatformInput {
				input := provisionOSInput()
				input.MachinePreparation.NetworkMode = "static"
				input.MachinePreparation.Assignments = []platformdomain.MachineNetworkAssignment{staticAssignment("w1", "192.168.100.200")}
				return input
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service, launcher, _ := newDeployHarness(tc.servers...)

			_, err := service.Deploy(context.Background(), tc.input())
			if tc.wantErr {
				if !errors.Is(err, platformdomain.ErrInvalidDeployment) {
					t.Fatalf("Deploy() error = %v, want ErrInvalidDeployment", err)
				}
				if launcher.launched != nil {
					t.Errorf("a refused deploy launched %v", launcher.launched)
				}
				return
			}
			if err != nil {
				t.Fatalf("Deploy() error = %v, want the deploy accepted", err)
			}
		})
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
