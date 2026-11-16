package app

import (
	"context"
	"slices"
	"strings"
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
		}, NodeNames: map[string]string{"s3": "gpu-03", "s4": "gpu-04"}},
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
	// The drain runs on the primary controller alone and names the leaving nodes by their stored
	// node names, so a node that cannot be reached is drained too.
	if !slices.Equal(serverIDsOf(drain.Targets), []string{"s1"}) {
		t.Errorf("drain targets = %v, want the primary controller", serverIDsOf(drain.Targets))
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
	if first["name"] != "gpu-03" || second["name"] != "gpu-04" {
		t.Errorf("drain node names = %v, %v", first["name"], second["name"])
	}
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
	// The members that stay are reconfigured from what they recorded, not redeployed.
	if playbook, _ := reconfigure.Parameters["playbook"].(string); playbook != "reconfigure-slurm" {
		t.Errorf("reconfigure playbook = %q, want reconfigure-slurm", playbook)
	}
}

// A reconfigure (groups and partitions alone) compiles the Slurm module's reconfigure template: one
// reconfigure-slurm Task over every member that detects no node again, under the reconfigure-slurm
// kind, which is not a deploy, and no gate. A rescan is the same Task detecting the rescanned node
// again, then the gate on that node alone.
func TestSlurmReconfigureAndRescanCompileTheModuleTemplate(t *testing.T) {
	modules := testModules(t)
	launcher := slurmCompositionLauncher(t, map[string]bool{"s2": true})
	launcher.registry, launcher.catalog = modules.registry, workflowPlanCatalog(modules.workflowKinds, modules.software)
	gate, _ := testValidationGate(t)
	launcher.gate = gate
	launch := platformdomain.DeploymentLaunch{
		Platform:        &platformdomain.Platform{ID: "plat-1", Name: "lab", Type: platformdomain.PlatformTypeSlurm, SiteID: "site-1"},
		OperationID:     "op-1",
		TargetServerIDs: []string{"s1", "s2", "s3"},
		RequestedBy:     "admin",
		TrustedVars: map[string]any{
			"swallow_slurm_controller_ids": []string{"s1"}, "swallow_slurm_primary_controller_id": "s1",
			"swallow_slurm_compute_ids": []string{"s2", "s3"}, "swallow_slurm_gpu_compute_ids": []string{"s2", "s3"},
		},
		Omnistat:    &platformdomain.OmnistatLaunch{ObservabilityServerID: "s1", ComputeServerIDs: []string{"s2", "s3"}},
		Realization: platformdomain.RealizeReconfigure,
	}
	plan := func(launch platformdomain.DeploymentLaunch) *preparedPlatformDeployment {
		t.Helper()
		prepared, err := launcher.PrepareDeployment(context.Background(), launch)
		if err != nil {
			t.Fatalf("PrepareDeployment(%s) error = %v", launch.Realization, err)
		}
		return prepared.(*preparedPlatformDeployment)
	}

	reconfigure := plan(launch)
	workflow := reconfigure.workflow
	if err := launcher.catalog.Validate(workflow.Steps, workflow.Jobs); err != nil {
		t.Fatalf("the plan catalog refuses the reconfigure: %v", err)
	}
	if workflow.Kind != reconfigureSlurmKind || workflow.Definition != "platform/slurm/slurm-reconfigure" ||
		len(workflow.Steps) != 1 || reconfigure.gate != nil || len(reconfigure.composed.installs) != 0 {
		t.Fatalf("reconfigure = kind %s definition %s steps %d gate %v installs %d; want one reconfigure Task",
			workflow.Kind, workflow.Definition, len(workflow.Steps), reconfigure.gate != nil, len(reconfigure.composed.installs))
	}
	step := workflow.Steps[0]
	if playbook, _ := step.Parameters["playbook"].(string); playbook != "reconfigure-slurm" ||
		!slices.Equal(serverIDsOf(step.Targets), []string{"s1", "s2", "s3"}) {
		t.Errorf("reconfigure Task = playbook %q targets %v", playbook, serverIDsOf(step.Targets))
	}
	if ids, _ := extraVars(t, step)[varRescanIDs].([]string); len(ids) != 0 {
		t.Errorf("a reconfigure detects %v again, want no node", ids)
	}

	launch.Realization, launch.ChangedServerIDs = platformdomain.RealizeRescan, []string{"s2"}
	rescan := plan(launch)
	workflow = rescan.workflow
	if err := launcher.gate.launcher.catalog.Validate(workflow.Steps, workflow.Jobs); err != nil {
		t.Fatalf("the plan catalog refuses the rescan: %v", err)
	}
	byID := map[string]workflowdomain.Task{}
	for _, step := range workflow.Steps {
		byID[step.ID] = step
	}
	detect, check := byID["reconfigure-slurm"], byID[gateTaskPrefix+slurmAcceptanceSuite]
	if ids, _ := extraVars(t, detect)[varRescanIDs].([]string); !slices.Equal(ids, []string{"s2"}) {
		t.Errorf("rescan detects %v again, want s2", ids)
	}
	parameters, _ := extraVars(t, check)["swallow_validation_parameters"].(map[string]any)
	if workflow.Kind != reconfigureSlurmKind || rescan.gate == nil || !slices.Equal(check.DependsOn, []string{"reconfigure-slurm"}) ||
		!slices.Equal(parameters["nodes"].([]string), []string{"s2"}) {
		t.Errorf("rescan = kind %s, gate check depends %v, nodes %v; want the gate on s2 after the reconfigure",
			workflow.Kind, check.DependsOn, parameters["nodes"])
	}
	for _, step := range workflow.Steps {
		if step.ID == installPlatformStepID || strings.HasPrefix(step.ID, "install-") {
			t.Errorf("a rescan plans %s; it must not redeploy", step.ID)
		}
	}
	labelled := false
	for _, job := range workflow.Jobs {
		labelled = labelled || job.Name == "validate-slurm" && job.Label == "Validate Slurm"
	}
	if !labelled {
		t.Errorf("Jobs = %+v, want the gate's validate-slurm Job labelled", workflow.Jobs)
	}

	launch.ChangedServerIDs = nil
	if _, err := launcher.PrepareDeployment(context.Background(), launch); err == nil {
		t.Error("a rescan without a node to detect again was planned")
	}
}

// A leaving member without a stored node name cannot be drained by name, so the scale-in is refused.
func TestSlurmScaleInNeedsEveryNodeName(t *testing.T) {
	modules := testModules(t)
	launcher := slurmCompositionLauncher(t, nil)
	launcher.registry, launcher.catalog = modules.registry, workflowPlanCatalog(modules.workflowKinds, modules.software)
	launch := platformdomain.DeploymentLaunch{
		Platform: &platformdomain.Platform{ID: "plat-1", Name: "lab"}, TargetServerIDs: []string{"s1"},
		TrustedVars: map[string]any{"swallow_slurm_primary_controller_id": "s1"},
		ScaleIn:     &platformdomain.ScaleInLaunch{Removals: []platformdomain.MemberRemoval{{ServerID: "s3"}}},
	}
	if _, err := launcher.prepareSlurmScaleIn(context.Background(), launch); err == nil {
		t.Error("prepareSlurmScaleIn() without node names succeeded")
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
