package application

import (
	"context"
	"errors"
	"slices"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// Slurm deployment validation is the load-bearing part of the Slurm path: it must accept the
// per-daemon role model (a Server may run slurmctld, slurmd, or both), reject topologies that
// cannot form a cluster, and allow an ephemeral OS. These tests exercise
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

// A login-only node (no slurmctld/slurmd) is a valid submission/client host, and in HA it is
// the shared-storage server: it must be selected as the controller-state server.
func TestDeploySlurmAcceptsLoginOnlyNodeAsStateServer(t *testing.T) {
	servers := append(slurmServers(), deployedServer("s4", "lab-slurm-login", "site-1", "192.168.100.9"))
	service, launcher, _ := newDeployHarness(servers...)
	input := validSlurmInput()
	input.SlurmSpec.NodeAssignments = []platformdomain.SlurmNodeAssignment{
		{ServerID: "s1", Controller: true, Compute: true},
		{ServerID: "s2", Controller: true, Compute: true},
		{ServerID: "s3", Compute: true},
		{ServerID: "s4", Login: true},
	}

	if _, err := service.Deploy(context.Background(), input); err != nil {
		t.Fatalf("a login-only node should be accepted, got %v", err)
	}
	if launcher.launched == nil {
		t.Fatal("deploy should launch")
	}
	vars := launcher.launched.TrustedVars
	logins, _ := vars[varSlurmLoginIDs].([]string)
	if len(logins) != 1 || logins[0] != "s4" {
		t.Errorf("login ids = %v, want [s4]", vars[varSlurmLoginIDs])
	}
	if vars[varSlurmStateServer] != "s4" {
		t.Errorf("HA state server = %v, want the login node s4", vars[varSlurmStateServer])
	}
	if vars[varSlurmLoginConfigMode] != "configless" || vars[varSlurmLoginUseSackd] != true {
		t.Errorf("login config = %v/%v, want configless/true", vars[varSlurmLoginConfigMode], vars[varSlurmLoginUseSackd])
	}
	// s4 is a deployment target even though it runs no daemon.
	if !slices.Contains(launcher.launched.TargetServerIDs, "s4") {
		t.Errorf("target ids = %v, want the login node s4 included", launcher.launched.TargetServerIDs)
	}
}

// Self-hosted workload storage exports from the login node and requires one.
func TestDeploySlurmWorkloadSelfHosted(t *testing.T) {
	servers := append(slurmServers(), deployedServer("s4", "lab-slurm-login", "site-1", "192.168.100.9"))
	service, launcher, _ := newDeployHarness(servers...)
	input := validSlurmInput()
	input.SlurmSpec.NodeAssignments = []platformdomain.SlurmNodeAssignment{
		{ServerID: "s1", Controller: true, Compute: true},
		{ServerID: "s2", Compute: true},
		{ServerID: "s4", Login: true},
	}
	input.SlurmSpec.WorkloadStorage = platformdomain.SlurmWorkloadStorageSpec{
		Enabled: true, Mode: platformdomain.SlurmWorkloadStorageSelfHosted,
	}

	if _, err := service.Deploy(context.Background(), input); err != nil {
		t.Fatalf("self-hosted workload storage with a login node should be accepted, got %v", err)
	}
	vars := launcher.launched.TrustedVars
	if vars[varSlurmWorkloadEnabled] != true || vars[varSlurmWorkloadMode] != "self-hosted" {
		t.Errorf("workload enabled/mode = %v/%v, want true/self-hosted", vars[varSlurmWorkloadEnabled], vars[varSlurmWorkloadMode])
	}
	if vars[varSlurmWorkloadServer] != "s4" {
		t.Errorf("workload server = %v, want the login node s4", vars[varSlurmWorkloadServer])
	}
	if vars[varSlurmWorkloadExport] != "/srv/slurm-workspace" {
		t.Errorf("workload export = %v, want /srv/slurm-workspace", vars[varSlurmWorkloadExport])
	}
	if vars[varSlurmWorkloadMountPath] != defaultWorkloadMountPath {
		t.Errorf("workload mount path = %v, want the default %v", vars[varSlurmWorkloadMountPath], defaultWorkloadMountPath)
	}
	if vars[varSlurmWorkloadFstype] != "nfs" || vars[varSlurmWorkloadMountOpts] != "rw,_netdev,hard,timeo=600,retrans=2,vers=3" {
		t.Errorf("self-hosted mount protocol = %v/%v, want nfs with vers=3", vars[varSlurmWorkloadFstype], vars[varSlurmWorkloadMountOpts])
	}
}

func TestDeploySlurmWorkloadSelfHostedRequiresLogin(t *testing.T) {
	service, launcher, _ := newDeployHarness(slurmServers()...)
	input := validSlurmInput() // single controller, no login node
	input.SlurmSpec.WorkloadStorage = platformdomain.SlurmWorkloadStorageSpec{
		Enabled: true, Mode: platformdomain.SlurmWorkloadStorageSelfHosted,
	}
	if _, err := service.Deploy(context.Background(), input); !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Fatalf("self-hosted workload storage without a login node should be rejected, got %v", err)
	}
	if launcher.launched != nil {
		t.Fatal("no deploy should launch")
	}
}

