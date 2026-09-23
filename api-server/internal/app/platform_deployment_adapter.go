package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	platformapp "github.com/maple52046/swallow/internal/platform/application"
	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/pagination"
)

const (
	deployKubernetesKind        = "deploy-kubernetes"
	uninstallKubernetesKind     = "uninstall-kubernetes"
	uninstallKubernetesPlaybook = "uninstall-kubernetes"
	// configureSlurmKind is the workflow kind for a Slurm platform deployment, and
	// deploySlurmPlaybook is the manifest playbook it runs. The playbook name is hardcoded
	// on the launcher (like uninstall-kubernetes) so a Slurm deploy needs no site
	// playbookMappings entry.
	configureSlurmKind  = "configure-slurm"
	deploySlurmPlaybook = "deploy-slurm"
	// uninstallSlurmKind / uninstallSlurmPlaybook mirror the k0s uninstall: a hardcoded
	// playbook name (no site playbookMappings entry) that removes the Slurm configuration and
	// daemons while leaving the host OS and image-supplied packages intact.
	uninstallSlurmKind     = "uninstall-slurm"
	uninstallSlurmPlaybook = "uninstall-slurm"
	restoreExportersVar    = "swallow_restore_ansible_exporters"
	platformNameVar        = "swallow_platform_name"
)

// Deploy Jobs (ADR 017): the k0s deployment is composed of two reusable, convergent Jobs.
// `ensure-os` brings every target Server to a booted, SSH-reachable OS (provisioner Tasks
// plus the readiness gate); `configure-k0s` installs and validates the Platform on top. The
// Workflow runs each Job as a Temporal child workflow in cross-Job dependency order.
const (
	jobEnsureOS       = "ensure-os"
	jobConfigureK0s   = "configure-k0s"
	jobConfigureSlurm = "configure-slurm"
)

// Uninstall Jobs group the uninstall Workflow's Tasks the same way the deploy Jobs group a
// deployment (ADR 017), so the API `steps[].job` and the dashboard present uninstall with the
// same Job grouping (and per-Task events) as deploy. Keeping the servers is a single
// `uninstall-platform` Job that removes the platform software; releasing the servers is a
// per-server `release-servers` Job followed by a `finalize-uninstall` Job that cleans the
// Platform projections once every release succeeds. As with deploy, cross-Job `DependsOn`
// (the finalize Task depending on all releases) drives the Job order at execution time.
//
// Assigning a Job to every uninstall Task is required, not cosmetic: the Temporal orchestrator
// switches to the child-Job execution path as soon as any Task carries a Job, so a mix of
// jobbed and job-less Tasks would place the job-less ones in an unnamed bucket.
const (
	jobUninstallPlatform = "uninstall-platform"
	jobReleaseServers    = "release-servers"
	jobFinalizeUninstall = "finalize-uninstall"
)

// platformDeploymentLauncher composes optional MAAS preparation and k0s automation into
// one durable Operation while retaining the legacy launcher for unavailable Temporal.
type platformDeploymentLauncher struct {
	operations     *operationapp.ExecutionService
	orchestrations *operationapp.WorkflowService
	deployments    *provisioningapp.DeployServersUseCase
	servers        serverdomain.ServerRepository
}

// Launch builds the durable Operation that deploys a Platform. It dispatches on the platform
// type: k0s (Kubernetes) and Slurm share the ensure-os Job (OS provisioning plus the SSH
// readiness gate) and differ only in the configure Job's playbook, workflow kind, and
// post-install validation.
func (l platformDeploymentLauncher) Launch(ctx context.Context, launch platformdomain.DeploymentLaunch) (string, error) {
	if launch.Platform != nil && launch.Platform.Type == platformdomain.PlatformTypeSlurm {
		return l.launchSlurm(ctx, launch)
	}
	return l.launchKubernetes(ctx, launch)
}

