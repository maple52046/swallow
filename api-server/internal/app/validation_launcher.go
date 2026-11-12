package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	validationapp "github.com/maple52046/swallow/internal/core/validation/application"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// The Validation Module that runs user-defined suites, and its template (validation.md §2.2).
const (
	scriptModuleID       = "script"
	scriptModuleTemplate = "run-script"
)

// The completion types of a Validation Run's Tasks (validation.md §4): validation-result records
// what a check Task's playbook returned; validation-run-state projects every Task transition onto
// the run's status and verdict.
const (
	completionValidationResult   = "validation-result"
	completionValidationRunState = "validation-run-state"
	completionParamRun           = "runId"
	completionParamStage         = "stage"
)

// The Task kind and Job of a run's final Task, and the Job of its readiness check.
const (
	completeValidationTaskKind = "complete-validation"
	completeValidationJob      = "complete-validation"
	validationReadyTask        = "validation-ready"
)

// validationSuites restates the suites the build's modules declare in the validation domain's
// terms: each with its template, its checks, and its parameter schema.
func validationSuites(registry *moduledomain.Registry) ([]validationdomain.Suite, error) {
	var suites []validationdomain.Suite
	for _, declared := range registry.Validations() {
		declaration := declared.Declaration
		suite := validationdomain.Suite{
			ID: declaration.ID, Source: validationdomain.SourceModule,
			Module: &validationdomain.ModuleRef{Kind: string(declared.Module.Kind), ID: declared.Module.ID, Version: declared.Module.Version},
			Name:   declaration.Title, Description: declaration.Description,
			Target: validationdomain.TargetKind(declaration.Target), PlatformType: declaration.PlatformType,
			Impact: validationdomain.Impact(declaration.Impact), BudgetSeconds: declaration.BudgetSeconds,
			Template: declared.Template, Experimental: declaration.Experimental,
		}
		for _, check := range declaration.Checks {
			suite.Checks = append(suite.Checks, validationdomain.Check{ID: check.ID, Title: check.Title, Description: check.Description, Gating: check.Gating})
		}
		if len(declaration.Parameters) > 0 {
			schema, err := moduledomain.ParseSpecSchema(declaration.Parameters)
			if err != nil {
				return nil, fmt.Errorf("module %s suite %s parameters: %w", declared.Module.Ref(), declaration.ID, err)
			}
			suite.Parameters, suite.ParametersJSON = schema, append([]byte(nil), declaration.Parameters...)
		}
		suites = append(suites, suite)
	}
	return suites, nil
}

// validationLauncher is the validation context's RunLauncher: it compiles a report run's Workflow
// from its suite's template (a user suite's from the script module's), checks each Ansible Task
// through the same acceptance as every other (automation, Server Lock, the release allowlist,
// artifacts), and accepts it unattended, budgeted, and preemptible unless disruptive.
type validationLauncher struct {
	operations *workflowapp.ExecutionService
	workflows  *workflowapp.WorkflowService
	catalog    workflowdomain.PlanCatalog
	registry   *moduledomain.Registry
	// ntp gives the checks the installation's NTP server (swallow_ntp_servers).
	ntp ntpStepPlanner
}

// runRefusals are the acceptance errors of a run's Workflow that a retry would only repeat until
// something outside the run changes: a busy, locked, or not deployed target, automation that cannot
// run, or artifacts that cannot be fetched.
var runRefusals = []error{
	workflowdomain.ErrTargetsBusy, workflowdomain.ErrTargetLocked, workflowdomain.ErrTargetStateInvalid,
	workflowdomain.ErrPolicyConflict, workflowdomain.ErrAutomationConfigNotFound, workflowdomain.ErrAutomationDisabled,
	workflowdomain.ErrAutomationCredentialMissing, workflowdomain.ErrPlaybookNotAllowed,
	workflowapp.ErrInvalidOperation, workflowapp.ErrTargetsSpanSites,
	serverdomain.ErrServerLocked, serverdomain.ErrServerLockUnavailable, localrepodomain.ErrLocalRepositoryUnavailable,
}

// refusedRun marks an acceptance refusal with validationdomain.ErrRunRefused, keeping the original
// error for the API's mapping, so a schedule skips the occurrence instead of retrying it.
func refusedRun(err error) error {
	for _, refusal := range runRefusals {
		if errors.Is(err, refusal) {
			return fmt.Errorf("%w: %w", validationdomain.ErrRunRefused, err)
		}
	}
	return err
}

// PrepareRun implements validationapp.RunLauncher.
func (l validationLauncher) PrepareRun(ctx context.Context, request validationapp.LaunchRequest) (validationapp.PreparedRun, error) {
	prepared, err := l.prepareRun(ctx, request)
	return prepared, refusedRun(err)
}

