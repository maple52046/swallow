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
	// A single controller keeps state local and needs no state server.
	if vars[varSlurmControllerStateMode] != "local" {
		t.Errorf("controller state mode = %v, want local for one controller", vars[varSlurmControllerStateMode])
	}
	if _, ok := vars[varSlurmStateServer]; ok {
		t.Errorf("single-controller deploy must not name a state server, got %v", vars[varSlurmStateServer])
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
// controller can recover state. Swallow now provisions it: the deploy must succeed without an
// operator-supplied path, mark the controller state shared, and select an off-controller state
// server (a compute-only node) to export it.
func TestDeploySlurmHighAvailabilityProvisionsSharedState(t *testing.T) {
	service, launcher, _ := newDeployHarness(slurmServers()...)
	input := validSlurmInput()
	// s1,s2 are controller+compute; s3 is compute-only, so it is the preferred state server.
	input.SlurmSpec.NodeAssignments = []platformdomain.SlurmNodeAssignment{
		{ServerID: "s1", Controller: true, Compute: true},
		{ServerID: "s2", Controller: true, Compute: true},
		{ServerID: "s3", Compute: true},
	}

	if _, err := service.Deploy(context.Background(), input); err != nil {
		t.Fatalf("HA deploy should succeed without an operator state location, got %v", err)
	}
	if launcher.launched == nil {
		t.Fatal("HA deploy should launch")
	}
	vars := launcher.launched.TrustedVars
	if vars[varSlurmHighAvailability] != true {
		t.Errorf("high availability = %v, want true for multiple controllers", vars[varSlurmHighAvailability])
	}
	if vars[varSlurmControllerStateMode] != "shared" {
		t.Errorf("controller state mode = %v, want shared for HA", vars[varSlurmControllerStateMode])
	}
	if vars[varSlurmStateServer] != "s3" {
		t.Errorf("state server = %v, want the compute-only node s3", vars[varSlurmStateServer])
	}
	if vars[varSlurmStateExport] != "/srv/slurm-state" {
		t.Errorf("state export = %v, want /srv/slurm-state", vars[varSlurmStateExport])
	}
	// No operator path was supplied, so the playbook applies its own shared default.
	if _, ok := vars[varSlurmStateSaveLocation]; ok {
		t.Errorf("state save location should be unset when the operator supplied none, got %v", vars[varSlurmStateSaveLocation])
	}
}

// When every node is also a controller there is no off-controller host, so the state server
// falls back to the primary controller, and an operator-supplied path overrides the default.
func TestDeploySlurmHighAvailabilityFallsBackToPrimaryStateServer(t *testing.T) {
	service, launcher, _ := newDeployHarness(slurmServers()...)
	input := validSlurmInput()
	input.SlurmSpec.NodeAssignments = []platformdomain.SlurmNodeAssignment{
		{ServerID: "s1", Controller: true, Compute: true},
		{ServerID: "s2", Controller: true, Compute: true},
		{ServerID: "s3", Controller: true, Compute: true},
	}
	input.SlurmSpec.StateSaveLocation = "/mnt/slurm-state"

	if _, err := service.Deploy(context.Background(), input); err != nil {
		t.Fatalf("all-controller HA deploy should succeed, got %v", err)
	}
	if launcher.launched == nil {
		t.Fatal("HA deploy should launch")
	}
	vars := launcher.launched.TrustedVars
	if vars[varSlurmStateServer] != "s1" {
		t.Errorf("state server = %v, want the primary controller s1 as fallback", vars[varSlurmStateServer])
	}
	if vars[varSlurmStateSaveLocation] != "/mnt/slurm-state" {
		t.Errorf("state save location var = %v, want the operator override /mnt/slurm-state", vars[varSlurmStateSaveLocation])
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