func (l platformDeploymentLauncher) launchKubernetes(ctx context.Context, launch platformdomain.DeploymentLaunch) (string, error) {
	provisionFirst := launch.MachinePreparation.Mode == platformdomain.MachinePreparationProvisionOS
	if l.orchestrations == nil {
		if provisionFirst {
			return "", fmt.Errorf("durable Platform provisioning is unavailable")
		}
		item, err := l.operations.Create(ctx, operationapp.CreateExecutionInput{
			Kind: deployKubernetesKind, Intent: "Deploy k0s platform " + launch.Platform.Name,
			TargetServerIDs: launch.TargetServerIDs, PlatformID: launch.Platform.ID,
			TrustedVars: launch.TrustedVars, SecretVars: launch.SecretVars,
			RequestedBy: launch.RequestedBy,
		})
		if err != nil {
			return "", err
		}
		return item.ID, nil
	}

	operationID := uuid.NewString()
	prepared, err := l.operations.PrepareAnsibleStep(ctx, operationapp.CreateExecutionInput{
		Kind: deployKubernetesKind, Intent: "Deploy k0s platform " + launch.Platform.Name,
		TargetServerIDs: launch.TargetServerIDs, PlatformID: launch.Platform.ID,
		TrustedVars: launch.TrustedVars, SecretVars: launch.SecretVars,
		RequestedBy: launch.RequestedBy,
	}, operationID, provisionFirst)
	if err != nil {
		return "", err
	}

	ensure, err := l.buildEnsureOS(ctx, launch, provisionFirst, prepared.Targets)
	if err != nil {
		return "", err
	}
	steps := ensure.steps
	steps = append(steps, operationdomain.Task{
		ID: installPlatformStepID, Kind: "ansible-playbook", Name: "Install k0s Platform",
		Job:      jobConfigureK0s,
		Executor: operationdomain.RunnerKindAnsible, DependsOn: ensure.installDeps,
		Targets:    prepared.Targets,
		Parameters: map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars},
	})
	steps = append(steps, operationdomain.Task{
		ID: "validate-platform", Kind: "validate-platform-health", Name: "Validate Platform health",
		Job:      jobConfigureK0s,
		Executor: operationdomain.RunnerKindInternal, DependsOn: []string{installPlatformStepID},
		Targets: prepared.Targets,
	})

	created, err := l.createDeploymentWorkflow(ctx, operationID, operationdomain.WorkflowKindDeployKubernetes,
		"Deploy k0s Platform "+launch.Platform.Name, launch, prepared, steps, ensure)
	if err != nil {
		return "", err
	}
	materializeInitialDeployments(ctx, l.servers, created.ID, steps)
	return created.ID, nil
}

// launchSlurm builds the durable Slurm deployment Operation. It reuses the ensure-os Job to
// bring targets to a booted, SSH-reachable OS (with the operator's Slurm image in provision_os
// mode) and then runs the hardcoded deploy-slurm playbook. Unlike k0s it adds no internal
// validate-platform-health Task: the deployed reader integration only exists once the
// install step's result is recorded, so cluster validation lives in the playbook itself and
// membership is filled in by the background sync. Durable orchestration is required; there is
// no legacy single-step Slurm path.
func (l platformDeploymentLauncher) launchSlurm(ctx context.Context, launch platformdomain.DeploymentLaunch) (string, error) {
	if l.orchestrations == nil {
		return "", fmt.Errorf("durable Slurm deployment is unavailable")
	}
	provisionFirst := launch.MachinePreparation.Mode == platformdomain.MachinePreparationProvisionOS

	operationID := uuid.NewString()
	prepared, err := l.operations.PrepareAnsibleStep(ctx, operationapp.CreateExecutionInput{
		Kind: configureSlurmKind, PlaybookName: deploySlurmPlaybook,
		Intent:          "Deploy Slurm platform " + launch.Platform.Name,
		TargetServerIDs: launch.TargetServerIDs, PlatformID: launch.Platform.ID,
		TrustedVars: launch.TrustedVars, SecretVars: launch.SecretVars,
		RequestedBy: launch.RequestedBy,
	}, operationID, provisionFirst)
	if err != nil {
		return "", err
	}

	ensure, err := l.buildEnsureOS(ctx, launch, provisionFirst, prepared.Targets)
	if err != nil {
		return "", err
	}
	steps := ensure.steps
	steps = append(steps, operationdomain.Task{
		ID: installPlatformStepID, Kind: "ansible-playbook", Name: "Install Slurm Platform",
		Job:      jobConfigureSlurm,
		Executor: operationdomain.RunnerKindAnsible, DependsOn: ensure.installDeps,
		Targets:    prepared.Targets,
		Parameters: map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars},
	})

	created, err := l.createDeploymentWorkflow(ctx, operationID, operationdomain.WorkflowKindConfigureSlurm,
		"Deploy Slurm Platform "+launch.Platform.Name, launch, prepared, steps, ensure)
	if err != nil {
		return "", err
	}
	materializeInitialDeployments(ctx, l.servers, created.ID, steps)
	return created.ID, nil
}

