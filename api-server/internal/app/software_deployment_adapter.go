package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	softwareapp "github.com/maple52046/swallow/internal/core/software/application"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// Software deployment Jobs (ADR 017 / decision 038). A standalone software install composes three
// convergent Jobs on one parent Workflow: prepare-hosts brings each already-deployed target to SSH
// readiness, configure-<kind> runs the one idempotent software playbook, and record-software writes
// the swallow-owned Software Assignment. A platform deploy that pulls in a software component
// reuses the same playbooks and record step as Jobs of its own Workflow (omnistat_adapter.go); it
// must never be a nested child Workflow.
const (
	jobPrepareHosts   = "prepare-hosts"
	jobRecordSoftware = "record-software"

	// softwareInstallStepID is the ansible Task that installs (or uninstalls) the software. An
	// install declares the failure marking of its assignments, which covers a terminal failure
	// after which the record step never runs.
	softwareInstallStepID = "configure-software"
	// softwareRecordStepID is the internal Task that flips the assignment to installed (install) or
	// absent (uninstall) on success.
	softwareRecordStepID = "record-software"
	// softwareWaitStepID gates on SSH readiness before the software playbook runs.
	softwareWaitStepID = "wait-for-ssh"
)

// softwareStepKindRecord and softwareStepKindClear are the internal Task kinds the software record
// Job uses: record marks the assignment installed, clear marks it absent.
const (
	softwareStepKindRecord = "record-software-assignment"
	softwareStepKindClear  = "clear-software-assignment"
)

// softwarePlaybook is a software kind's install or uninstall Workflow kind and playbook. Both are
// derived from the kind, which the Software module's manifest declares, so a software install needs
// no site playbookMappings entry.
type softwarePlaybook struct {
	workflowKind workflowdomain.WorkflowKind
	playbook     string
}

// installPlaybook is the configure-<kind> Workflow kind and deploy-<kind> playbook.
func installPlaybook(kind softwaredomain.Kind) softwarePlaybook {
	return softwarePlaybook{workflowdomain.WorkflowKind(kind.InstallWorkflowKind()), kind.InstallPlaybook()}
}

// uninstallPlaybook is the uninstall-<kind> Workflow kind and playbook.
func uninstallPlaybook(kind softwaredomain.Kind) softwarePlaybook {
	return softwarePlaybook{workflowdomain.WorkflowKind(kind.UninstallWorkflowKind()), kind.UninstallPlaybook()}
}

// softwareDeploymentLauncher composes the durable software Workflow. It reuses the operation
// layer's PrepareAnsibleStep (which resolves the site, enforces the deployed/lock preconditions,
// and assembles trusted vars) and the WorkflowService, so software deployment shares the platform
// deployment machinery rather than owning a second engine.
type softwareDeploymentLauncher struct {
	operations     *workflowapp.ExecutionService
	orchestrations *workflowapp.WorkflowService
	// modules gives each software Workflow its module's definition (ADR 075).
	modules *moduledomain.Registry
}

// preparedSoftwareWorkflow is the plan the software launcher's prepare steps return
// (softwareapp.PreparedSoftwareWorkflow): the complete Workflow, not yet written.
type preparedSoftwareWorkflow struct {
	workflow workflowapp.CreateWorkflowInput
}

// WorkflowID is the preassigned id the Workflow gets when persisted.
func (p *preparedSoftwareWorkflow) WorkflowID() string { return p.workflow.ID }

// PrepareInstall plans the install Workflow: prepare-hosts -> configure-<kind> -> record-software.
func (l softwareDeploymentLauncher) PrepareInstall(ctx context.Context, launch softwareapp.SoftwareLaunch) (softwareapp.PreparedSoftwareWorkflow, error) {
	return l.prepare(ctx, launch, installPlaybook(launch.Kind), softwareStepKindRecord, "install", "Install "+launchLabel(launch))
}

