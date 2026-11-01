package app

import (
	"encoding/json"
	"slices"
	"testing"

	platformdelivery "github.com/maple52046/swallow/internal/core/platform/delivery"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// The deploy request rebuilt from a Slurm deployment's recorded variables (as a store round trip
// leaves them) reads back as the deployment that recorded them, with the default self-hosted mount
// options dropped as not requested.
func TestAdoptionRebuildsTheSlurmRequest(t *testing.T) {
	vars := map[string]any{
		"swallow_slurm_cluster_name": "lab", "swallow_slurm_api_version": "v0.0.42",
		"swallow_slurm_state_save_location": "/var/spool/slurmctld",
		"swallow_slurm_controller_ids":      []any{"a"}, "swallow_slurm_compute_ids": []any{"a", "b"}, "swallow_slurm_login_ids": []any{"c"},
		"swallow_slurm_workload_enabled": true, "swallow_slurm_workload_mode": "self-hosted",
		"swallow_slurm_workload_mount_path": "/shared", "swallow_slurm_workload_mount_options": defaultSelfHostedMountOptions,
	}
	platform := &platformdomain.Platform{
		ID: "p", Name: "lab", Type: platformdomain.PlatformTypeSlurm, GPUStackOwner: platformdomain.GPUStackOwnerProvisioning, PMIx: true,
		Omnistat: &platformdomain.Omnistat{Enabled: true, InstallMethod: platformdomain.ObservabilityInstallMethod("container")},
	}
	body := adoptionRequest(platform, nil, vars)
	body["siteId"], body["name"], body["type"] = "site-1", "lab", "slurm"
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	input, err := platformdelivery.DeployInputFromRequest(raw)
	if err != nil {
		t.Fatalf("DeployInputFromRequest() error = %v", err)
	}
	got := input.SlurmSpec
	if got.ClusterName != "lab" || got.APIVersion != "v0.0.42" || got.StateSaveLocation != "/var/spool/slurmctld" || !got.PMIx {
		t.Errorf("slurm fields = %+v", got)
	}
	if !slices.Equal(got.ControllerServerIDs(), []string{"a"}) || !slices.Equal(got.ComputeServerIDs(), []string{"a", "b"}) || !slices.Equal(got.LoginServerIDs(), []string{"c"}) {
		t.Errorf("node assignments = %+v", got.NodeAssignments)
	}
	storage := got.WorkloadStorage
	if !storage.Enabled || storage.Mode != platformdomain.SlurmWorkloadStorageSelfHosted || storage.MountPath != "/shared" || storage.NFSMountOptions != "" {
		t.Errorf("workload storage = %+v", storage)
	}
	if input.Omnistat == nil || !input.Omnistat.Enabled || input.GPUStackOwner != "provisioning" {
		t.Errorf("features: omnistat %+v owner %q", input.Omnistat, input.GPUStackOwner)
	}
}

// A Kubernetes deployment's request comes back with its role assignments and network fields; the
// VIP prefix is kept only with a VIP.
func TestAdoptionRebuildsTheKubernetesRequest(t *testing.T) {
	vars := map[string]any{"swallow_k0s_version": "v1.36.3+k0s.2", "swallow_k0s_pod_cidr": "10.244.0.0/16", "swallow_k0s_api_vip_prefix": int32(24)}
	roles := []platformdomain.RoleAssignment{{ServerID: "c1", Role: platformdomain.NodeRoleControlPlane, RunWorkloads: true}, {ServerID: "w1", Role: platformdomain.NodeRoleWorker}}
	body := adoptionRequest(&platformdomain.Platform{Type: platformdomain.PlatformTypeKubernetes}, roles, vars)
	body["siteId"], body["name"], body["type"] = "site-1", "lab", "kubernetes"
	raw, _ := json.Marshal(body)
	input, err := platformdelivery.DeployInputFromRequest(raw)
	if err != nil {
		t.Fatalf("DeployInputFromRequest() error = %v", err)
	}
	if input.Spec.K0sVersion != "v1.36.3+k0s.2" || input.Spec.PodCIDR != "10.244.0.0/16" || input.Spec.APIVIPPrefix != 0 {
		t.Errorf("spec = %+v", input.Spec)
	}
	if len(input.Spec.RoleAssignments) != 2 || !input.Spec.RoleAssignments[0].RunWorkloads {
		t.Errorf("role assignments = %+v", input.Spec.RoleAssignments)
	}
}
