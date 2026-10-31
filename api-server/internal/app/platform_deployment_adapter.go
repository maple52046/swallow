package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"

	"github.com/google/uuid"

	platformapp "github.com/maple52046/swallow/internal/core/platform/application"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	softwareapp "github.com/maple52046/swallow/internal/core/software/application"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
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
	// deletePlatformRecordVar carries the uninstall's deleteRecord choice on the Operation's
	// trusted vars, where the kept-servers completion (platform-uninstall) reads it back. The
	// uninstall playbooks ignore it.
	deletePlatformRecordVar = "swallow_delete_platform_record"
)

// deleteRecordParameter is the complete-uninstall Task parameter carrying the same choice on the
// release path, whose Workflow has no ansible step and so no extra vars.
const deleteRecordParameter = "deleteRecord"

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
	operations     *workflowapp.ExecutionService
	orchestrations *workflowapp.WorkflowService
	deployments    *provisioningapp.DeployServersUseCase
	servers        serverdomain.ServerRepository
	// bootMedia inserts ensure-boot-media into the ensure-os Job before each provision-os Task of
	// a Server with Boot Media enabled (decision 047). Its zero value adds nothing.
	bootMedia bootMediaPlanner
	// software plans and records the Managed Software a Slurm deployment composes for its
	// container runtime and Omnistat features (decision 063). Nil refuses such a deployment
	// rather than skipping the feature.
	software *softwareapp.SoftwareService
	// ntp adds configure-ntp after each provision-os Task of the ensure-os Job and the clock-check
	// variables to the install step (decision 065). Its zero value adds no Task.
	ntp ntpStepPlanner
}

// preparedPlatformDeployment is the plan the platform launchers' prepare steps return
// (platformdomain.PreparedWorkflow): the complete durable Workflow, with the Software Assignments
// a Slurm deployment composes, or, for a deployment without durable orchestration, the legacy
// single-step Operation. Only the launcher that prepared it persists it.
type preparedPlatformDeployment struct {
	workflow workflowapp.CreateWorkflowInput
	composed composedPlan
	// legacy is set instead of workflow when durable orchestration is unavailable.
	legacy *workflowapp.CreateExecutionInput
}

// OperationID is the preassigned Workflow id; the legacy path learns its id only when persisted.
func (p *preparedPlatformDeployment) OperationID() string { return p.workflow.ID }

// PrepareDeployment plans the durable Operation that deploys a Platform (see
// platformdomain.DeploymentLauncher): it performs the provider reads, the lock check, and the Task
// planning, and writes no durable record. It dispatches on the platform type: k0s (Kubernetes)
// and Slurm share the ensure-os Job (OS provisioning plus the SSH readiness gate) and differ only
// in the configure Job's playbook, workflow kind, and post-install validation.
func (l platformDeploymentLauncher) PrepareDeployment(ctx context.Context, launch platformdomain.DeploymentLaunch) (platformdomain.PreparedWorkflow, error) {
	if launch.Platform != nil && launch.Platform.Type == platformdomain.PlatformTypeSlurm {
		return l.prepareSlurm(ctx, launch)
	}
	return l.prepareKubernetes(ctx, launch)
}

// PersistDeployment writes a plan from PrepareDeployment: the Workflow (with its sealed secrets
// and first event), the Software Assignments a Slurm deployment composes, and the initial
// deployment projection of every Server it provisions. The caller runs it inside the store
// transaction that also writes the Platform and its claims, so every failure here is returned
// rather than logged: any one of them rolls the whole deployment back.
func (l platformDeploymentLauncher) PersistDeployment(ctx context.Context, prepared platformdomain.PreparedWorkflow) (string, error) {
	plan, ok := prepared.(*preparedPlatformDeployment)
	if !ok || plan == nil {
		return "", fmt.Errorf("persist platform deployment: plan %T was not prepared by this launcher", prepared)
	}
	if plan.legacy != nil {
		item, err := l.operations.Create(ctx, *plan.legacy)
		if err != nil {
			return "", err
		}
		return item.ID, nil
	}
	created, err := l.orchestrations.Create(ctx, plan.workflow)
	if err != nil {
		return "", err
	}
	if len(plan.composed.installs) > 0 {
		if err := l.recordComposedInstalls(ctx, plan.composed, created.ID); err != nil {
			return "", fmt.Errorf("record composed software assignments: %w", err)
		}
	}
	if err := persistInitialDeployments(ctx, l.servers, created.ID, plan.workflow.Steps); err != nil {
		return "", err
	}
	return created.ID, nil
}

