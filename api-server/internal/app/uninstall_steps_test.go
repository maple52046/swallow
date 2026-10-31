package app

import (
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

func stepByID(steps []workflowdomain.Task, id string) (workflowdomain.Task, bool) {
	for _, step := range steps {
		if step.ID == id {
			return step, true
		}
	}
	return workflowdomain.Task{}, false
}

// TestUninstallStepsReleaseSkipsUninstall verifies the release path releases every server
// directly (no uninstall-platform ansible step) and finalizes with an internal
// complete-uninstall step that depends on all releases. It also verifies the Job grouping
// that mirrors deploy: every release Task is in the release-servers Job and the finalize Task
// is in the finalize-uninstall Job, so the ordering is driven by cross-Job dependency.
func TestUninstallStepsReleaseSkipsUninstall(t *testing.T) {
	launch := platformdomain.UninstallLaunch{
		Platform:        &platformdomain.Platform{ID: "platform-a", Name: "lab", Type: platformdomain.PlatformTypeSlurm},
		TargetServerIDs: []string{"srv-1", "srv-2"},
		ReleaseServers:  true,
		ReleaseOptions:  platformdomain.ServerReleaseOptions{Erase: true},
		DeleteRecord:    true,
	}
	prepared := &workflowapp.PreparedAnsibleStep{SiteID: "site-a"}

	steps := uninstallSteps(launch, prepared, "op-1", "Uninstall Slurm Platform")

	if _, ok := stepByID(steps, uninstallPlatformStepID); ok {
		t.Fatalf("release path must not include the %q ansible step", uninstallPlatformStepID)
	}
	for _, serverID := range launch.TargetServerIDs {
		release, ok := stepByID(steps, "release-"+serverID)
		if !ok {
			t.Fatalf("missing release step for %s", serverID)
		}
		if release.Kind != "release-os" || release.Executor != workflowdomain.RunnerKindProvisioner {
			t.Errorf("release step = %+v, want release-os/provisioner", release)
		}
		if release.Job != jobReleaseServers {
			t.Errorf("release step Job = %q, want %q", release.Job, jobReleaseServers)
		}
		if len(release.DependsOn) != 0 {
			t.Errorf("release step depends on %v, want no dependency (parallel)", release.DependsOn)
		}
	}
	finalize, ok := stepByID(steps, completeUninstallStepID)
	if !ok {
		t.Fatal("release path must include the complete-uninstall finalize step")
	}
	if finalize.Kind != "complete-uninstall" || finalize.Executor != workflowdomain.RunnerKindInternal {
		t.Errorf("finalize step = %+v, want complete-uninstall/internal", finalize)
	}
	if finalize.Job != jobFinalizeUninstall {
		t.Errorf("finalize step Job = %q, want %q", finalize.Job, jobFinalizeUninstall)
	}
	// The release path has no extra vars, so the operator's deleteRecord choice must ride on the
	// finalize Task that performs the cleanup.
	if got, _ := finalize.Parameters[deleteRecordParameter].(bool); !got {
		t.Errorf("finalize step parameters = %v, want %s=true", finalize.Parameters, deleteRecordParameter)
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

// TestUninstallStepsKeepServersUsesAnsibleStep verifies the keep-servers path is a single
// uninstall-platform ansible step grouped in the uninstall-platform Job (so it is grouped like
// deploy), with no release or finalize steps.
func TestUninstallStepsKeepServersUsesAnsibleStep(t *testing.T) {
	launch := platformdomain.UninstallLaunch{
		Platform:        &platformdomain.Platform{ID: "platform-a", Name: "lab", Type: platformdomain.PlatformTypeKubernetes},
		TargetServerIDs: []string{"srv-1"},
		ReleaseServers:  false,
	}
	prepared := &workflowapp.PreparedAnsibleStep{
		SiteID: "site-a", Playbook: "uninstall-kubernetes", Targets: []workflowdomain.ResourceReference{{Kind: "server", ID: "srv-1"}},
	}

	steps := uninstallSteps(launch, prepared, "op-1", "Uninstall k0s Platform")

	if len(steps) != 1 {
		t.Fatalf("keep-servers uninstall = %d steps, want 1", len(steps))
	}
	if steps[0].ID != uninstallPlatformStepID || steps[0].Executor != workflowdomain.RunnerKindAnsible {
		t.Fatalf("step = %+v, want the uninstall-platform ansible step", steps[0])
	}
	if steps[0].Job != jobUninstallPlatform {
		t.Errorf("keep-servers step Job = %q, want %q", steps[0].Job, jobUninstallPlatform)
	}
	if _, ok := stepByID(steps, completeUninstallStepID); ok {
		t.Error("keep-servers uninstall must not add a finalize step")
	}
}