// ensureOSPlan is the ensure-os Job's Tasks plus what the caller needs to finish the
// Workflow: the install step's dependency ids, which Task ids carry per-step provisioning
// secrets, and the frozen provisioning snapshot recorded in the operation intent.
type ensureOSPlan struct {
	steps         []operationdomain.Task
	installDeps   []string
	secretStepIDs []string
	frozen        *provisioningapp.DeployServersInput
	userData      string
}

// buildEnsureOS constructs the ensure-os Job shared by every platform deployment. In
// provision_os mode it provisions only targets that still need an OS (ADR 017 convergence):
// a target already `deployed` in the same batch is reused as-is and only gets the SSH
// readiness gate, so one deploy can mix ready and deployed servers. In existing_os mode it
// adds a single readiness gate over the prepared targets. preparedTargets is the ansible
// step's resolved target set, used for the existing_os gate.
func (l platformDeploymentLauncher) buildEnsureOS(
	ctx context.Context,
	launch platformdomain.DeploymentLaunch,
	provisionFirst bool,
	preparedTargets []operationdomain.ResourceReference,
) (ensureOSPlan, error) {
	plan := ensureOSPlan{
		steps:         make([]operationdomain.Task, 0, len(launch.TargetServerIDs)+2),
		secretStepIDs: make([]string, 0, len(launch.TargetServerIDs)),
	}
	if !provisionFirst {
		plan.steps = append(plan.steps, operationdomain.Task{
			ID: "wait-for-ssh", Kind: "wait-for-ssh", Name: "Verify existing OS SSH readiness",
			Job:      jobEnsureOS,
			Executor: operationdomain.RunnerKindInternal, Targets: preparedTargets,
		})
		plan.installDeps = []string{"wait-for-ssh"}
		return plan, nil
	}

	if l.deployments == nil {
		return ensureOSPlan{}, fmt.Errorf("durable Platform provisioning is unavailable")
	}
	dependencies := make([]string, 0, len(launch.TargetServerIDs))
	readyIDs := make([]string, 0, len(launch.TargetServerIDs))
	deployedTargets := make([]operationdomain.ResourceReference, 0, len(launch.TargetServerIDs))
	for _, serverID := range launch.TargetServerIDs {
		server, err := l.servers.FindByID(ctx, serverID)
		if err != nil {
			return ensureOSPlan{}, err
		}
		if server.Provisioning != nil && server.Provisioning.State == "deployed" {
			deployedTargets = append(deployedTargets, operationdomain.ResourceReference{Kind: "server", ID: serverID})
			continue
		}
		readyIDs = append(readyIDs, serverID)
	}
	if len(readyIDs) > 0 {
		batch, resolvedUserData, resolveErr := l.deployments.ResolveOperationInput(
			ctx,
			platformProvisioningInput(launch.MachinePreparation, readyIDs),
		)
		if resolveErr != nil {
			return ensureOSPlan{}, resolveErr
		}
		plan.frozen = &batch
		plan.userData = resolvedUserData
		for _, serverID := range readyIDs {
			stepID := "provision-" + serverID
			targetRequest := batch
			targetRequest.ServerIDs = []string{serverID}
			if batch.Network != nil {
				network := *batch.Network
				network.Assignments = nil
				for _, assignment := range batch.Network.Assignments {
					if assignment.ServerID == serverID {
						network.Assignments = []provisioningapp.DeploymentNetworkAssignmentInput{assignment}
					}
				}
				targetRequest.Network = &network
			}
			plan.steps = append(plan.steps, operationdomain.Task{
				ID: stepID, Kind: "provision-os", Name: "Provision and verify operating system on " + serverID,
				Job:        jobEnsureOS,
				Executor:   operationdomain.RunnerKindProvisioner,
				Targets:    []operationdomain.ResourceReference{{Kind: "server", ID: serverID}},
				Parameters: map[string]any{"request": structToMap(targetRequest)},
			})
			dependencies = append(dependencies, stepID)
			plan.secretStepIDs = append(plan.secretStepIDs, stepID)
		}
	}
	if len(deployedTargets) > 0 {
		plan.steps = append(plan.steps, operationdomain.Task{
			ID: "wait-for-ssh", Kind: "wait-for-ssh", Name: "Verify existing OS SSH readiness",
			Job:      jobEnsureOS,
			Executor: operationdomain.RunnerKindInternal, Targets: deployedTargets,
		})
		dependencies = append(dependencies, "wait-for-ssh")
	}
	plan.installDeps = dependencies
	return plan, nil
}