// prepareKubernetes plans the k0s deployment: the ensure-os Job, the install and health-validation
// Tasks, and the optional GPU Operator Job, under the Workflow id the deploy use case preassigned.
// Without durable orchestration it plans the legacy single-step Operation instead, which supports
// neither OS provisioning nor the GPU Operator.
func (l platformDeploymentLauncher) prepareKubernetes(ctx context.Context, launch platformdomain.DeploymentLaunch) (*preparedPlatformDeployment, error) {
	provisionFirst := launch.MachinePreparation.Mode == platformdomain.MachinePreparationProvisionOS
	launch.TrustedVars = l.ntp.withCheckVars(launch.TrustedVars, launch.TimeSyncCheck)
	if l.orchestrations == nil {
		if provisionFirst {
			return nil, fmt.Errorf("durable Platform provisioning is unavailable")
		}
		if launch.GPUOperatorVars != nil {
			return nil, fmt.Errorf("durable GPU Operator installation is unavailable")
		}
		return &preparedPlatformDeployment{legacy: &workflowapp.CreateExecutionInput{
			Kind: deployKubernetesKind, Intent: "Deploy k0s platform " + launch.Platform.Name,
			TargetServerIDs: launch.TargetServerIDs, PlatformID: launch.Platform.ID,
			TrustedVars: launch.TrustedVars, SecretVars: launch.SecretVars,
			RequestedBy: launch.RequestedBy,
		}}, nil
	}

	operationID := launch.OperationID
	if operationID == "" {
		operationID = uuid.NewString()
	}
	prepared, err := l.operations.PrepareAnsibleStep(ctx, workflowapp.CreateExecutionInput{
		Kind: deployKubernetesKind, Intent: "Deploy k0s platform " + launch.Platform.Name,
		TargetServerIDs: launch.TargetServerIDs, PlatformID: launch.Platform.ID,
		TrustedVars: launch.TrustedVars, SecretVars: launch.SecretVars,
		RequestedBy: launch.RequestedBy,
	}, operationID, provisionFirst)
	if err != nil {
		return nil, err
	}
	var gpuPrepared *workflowapp.PreparedAnsibleStep
	if launch.GPUOperatorVars != nil {
		gpuPrepared, err = prepareGPUOperatorStep(ctx, l.operations, operationID, launch.Platform.ID,
			"Install AMD GPU Operator on "+launch.Platform.Name, launch.TargetServerIDs,
			launch.GPUOperatorVars, launch.RequestedBy, provisionFirst)
		if err != nil {
			return nil, err
		}
	}

	ensure, err := l.buildEnsureOS(ctx, launch, operationID, provisionFirst, prepared)
	if err != nil {
		return nil, err
	}
	steps := ensure.steps
	steps = append(steps, workflowdomain.Task{
		ID: installPlatformStepID, Kind: "ansible-playbook", Name: "Install k0s Platform",
		Job:      jobConfigureK0s,
		Executor: workflowdomain.RunnerKindAnsible, DependsOn: ensure.installDeps,
		Targets:    prepared.Targets,
		Parameters: map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars},
		Completion: []workflowdomain.Completion{{Type: completionKubernetesCredential}},
	})
	steps = append(steps, workflowdomain.Task{
		ID: "validate-platform", Kind: "validate-platform-health", Name: "Validate Platform health",
		Job:      jobConfigureK0s,
		Executor: workflowdomain.RunnerKindInternal, DependsOn: []string{installPlatformStepID},
		Targets: prepared.Targets,
	})
	if gpuPrepared != nil {
		// The operator is installed only on a validated cluster, so its Job depends on the
		// health gate and the deployment succeeds only when the operator does (decision 062).
		steps = append(steps, gpuOperatorTask(gpuPrepared, launch.Platform.ID, operationID,
			[]string{"validate-platform"}, "Install AMD GPU Operator"))
	}

	workflow, err := l.deploymentWorkflowInput(ctx, operationID, workflowdomain.WorkflowKindDeployKubernetes,
		"Deploy k0s Platform "+launch.Platform.Name, launch, prepared, steps, ensure)
	if err != nil {
		return nil, err
	}
	return &preparedPlatformDeployment{workflow: workflow}, nil
}