func (l validationLauncher) prepareRun(ctx context.Context, request validationapp.LaunchRequest) (validationapp.PreparedRun, error) {
	run, suite := request.Run, request.Suite
	template, definition, err := l.template(suite)
	if err != nil {
		return validationapp.PreparedRun{}, err
	}
	workflowID := uuid.NewString()
	input := workflowdomain.TemplateInput{Servers: run.Target.ServerIDs, After: []string{validationReadyTask}}
	targets := slices.Clone(run.Target.ServerIDs)
	var resources []workflowdomain.ResourceReference
	if request.Platform != nil {
		input.Servers, input.Access, input.Members = nil, run.Target.ServerIDs, run.Target.MemberIDs
		resources = []workflowdomain.ResourceReference{{Kind: "platform", ID: request.Platform.ID}}
	}
	compiled, err := template.Compile(l.catalog, input)
	if err != nil {
		return validationapp.PreparedRun{}, err
	}
	steps, err := l.prepareChecks(ctx, workflowID, run, suite, request.Platform, compiled.Tasks, false)
	if err != nil {
		return validationapp.PreparedRun{}, err
	}
	for _, step := range steps {
		for _, target := range step.Targets {
			if target.Kind == "server" && !slices.Contains(targets, target.ID) {
				targets = append(targets, target.ID)
			}
		}
	}
	ready := workflowdomain.Task{
		ID: validationReadyTask, Kind: "wait-for-ssh", Name: "Verify SSH readiness", Job: jobPrepareHosts,
		Executor: workflowdomain.RunnerKindInternal, Targets: serverResourceRefs(targets),
	}
	steps = append([]workflowdomain.Task{ready}, append(steps, completeValidationTask(completeValidationTaskKind, completeValidationJob, run, steps))...)
	jobs := append([]workflowdomain.JobDeclaration{{Name: jobPrepareHosts, Retry: workflowdomain.RetryJob}}, compiled.Jobs...)
	jobs = append(jobs, workflowdomain.JobDeclaration{Name: completeValidationJob, Label: "Record Results", Retry: workflowdomain.RetryTask})
	platformID := ""
	if request.Platform != nil {
		platformID = request.Platform.ID
	}
	return validationapp.PreparedRun{WorkflowID: workflowID, Plan: workflowapp.CreateWorkflowInput{
		ID: workflowID, Kind: workflowdomain.WorkflowKindRunValidation,
		IntentSummary:  "Validate with " + suite.Name,
		IntentSnapshot: map[string]any{"validationRunId": run.ID, "suiteId": suite.ID, "mode": string(run.Mode)},
		Definition:     definition, DefinitionVersion: template.Version,
		SiteID: run.SiteID, PlatformID: platformID, TargetServerIDs: targets, TargetResources: resources,
		Steps: steps, Jobs: jobs, RequestedBy: run.RequestedBy,
		Unattended: true, BudgetSeconds: suite.BudgetSeconds, Preemptible: suite.Impact.Preemptible(),
	}}, nil
}

// PersistRun implements validationapp.RunLauncher; it joins the caller's transaction.
func (l validationLauncher) PersistRun(ctx context.Context, prepared validationapp.PreparedRun) error {
	input, ok := prepared.Plan.(workflowapp.CreateWorkflowInput)
	if !ok {
		return errors.New("validation launcher: the prepared run carries no Workflow plan")
	}
	_, err := l.workflows.Create(ctx, input)
	return refusedRun(err)
}

// template returns the template that runs suite and the definition its Workflow records.
func (l validationLauncher) template(suite validationdomain.Suite) (workflowdomain.Template, string, error) {
	if suite.Source == validationdomain.SourceModule && suite.Module != nil {
		module, ok := l.registry.Module(moduledomain.Kind(suite.Module.Kind), suite.Module.ID)
		if !ok {
			return workflowdomain.Template{}, "", fmt.Errorf("module %s/%s of suite %s is not in this build", suite.Module.Kind, suite.Module.ID, suite.ID)
		}
		return suite.Template, module.Manifest.Definition(), nil
	}
	module, ok := l.registry.Module(moduledomain.KindValidation, scriptModuleID)
	if !ok {
		return workflowdomain.Template{}, "", errors.New("the validation/script module that runs user suites is not in this build")
	}
	template, ok := module.Manifest.Templates[scriptModuleTemplate]
	if !ok {
		return workflowdomain.Template{}, "", fmt.Errorf("module %s declares no %s template", module.Manifest.Ref(), scriptModuleTemplate)
	}
	return template, module.Manifest.Definition(), nil
}