// PrepareUninstall plans the uninstall Workflow: prepare-hosts -> configure-<kind> (uninstall
// playbook) -> record-software (clear).
func (l softwareDeploymentLauncher) PrepareUninstall(ctx context.Context, launch softwareapp.SoftwareLaunch) (softwareapp.PreparedSoftwareWorkflow, error) {
	return l.prepare(ctx, launch, uninstallPlaybook(launch.Kind), softwareStepKindClear, "uninstall", "Uninstall "+launchLabel(launch))
}

// PersistWorkflow writes a plan from PrepareInstall or PrepareUninstall. The software service runs
// it inside the transaction that also writes the Software Assignments; a busy target is detected
// here and returned unchanged.
func (l softwareDeploymentLauncher) PersistWorkflow(ctx context.Context, prepared softwareapp.PreparedSoftwareWorkflow) (string, error) {
	plan, ok := prepared.(*preparedSoftwareWorkflow)
	if !ok || plan == nil {
		return "", fmt.Errorf("persist software workflow: plan %T was not prepared by this launcher", prepared)
	}
	created, err := l.orchestrations.Create(ctx, plan.workflow)
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

// launchLabel is the kind's operator-facing name ("Docker CE", "NFS"), so Workflow intents and Step
// names spell the product as it is written rather than as its slug; without one it is the slug.
func launchLabel(launch softwareapp.SoftwareLaunch) string {
	if launch.Label != "" {
		return launch.Label
	}
	return string(launch.Kind)
}

// prepare is the shared install/uninstall composition: the lock check, playbook, and target
// resolution read outside the store, and the Workflow is returned unwritten under a preassigned id.
// recordKind selects the internal record step behaviour (mark installed vs absent); mode selects
// whether the ansible Task declares the failure marking, which only an install does.
func (l softwareDeploymentLauncher) prepare(
	ctx context.Context,
	launch softwareapp.SoftwareLaunch,
	mapping softwarePlaybook,
	recordKind, mode, summary string,
) (*preparedSoftwareWorkflow, error) {
	if l.orchestrations == nil {
		return nil, fmt.Errorf("durable software deployment is unavailable")
	}
	module, ok := l.modules.Module(moduledomain.KindSoftware, string(launch.Kind))
	if !ok {
		return nil, fmt.Errorf("%w: %s", softwaredomain.ErrUnknownKind, launch.Kind)
	}
	operationID := uuid.NewString()
	prepared, err := l.operations.PrepareAnsibleStep(ctx, workflowapp.CreateExecutionInput{
		Kind: string(mapping.workflowKind), PlaybookName: mapping.playbook,
		Intent: summary, TargetServerIDs: launch.ServerIDs,
		TrustedVars: launch.TrustedVars, RequestedBy: launch.RequestedBy,
	}, operationID, false)
	if err != nil {
		return nil, err
	}

	softwareParams := map[string]any{
		"softwareKind": string(launch.Kind),
		"serverIds":    launch.ServerIDs,
	}
	steps := []workflowdomain.Task{
		{
			ID: softwareWaitStepID, Kind: "wait-for-ssh", Name: "Verify SSH readiness",
			Job:      jobPrepareHosts,
			Executor: workflowdomain.RunnerKindInternal, Targets: prepared.Targets,
		},
		{
			ID: softwareInstallStepID, Kind: "ansible-playbook",
			Name:       summary,
			Job:        softwareConfigureJob(launch.Kind),
			Executor:   workflowdomain.RunnerKindHostAutomation,
			DependsOn:  []string{softwareWaitStepID},
			Targets:    prepared.Targets,
			Parameters: map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars},
			Completion: installCompletions(mode, launch.Kind, launch.ServerIDs),
		},
		{
			ID: softwareRecordStepID, Kind: recordKind, Name: "Record software assignment",
			Job:        jobRecordSoftware,
			Executor:   workflowdomain.RunnerKindInternal,
			DependsOn:  []string{softwareInstallStepID},
			Targets:    serverResourceRefs(launch.ServerIDs),
			Parameters: softwareParams,
		},
	}

	return &preparedSoftwareWorkflow{workflow: workflowapp.CreateWorkflowInput{
		ID: operationID, Kind: mapping.workflowKind,
		IntentSummary:  summary,
		IntentSnapshot: map[string]any{"softwareKind": string(launch.Kind), "mode": mode},
		Definition:     module.Manifest.Definition(), DefinitionVersion: 1,
		SiteID:          prepared.SiteID,
		TargetServerIDs: launch.ServerIDs,
		Steps:           steps, Jobs: declareJobs(steps, workflowdomain.RetryJob), RequestedBy: launch.RequestedBy,
	}}, nil
}

