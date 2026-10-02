package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	softwareapp "github.com/maple52046/swallow/internal/software/application"
	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// Software deployment Jobs (ADR 017 / decision 038). A standalone software install composes three
// convergent Jobs on one parent Workflow: prepare-hosts brings each already-deployed target to SSH
// readiness, configure-<kind> runs the one idempotent software playbook, and record-software writes
// the swallow-owned Software Assignment. This is the same Job-composition a future platform deploy
// would reuse to pull in a software component; it must never be a nested child Workflow.
const (
	jobPrepareHosts   = "prepare-hosts"
	jobRecordSoftware = "record-software"

	// softwareInstallStepID is the ansible Task that installs (or uninstalls) the software. It
	// carries the assignment identity so the software step observer can mark the assignment failed
	// when this Task ends in a terminal failure and the record step never runs.
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

// softwarePlaybook maps a software kind to its deploy/uninstall Workflow kind and hardcoded
// playbook name. Names are hardcoded on the launcher (like Slurm), so a software install needs no
// site playbookMappings entry.
type softwarePlaybook struct {
	workflowKind operationdomain.WorkflowKind
	playbook     string
}

var softwareInstallPlaybooks = map[softwaredomain.Kind]softwarePlaybook{
	softwaredomain.KindDockerCE: {operationdomain.WorkflowKindConfigureDockerCE, "deploy-docker-ce"},
	softwaredomain.KindPodman:   {operationdomain.WorkflowKindConfigurePodman, "deploy-podman"},
	softwaredomain.KindNFS:      {operationdomain.WorkflowKindConfigureNFS, "deploy-nfs"},
}

var softwareUninstallPlaybooks = map[softwaredomain.Kind]softwarePlaybook{
	softwaredomain.KindDockerCE: {operationdomain.WorkflowKindUninstallDockerCE, "uninstall-docker-ce"},
	softwaredomain.KindPodman:   {operationdomain.WorkflowKindUninstallPodman, "uninstall-podman"},
	softwaredomain.KindNFS:      {operationdomain.WorkflowKindUninstallNFS, "uninstall-nfs"},
}

// softwareDeploymentLauncher composes the durable software Workflow. It reuses the operation
// layer's PrepareAnsibleStep (which resolves the site, enforces the deployed/lock preconditions,
// and assembles trusted vars) and the WorkflowService, so software deployment shares the platform
// deployment machinery rather than owning a second engine.
type softwareDeploymentLauncher struct {
	operations     *operationapp.ExecutionService
	orchestrations *operationapp.WorkflowService
}

// LaunchInstall builds the install Workflow: prepare-hosts -> configure-<kind> -> record-software.
func (l softwareDeploymentLauncher) LaunchInstall(ctx context.Context, launch softwareapp.SoftwareLaunch) (string, error) {
	mapping, ok := softwareInstallPlaybooks[launch.Kind]
	if !ok {
		return "", fmt.Errorf("%w: %s", softwaredomain.ErrUnknownKind, launch.Kind)
	}
	return l.launch(ctx, launch, mapping, softwareStepKindRecord, "install", "Install "+softwareLabel(launch.Kind))
}

// LaunchUninstall builds the uninstall Workflow: prepare-hosts -> configure-<kind> (uninstall
// playbook) -> record-software (clear).
func (l softwareDeploymentLauncher) LaunchUninstall(ctx context.Context, launch softwareapp.SoftwareLaunch) (string, error) {
	mapping, ok := softwareUninstallPlaybooks[launch.Kind]
	if !ok {
		return "", fmt.Errorf("%w: %s", softwaredomain.ErrUnknownKind, launch.Kind)
	}
	return l.launch(ctx, launch, mapping, softwareStepKindClear, "uninstall", "Uninstall "+softwareLabel(launch.Kind))
}

// softwareLabel is the catalog's operator-facing name for a kind ("Docker CE", "NFS"), so Workflow
// intents and Step names spell the product as it is written rather than as its slug; an unknown kind
// falls back to the slug.
func softwareLabel(kind softwaredomain.Kind) string {
	if entry, ok := softwaredomain.LookupKind(kind); ok && entry.Label != "" {
		return entry.Label
	}
	return string(kind)
}

// launch is the shared install/uninstall composition. recordKind selects the internal record step
// behaviour (mark installed vs absent); mode is stamped on the ansible Task so the failure observer
// only marks assignments failed for an install.
func (l softwareDeploymentLauncher) launch(
	ctx context.Context,
	launch softwareapp.SoftwareLaunch,
	mapping softwarePlaybook,
	recordKind, mode, summary string,
) (string, error) {
	if l.orchestrations == nil {
		return "", fmt.Errorf("durable software deployment is unavailable")
	}
	operationID := uuid.NewString()
	prepared, err := l.operations.PrepareAnsibleStep(ctx, operationapp.CreateExecutionInput{
		Kind: string(mapping.workflowKind), PlaybookName: mapping.playbook,
		Intent: summary, TargetServerIDs: launch.ServerIDs,
		TrustedVars: launch.TrustedVars, RequestedBy: launch.RequestedBy,
	}, operationID, false)
	if err != nil {
		return "", err
	}

	softwareParams := map[string]any{
		"softwareKind": string(launch.Kind),
		"serverIds":    launch.ServerIDs,
	}
	steps := []operationdomain.Task{
		{
			ID: softwareWaitStepID, Kind: "wait-for-ssh", Name: "Verify SSH readiness",
			Job:      jobPrepareHosts,
			Executor: operationdomain.RunnerKindInternal, Targets: prepared.Targets,
		},
		{
			ID: softwareInstallStepID, Kind: "ansible-playbook",
			Name:      summary,
			Job:       "configure-" + string(launch.Kind),
			Executor:  operationdomain.RunnerKindAnsible,
			DependsOn: []string{softwareWaitStepID},
			Targets:   prepared.Targets,
			Parameters: map[string]any{
				"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars,
				"softwareKind": string(launch.Kind), "serverIds": launch.ServerIDs,
				"softwareMode": mode,
			},
		},
		{
			ID: softwareRecordStepID, Kind: recordKind, Name: "Record software assignment",
			Job:        jobRecordSoftware,
			Executor:   operationdomain.RunnerKindInternal,
			DependsOn:  []string{softwareInstallStepID},
			Targets:    serverResourceRefs(launch.ServerIDs),
			Parameters: softwareParams,
		},
	}

	created, err := l.orchestrations.Create(ctx, operationapp.CreateWorkflowInput{
		ID: operationID, Kind: mapping.workflowKind,
		IntentSummary:  summary,
		IntentSnapshot: map[string]any{"softwareKind": string(launch.Kind), "mode": mode},
		Definition:     "software-deployment", DefinitionVersion: 1,
		SiteID:          prepared.SiteID,
		TargetServerIDs: launch.ServerIDs,
		Steps:           steps, RequestedBy: launch.RequestedBy,
	})
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

func serverResourceRefs(serverIDs []string) []operationdomain.ResourceReference {
	refs := make([]operationdomain.ResourceReference, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		refs = append(refs, operationdomain.ResourceReference{Kind: "server", ID: serverID})
	}
	return refs
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