// prepareSlurm plans the durable Slurm deployment Operation. It reuses the ensure-os Job to
// bring targets to a booted, SSH-reachable OS (with the operator's Slurm image in provision_os
// mode) and then runs the hardcoded deploy-slurm playbook. Unlike k0s it adds no internal
// validate-platform-health Task: the deployed reader integration only exists once the
// install step's result is recorded, so cluster validation lives in the playbook itself and
// membership is filled in by the background sync. Durable orchestration is required; there is
// no legacy single-step Slurm path. With the container runtime or Omnistat feature (decision 063)
// the Workflow also carries the composed software Jobs and the configure-omnistat Job after
// install-platform, under the Workflow id the deploy use case preassigned for the Omnistat
// record's fence. The composed Software Assignments are written by PersistDeployment.
func (l platformDeploymentLauncher) prepareSlurm(ctx context.Context, launch platformdomain.DeploymentLaunch) (*preparedPlatformDeployment, error) {
	if l.orchestrations == nil {
		return nil, fmt.Errorf("durable Slurm deployment is unavailable")
	}
	provisionFirst := launch.MachinePreparation.Mode == platformdomain.MachinePreparationProvisionOS
	launch.TrustedVars = l.ntp.withCheckVars(launch.TrustedVars, launch.TimeSyncCheck)

	operationID := launch.OperationID
	if operationID == "" {
		operationID = uuid.NewString()
	}
	prepared, err := l.operations.PrepareAnsibleStep(ctx, workflowapp.CreateExecutionInput{
		Kind: configureSlurmKind, PlaybookName: deploySlurmPlaybook,
		Intent:          "Deploy Slurm platform " + launch.Platform.Name,
		TargetServerIDs: launch.TargetServerIDs, PlatformID: launch.Platform.ID,
		TrustedVars: launch.TrustedVars, SecretVars: launch.SecretVars,
		RequestedBy: launch.RequestedBy,
	}, operationID, provisionFirst)
	if err != nil {
		return nil, err
	}

	ensure, err := l.buildEnsureOS(ctx, launch, operationID, provisionFirst, prepared)
	if err != nil {
		return nil, err
	}
	steps := ensure.steps
	steps = append(steps, workflowdomain.Task{
		ID: installPlatformStepID, Kind: "ansible-playbook", Name: "Install Slurm Platform",
		Job:      jobConfigureSlurm,
		Executor: workflowdomain.RunnerKindAnsible, DependsOn: ensure.installDeps,
		Targets:    prepared.Targets,
		Parameters: map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars},
		Completion: []workflowdomain.Completion{{Type: completionSlurmCredential}},
	})
	composed, err := l.planSlurmComposition(ctx, launch, operationID, provisionFirst)
	if err != nil {
		return nil, err
	}
	steps = append(steps, composed.steps...)

	workflow, err := l.deploymentWorkflowInput(ctx, operationID, workflowdomain.WorkflowKindConfigureSlurm,
		"Deploy Slurm Platform "+launch.Platform.Name, launch, prepared, steps, ensure)
	if err != nil {
		return nil, err
	}
	return &preparedPlatformDeployment{workflow: workflow, composed: composed}, nil
}

// ensureOSPlan is the ensure-os Job's Tasks plus what the caller needs to finish the
// Workflow: the install step's dependency ids, which Task ids carry per-step provisioning
// secrets, and the frozen provisioning snapshot recorded in the operation intent.
type ensureOSPlan struct {
	steps         []workflowdomain.Task
	installDeps   []string
	secretStepIDs []string
	frozen        *provisioningapp.DeployServersInput
	userData      string
}

