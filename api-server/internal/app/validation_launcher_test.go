package app

import (
	"context"
	"slices"
	"testing"
	"time"

	validationapp "github.com/maple52046/swallow/internal/core/validation/application"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// testValidationLauncher plans over deployed test Servers with the build's modules and an NTP server.
func testValidationLauncher(t *testing.T) validationLauncher {
	t.Helper()
	modules := testModules(t)
	return validationLauncher{
		operations: newTestExecutionService(slurmTestServers{}, ntpTestAutomation{}, slurmTestCatalog{}, modules.workflowKinds),
		catalog:    workflowPlanCatalog(modules.workflowKinds, modules.software), registry: modules.registry,
		ntp: ntpStepPlanner{server: "10.0.0.1"},
	}
}

// testLaunchRequest is a report run of suite: on Servers s1 and s2, or on a Platform of the suite's
// type entered through login with compute member c1. A user suite's run records its script and runs
// as alice, as Service.Start records them.
func testLaunchRequest(suite validationdomain.Suite) validationapp.LaunchRequest {
	run := validationdomain.Run{ID: "run-1", SiteID: "site-1", Checks: suite.Checks, Mode: validationdomain.ModeReport,
		RequestedBy: "admin", Target: validationdomain.Target{Kind: suite.Target, ServerIDs: []string{"s1", "s2"}}}
	if suite.Source == validationdomain.SourceUser {
		run.Suite = validationdomain.RunSuite{ID: suite.ID, Source: suite.Source, Revision: suite.Revision, ScriptSHA256: suite.ScriptSHA256, Script: suite.Script}
		run.RequestedByID, run.RunAs = "user-alice", &validationdomain.RunAccount{Username: "alice", UIDNumber: 10005}
	}
	request := validationapp.LaunchRequest{Run: run, Suite: suite}
	if suite.Target == validationdomain.TargetPlatform {
		request.Run.Target = validationdomain.Target{Kind: suite.Target, PlatformID: "p-1", ServerIDs: []string{"login"},
			MemberIDs: []string{"login", "c1"}}
		request.Platform = &validationapp.TargetPlatform{ID: "p-1", SiteID: "site-1", Type: suite.PlatformType, Active: true,
			AccessServerIDs: []string{"login"}, Members: []validationapp.PlatformMember{
				{ServerID: "login", Roles: []string{"login"}}, {ServerID: "c1", Roles: []string{"compute"}}}}
	}
	return request
}

// A module suite carries the experimental flag its module declares: the suites written ahead of
// their redesign are previews, while slurm-acceptance, the checks the Slurm deployment gates on, is
// not.
func TestModuleSuitesCarryTheirExperimentalFlag(t *testing.T) {
	suites, err := validationSuites(testModules(t).registry)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"host-baseline": true, "network-mesh": true, "amd-gpu-basic": true, "amd-gpu-ras": true, "kubernetes-smoke": true,
		slurmAcceptanceSuite: false,
	}
	for _, suite := range suites {
		if experimental, ok := want[suite.ID]; ok && suite.Experimental != experimental {
			t.Errorf("suite %s experimental = %v, want %v", suite.ID, suite.Experimental, experimental)
		}
		delete(want, suite.ID)
	}
	if len(want) > 0 {
		t.Errorf("the build declares no suite %v", want)
	}
}