// createDeploymentWorkflow persists the deployment Workflow shared by k0s and Slurm. It seals
// the install step's platform secrets and, in provision_os mode with replace cloud-init, the
// resolved user data, and records the machine-preparation and frozen provisioning snapshots in
// the operation intent so a retry replays the same inputs.
func (l platformDeploymentLauncher) createDeploymentWorkflow(
	ctx context.Context,
	operationID string,
	kind operationdomain.WorkflowKind,
	summary string,
	launch platformdomain.DeploymentLaunch,
	prepared *operationapp.PreparedAnsibleStep,
	steps []operationdomain.Task,
	ensure ensureOSPlan,
) (*operationapp.WorkflowItem, error) {
	preparation := launch.MachinePreparation
	preparation.UserData = ""
	stepSecrets := map[string]map[string]any{}
	if len(launch.SecretVars) > 0 {
		stepSecrets[installPlatformStepID] = launch.SecretVars
	}
	sharedSecrets := map[string]any{}
	if ensure.frozen != nil && ensure.frozen.UserData.Mode == "replace" && ensure.userData != "" {
		sharedSecrets["userData"] = ensure.userData
	}
	intentSnapshot := map[string]any{
		"machinePreparation": structToMap(preparation), "extraVars": prepared.ExtraVars,
	}
	if ensure.frozen != nil {
		intentSnapshot["resolvedProvisioning"] = structToMap(*ensure.frozen)
	}
	return l.orchestrations.Create(ctx, operationapp.CreateWorkflowInput{
		ID: operationID, Kind: kind,
		IntentSummary:  summary,
		IntentSnapshot: intentSnapshot,
		Definition:     "platform-deployment", DefinitionVersion: 1,
		SiteID: prepared.SiteID, PlatformID: launch.Platform.ID,
		TargetServerIDs: launch.TargetServerIDs,
		TargetResources: []operationdomain.ResourceReference{{Kind: "platform", ID: launch.Platform.ID}},
		Steps:           steps, RequestedBy: launch.RequestedBy, RequestCorrelation: launch.RequestCorrelation,
		SecretStepIDs: ensure.secretStepIDs, SecretValues: sharedSecrets,
		StepSecretValues: stepSecrets,
	})
}

// platformOperationCanceler implements platformdomain.PlatformOperationCanceler by finding
// the platform's active durable Operations and canceling each one, which releases their
// resource leases so deleting the platform frees its member servers.
type platformOperationCanceler struct {
	orchestrations *operationapp.WorkflowService
}

// activeOperationsPageSize bounds one platform's active Operation listing. A platform never
// runs anywhere near this many concurrent durable Operations, so a single page is complete.
const activeOperationsPageSize = 100

func (c platformOperationCanceler) CancelActiveForPlatform(ctx context.Context, platformID string) error {
	if c.orchestrations == nil {
		return nil
	}
	items, _, err := c.orchestrations.List(ctx, operationapp.ListOperationsInput{
		PlatformID: platformID,
		Active:     true,
		Page:       pagination.Page{Page: 1, PageSize: activeOperationsPageSize},
	})
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := c.orchestrations.Cancel(ctx, item.ID); err != nil {
			// A run that reached a terminal state between listing and canceling is already
			// in the desired end state; tolerate that control conflict and continue so one
			// finished Operation cannot block deleting the platform.
			if errors.Is(err, operationdomain.ErrWorkflowControlConflict) {
				continue
			}
			return fmt.Errorf("cancel platform operation %s: %w", item.ID, err)
		}
	}
	return nil
}

