package app

import (
	"testing"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
)

func stepByID(steps []operationdomain.Task, id string) (operationdomain.Task, bool) {
	for _, step := range steps {
		if step.ID == id {
			return step, true
		}
	}
	return operationdomain.Task{}, false
}

// TestUninstallStepsReleaseSkipsUninstall verifies the release path releases every server
// directly (no uninstall-platform ansible step) and finalizes with an internal
// complete-uninstall step that depends on all releases.
func TestUninstallStepsReleaseSkipsUninstall(t *testing.T) {
	launch := platformdomain.UninstallLaunch{
		Platform:        &platformdomain.Platform{ID: "platform-a", Name: "lab", Type: platformdomain.PlatformTypeSlurm},
		TargetServerIDs: []string{"srv-1", "srv-2"},
		ReleaseServers:  true,
		ReleaseOptions:  platformdomain.ServerReleaseOptions{Erase: true},
	}
	prepared := &operationapp.PreparedAnsibleStep{SiteID: "site-a"}

	steps := uninstallSteps(launch, prepared, "op-1", "Uninstall Slurm Platform")

	if _, ok := stepByID(steps, uninstallPlatformStepID); ok {
		t.Fatalf("release path must not include the %q ansible step", uninstallPlatformStepID)
	}
	for _, serverID := range launch.TargetServerIDs {
		release, ok := stepByID(steps, "release-"+serverID)
		if !ok {
			t.Fatalf("missing release step for %s", serverID)
		}
		if release.Kind != "release-os" || release.Executor != operationdomain.RunnerKindProvisioner {
			t.Errorf("release step = %+v, want release-os/provisioner", release)
		}
		if len(release.DependsOn) != 0 {
			t.Errorf("release step depends on %v, want no dependency (parallel)", release.DependsOn)
		}
	}
	finalize, ok := stepByID(steps, completeUninstallStepID)
	if !ok {
		t.Fatal("release path must include the complete-uninstall finalize step")
	}
	if finalize.Kind != "complete-uninstall" || finalize.Executor != operationdomain.RunnerKindInternal {
		t.Errorf("finalize step = %+v, want complete-uninstall/internal", finalize)
	}
	if len(finalize.DependsOn) != len(launch.TargetServerIDs) {
		t.Fatalf("finalize dependsOn = %v, want one per release", finalize.DependsOn)
	}
	for _, serverID := range launch.TargetServerIDs {
		found := false
		for _, dep := range finalize.DependsOn {
			if dep == "release-"+serverID {
				found = true
			}
		}
		if !found {
			t.Errorf("finalize step must depend on release-%s", serverID)
		}
	}
}

// TestUninstallStepsKeepServersUsesAnsibleStep verifies the keep-servers path is unchanged: a
// single uninstall-platform ansible step and no release or finalize steps.
func TestUninstallStepsKeepServersUsesAnsibleStep(t *testing.T) {
	launch := platformdomain.UninstallLaunch{
		Platform:        &platformdomain.Platform{ID: "platform-a", Name: "lab", Type: platformdomain.PlatformTypeKubernetes},
		TargetServerIDs: []string{"srv-1"},
		ReleaseServers:  false,
	}
	prepared := &operationapp.PreparedAnsibleStep{
		SiteID: "site-a", Playbook: "uninstall-kubernetes", Targets: []operationdomain.ResourceReference{{Kind: "server", ID: "srv-1"}},
	}

	steps := uninstallSteps(launch, prepared, "op-1", "Uninstall k0s Platform")

	if len(steps) != 1 {
		t.Fatalf("keep-servers uninstall = %d steps, want 1", len(steps))
	}
	if steps[0].ID != uninstallPlatformStepID || steps[0].Executor != operationdomain.RunnerKindAnsible {
		t.Fatalf("step = %+v, want the uninstall-platform ansible step", steps[0])
	}
	if _, ok := stepByID(steps, completeUninstallStepID); ok {
		t.Error("keep-servers uninstall must not add a finalize step")
	}
}