// A user suite's playbook gets the script the run recorded and the account the run uses, never an
// account the suite names; a module suite's gets neither.
func TestUserSuiteVarsCarryTheRunsAccount(t *testing.T) {
	user, err := validationdomain.NewUserSuite("u-1", validationdomain.UserSuiteInput{Name: "id", Script: "#!/bin/sh\nid\n", Sudo: true},
		validationdomain.Author{UserID: "user-bob", Username: "bob"}, time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	run := validationdomain.Run{ID: "r-1", Mode: validationdomain.ModeReport, RequestedByID: "user-alice",
		Suite:  validationdomain.RunSuite{ID: user.ID, Source: validationdomain.SourceUser, Revision: 1, Script: user.Script},
		RunAs:  &validationdomain.RunAccount{Username: "alice", UIDNumber: 10005, Sudo: true},
		Target: validationdomain.Target{Kind: validationdomain.TargetServers, ServerIDs: []string{"s-1"}}}
	vars := validationVars(run, user, nil)
	requester, _ := vars["swallow_validation_requester"].(map[string]any)
	if requester["userId"] != "user-alice" || requester["username"] != "alice" || requester["uidNumber"] != 10005 ||
		vars["swallow_validation_sudo"] != true || vars["swallow_validation_script"] != user.Script || vars["swallow_validation_timeout_seconds"] != 600 {
		t.Errorf("user suite vars = %v", vars)
	}
	module := validationdomain.Suite{ID: "host-baseline", Source: validationdomain.SourceModule}
	vars = validationVars(validationdomain.Run{ID: "r-2", Mode: validationdomain.ModeReport}, module, nil)
	for _, key := range []string{"swallow_validation_requester", "swallow_validation_sudo", "swallow_validation_script"} {
		if _, ok := vars[key]; ok {
			t.Errorf("module suite vars carry %s", key)
		}
	}
}

// Every suite the build declares, and a user suite, plans a report run the plan catalog accepts: SSH
// readiness first, the suite's check Tasks each recording results and projecting the run, and the
// final complete-validation Task after all of them; unattended, within the suite's budget, and
// preemptible unless disruptive.
func TestEverySuitePlansAnAcceptableReportRun(t *testing.T) {
	launcher := testValidationLauncher(t)
	suites, err := validationSuites(testModules(t).registry)
	if err != nil {
		t.Fatal(err)
	}
	user, err := validationdomain.NewUserSuite("u-1", validationdomain.UserSuiteInput{Name: "mounts", Script: "#!/bin/sh\nexit 0\n"},
		validationdomain.Author{UserID: "user-alice", Username: "alice"}, time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, suite := range append(suites, user) {
		t.Run(suite.ID, func(t *testing.T) {
			prepared, err := launcher.PrepareRun(context.Background(), testLaunchRequest(suite))
			if err != nil {
				t.Fatalf("PrepareRun() error = %v", err)
			}
			plan, ok := prepared.Plan.(workflowapp.CreateWorkflowInput)
			if !ok || plan.ID != prepared.WorkflowID || plan.Kind != workflowdomain.WorkflowKindRunValidation {
				t.Fatalf("plan = %+v, want a run-validation Workflow with the prepared id", prepared.Plan)
			}
			if err := launcher.catalog.Validate(plan.Steps, plan.Jobs); err != nil {
				t.Fatalf("the plan catalog refuses the plan: %v", err)
			}
			if !plan.Unattended || plan.BudgetSeconds != suite.BudgetSeconds || plan.Preemptible != (suite.Impact != validationdomain.ImpactDisruptive) {
				t.Errorf("unattended %v, budget %d, preemptible %v; want unattended, %d, %v", plan.Unattended, plan.BudgetSeconds,
					plan.Preemptible, suite.BudgetSeconds, suite.Impact != validationdomain.ImpactDisruptive)
			}
			steps := plan.Steps
			if first := steps[0]; first.ID != validationReadyTask || first.Kind != "wait-for-ssh" {
				t.Errorf("first Task = %s (%s), want the SSH readiness check", first.ID, first.Kind)
			}
			last := steps[len(steps)-1]
			if last.Kind != completeValidationTaskKind || !slices.Contains(last.ContinueOn, workflowdomain.TaskFailed) {
				t.Errorf("last Task = %s (%s), want complete-validation that runs after failures", last.ID, last.Kind)
			}
			checks := 0
			for _, step := range steps[1 : len(steps)-1] {
				if step.Executor != workflowdomain.RunnerKindHostAutomation {
					continue
				}
				checks++
				if !slices.Contains(last.DependsOn, step.ID) {
					t.Errorf("complete-validation does not wait for check Task %s", step.ID)
				}
				types := []string{}
				for _, completion := range step.Completion {
					types = append(types, completion.Type)
				}
				if !slices.Equal(types, []string{completionValidationResult, completionValidationRunState}) {
					t.Errorf("check Task %s completions = %v", step.ID, types)
				}
				vars, _ := step.Parameters["extraVars"].(map[string]any)
				if vars["swallow_validation_run_id"] != "run-1" || vars["swallow_validation_suite"] != suite.ID ||
					!slices.Equal(vars[ntpServersVar].([]string), []string{"10.0.0.1"}) {
					t.Errorf("check Task %s trusted vars = %v", step.ID, vars)
				}
				if suite.Source == validationdomain.SourceUser && vars["swallow_validation_script"] != user.Script {
					t.Errorf("the user suite's Task does not carry its script: %v", vars)
				}
			}
			if checks == 0 {
				t.Error("the plan has no check Task")
			}
			assertCompleteRuns(t, steps, last.ID)
		})
	}
}

// assertCompleteRuns fails unless Task completeID is ready to run once every other Task succeeded,
// failed, or was skipped: complete-validation records the run, and fails a gate, whatever its checks
// ended as. A non-empty ContinueOn replaces the default outcomes, so it must list succeeded too.
func assertCompleteRuns(t *testing.T, steps []workflowdomain.Task, completeID string) {
	t.Helper()
	for _, outcome := range []workflowdomain.TaskStatus{workflowdomain.TaskSucceeded, workflowdomain.TaskFailed, workflowdomain.TaskSkipped} {
		ended := slices.Clone(steps)
		complete := -1
		for i := range ended {
			ended[i].Status = outcome
			if ended[i].ID == completeID {
				ended[i].Status, complete = workflowdomain.TaskPending, i
			}
		}
		if schedule := workflowdomain.PlanWith(ended, len(ended), workflowdomain.PlanPolicy{}); !slices.Contains(schedule.Ready, complete) {
			t.Errorf("after the other Tasks %s, %s is not ready (skipped %v)", outcome, completeID, schedule.Skip)
		}
	}
}
