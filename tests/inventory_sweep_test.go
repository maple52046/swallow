package tests

import (
	"context"
	"testing"

	provisioningapp "github.com/AFDEAPAC/swallow/internal/provisioning/application"
	provisioningdomain "github.com/AFDEAPAC/swallow/internal/provisioning/domain"
	serverdomain "github.com/AFDEAPAC/swallow/internal/server/domain"
)

// The GPU inventory is the whole reason the fleet's GPU count is not permanently zero, so
// the sweep must actually write it onto the servers it finds.
func TestInventorySweep_PopulatesGPUsOnServers(t *testing.T) {
	f := setupReconcile(t)
	f.provider.withMachine(testMachine("abc123", "gpu-node-01"))
	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var serverID, machineID string
	for id, s := range f.servers.servers {
		serverID, machineID = id, s.Source.ProviderMachineID
	}
	f.provider.gpus[machineID] = []provisioningdomain.GPU{{Vendor: "NVIDIA Corporation", Model: "A100", Count: 8}}

	sweep := provisioningapp.NewInventorySweepUseCase(f.integrations, f.servers, f.factory)
	reports, err := sweep.ExecuteAll(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(reports) != 1 || reports[0].Updated != 1 {
		t.Fatalf("expected 1 server updated, got %+v", reports)
	}

	gpus := f.servers.servers[serverID].Observed.GPUs
	if len(gpus) != 1 || gpus[0].Model != "A100" || gpus[0].Count != 8 {
		t.Fatalf("GPU inventory not written: %+v", gpus)
	}
}

// A reconcile pass rewrites the projection every interval; it must not wipe the GPU
// inventory the sweep wrote on its own slower cadence, the same way it leaves membership
// alone.
func TestReconcile_PreservesSweptGPUs(t *testing.T) {
	f := setupReconcile(t)
	f.provider.withMachine(testMachine("abc123", "gpu-node-01"))
	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	var serverID, machineID string
	for id, s := range f.servers.servers {
		serverID, machineID = id, s.Source.ProviderMachineID
	}
	f.provider.gpus[machineID] = []provisioningdomain.GPU{{Vendor: "NVIDIA Corporation", Model: "A100", Count: 8}}
	sweep := provisioningapp.NewInventorySweepUseCase(f.integrations, f.servers, f.factory)
	if _, err := sweep.ExecuteAll(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	// A second reconcile, standing in for the next interval tick.
	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	gpus := f.servers.servers[serverID].Observed.GPUs
	if len(gpus) != 1 || gpus[0].Count != 8 {
		t.Fatalf("reconcile wiped the swept GPU inventory: %+v", gpus)
	}
}

// A provisioner that cannot enumerate attached hardware is not a failure: the sweep
// simply contributes no GPU data for it rather than erroring the whole pass.
func TestInventorySweep_SkipsProvisionerWithoutInventoryCapability(t *testing.T) {
	f := setupReconcile(t)
	f.provider.withMachine(testMachine("abc123", "gpu-node-01"))
	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// Swap in a provisioner that implements only the base interface.
	f.factory.providers[testIntegrationID] = &minimalProvider{machines: map[string]*provisioningdomain.Machine{}}

	sweep := provisioningapp.NewInventorySweepUseCase(f.integrations, f.servers, f.factory)
	reports, err := sweep.ExecuteAll(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(reports) != 1 || reports[0].Error != nil || reports[0].Updated != 0 {
		t.Fatalf("expected a clean no-op, got %+v", reports)
	}
}

// The mirrored hardware fields have to reach the server projection, since caching them is
// what lets the fleet be grouped by hardware without opening each machine.
func TestReconcile_ProjectsMirroredHardwareFields(t *testing.T) {
	f := setupReconcile(t)
	machine := testMachine("abc123", "gpu-node-01")
	machine.SystemVendor = "Dell Inc."
	machine.SystemProduct = "PowerEdge R760xa"
	machine.CPUModel = "Intel(R) Xeon(R) Platinum 8480+"
	machine.Pod = "kvm-host-3"
	machine.Locked = true
	machine.CommissioningStatus = "Passed"
	machine.TestingStatus = "Passed"
	machine.Tags = []string{"gpu", "a100"}
	f.provider.withMachine(machine)

	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var s *serverdomain.Server
	for _, server := range f.servers.servers {
		s = server
	}
	if s.Observed.SystemVendor != "Dell Inc." || s.Observed.SystemProduct != "PowerEdge R760xa" {
		t.Errorf("system vendor/product not projected: %+v", s.Observed)
	}
	if s.Observed.CPUModel == "" || s.Observed.ProviderPod != "kvm-host-3" {
		t.Errorf("cpu model / pod not projected: %+v", s.Observed)
	}
	if len(s.Observed.Tags) != 2 {
		t.Errorf("tags not projected: %+v", s.Observed.Tags)
	}
	if !s.Provisioning.Locked || s.Provisioning.CommissioningStatus != "Passed" {
		t.Errorf("provisioning validation fields not projected: %+v", s.Provisioning)
	}
}
