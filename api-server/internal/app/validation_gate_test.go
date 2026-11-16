package app

import (
	"context"
	"slices"
	"strings"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	"github.com/maple52046/swallow/internal/core/store/storetest"
	validationapp "github.com/maple52046/swallow/internal/core/validation/application"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
	"github.com/maple52046/swallow/internal/core/validation/domain/validationtest"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	"github.com/maple52046/swallow/modules/platform/slurm"
)

// gateTestTargets is never asked: a gate's targets come from its deployment.
type gateTestTargets struct{ validationapp.Targets }

type gateTestLauncher struct{ validationapp.RunLauncher }

type gateTestCanceler struct{ validationapp.WorkflowCanceler }

// gateTestStarters is never asked: a gate composes only module suites.
type gateTestStarters struct{ validationapp.StarterAccess }

// testValidationGate is a gate over the build's suites with an in-memory run store.
func testValidationGate(t *testing.T) (*validationGate, *validationtest.Runs) {
	t.Helper()
	modules := testModules(t)
	suites, err := validationSuites(modules.registry)
	if err != nil {
		t.Fatal(err)
	}
	users, runs := validationtest.NewSuites(), validationtest.NewRuns()
	catalog, err := validationapp.NewCatalog(suites, users)
	if err != nil {
		t.Fatal(err)
	}
	service, err := validationapp.NewService(validationapp.ServiceDeps{
		Transactor: &storetest.Transactor{}, Catalog: catalog, Users: users, Runs: runs, Schedules: validationtest.NewSchedules(),
		Targets: gateTestTargets{}, Starters: gateTestStarters{}, Launcher: gateTestLauncher{}, Canceler: gateTestCanceler{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &validationGate{service: service, launcher: testValidationLauncher(t)}, runs
}

// A Slurm deployment composes slurm-acceptance last (validation.md §5): after install-platform and the
// composed software, because its container GPU check needs the container runtime. Its check enters through the primary controller in gate mode,
// over every member by role, expecting GPU GRES on the GPU compute nodes; complete-validation shares
// the suite's Job, so a Job retry reruns the checks; and the gate run is written with the Workflow.
func TestSlurmDeploymentComposesTheAcceptanceGate(t *testing.T) {
	launcher := slurmCompositionLauncher(t, map[string]bool{"s2": true})
	gate, runs := testValidationGate(t)
	launcher.gate = gate
	launch := platformdomain.DeploymentLaunch{
		Platform:           &platformdomain.Platform{ID: "plat-1", Name: "lab", Type: platformdomain.PlatformTypeSlurm},
		OperationID:        "op-1",
		TargetServerIDs:    []string{"s1", "s2", "s3"},
		RequestedBy:        "admin",
		MachinePreparation: platformdomain.MachinePreparation{Mode: platformdomain.MachinePreparationExistingOS},
		TrustedVars: map[string]any{
			"swallow_slurm_controller_ids": []string{"s1"}, "swallow_slurm_primary_controller_id": "s1",
			"swallow_slurm_compute_ids": []string{"s2", "s3"}, "swallow_slurm_login_ids": []string{"s3"},
			"swallow_slurm_gpu_compute_ids": []string{"s2"},
		},
		ContainerRuntime: &platformdomain.ContainerRuntimeLaunch{Kind: platformdomain.ContainerRuntimeDockerCE, ServerIDs: []string{"s2"}},
	}
	prepared, err := launcher.PrepareDeployment(context.Background(), launch)
	if err != nil {
		t.Fatalf("PrepareDeployment() error = %v", err)
	}
	plan := prepared.(*preparedPlatformDeployment)
	workflow := plan.workflow
	if err := launcher.gate.launcher.catalog.Validate(workflow.Steps, workflow.Jobs); err != nil {
		t.Fatalf("the plan catalog refuses the deployment: %v", err)
	}
	byID := map[string]workflowdomain.Task{}
	for _, step := range workflow.Steps {
		byID[step.ID] = step
	}
	check, complete := byID[gateTaskPrefix+slurmAcceptanceSuite], byID[gateCompleteTaskID]
	// The gate runs last, after the composed software: its container GPU check runs in the container
	// runtime the deployment installs (decision 083).
	last := workflow.Steps[slices.IndexFunc(workflow.Steps, func(step workflowdomain.Task) bool { return step.ID == check.ID })-1].ID
	if check.Job != "validate-slurm" || !slices.Equal(check.DependsOn, []string{last}) || last == installPlatformStepID ||
		!slices.Equal(check.Targets, serverRefs("s1")) || check.TimeoutSeconds != 3300 {
		t.Errorf("gate check = Job %s DependsOn %v Targets %v timeout %d", check.Job, check.DependsOn, check.Targets, check.TimeoutSeconds)
	}
	vars := extraVars(t, check)
	members, _ := vars["swallow_validation_members"].([]map[string]any)
	parameters, _ := vars["swallow_validation_parameters"].(map[string]any)
	if vars["swallow_validation_mode"] != string(validationdomain.ModeGate) || vars["swallow_validation_suite"] != slurmAcceptanceSuite ||
		len(members) != 3 || !slices.Equal(parameters["gpuServers"].([]string), []string{"s2"}) ||
		parameters["containerImage"] != slurm.GPUTestImage || parameters["containerRuntime"] != "docker-ce" {
		t.Errorf("gate check vars = %v", vars)
	}
	if complete.Job != "validate-slurm" || complete.Kind != completeValidationTaskKind ||
		!slices.Equal(complete.DependsOn, []string{check.ID}) || complete.Parameters[completionParamMode] != string(validationdomain.ModeGate) {
		t.Errorf("gate complete Task = %+v", complete)
	}
	assertCompleteRuns(t, workflow.Steps, gateCompleteTaskID)
	if runtime, ok := byID["install-docker-ce"]; !ok || !slices.Equal(runtime.DependsOn, []string{installPlatformStepID}) {
		t.Errorf("the composed container runtime install (%v) does not follow the install: DependsOn %v", ok, runtime.DependsOn)
	}
	for _, step := range workflow.Steps {
		if strings.Contains(step.ID, "probe") {
			t.Errorf("the deployment plans %s; the container GPU check pulls its image on the node itself", step.ID)
		}
	}
	labelled := false
	for _, job := range workflow.Jobs {
		if job.Name == "validate-slurm" {
			labelled = job.Label == "Validate Slurm" && job.Retry == workflowdomain.RetryJob
		}
	}
	if !labelled {
		t.Errorf("Jobs = %+v, want validate-slurm labelled Validate Slurm with Job retry", workflow.Jobs)
	}
	run := plan.gate.run
	if run.Mode != validationdomain.ModeGate || run.WorkflowID != "op-1" || run.Trigger.Kind != validationdomain.TriggerDeployment ||
		!slices.Equal(run.Target.ServerIDs, []string{"s1"}) || len(run.Target.MemberIDs) != 3 {
		t.Errorf("gate run = %+v", run)
	}
	if _, total, _ := runs.List(context.Background(), validationdomain.RunFilter{}); total != 0 {
		t.Error("preparing the deployment wrote the gate run")
	}
}
