package application

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// fakeGPUOperatorRecorder applies GPUOperatorRecorder writes to the deploy fake's in-memory
// Platforms, so tests read the record back the way the API would.
type fakeGPUOperatorRecorder struct {
	platforms *deployFakePlatformRepo
}

func (r *fakeGPUOperatorRecorder) SetGPUOperator(
	_ context.Context, id string, operator *platformdomain.GPUOperator, owner platformdomain.GPUStackOwner,
) error {
	platform, ok := r.platforms.platforms[id]
	if !ok {
		return platformdomain.ErrPlatformNotFound
	}
	if operator == nil {
		platform.GPUOperator = nil
	} else {
		copied := *operator
		platform.GPUOperator = &copied
	}
	platform.GPUStackOwner = owner
	return nil
}

func (r *fakeGPUOperatorRecorder) UpdateGPUOperatorState(
	context.Context, string, string, platformdomain.GPUOperatorState, time.Time,
) (bool, error) {
	return false, nil
}

// withAMDGPU gives a Server one AMD compute GPU group, which lets a workload node enable the GPU
// Operator.
func withAMDGPU(server *serverdomain.Server) *serverdomain.Server {
	server.Observed.GPUs = []serverdomain.GPU{{
		Vendor: "Advanced Micro Devices, Inc. [AMD/ATI]", Count: 8, Kind: serverdomain.GPUKindCompute,
	}}
	return server
}

func gpuServers() []*serverdomain.Server {
	servers := haServers()
	withAMDGPU(servers[0]) // c1: a dedicated controller, so it does not count
	withAMDGPU(servers[3]) // w1: the only workload node
	return servers
}

func TestDeployWithGPUOperatorRecordsTheWorkflowBeforeLaunch(t *testing.T) {
	service, launcher, platforms := newDeployHarness(gpuServers()...)
	service.AttachGPUOperatorRecorder(&fakeGPUOperatorRecorder{platforms: platforms})
	input := validDeployInput()
	input.GPUStackOwner = ""
	input.GPUOperator = &platformdomain.GPUOperatorSettings{Enabled: true, NodeLabeller: true}

	result, err := service.Deploy(context.Background(), input)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	launch := launcher.launched
	if launch == nil || launch.OperationID == "" {
		t.Fatalf("Deploy launch = %+v, want a preassigned OperationID", launch)
	}
	platform := platforms.platforms[result.PlatformID]
	if platform.GPUStackOwner != platformdomain.GPUStackOwnerGPUOperator {
		t.Errorf("gpuStackOwner = %q, want gpu-operator derived from the enabled operator", platform.GPUStackOwner)
	}
	record := platform.GPUOperator
	if record == nil || record.LastWorkflowID != launch.OperationID || record.State != platformdomain.GPUOperatorInstalling {
		t.Fatalf("GPU Operator record = %+v, want installing and fenced on %s", record, launch.OperationID)
	}
	vars := launch.GPUOperatorVars
	if _, listed := vars["swallow_amd_gpu_operator_node_ids"]; listed {
		t.Errorf("GPU Operator vars = %v, want no node list: the operator's NFD decides the nodes", vars)
	}
	if vars[varGPUOperatorState] != "present" || vars[varGPUOperatorControllerID] != "c1" ||
		vars[varGPUOperatorAllocationMode] != "device-plugin" || vars[varGPUOperatorVersion] != "v1.5.1" {
		t.Errorf("GPU Operator vars = %v, want present device-plugin v1.5.1 run from c1", vars)
	}
	images, _ := vars[varGPUOperatorImages].(map[string]any)
	if images["devicePlugin"] != "docker.io/rocm/k8s-device-plugin:1.31.0.11" {
		t.Errorf("GPU Operator images = %v, want the release's pinned device plugin", images)
	}
}