// platformMachinePreparationValidator translates the Platform-owned intent to the active
// provisioning contract and performs its full side-effect-free preflight.
type platformMachinePreparationValidator struct {
	deployments *provisioningapp.DeployServersUseCase
}

func (v platformMachinePreparationValidator) Validate(ctx context.Context, _ string, serverIDs []string, preparation platformdomain.MachinePreparation) error {
	return v.deployments.Validate(ctx, platformProvisioningInput(preparation, serverIDs))
}

func platformProvisioningInput(preparation platformdomain.MachinePreparation, serverIDs []string) provisioningapp.DeployServersInput {
	input := provisioningapp.DeployServersInput{
		ServerIDs: append([]string(nil), serverIDs...), TemplateID: preparation.TemplateID,
		Settings: provisioningapp.DeploymentSettingsInput{ImageID: optionalStringPointer(preparation.ImageID), Ephemeral: preparation.Ephemeral},
		UserData: provisioningapp.DeploymentUserDataInput{Mode: preparation.UserDataMode, Value: preparation.UserData},
	}
	if preparation.NetworkMode != "" {
		input.Network = &provisioningapp.DeploymentNetworkInput{
			Mode: preparation.NetworkMode, SubnetID: preparation.SubnetID,
			DefaultGateway: preparation.DefaultGateway,
			Assignments:    make([]provisioningapp.DeploymentNetworkAssignmentInput, len(preparation.Assignments)),
		}
		for index, assignment := range preparation.Assignments {
			input.Network.Assignments[index] = provisioningapp.DeploymentNetworkAssignmentInput{
				ServerID: assignment.ServerID, InterfaceID: assignment.InterfaceID,
				SubnetID: assignment.SubnetID, IPAddress: assignment.IPAddress,
			}
		}
	}
	return input
}

func optionalStringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// installPlatformStepID is the ansible Task that installs the platform (k0s or Slurm) on top
// of the ensure-os Job. Its result carries the deployment credential, and it is the step that
// carries platform secrets, so both launchers and the completion observer refer to it by this id.
const installPlatformStepID = "install-platform"

// uninstallPlatformStepID is the durable ansible uninstall step used when the servers are kept
// (platform software removed, hosts preserved).
const uninstallPlatformStepID = "uninstall-platform"

// completeUninstallStepID is the internal finalize step used when the uninstall also releases
// the servers: releasing wipes the OS, so the platform-software uninstall is skipped and this
// step performs the Platform projection cleanup that AnsibleStepSucceeded would otherwise do.
const completeUninstallStepID = "complete-uninstall"