// prepareChecks turns the compiled host automation Tasks into accepted Ansible Tasks: each one's
// playbook and frozen extra variables (the validation vars, the template's static vars, and the
// standard and artifact vars every Ansible Task gets) and the run's two completions. provisionFirst
// lets a gate's checks target Servers its deployment provisions first.
func (l validationLauncher) prepareChecks(ctx context.Context, workflowID string, run validationdomain.Run,
	suite validationdomain.Suite, platform *validationapp.TargetPlatform, compiled []workflowdomain.Task, provisionFirst bool,
) ([]workflowdomain.Task, error) {
	steps := make([]workflowdomain.Task, 0, len(compiled))
	for _, step := range compiled {
		if step.Executor == workflowdomain.RunnerKindHostAutomation {
			playbook, _ := step.Parameters["playbook"].(string)
			trusted := validationVars(run, suite, platform)
			trusted[ntpServersVar] = l.ntp.servers()
			if vars, ok := step.Parameters["vars"].(map[string]any); ok {
				maps.Copy(trusted, vars)
			}
			platformID := ""
			if platform != nil {
				platformID = platform.ID
			}
			prepared, err := l.operations.PrepareAnsibleStep(ctx, workflowapp.CreateExecutionInput{
				Kind: string(workflowdomain.WorkflowKindRunValidation), PlaybookName: playbook, PlatformID: platformID,
				TargetServerIDs: serverIDsOf(step.Targets), TrustedVars: trusted, RequestedBy: run.RequestedBy,
			}, workflowID, provisionFirst)
			if err != nil {
				return nil, err
			}
			step.Parameters = map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars}
			step.Completion = []workflowdomain.Completion{
				{Type: completionValidationResult, Params: map[string]any{completionParamRun: run.ID}},
				{Type: completionValidationRunState, Params: map[string]any{completionParamRun: run.ID, completionParamStage: string(validationapp.StageCheck)}},
			}
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// completeValidationTask is a run's final Task, id: it depends on every check Task and runs whatever
// they ended as, so the run always completes (or, canceled, ends canceled) through its projection;
// in gate mode it fails the deployment when a gating check did not pass. job is the Job it belongs
// to: its own in a report run, the suite's in a gate, so retrying a failed gate reruns the checks.
func completeValidationTask(id, job string, run validationdomain.Run, checks []workflowdomain.Task) workflowdomain.Task {
	dependsOn := make([]string, 0, len(checks))
	for _, step := range checks {
		if step.ID != validationReadyTask {
			dependsOn = append(dependsOn, step.ID)
		}
	}
	return workflowdomain.Task{
		ID: id, Kind: completeValidationTaskKind, Name: "Record the validation results",
		Job: job, Executor: workflowdomain.RunnerKindInternal, DependsOn: dependsOn,
		ContinueOn: []workflowdomain.TaskStatus{workflowdomain.TaskSucceeded, workflowdomain.TaskFailed, workflowdomain.TaskSkipped, workflowdomain.TaskCanceled},
		Parameters: map[string]any{completionParamRun: run.ID, completionParamMode: string(run.Mode)},
		Completion: []workflowdomain.Completion{
			{Type: completionValidationRunState, Params: map[string]any{completionParamRun: run.ID, completionParamStage: string(validationapp.StageComplete)}},
		},
	}
}

// validationVars are the trusted variables every validation playbook receives (validation.md §3).
func validationVars(run validationdomain.Run, suite validationdomain.Suite, platform *validationapp.TargetPlatform) map[string]any {
	checks := make([]string, 0, len(run.Checks))
	for _, check := range run.Checks {
		checks = append(checks, check.ID)
	}
	targets := run.Target.ServerIDs
	members := []map[string]any{}
	if platform != nil {
		targets = run.Target.MemberIDs
		for _, member := range platform.Members {
			members = append(members, map[string]any{"serverId": member.ServerID, "roles": slices.Clone(member.Roles)})
		}
	}
	parameters := run.Parameters
	if parameters == nil {
		parameters = map[string]any{}
	}
	vars := map[string]any{
		"swallow_validation_run_id": run.ID, "swallow_validation_suite": suite.ID, "swallow_validation_mode": string(run.Mode),
		"swallow_validation_checks": checks, "swallow_validation_parameters": parameters,
		"swallow_validation_targets": slices.Clone(targets), "swallow_validation_platform_id": run.Target.PlatformID,
		"swallow_validation_members": members,
	}
	if suite.Source == validationdomain.SourceUser {
		vars["swallow_validation_script"] = suite.Script
		vars["swallow_validation_run_as"] = string(suite.RunAs)
		vars["swallow_validation_timeout_seconds"] = suite.TimeoutSeconds
	}
	return vars
}

func serverIDsOf(targets []workflowdomain.ResourceReference) []string {
	ids := make([]string, 0, len(targets))
	for _, target := range targets {
		if target.Kind == "server" {
			ids = append(ids, target.ID)
		}
	}
	return ids
}