func TestDeployWithoutGPUOperatorKeepsProvisioningOwner(t *testing.T) {
	service, launcher, platforms := newDeployHarness(haServers()...)
	input := validDeployInput()
	input.GPUStackOwner = ""

	result, err := service.Deploy(context.Background(), input)
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if launcher.launched.GPUOperatorVars != nil || launcher.launched.OperationID != "" {
		t.Errorf("launch = %+v, want no GPU Operator Job and a launcher-chosen id", launcher.launched)
	}
	platform := platforms.platforms[result.PlatformID]
	if platform.GPUStackOwner != platformdomain.GPUStackOwnerProvisioning || platform.GPUOperator != nil {
		t.Errorf("platform = %+v, want provisioning and no GPU Operator record", platform)
	}
}

func TestDeployGPUOperatorRejectsContradictionsBeforeCreatingAnything(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*DeployPlatformInput)
	}{
		{name: "provisioning owner", modify: func(input *DeployPlatformInput) { input.GPUStackOwner = "provisioning" }},
		{name: "DRA on an old cluster", modify: func(input *DeployPlatformInput) {
			input.Spec.K0sVersion = "v1.33.1+k0s.0"
			input.GPUOperator.AllocationMode = platformdomain.GPUAllocationDRA
		}},
		{name: "a Slurm platform", modify: func(input *DeployPlatformInput) { input.Type = platformdomain.PlatformTypeSlurm }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service, launcher, platforms := newDeployHarness(gpuServers()...)
			service.AttachGPUOperatorRecorder(&fakeGPUOperatorRecorder{platforms: platforms})
			input := validDeployInput()
			input.GPUStackOwner = ""
			input.GPUOperator = &platformdomain.GPUOperatorSettings{Enabled: true}
			tc.modify(&input)

			_, err := service.Deploy(context.Background(), input)
			if !errors.Is(err, platformdomain.ErrInvalidGPUOperator) {
				t.Fatalf("Deploy error = %v, want ErrInvalidGPUOperator", err)
			}
			if launcher.launched != nil || len(platforms.platforms) != 0 {
				t.Errorf("Deploy created %d platforms and launched %v, want nothing", len(platforms.platforms), launcher.launched)
			}
		})
	}
}

func TestDeployGPUOperatorNeedsAnAMDWorkloadNode(t *testing.T) {
	servers := haServers()
	withAMDGPU(servers[0]) // only c1, a dedicated controller, has an AMD GPU
	service, _, platforms := newDeployHarness(servers...)
	service.AttachGPUOperatorRecorder(&fakeGPUOperatorRecorder{platforms: platforms})
	input := validDeployInput()
	input.GPUStackOwner = ""
	input.GPUOperator = &platformdomain.GPUOperatorSettings{Enabled: true}

	if _, err := service.Deploy(context.Background(), input); !errors.Is(err, platformdomain.ErrInvalidGPUOperator) {
		t.Errorf("Deploy without AMD GPUs error = %v, want ErrInvalidGPUOperator", err)
	}
}

// recordingGPUOperatorLauncher captures the change launch and can refuse it.
type recordingGPUOperatorLauncher struct {
	launched *platformdomain.GPUOperatorLaunch
	err      error
}

func (l *recordingGPUOperatorLauncher) LaunchGPUOperator(_ context.Context, launch platformdomain.GPUOperatorLaunch) (string, error) {
	l.launched = &launch
	if l.err != nil {
		return "", l.err
	}
	return launch.WorkflowID, nil
}