// softwareConfigureJob is the Job that runs a Software kind's playbook, "configure-<kind>", in a
// software Workflow and in a platform deploy that composes the kind.
func softwareConfigureJob(kind softwaredomain.Kind) string {
	return "configure-" + string(kind)
}

func serverResourceRefs(serverIDs []string) []workflowdomain.ResourceReference {
	refs := make([]workflowdomain.ResourceReference, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		refs = append(refs, workflowdomain.ResourceReference{Kind: "server", ID: serverID})
	}
	return refs
}

// deviceMetricsExporterAssignments implements the platform context's DeviceMetricsExporterAssignments
// port on the Software Assignment repository: a successful Slurm uninstall removed the exporter
// from the Platform's Servers, so their device-metrics-exporter records become absent (decision
// 068). The record keeps its last Workflow, like the sweep that marks a released Server's records.
type deviceMetricsExporterAssignments struct {
	assignments softwaredomain.AssignmentRepository
}

// MarkDeviceMetricsExporterRemoved skips a Server without a live record and reports every write
// failure, after trying the other Servers.
func (a deviceMetricsExporterAssignments) MarkDeviceMetricsExporterRemoved(ctx context.Context, serverIDs []string) error {
	var errs []error
	for _, serverID := range serverIDs {
		existing, err := a.assignments.FindByServerAndKind(ctx, serverID, softwaredomain.KindDeviceMetricsExporter)
		if errors.Is(err, softwaredomain.ErrAssignmentNotFound) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if existing.State == softwaredomain.StateAbsent {
			continue
		}
		if err := a.assignments.SetState(ctx, serverID, softwaredomain.KindDeviceMetricsExporter,
			softwaredomain.StateAbsent, existing.LastWorkflowID, nil); err != nil && !errors.Is(err, softwaredomain.ErrAssignmentNotFound) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// kubernetesMembershipChecker reports whether a Server is a Kubernetes Platform member, which gates
// the container-runtime software kinds. It reads the Server's membership axis for the platform id
// and confirms that platform's type is kubernetes; a Slurm member or a non-member is allowed.
type kubernetesMembershipChecker struct {
	servers   serverdomain.ServerRepository
	platforms platformdomain.PlatformRepository
}

func (c kubernetesMembershipChecker) IsKubernetesMember(ctx context.Context, serverID string) (bool, error) {
	server, err := c.servers.FindByID(ctx, serverID)
	if err != nil {
		return false, err
	}
	if server.Membership == nil || server.Membership.PlatformID == "" {
		return false, nil
	}
	platform, err := c.platforms.FindByID(ctx, server.Membership.PlatformID)
	if err != nil {
		// A membership pointing at a platform swallow can no longer read is not a positive
		// Kubernetes-member signal; treat it as not a member rather than blocking the install.
		return false, nil
	}
	return platform.Type == platformdomain.PlatformTypeKubernetes, nil
}

// installCompletions declares the failure marking of an install's Assignments; the record Task owns
// the success path, and an uninstall failure leaves its Assignments uninstalling for a retry.
func installCompletions(mode string, kind softwaredomain.Kind, serverIDs []string) []workflowdomain.Completion {
	if mode != "install" {
		return nil
	}
	return []workflowdomain.Completion{softwareFailure(kind, serverIDs)}
}