// buildEnsureOS constructs the ensure-os Job shared by every platform deployment. In
// provision_os mode it provisions only targets that still need an OS (ADR 017 convergence):
// a target already `deployed` in the same batch is reused as-is and only gets the SSH
// readiness gate, so one deploy can mix ready and deployed servers. In existing_os mode it
// adds a single readiness gate over the prepared targets. prepared is the install step's
// resolved target set, used for the existing_os gate.
//
// Time (decision 065) is part of an OS being ready, so each Server this Job provisions is followed
// by its own configure-ntp-<serverId>, exactly as in a Deploy OS Workflow, and the install step
// depends on it; reused and existing_os Servers are not reconfigured. The clock check belongs to
// each platform's install playbook, not to this Job.
func (l platformDeploymentLauncher) buildEnsureOS(
	ctx context.Context,
	launch platformdomain.DeploymentLaunch,
	operationID string,
	provisionFirst bool,
	prepared *workflowapp.PreparedAnsibleStep,
) (ensureOSPlan, error) {
	plan, err := l.buildOSReadiness(ctx, launch, provisionFirst, prepared.Targets)
	if err != nil {
		return ensureOSPlan{}, err
	}
	var provisioned []string
	for _, step := range plan.steps {
		if step.Kind == "provision-os" {
			provisioned = append(provisioned, firstTargetServer(step))
		}
	}
	ntpTasks, err := l.ntp.configureTasks(ctx, operationID, launch.RequestedBy, jobEnsureOS, provisioned)
	if err != nil {
		return ensureOSPlan{}, err
	}
	for _, task := range ntpTasks {
		plan.steps = append(plan.steps, task)
		plan.installDeps = append(plan.installDeps, task.ID)
	}
	return plan, nil
}

// buildOSReadiness builds the part of the ensure-os Job that brings targets to a booted,
// SSH-reachable OS: provision-os per target that needs one plus a wait-for-ssh gate over the
// rest (provision_os), or a single wait-for-ssh gate over preparedTargets (existing_os).
func (l platformDeploymentLauncher) buildOSReadiness(
	ctx context.Context,
	launch platformdomain.DeploymentLaunch,
	provisionFirst bool,
	preparedTargets []workflowdomain.ResourceReference,
) (ensureOSPlan, error) {
	plan := ensureOSPlan{
		steps:         make([]workflowdomain.Task, 0, len(launch.TargetServerIDs)+3),
		secretStepIDs: make([]string, 0, len(launch.TargetServerIDs)),
	}
	if !provisionFirst {
		plan.steps = append(plan.steps, workflowdomain.Task{
			ID: "wait-for-ssh", Kind: "wait-for-ssh", Name: "Verify existing OS SSH readiness",
			Job:      jobEnsureOS,
			Executor: workflowdomain.RunnerKindInternal, Targets: preparedTargets,
		})
		plan.installDeps = []string{"wait-for-ssh"}
		return plan, nil
	}

	if l.deployments == nil {
		return ensureOSPlan{}, fmt.Errorf("durable Platform provisioning is unavailable")
	}
	dependencies := make([]string, 0, len(launch.TargetServerIDs))
	readyIDs := make([]string, 0, len(launch.TargetServerIDs))
	deployedTargets := make([]workflowdomain.ResourceReference, 0, len(launch.TargetServerIDs))
	for _, serverID := range launch.TargetServerIDs {
		server, err := l.servers.FindByID(ctx, serverID)
		if err != nil {
			return ensureOSPlan{}, err
		}
		if server.Provisioning != nil && server.Provisioning.State == "deployed" {
			deployedTargets = append(deployedTargets, workflowdomain.ResourceReference{Kind: "server", ID: serverID})
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
			plan.steps = append(plan.steps, workflowdomain.Task{
				ID: stepID, Kind: "provision-os", Name: "Provision and verify operating system on " + serverID,
				Job:        jobEnsureOS,
				Executor:   workflowdomain.RunnerKindProvisioner,
				Targets:    []workflowdomain.ResourceReference{{Kind: "server", ID: serverID}},
				Parameters: map[string]any{"request": structToMap(targetRequest)},
				OnCancel:   returnCanceledDeployment(),
				Completion: []workflowdomain.Completion{{Type: completionServerDeployment}},
			})
			dependencies = append(dependencies, stepID)
			plan.secretStepIDs = append(plan.secretStepIDs, stepID)
		}
	}
	if len(deployedTargets) > 0 {
		plan.steps = append(plan.steps, workflowdomain.Task{
			ID: "wait-for-ssh", Kind: "wait-for-ssh", Name: "Verify existing OS SSH readiness",
			Job:      jobEnsureOS,
			Executor: workflowdomain.RunnerKindInternal, Targets: deployedTargets,
		})
		dependencies = append(dependencies, "wait-for-ssh")
	}
	plan.installDeps = dependencies
	return plan, nil
}