// gpuOperatorHarness is an active Swallow-deployed HA Platform "p1" whose c1 controller also
// runs workloads, with AMD GPUs on c1 and w1.
func gpuOperatorHarness(t *testing.T, state platformdomain.PlatformLifecycleState) (*GPUOperatorService, *recordingGPUOperatorLauncher, *deployFakePlatformRepo) {
	t.Helper()
	platforms := &deployFakePlatformRepo{platforms: map[string]*platformdomain.Platform{
		"p1": {ID: "p1", SiteID: "site-1", Name: "lab-k0s", Type: platformdomain.PlatformTypeKubernetes,
			GPUStackOwner: platformdomain.GPUStackOwnerProvisioning},
	}}
	servers := &deployFakeServerRepo{servers: map[string]*serverdomain.Server{}}
	for _, server := range gpuServers() {
		servers.servers[server.ID] = server
	}
	roles := []platformdomain.RoleAssignment{
		{ServerID: "c1", Role: platformdomain.NodeRoleControlPlane, RunWorkloads: true},
		{ServerID: "c2", Role: platformdomain.NodeRoleControlPlane},
		{ServerID: "c3", Role: platformdomain.NodeRoleControlPlane},
		{ServerID: "w1", Role: platformdomain.NodeRoleWorker},
	}
	lifecycle := &deployFakeLifecycleReader{snapshots: map[string]platformdomain.LifecycleSnapshot{
		"p1": {
			Origin: platformdomain.PlatformOriginDeployed, State: state,
			Deployment: &platformdomain.LifecycleOperation{
				ID: "deploy-1", TargetServerIDs: []string{"c1", "c2", "c3", "w1"},
				Intent: &platformdomain.LifecycleDeployment{RoleAssignments: roles, K0sVersion: "v1.36.3+k0s.2"},
			},
		},
	}}
	launcher := &recordingGPUOperatorLauncher{}
	service := NewGPUOperatorService(platforms, &fakeGPUOperatorRecorder{platforms: platforms}, servers, lifecycle, launcher, nil)
	return service, launcher, platforms
}

