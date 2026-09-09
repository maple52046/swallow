package application

import (
	"context"
	"errors"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// Slurm deployment validation is the load-bearing part of the Slurm path: it must accept the
// per-daemon role model (a Server may run slurmctld, slurmd, or both), reject topologies that
// cannot form a cluster, and — unlike Kubernetes — allow an ephemeral OS. These tests exercise
// it against the same in-memory fakes as the Kubernetes deploy tests.

func slurmServers() []*serverdomain.Server {
	return []*serverdomain.Server{
		deployedServer("s1", "lab-slurm-1", "site-1", "192.168.100.2"),
		deployedServer("s2", "lab-slurm-2", "site-1", "192.168.100.3"),
		deployedServer("s3", "lab-slurm-3", "site-1", "192.168.100.4"),
	}
}

func validSlurmInput() DeployPlatformInput {
	return DeployPlatformInput{
		SiteID: "site-1",
		Name:   "Lab Slurm",
		Type:   platformdomain.PlatformTypeSlurm,
		SlurmSpec: platformdomain.SlurmDeploymentSpec{
			NodeAssignments: []platformdomain.SlurmNodeAssignment{
				{ServerID: "s1", Controller: true, Compute: true},
				{ServerID: "s2", Compute: true},
				{ServerID: "s3", Compute: true},
			},
		},
		RequestedBy: "admin",
	}
}

func TestDeploySlurmSucceedsAndBuildsTrustedVars(t *testing.T) {
	service, launcher, platforms := newDeployHarness(slurmServers()...)

	result, err := service.Deploy(context.Background(), validSlurmInput())
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	created, ok := platforms.platforms[result.PlatformID]
	if !ok {
		t.Fatal("platform was not created")
	}
	if created.Type != platformdomain.PlatformTypeSlurm {
		t.Errorf("platform type = %v, want slurm", created.Type)
	}
	if launcher.launched == nil {
		t.Fatal("launcher was not called")
	}

	vars := launcher.launched.TrustedVars
	controllers, _ := vars[varSlurmControllerIDs].([]string)
	if len(controllers) != 1 || controllers[0] != "s1" {
		t.Errorf("controller ids = %v, want [s1]", vars[varSlurmControllerIDs])
	}
	computes, _ := vars[varSlurmComputeIDs].([]string)
	if len(computes) != 3 {
		t.Errorf("compute ids = %v, want three compute nodes", vars[varSlurmComputeIDs])
	}
	if vars[varSlurmPrimaryController] != "s1" {
		t.Errorf("primary controller = %v, want s1", vars[varSlurmPrimaryController])
	}
	if vars[varSlurmHighAvailability] != false {
		t.Errorf("high availability = %v, want false for one controller", vars[varSlurmHighAvailability])
	}
	// ClusterName defaults to a sanitized platform name: "Lab Slurm" -> "lab-slurm".
	if vars[varSlurmClusterName] != "lab-slurm" {
		t.Errorf("cluster name = %v, want lab-slurm", vars[varSlurmClusterName])
	}
	// s1 runs both daemons but must appear once in the deployment target set.
	if len(launcher.launched.TargetServerIDs) != 3 {
		t.Errorf("target server ids = %v, want three deduplicated targets", launcher.launched.TargetServerIDs)
	}
}

func TestDeploySlurmRejectsNoController(t *testing.T) {
	service, launcher, platforms := newDeployHarness(slurmServers()...)
	input := validSlurmInput()
	input.SlurmSpec.NodeAssignments = []platformdomain.SlurmNodeAssignment{
		{ServerID: "s1", Compute: true},
		{ServerID: "s2", Compute: true},
	}

	_, err := service.Deploy(context.Background(), input)
	if !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Fatalf("Deploy() error = %v, want ErrInvalidDeployment", err)
	}
	if len(platforms.platforms) != 0 || launcher.launched != nil {
		t.Error("a controller-less Slurm deploy must create nothing")
	}
}

