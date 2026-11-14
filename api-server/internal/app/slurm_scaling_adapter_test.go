package app

import (
	"context"
	"slices"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// A scale-in compiles the Slurm module's scale-in template: drain from the primary controller, then
// reconfigure every member that stays, then clean the leaving nodes; a leaving node whose Server is
// released gets a release Task after the clean, and the finalize Task waits for all of them. The
// drain carries each leaving node's timeout and force choice, and the plan passes the catalog.
func TestSlurmScaleInCompilesTheModuleTemplate(t *testing.T) {
	modules := testModules(t)
	launcher := slurmCompositionLauncher(t, map[string]bool{"s3": true, "s4": true})
	launcher.registry, launcher.catalog = modules.registry, workflowPlanCatalog(modules.workflowKinds, modules.software)
	launch := platformdomain.DeploymentLaunch{
		Platform:        &platformdomain.Platform{ID: "plat-1", Name: "lab", Type: platformdomain.PlatformTypeSlurm, SiteID: "site-1"},
		OperationID:     "op-1",
		TargetServerIDs: []string{"s1", "s2"},
		RequestedBy:     "admin",
		TrustedVars:     map[string]any{"swallow_slurm_primary_controller_id": "s1"},
		ScaleIn: &platformdomain.ScaleInLaunch{Removals: []platformdomain.MemberRemoval{
			{ServerID: "s3", DrainTimeoutSeconds: 120, Force: true, ReleaseServer: true},
			{ServerID: "s4"},
		}},
	}
	plan, err := launcher.prepareSlurmScaleIn(context.Background(), launch)
	if err != nil {
		t.Fatalf("prepareSlurmScaleIn() error = %v", err)
	}
	workflow := plan.workflow
	if err := launcher.catalog.Validate(workflow.Steps, workflow.Jobs); err != nil {
		t.Fatalf("the plan catalog refuses the scale-in: %v", err)
	}
	if workflow.Kind != scaleInSlurmKind || workflow.Definition != "platform/slurm/slurm-scale-in" ||
		!slices.Equal(workflow.TargetServerIDs, []string{"s1", "s2", "s3", "s4"}) {
		t.Errorf("workflow = kind %s definition %s targets %v", workflow.Kind, workflow.Definition, workflow.TargetServerIDs)
	}
	byID := map[string]workflowdomain.Task{}
	var order []string
	for _, step := range workflow.Steps {
		byID[step.ID] = step
		order = append(order, step.ID)
	}
	drain, reconfigure, clean := byID["drain-nodes"], byID["reconfigure-slurm"], byID["clean-nodes"]
	// The leaving nodes are in the drain's inventory only to report their Slurm node names.
	if !slices.Equal(serverIDsOf(drain.Targets), []string{"s1", "s3", "s4"}) {
		t.Errorf("drain targets = %v, want the primary controller and the leaving nodes", serverIDsOf(drain.Targets))
	}
	if !slices.Equal(serverIDsOf(reconfigure.Targets), []string{"s1", "s2"}) || !slices.Equal(reconfigure.DependsOn, []string{"drain-nodes"}) {
		t.Errorf("reconfigure = targets %v depends %v", serverIDsOf(reconfigure.Targets), reconfigure.DependsOn)
	}
	if !slices.Equal(serverIDsOf(clean.Targets), []string{"s3", "s4"}) || !slices.Equal(clean.DependsOn, []string{"reconfigure-slurm"}) {
		t.Errorf("clean = targets %v depends %v", serverIDsOf(clean.Targets), clean.DependsOn)
	}
	nodes, _ := extraVars(t, drain)[varScaleInNodes].([]any)
	if len(nodes) != 2 {
		t.Fatalf("drain nodes = %v", nodes)
	}
	first, second := nodes[0].(map[string]any), nodes[1].(map[string]any)
	if first["server_id"] != "s3" || first["drain_timeout_seconds"] != float64(120) && first["drain_timeout_seconds"] != 120 || first["force"] != true {
		t.Errorf("s3 drain = %v", first)
	}
	if second["drain_timeout_seconds"] != float64(platformdomain.DefaultDrainTimeoutSeconds) && second["drain_timeout_seconds"] != platformdomain.DefaultDrainTimeoutSeconds {
		t.Errorf("s4 drain timeout = %v, want the default", second["drain_timeout_seconds"])
	}
	release, ok := byID["release-s3"]
	if !ok || release.Kind != "release-os" || !slices.Equal(release.DependsOn, []string{"clean-nodes"}) {
		t.Errorf("release-s3 = %+v", release)
	}
	if _, released := byID["release-s4"]; released {
		t.Error("s4 is released though its removal keeps the Server")
	}
	finalize := byID[completeScaleInStepID]
	if !slices.Equal(finalize.DependsOn, []string{"clean-nodes", "release-s3"}) || order[len(order)-1] != completeScaleInStepID {
		t.Errorf("finalize = %+v; order %v", finalize, order)
	}
	if ids, _ := finalize.Parameters["serverIds"].([]string); !slices.Equal(ids, []string{"s3", "s4"}) {
		t.Errorf("finalize serverIds = %v", finalize.Parameters["serverIds"])
	}
}

// A scale-in with nothing to remove is refused rather than planned empty.
func TestSlurmScaleInNeedsMembersToRemove(t *testing.T) {
	modules := testModules(t)
	launcher := slurmCompositionLauncher(t, nil)
	launcher.registry, launcher.catalog = modules.registry, workflowPlanCatalog(modules.workflowKinds, modules.software)
	launch := platformdomain.DeploymentLaunch{Platform: &platformdomain.Platform{ID: "plat-1", Name: "lab"}, ScaleIn: &platformdomain.ScaleInLaunch{}}
	if _, err := launcher.prepareSlurmScaleIn(context.Background(), launch); err == nil {
		t.Error("prepareSlurmScaleIn() without removals succeeded")
	}
}