func TestConfigureGPUOperatorRecordsAndLaunchesTheChange(t *testing.T) {
	service, launcher, platforms := gpuOperatorHarness(t, platformdomain.PlatformLifecycleActive)

	result, err := service.Configure(context.Background(), ConfigureGPUOperatorInput{
		PlatformID: "p1",
		Settings:   platformdomain.GPUOperatorSettings{Enabled: true, AllocationMode: platformdomain.GPUAllocationDRA},
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	launch := launcher.launched
	if launch == nil || result.WorkflowID != launch.WorkflowID {
		t.Fatalf("Configure result = %+v, launch = %+v; want the launched Workflow id", result, launch)
	}
	if !reflect.DeepEqual(launch.TargetServerIDs, []string{"c1", "c2", "c3", "w1"}) {
		t.Errorf("targets = %v, want the deployment targets", launch.TargetServerIDs)
	}
	images, _ := launch.TrustedVars[varGPUOperatorImages].(map[string]any)
	if launch.TrustedVars[varGPUOperatorAllocationMode] != "dra" || images["draDriver"] != "docker.io/rocm/k8s-gpu-dra-driver:v1.0.1" {
		t.Errorf("vars = %v, want dra with the pinned DRA driver", launch.TrustedVars)
	}
	if _, listed := launch.TrustedVars["swallow_amd_gpu_operator_node_ids"]; listed {
		t.Errorf("vars = %v, want no node list: the operator's NFD decides the nodes", launch.TrustedVars)
	}
	platform := platforms.platforms["p1"]
	if platform.GPUOperator.LastWorkflowID != launch.WorkflowID || platform.GPUOperator.State != platformdomain.GPUOperatorInstalling ||
		platform.GPUStackOwner != platformdomain.GPUStackOwnerGPUOperator {
		t.Errorf("platform = %+v (%+v), want an installing record fenced on the launch", platform, platform.GPUOperator)
	}
}

func TestConfigureGPUOperatorUninstallKeepsTheSettings(t *testing.T) {
	service, launcher, platforms := gpuOperatorHarness(t, platformdomain.PlatformLifecycleActive)
	previous := platformdomain.GPUOperatorSettings{
		Enabled: true, Version: "v1.5.1", AllocationMode: platformdomain.GPUAllocationDevicePlugin,
		ResourceNaming: platformdomain.GPUResourceNamingSingle, NodeLabeller: true,
	}
	platforms.platforms["p1"].GPUOperator = &platformdomain.GPUOperator{Settings: previous, State: platformdomain.GPUOperatorReady}
	platforms.platforms["p1"].GPUStackOwner = platformdomain.GPUStackOwnerGPUOperator

	if _, err := service.Configure(context.Background(), ConfigureGPUOperatorInput{PlatformID: "p1"}); err != nil {
		t.Fatalf("Configure(uninstall): %v", err)
	}
	record := platforms.platforms["p1"].GPUOperator
	want := previous
	want.Enabled = false
	if record.State != platformdomain.GPUOperatorUninstalling || !reflect.DeepEqual(record.Settings, want) {
		t.Errorf("record = %+v, want uninstalling with the previous settings disabled", record)
	}
	if launcher.launched.TrustedVars[varGPUOperatorState] != "absent" || !launcher.launched.Uninstall {
		t.Errorf("launch = %+v, want an uninstall", launcher.launched)
	}
}

func TestConfigureGPUOperatorRefusesWhatTheStateDoesNotAllow(t *testing.T) {
	t.Run("platform not active", func(t *testing.T) {
		service, launcher, _ := gpuOperatorHarness(t, platformdomain.PlatformLifecycleDeploying)
		_, err := service.Configure(context.Background(), ConfigureGPUOperatorInput{
			PlatformID: "p1", Settings: platformdomain.GPUOperatorSettings{Enabled: true},
		})
		if !errors.Is(err, platformdomain.ErrGPUOperatorConflict) || launcher.launched != nil {
			t.Errorf("Configure error = %v, launched = %v; want ErrGPUOperatorConflict and no launch", err, launcher.launched)
		}
	})
	t.Run("uninstall without a managed operator", func(t *testing.T) {
		service, _, _ := gpuOperatorHarness(t, platformdomain.PlatformLifecycleActive)
		_, err := service.Configure(context.Background(), ConfigureGPUOperatorInput{PlatformID: "p1"})
		if !errors.Is(err, platformdomain.ErrGPUOperatorConflict) {
			t.Errorf("Configure(uninstall) error = %v, want ErrGPUOperatorConflict", err)
		}
	})
}

func TestConfigureGPUOperatorRestoresTheRecordWhenTheLaunchIsRefused(t *testing.T) {
	service, launcher, platforms := gpuOperatorHarness(t, platformdomain.PlatformLifecycleActive)
	launcher.err = operationdomain.ErrTargetsBusy

	_, err := service.Configure(context.Background(), ConfigureGPUOperatorInput{
		PlatformID: "p1", Settings: platformdomain.GPUOperatorSettings{Enabled: true},
	})
	if !errors.Is(err, operationdomain.ErrTargetsBusy) {
		t.Fatalf("Configure error = %v, want ErrTargetsBusy", err)
	}
	platform := platforms.platforms["p1"]
	if platform.GPUOperator != nil || platform.GPUStackOwner != platformdomain.GPUStackOwnerProvisioning {
		t.Errorf("platform = %+v, want the record removed and provisioning restored", platform)
	}
}

func TestPlatformPolicyCannotLeaveAManagedGPUOperator(t *testing.T) {
	platforms := &deployFakePlatformRepo{platforms: map[string]*platformdomain.Platform{
		"p1": {ID: "p1", Type: platformdomain.PlatformTypeKubernetes, GPUStackOwner: platformdomain.GPUStackOwnerGPUOperator,
			GPUOperator: &platformdomain.GPUOperator{State: platformdomain.GPUOperatorReady}},
	}}
	service := NewPlatformService(platforms, nil, nil, nil, nil)
	owner := string(platformdomain.GPUStackOwnerProvisioning)

	_, err := service.Update(context.Background(), "p1", UpdatePlatformInput{GPUStackOwner: &owner})
	if !errors.Is(err, platformdomain.ErrGPUOperatorConflict) {
		t.Errorf("Update(gpuStackOwner=provisioning) error = %v, want ErrGPUOperatorConflict", err)
	}
}