func TestDeploySlurmRejectsNoCompute(t *testing.T) {
	service, _, _ := newDeployHarness(slurmServers()...)
	input := validSlurmInput()
	input.SlurmSpec.NodeAssignments = []platformdomain.SlurmNodeAssignment{
		{ServerID: "s1", Controller: true},
	}

	_, err := service.Deploy(context.Background(), input)
	if !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Fatalf("Deploy() error = %v, want ErrInvalidDeployment", err)
	}
}

func TestDeploySlurmRejectsNodeWithoutDaemon(t *testing.T) {
	service, _, _ := newDeployHarness(slurmServers()...)
	input := validSlurmInput()
	input.SlurmSpec.NodeAssignments = []platformdomain.SlurmNodeAssignment{
		{ServerID: "s1", Controller: true, Compute: true},
		{ServerID: "s2"}, // neither daemon
	}

	_, err := service.Deploy(context.Background(), input)
	if !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Fatalf("Deploy() error = %v, want ErrInvalidDeployment", err)
	}
}

// A multi-controller (HA) Slurm deployment needs a shared StateSaveLocation so a backup
// controller can recover state; Swallow does not provision that shared storage, so the deploy
// must be told where it is.
func TestDeploySlurmHighAvailabilityRequiresStateSaveLocation(t *testing.T) {
	haAssignments := []platformdomain.SlurmNodeAssignment{
		{ServerID: "s1", Controller: true, Compute: true},
		{ServerID: "s2", Controller: true, Compute: true},
		{ServerID: "s3", Compute: true},
	}

	service, launcher, _ := newDeployHarness(slurmServers()...)
	missing := validSlurmInput()
	missing.SlurmSpec.NodeAssignments = haAssignments
	if _, err := service.Deploy(context.Background(), missing); !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Fatalf("HA without stateSaveLocation should be rejected, got %v", err)
	}
	if launcher.launched != nil {
		t.Fatal("no HA deploy should launch without a shared state location")
	}

	service, launcher, _ = newDeployHarness(slurmServers()...)
	provided := validSlurmInput()
	provided.SlurmSpec.NodeAssignments = haAssignments
	provided.SlurmSpec.StateSaveLocation = "/mnt/slurm-state"
	if _, err := service.Deploy(context.Background(), provided); err != nil {
		t.Fatalf("HA with a shared state location should be accepted, got %v", err)
	}
	if launcher.launched == nil {
		t.Fatal("HA deploy with a shared state location should launch")
	}
	if launcher.launched.TrustedVars[varSlurmStateSaveLocation] != "/mnt/slurm-state" {
		t.Errorf("state save location var = %v, want /mnt/slurm-state", launcher.launched.TrustedVars[varSlurmStateSaveLocation])
	}
}

// Unlike Kubernetes, a Slurm deployment must accept an ephemeral (run-from-RAM) provision_os
// OS: the k0s ephemeral guard is deliberately not reused for Slurm.
func TestDeploySlurmAllowsEphemeralProvisionOS(t *testing.T) {
	servers := slurmServers()
	servers[0].Provisioning.State = "ready" // needs an OS provisioned first
	service, launcher, _ := newDeployHarness(servers...)
	service.AttachMachinePreparationValidator(&stubMachinePrep{})

	input := validSlurmInput()
	ephemeral := true
	input.MachinePreparation = platformdomain.MachinePreparation{
		Mode:        platformdomain.MachinePreparationProvisionOS,
		ImageID:     "slurm-image",
		Ephemeral:   &ephemeral,
		NetworkMode: "dhcp",
	}

	if _, err := service.Deploy(context.Background(), input); err != nil {
		t.Fatalf("ephemeral provision_os Slurm deploy should be allowed, got %v", err)
	}
	if launcher.launched == nil {
		t.Fatal("an ephemeral Slurm deploy should launch")
	}
}