func (l platformDeploymentLauncher) LaunchUninstall(
	ctx context.Context,
	launch platformdomain.UninstallLaunch,
) (string, error) {
	// Uninstall reuses one flow for both platform types and differs only in the workflow kind,
	// the hardcoded playbook, and operator-facing labels.
	uninstallKind := uninstallKubernetesKind
	uninstallPlaybook := uninstallKubernetesPlaybook
	workflowKind := operationdomain.WorkflowKindUninstallKubernetes
	stepName := "Uninstall k0s Platform"
	intent := "Uninstall k0s platform " + launch.Platform.Name
	summary := "Uninstall k0s Platform " + launch.Platform.Name
	if launch.Platform.Type == platformdomain.PlatformTypeSlurm {
		uninstallKind = uninstallSlurmKind
		uninstallPlaybook = uninstallSlurmPlaybook
		workflowKind = operationdomain.WorkflowKindUninstallSlurm
		stepName = "Uninstall Slurm Platform"
		intent = "Uninstall Slurm platform " + launch.Platform.Name
		summary = "Uninstall Slurm Platform " + launch.Platform.Name
	}

	trustedVars := map[string]any{
		restoreExportersVar: launch.RestoreExporters,
		platformNameVar:     launch.Platform.Name,
	}
	if l.orchestrations == nil {
		// Server release is a multi-step durable operation; without Temporal we can only
		// run the legacy single-step uninstall, so reject a release request rather than
		// silently dropping it.
		if launch.ReleaseServers {
			return "", fmt.Errorf("durable Platform uninstall is unavailable; releasing servers requires it")
		}
		item, err := l.operations.Create(ctx, operationapp.CreateExecutionInput{
			Kind: uninstallKind, Intent: intent,
			TargetServerIDs: launch.TargetServerIDs, PlatformID: launch.Platform.ID,
			PlaybookName:       uninstallPlaybook,
			TrustedVars:        trustedVars,
			RetryOfOperationID: launch.RetryOfOperationID,
			RequestedBy:        launch.RequestedBy,
		})
		if err != nil {
			return "", err
		}
		return item.ID, nil
	}

	operationID := uuid.NewString()
	prepared, err := l.operations.PrepareAnsibleStep(ctx, operationapp.CreateExecutionInput{
		Kind: uninstallKind, Intent: intent,
		TargetServerIDs: launch.TargetServerIDs, PlatformID: launch.Platform.ID,
		PlaybookName: uninstallPlaybook, TrustedVars: trustedVars,
		RequestedBy: launch.RequestedBy,
	}, operationID, false)
	if err != nil {
		return "", err
	}

	steps := uninstallSteps(launch, prepared, operationID, stepName)

	created, err := l.orchestrations.Create(ctx, operationapp.CreateWorkflowInput{
		ID: operationID, Kind: workflowKind,
		IntentSummary:  summary,
		IntentSnapshot: map[string]any{"platformName": launch.Platform.Name, "releaseServers": launch.ReleaseServers},
		Definition:     "platform-uninstall", DefinitionVersion: 1,
		SiteID: prepared.SiteID, PlatformID: launch.Platform.ID,
		TargetServerIDs: launch.TargetServerIDs,
		TargetResources: []operationdomain.ResourceReference{{Kind: "platform", ID: launch.Platform.ID}},
		Steps:           steps, RetryOfOperationID: launch.RetryOfOperationID, RequestedBy: launch.RequestedBy,
	})
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

// uninstallSteps composes the durable uninstall Tasks.
//
// When the servers are kept, it is the single ansible uninstall step (whose AnsibleStepSucceeded
// hook clears projections and restores exporters). When the servers are released, it skips that
// step entirely - releasing wipes the OS, so removing the platform software first is redundant
// and can fail on a half-gone node - and instead releases every member in parallel, then runs an
// internal complete-uninstall finalize step that depends on all releases and performs the
// projection cleanup the omitted ansible hook would have done. This release shortcut is only for
// a whole-platform uninstall; a future scale-in keeps the per-node uninstall.
func uninstallSteps(
	launch platformdomain.UninstallLaunch,
	prepared *operationapp.PreparedAnsibleStep,
	operationID, stepName string,
) []operationdomain.Task {
	if !launch.ReleaseServers {
		return []operationdomain.Task{{
			ID: uninstallPlatformStepID, Kind: "ansible-playbook", Name: stepName,
			Job:      jobUninstallPlatform,
			Executor: operationdomain.RunnerKindAnsible, Targets: prepared.Targets,
			Parameters: map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars},
		}}
	}

	steps := make([]operationdomain.Task, 0, len(launch.TargetServerIDs)+1)
	releaseDeps := make([]string, 0, len(launch.TargetServerIDs))
	for _, serverID := range launch.TargetServerIDs {
		releaseInput := provisioningapp.ReleaseServerInput{
			ServerID:        serverID,
			Erase:           launch.ReleaseOptions.Erase,
			SecureErase:     launch.ReleaseOptions.SecureErase,
			QuickErase:      launch.ReleaseOptions.QuickErase,
			UnbindStaticIPs: launch.ReleaseOptions.UnbindStaticIPs,
			Comment:         "Release while uninstalling platform " + launch.Platform.Name,
			RequestID:       operationID,
		}
		stepID := "release-" + serverID
		steps = append(steps, operationdomain.Task{
			ID: stepID, Kind: "release-os", Name: "Release " + serverID,
			Job:        jobReleaseServers,
			Executor:   operationdomain.RunnerKindProvisioner,
			Targets:    []operationdomain.ResourceReference{{Kind: "server", ID: serverID}},
			Parameters: map[string]any{"request": structToMap(releaseInput)},
		})
		releaseDeps = append(releaseDeps, stepID)
	}
	// Internal finalize step: v3 Platform completion is otherwise tied to the ansible step
	// (AnsibleStepSucceeded), which this path omits, so this clears the Platform's membership,
	// owned credential Integration, and sync projection once every release succeeds. Exporter
	// restoration stays off (a released host is wiped).
	steps = append(steps, operationdomain.Task{
		ID: completeUninstallStepID, Kind: "complete-uninstall", Name: "Finalize platform uninstall",
		Job:      jobFinalizeUninstall,
		Executor: operationdomain.RunnerKindInternal, DependsOn: releaseDeps,
		Targets: []operationdomain.ResourceReference{{Kind: "platform", ID: launch.Platform.ID}},
	})
	return steps
}