// External workload storage mounts an operator NFS URL and runs no swallow server.
func TestDeploySlurmWorkloadExternal(t *testing.T) {
	service, launcher, _ := newDeployHarness(slurmServers()...)
	input := validSlurmInput()
	input.SlurmSpec.WorkloadStorage = platformdomain.SlurmWorkloadStorageSpec{
		Enabled: true, Mode: platformdomain.SlurmWorkloadStorageExternal,
		NFSURL: "10.0.0.9:/export/data", MountPath: "/data",
	}

	if _, err := service.Deploy(context.Background(), input); err != nil {
		t.Fatalf("external workload storage should be accepted, got %v", err)
	}
	vars := launcher.launched.TrustedVars
	if vars[varSlurmWorkloadMode] != "external" {
		t.Errorf("workload mode = %v, want external", vars[varSlurmWorkloadMode])
	}
	if vars[varSlurmWorkloadSource] != "10.0.0.9:/export/data" {
		t.Errorf("workload source = %v, want the operator url", vars[varSlurmWorkloadSource])
	}
	if vars[varSlurmWorkloadMountPath] != "/data" {
		t.Errorf("workload mount path = %v, want /data", vars[varSlurmWorkloadMountPath])
	}
	if vars[varSlurmWorkloadFstype] != "nfs4" {
		t.Errorf("external workload fstype = %v, want nfs4", vars[varSlurmWorkloadFstype])
	}
	if _, ok := vars[varSlurmWorkloadServer]; ok {
		t.Errorf("external workload storage must not name a server, got %v", vars[varSlurmWorkloadServer])
	}
}

func TestDeploySlurmWorkloadRejectsInvalidStorage(t *testing.T) {
	cases := []struct {
		name    string
		storage platformdomain.SlurmWorkloadStorageSpec
	}{
		{"external without url", platformdomain.SlurmWorkloadStorageSpec{Enabled: true, Mode: platformdomain.SlurmWorkloadStorageExternal}},
		{"unsupported type", platformdomain.SlurmWorkloadStorageSpec{Enabled: true, Mode: platformdomain.SlurmWorkloadStorageExternal, Type: "cephfs", NFSURL: "h:/p"}},
		{"home mount path", platformdomain.SlurmWorkloadStorageSpec{Enabled: true, Mode: platformdomain.SlurmWorkloadStorageExternal, NFSURL: "h:/p", MountPath: "/home"}},
		{"unknown mode", platformdomain.SlurmWorkloadStorageSpec{Enabled: true, Mode: "managed", NFSURL: "h:/p"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, launcher, _ := newDeployHarness(slurmServers()...)
			input := validSlurmInput()
			input.SlurmSpec.WorkloadStorage = tc.storage
			if _, err := service.Deploy(context.Background(), input); !errors.Is(err, platformdomain.ErrInvalidDeployment) {
				t.Fatalf("Deploy() error = %v, want ErrInvalidDeployment", err)
			}
			if launcher.launched != nil {
				t.Error("an invalid workload storage deploy must launch nothing")
			}
		})
	}
}

// A Slurm deployment accepts an ephemeral (run-from-RAM) provision_os OS and preserves that
// provider intent in the launch request.
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