// deploymentWorkflowInput builds the deployment Workflow shared by k0s and Slurm without writing
// it. It carries the install step's platform secrets and, in provision_os mode with replace
// cloud-init, the resolved user data for sealing at persist time, and records the
// machine-preparation and frozen provisioning snapshots in the operation intent so a retry
// replays the same inputs. Boot Media's ensure Tasks are planned here, from the Servers' stored
// settings, so persisting needs no further read.
func (l platformDeploymentLauncher) deploymentWorkflowInput(
	ctx context.Context,
	operationID string,
	kind workflowdomain.WorkflowKind,
	summary string,
	launch platformdomain.DeploymentLaunch,
	prepared *workflowapp.PreparedAnsibleStep,
	steps []workflowdomain.Task,
	ensure ensureOSPlan,
) (workflowapp.CreateWorkflowInput, error) {
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
	steps, err := l.bootMedia.withEnsureTasks(ctx, steps)
	if err != nil {
		return workflowapp.CreateWorkflowInput{}, err
	}
	return workflowapp.CreateWorkflowInput{
		ID: operationID, Kind: kind,
		IntentSummary:  summary,
		IntentSnapshot: intentSnapshot,
		Definition:     "platform-deployment", DefinitionVersion: 1,
		SiteID: prepared.SiteID, PlatformID: launch.Platform.ID,
		TargetServerIDs: launch.TargetServerIDs,
		TargetResources: []workflowdomain.ResourceReference{{Kind: "platform", ID: launch.Platform.ID}},
		Steps:           steps, Jobs: declareJobs(steps, workflowdomain.RetryJob),
		RequestedBy: launch.RequestedBy, RequestCorrelation: launch.RequestCorrelation,
		SecretStepIDs: ensure.secretStepIDs, SecretValues: sharedSecrets,
		StepSecretValues: stepSecrets,
	}, nil
}

// platformOperationCanceler implements platformdomain.PlatformOperationCanceler by finding
// the platform's active durable Operations and canceling each one, which releases their
// resource leases so deleting the platform frees its member servers.
type platformOperationCanceler struct {
	orchestrations *workflowapp.WorkflowService
}

// activeOperationsPageSize bounds one platform's active Operation listing. A platform never
// runs anywhere near this many concurrent durable Operations, so a single page is complete.
const activeOperationsPageSize = 100