// platformDeploymentObserver translates successful platform operations into projections.
type platformDeploymentObserver struct {
	credentials *platformapp.DeploymentCredentialService
	platforms   *platformapp.PlatformService
	operations  *operationapp.ExecutionService
	servers     serverdomain.ServerRepository
}

func (o platformDeploymentObserver) OperationSucceeded(
	ctx context.Context,
	operation *operationdomain.ExecutionOperation,
	result operationdomain.RunnerResult,
) {
	if operation.PlatformID == "" {
		return
	}
	switch operation.Kind {
	case operationdomain.WorkflowKindDeployKubernetes:
		o.completeDeployment(ctx, operation, result)
	case operationdomain.WorkflowKindConfigureSlurm:
		o.completeSlurmDeployment(ctx, operation, result)
	case operationdomain.WorkflowKindUninstallKubernetes, operationdomain.WorkflowKindUninstallSlurm:
		o.completeUninstall(ctx, operation)
	}
}

func (o platformDeploymentObserver) completeDeployment(
	ctx context.Context,
	operation *operationdomain.ExecutionOperation,
	result operationdomain.RunnerResult,
) {
	credential := platformCredentialFromResult(result)
	if credential == nil {
		slog.Error("platform deployment succeeded but returned no credential",
			"operationId", operation.ID, "platformId", operation.PlatformID)
		return
	}
	if err := o.credentials.Record(ctx, operation.PlatformID, *credential); err != nil {
		slog.Error("record platform deployment credential",
			"operationId", operation.ID, "platformId", operation.PlatformID, "error", err)
	}
}

// completeSlurmDeployment records the slurmrestd reader credential a successful Slurm
// deployment wrote to its result file, turning the platform into one swallow can read
// membership from. A missing credential is logged rather than fatal here because the
// operation has already succeeded; the per-step AnsibleStepSucceeded hook is the path that
// fails the run when no credential is produced.
func (o platformDeploymentObserver) completeSlurmDeployment(
	ctx context.Context,
	operation *operationdomain.ExecutionOperation,
	result operationdomain.RunnerResult,
) {
	credential := slurmCredentialFromResult(result)
	if credential == nil {
		// Not an error: a Slurm cluster works without slurmrestd; membership simply cannot be
		// read until the image includes slurm-smd-slurmrestd. See AnsibleStepSucceeded.
		slog.Warn("slurm deployment produced no reader credential; membership will not populate until slurmrestd is available",
			"operationId", operation.ID, "platformId", operation.PlatformID)
		return
	}
	if err := o.credentials.RecordSlurm(ctx, operation.PlatformID, *credential); err != nil {
		slog.Error("record slurm deployment credential",
			"operationId", operation.ID, "platformId", operation.PlatformID, "error", err)
	}
}

func (o platformDeploymentObserver) completeUninstall(
	ctx context.Context,
	operation *operationdomain.ExecutionOperation,
) {
	if err := o.platforms.CompleteUninstall(ctx, operation.PlatformID); err != nil {
		slog.Error("complete platform uninstall projections",
			"operationId", operation.ID, "platformId", operation.PlatformID, "error", err)
		return
	}
	o.restoreExporters(ctx, operation)
}

func (o platformDeploymentObserver) restoreExporters(ctx context.Context, operation *operationdomain.ExecutionOperation) {
	restore, _ := operation.ExtraVars[restoreExportersVar].(bool)
	if !restore {
		return
	}

	targetIDs := make([]string, 0, len(operation.TargetServerIDs))
	for _, serverID := range operation.TargetServerIDs {
		server, err := o.servers.FindByID(ctx, serverID)
		if err != nil {
			slog.Warn("skip exporter restoration target",
				"operationId", operation.ID, "serverId", serverID, "error", err)
			continue
		}
		if server.Absent || server.Provisioning == nil ||
			server.Provisioning.State != "deployed" || server.Provisioning.Locked {
			continue
		}
		targetIDs = append(targetIDs, serverID)
	}
	if len(targetIDs) == 0 {
		return
	}

	if _, err := o.operations.Create(ctx, operationapp.CreateExecutionInput{
		Kind: string(operationdomain.WorkflowKindInstallExporters),
		Intent: "Restore host exporters after uninstalling platform " +
			stringField(operation.ExtraVars, platformNameVar),
		TargetServerIDs: targetIDs,
		PlatformID:      operation.PlatformID,
		RequestedBy:     "system",
	}); err != nil {
		slog.Error("queue exporter restoration after platform uninstall",
			"operationId", operation.ID, "platformId", operation.PlatformID, "error", err)
	}
}

// platformCredentialFromResult reads the credential written by a deployment playbook.
func platformCredentialFromResult(result operationdomain.RunnerResult) *platformapp.DeploymentCredential {
	if result.Data == nil {
		return nil
	}
	endpoint := stringField(result.Data, "apiEndpoint")
	token := stringField(result.Data, "token")
	if endpoint == "" || token == "" {
		return nil
	}
	return &platformapp.DeploymentCredential{
		APIEndpoint: endpoint, Token: token,
		CACertificate: stringField(result.Data, "caCertificate"),
	}
}

// slurmCredentialFromResult reads the slurmrestd reader credential a Slurm deployment
// playbook wrote to its result file: the slurmrestd base URL, a Slurm JWT, and the endpoint
// version the controller exposes. Endpoint and token are required; apiVersion is optional and
// lets the reader fall back to its default when absent.
func slurmCredentialFromResult(result operationdomain.RunnerResult) *platformapp.SlurmDeploymentCredential {
	if result.Data == nil {
		return nil
	}
	endpoint := stringField(result.Data, "slurmrestdEndpoint")
	token := stringField(result.Data, "token")
	if endpoint == "" || token == "" {
		return nil
	}
	return &platformapp.SlurmDeploymentCredential{
		Endpoint: endpoint, Token: token,
		APIVersion: stringField(result.Data, "apiVersion"),
	}
}

func stringField(data map[string]any, key string) string {
	if value, ok := data[key].(string); ok {
		return value
	}
	return ""
}

// AnsibleStepSucceeded adapts a v3 queue result to the same Platform completion policy
// used by historical v2 executions.
func (o platformDeploymentObserver) AnsibleStepSucceeded(
	ctx context.Context,
	execution *operationdomain.AnsibleExecution,
	result operationdomain.RunnerResult,
) error {
	if execution.PlatformID == "" {
		return nil
	}
	operation := &operationdomain.ExecutionOperation{
		ID: execution.OperationID, Kind: execution.Kind, SiteID: execution.SiteID,
		PlatformID: execution.PlatformID, TargetServerIDs: execution.TargetServerIDs,
		ExtraVars: execution.ExtraVars,
	}
	switch execution.Kind {
	case operationdomain.WorkflowKindDeployKubernetes:
		credential := platformCredentialFromResult(result)
		if credential == nil {
			return fmt.Errorf("deployment returned no Kubernetes credential")
		}
		return o.credentials.Record(ctx, execution.PlatformID, *credential)
	case operationdomain.WorkflowKindConfigureSlurm:
		credential := slurmCredentialFromResult(result)
		if credential == nil {
			// A Slurm cluster (slurmctld + slurmd) is fully functional without slurmrestd, which
			// only powers membership reads. Its absence is not a deploy failure: the platform is
			// deployed with no reader integration, and membership stays empty until the image
			// includes slurm-smd-slurmrestd. Do not fail the operation over it.
			slog.Warn("slurm deployment produced no reader credential; membership will not populate until slurmrestd is available",
				"operationId", execution.OperationID, "platformId", execution.PlatformID)
			return nil
		}
		return o.credentials.RecordSlurm(ctx, execution.PlatformID, *credential)
	case operationdomain.WorkflowKindUninstallKubernetes, operationdomain.WorkflowKindUninstallSlurm:
		if err := o.platforms.CompleteUninstall(ctx, execution.PlatformID); err != nil {
			return err
		}
		o.restoreExporters(ctx, operation)
	}
	return nil
}