func (c platformOperationCanceler) CancelActiveForPlatform(ctx context.Context, platformID string) error {
	if c.orchestrations == nil {
		return nil
	}
	items, _, err := c.orchestrations.List(ctx, workflowapp.ListOperationsInput{
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
			if errors.Is(err, workflowdomain.ErrWorkflowControlConflict) {
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

// platformNodeSubnetResolver implements platformdomain.NodeSubnetResolver from the provisioner's
// live NIC view. A provisioned Server's subnet is the one its network assignment names; any other
// Server's is the IPv4 subnet linked to its boot interface. A Server whose network cannot be read
// is left out, so the caller falls back rather than failing the deploy on an inspection error.
type platformNodeSubnetResolver struct {
	networks *provisioningapp.NetworkConfigurationService
}

func (r platformNodeSubnetResolver) NodeSubnets(
	ctx context.Context,
	serverIDs []string,
	preparation platformdomain.MachinePreparation,
) (map[string]netip.Prefix, error) {
	assigned := map[string]platformdomain.MachineNetworkAssignment{}
	if preparation.Mode == platformdomain.MachinePreparationProvisionOS {
		for _, assignment := range preparation.Assignments {
			assigned[assignment.ServerID] = assignment
		}
	}
	subnets := make(map[string]netip.Prefix, len(serverIDs))
	for _, id := range serverIDs {
		target, err := r.networks.Get(ctx, id)
		if err != nil || target == nil || target.Network == nil {
			continue
		}
		if subnet, ok := nodeSubnet(target.Network.Interfaces, assigned[id]); ok {
			subnets[id] = subnet
		}
	}
	return subnets, nil
}

// nodeSubnet picks the IPv4 subnet of the assigned interface and subnet when there is an
// assignment, otherwise of the boot interface's first IPv4 link.
func nodeSubnet(interfaces []provisioningdomain.NetworkInterface, assignment platformdomain.MachineNetworkAssignment) (netip.Prefix, bool) {
	parse := func(cidr string) (netip.Prefix, bool) {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr))
		if err != nil || !prefix.Addr().Is4() {
			return netip.Prefix{}, false
		}
		return prefix.Masked(), true
	}
	for _, nic := range interfaces {
		if assignment.InterfaceID != "" {
			if nic.ID != assignment.InterfaceID {
				continue
			}
			for _, subnet := range nic.AvailableSubnets {
				if subnet.ID == assignment.SubnetID {
					return parse(subnet.CIDR)
				}
			}
			for _, link := range nic.Links {
				if link.SubnetID == assignment.SubnetID {
					return parse(link.CIDR)
				}
			}
			return netip.Prefix{}, false
		}
		if !nic.Boot {
			continue
		}
		for _, link := range nic.Links {
			if prefix, ok := parse(link.CIDR); ok {
				return prefix, true
			}
		}
	}
	return netip.Prefix{}, false
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
// step performs the Platform projection cleanup the platform-uninstall completion would otherwise do.
const completeUninstallStepID = "complete-uninstall"

func (l platformDeploymentLauncher) LaunchUninstall(
	ctx context.Context,
	launch platformdomain.UninstallLaunch,
) (string, error) {
	// Uninstall reuses one flow for both platform types and differs only in the workflow kind,
	// the hardcoded playbook, and operator-facing labels.
	uninstallKind := uninstallKubernetesKind
	uninstallPlaybook := uninstallKubernetesPlaybook
	workflowKind := workflowdomain.WorkflowKindUninstallKubernetes
	stepName := "Uninstall k0s Platform"
	intent := "Uninstall k0s platform " + launch.Platform.Name
	summary := "Uninstall k0s Platform " + launch.Platform.Name
	if launch.Platform.Type == platformdomain.PlatformTypeSlurm {
		uninstallKind = uninstallSlurmKind
		uninstallPlaybook = uninstallSlurmPlaybook
		workflowKind = workflowdomain.WorkflowKindUninstallSlurm
		stepName = "Uninstall Slurm Platform"
		intent = "Uninstall Slurm platform " + launch.Platform.Name
		summary = "Uninstall Slurm Platform " + launch.Platform.Name
	}

	trustedVars := map[string]any{
		restoreExportersVar:     launch.RestoreExporters,
		platformNameVar:         launch.Platform.Name,
		deletePlatformRecordVar: launch.DeleteRecord,
	}
	if l.orchestrations == nil {
		// Server release is a multi-step durable operation; without Temporal we can only
		// run the legacy single-step uninstall, so reject a release request rather than
		// silently dropping it. Superseding running work needs the durable leases too.
		if launch.ReleaseServers {
			return "", fmt.Errorf("durable Platform uninstall is unavailable; releasing servers requires it")
		}
		if launch.CancelActiveWorkflows {
			return "", fmt.Errorf("durable Platform uninstall is unavailable; canceling active Workflows requires it")
		}
		item, err := l.operations.Create(ctx, workflowapp.CreateExecutionInput{
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
	prepared, err := l.operations.PrepareAnsibleStep(ctx, workflowapp.CreateExecutionInput{
		Kind: uninstallKind, Intent: intent,
		TargetServerIDs: launch.TargetServerIDs, PlatformID: launch.Platform.ID,
		PlaybookName: uninstallPlaybook, TrustedVars: trustedVars,
		RequestedBy: launch.RequestedBy,
	}, operationID, false)
	if err != nil {
		return "", err
	}

	steps := uninstallSteps(launch, prepared, operationID, stepName)

	var superseded []string
	if launch.CancelActiveWorkflows {
		if superseded, err = supersedeBlockingWorkflows(ctx, l.orchestrations, launch.TargetServerIDs); err != nil {
			return "", err
		}
	}
	created, err := l.orchestrations.Create(ctx, workflowapp.CreateWorkflowInput{
		ID: operationID, Kind: workflowKind,
		IntentSummary:  summary,
		IntentSnapshot: map[string]any{"platformName": launch.Platform.Name, "releaseServers": launch.ReleaseServers, "deleteRecord": launch.DeleteRecord},
		Definition:     "platform-uninstall", DefinitionVersion: 1,
		SiteID: prepared.SiteID, PlatformID: launch.Platform.ID,
		TargetServerIDs: launch.TargetServerIDs,
		TargetResources: []workflowdomain.ResourceReference{{Kind: "platform", ID: launch.Platform.ID}},
		Steps:           steps, Jobs: declareJobs(steps, workflowdomain.RetryJob),
		RetryOfOperationID: launch.RetryOfOperationID, RequestedBy: launch.RequestedBy,
		SupersededOperationIDs: superseded,
	})
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

// uninstallSteps composes the durable uninstall Tasks.
//
// When the servers are kept, it is the single ansible uninstall step (whose platform-uninstall
// completion clears projections and restores exporters). When the servers are released, it skips that
// step entirely - releasing wipes the OS, so removing the platform software first is redundant
// and can fail on a half-gone node - and instead releases every member in parallel, then runs an
// internal complete-uninstall finalize step that depends on all releases and performs the
// projection cleanup the omitted ansible hook would have done. This release shortcut is only for
// a whole-platform uninstall; a future scale-in keeps the per-node uninstall.
func uninstallSteps(
	launch platformdomain.UninstallLaunch,
	prepared *workflowapp.PreparedAnsibleStep,
	operationID, stepName string,
) []workflowdomain.Task {
	if !launch.ReleaseServers {
		return []workflowdomain.Task{{
			ID: uninstallPlatformStepID, Kind: "ansible-playbook", Name: stepName,
			Job:      jobUninstallPlatform,
			Executor: workflowdomain.RunnerKindAnsible, Targets: prepared.Targets,
			Parameters: map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars},
			Completion: []workflowdomain.Completion{{Type: completionPlatformUninstall}},
		}}
	}

	steps := make([]workflowdomain.Task, 0, len(launch.TargetServerIDs)+1)
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
		steps = append(steps, workflowdomain.Task{
			ID: stepID, Kind: "release-os", Name: "Release " + serverID,
			Job:        jobReleaseServers,
			Executor:   workflowdomain.RunnerKindProvisioner,
			Targets:    []workflowdomain.ResourceReference{{Kind: "server", ID: serverID}},
			Parameters: map[string]any{"request": structToMap(releaseInput)},
			Completion: []workflowdomain.Completion{{Type: completionServerDeploymentCleared}},
		})
		releaseDeps = append(releaseDeps, stepID)
	}
	// Internal finalize step: Platform completion is otherwise the ansible step's
	// platform-uninstall completion, and this path omits that step, so this clears the Platform's membership,
	// owned credential Integration, and sync projection once every release succeeds, and removes
	// the record when the operator asked for it. Exporter restoration stays off (a released host
	// is wiped).
	steps = append(steps, workflowdomain.Task{
		ID: completeUninstallStepID, Kind: "complete-uninstall", Name: "Finalize platform uninstall",
		Job:      jobFinalizeUninstall,
		Executor: workflowdomain.RunnerKindInternal, DependsOn: releaseDeps,
		Targets:    []workflowdomain.ResourceReference{{Kind: "platform", ID: launch.Platform.ID}},
		Parameters: map[string]any{deleteRecordParameter: launch.DeleteRecord},
	})
	return steps
}

// platformDeploymentObserver translates successful platform operations into projections.
type platformDeploymentObserver struct {
	credentials *platformapp.DeploymentCredentialService
	platforms   *platformapp.PlatformService
	operations  *workflowapp.ExecutionService
	servers     serverdomain.ServerRepository
}

func (o platformDeploymentObserver) OperationSucceeded(
	ctx context.Context,
	operation *workflowdomain.ExecutionOperation,
	result workflowdomain.RunnerResult,
) {
	if operation.PlatformID == "" {
		return
	}
	switch operation.Kind {
	case workflowdomain.WorkflowKindDeployKubernetes:
		o.completeDeployment(ctx, operation, result)
	case workflowdomain.WorkflowKindConfigureSlurm:
		o.completeSlurmDeployment(ctx, operation, result)
	case workflowdomain.WorkflowKindUninstallKubernetes, workflowdomain.WorkflowKindUninstallSlurm:
		o.completeUninstall(ctx, operation)
	}
}

func (o platformDeploymentObserver) completeDeployment(
	ctx context.Context,
	operation *workflowdomain.ExecutionOperation,
	result workflowdomain.RunnerResult,
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
// operation has already succeeded; the install Task's credential completion is the path that
// fails the Task when no credential is produced.
func (o platformDeploymentObserver) completeSlurmDeployment(
	ctx context.Context,
	operation *workflowdomain.ExecutionOperation,
	result workflowdomain.RunnerResult,
) {
	credential := slurmCredentialFromResult(result)
	if credential == nil {
		// Not an error: a Slurm cluster works without slurmrestd; membership simply cannot be
		// read until the image includes slurm-smd-slurmrestd. See the slurm-credential completion.
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
	operation *workflowdomain.ExecutionOperation,
) {
	deleteRecord, _ := operation.ExtraVars[deletePlatformRecordVar].(bool)
	if err := o.platforms.CompleteUninstall(ctx, operation.PlatformID, deleteRecord); err != nil {
		slog.Error("complete platform uninstall projections",
			"operationId", operation.ID, "platformId", operation.PlatformID, "error", err)
		return
	}
	o.restoreExporters(ctx, operation)
}

func (o platformDeploymentObserver) restoreExporters(ctx context.Context, operation *workflowdomain.ExecutionOperation) {
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

	if _, err := o.operations.Create(ctx, workflowapp.CreateExecutionInput{
		Kind: string(workflowdomain.WorkflowKindInstallExporters),
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
func platformCredentialFromResult(result workflowdomain.RunnerResult) *platformapp.DeploymentCredential {
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
func slurmCredentialFromResult(result workflowdomain.RunnerResult) *platformapp.SlurmDeploymentCredential {
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

// completeOmnistat records the Grafana URL a successful configure-omnistat run wrote to its result
// file as the Platform's `Omnistat Grafana` Monitoring Link. A missing URL fails the Task, because
// the playbook writes it only after Prometheus reported every exporter up; the Omnistat State
// itself is projected by the omnistat-state completion.
func (o platformDeploymentObserver) completeOmnistat(
	ctx context.Context,
	platformID string,
	result workflowdomain.RunnerResult,
) error {
	grafanaURL := ""
	if result.Data != nil {
		grafanaURL = stringField(result.Data, omnistatResultGrafanaURL)
	}
	if grafanaURL == "" {
		return fmt.Errorf("omnistat installation returned no Grafana URL")
	}
	return o.platforms.RecordOmnistatGrafana(ctx, platformID, grafanaURL)
}

// completeDeviceMetricsExporter records what a successful Slurm run of deploy-device-metrics-exporter
// wrote to its result file: the Grafana URL as the Platform's `Device Metrics Exporter Grafana`
// Monitoring Link, and the GPU identity the exporters showed. A missing URL fails the Task, because
// the playbook writes it only after Prometheus reported every exporter up; a missing or unknown
// identity is recorded as collapsed, never as distinct. The identity is fenced on the record id the
// deploy use case put in the run's variables (the same id the State is fenced on), falling back to
// this Workflow; the State itself is projected by the device-metrics-exporter-state completion.
func (o platformDeploymentObserver) completeDeviceMetricsExporter(
	ctx context.Context,
	platformID, workflowID string,
	extraVars map[string]any,
	result workflowdomain.RunnerResult,
) error {
	grafanaURL, identity := "", ""
	if result.Data != nil {
		grafanaURL = stringField(result.Data, deviceMetricsExporterResultGrafanaURL)
		identity = stringField(result.Data, deviceMetricsExporterResultGPUIdentity)
	}
	if grafanaURL == "" {
		return fmt.Errorf("device metrics exporter installation returned no Grafana URL")
	}
	recordID, _ := extraVars[platformapp.VarDeviceMetricsExporterRecordID].(string)
	if recordID == "" {
		recordID = workflowID
	}
	return o.platforms.RecordDeviceMetricsExporterResult(ctx, platformID, recordID, grafanaURL,
		platformdomain.ParseGPUIdentity(identity))
}

func stringField(data map[string]any, key string) string {
	if value, ok := data[key].(string); ok {
		return value
	}
	return ""
}

// finishUninstall finishes a platform uninstall whose playbook succeeded: it removes the
// Platform's swallow-owned state (and the record when the request asked for it) and restores the
// exporters the uninstall removed. Unlike completeUninstall, which serves the schema-v2 path, it
// returns the failure so the Task fails and can be retried.
func (o platformDeploymentObserver) finishUninstall(ctx context.Context, operation *workflowdomain.ExecutionOperation) error {
	// An uninstall accepted before deleteRecord existed has no such var and keeps the record.
	deleteRecord, _ := operation.ExtraVars[deletePlatformRecordVar].(bool)
	if err := o.platforms.CompleteUninstall(ctx, operation.PlatformID, deleteRecord); err != nil {
		return err
	}
	o.restoreExporters(ctx, operation)
	return nil
}
