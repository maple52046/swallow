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
	"github.com/maple52046/swallow/modules/platform/kubernetes/kubeapi"
	"github.com/maple52046/swallow/modules/platform/slurm/slurmapi"
	nodeexporter "github.com/maple52046/swallow/modules/software/node-exporter"
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
)

// deleteRecordParameter is the complete-uninstall Task parameter carrying the same choice on the
// release path, whose Workflow has no ansible step and so no extra vars.
const deleteRecordParameter = "deleteRecord"

// Deploy Jobs (decision 072): the k0s deployment is composed of two reusable, convergent Jobs.
// `ensure-os` brings every target Server to a booted, SSH-reachable OS (provisioner Tasks
// plus the readiness gate); `configure-k0s` installs and validates the Platform on top. Jobs are
// groupings in the Workflow's plan: the engine schedules their Tasks by their cross-Job
// dependencies, with no Temporal child workflow per Job.
const (
	jobEnsureOS       = "ensure-os"
	jobConfigureK0s   = "configure-k0s"
	jobConfigureSlurm = "configure-slurm"
)

// Uninstall Jobs group the uninstall Workflow's Tasks the same way the deploy Jobs group a
// deployment (decision 072), so the API `steps[].job` and the dashboard present uninstall with the
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

// platformDeploymentLauncher composes optional MAAS preparation and the platform automation into
// one durable Workflow.
type platformDeploymentLauncher struct {
	operations *workflowapp.ExecutionService
	// orchestrations is required: it accepts every deploy and uninstall Workflow the launcher plans.
	orchestrations *workflowapp.WorkflowService
	deployments    *provisioningapp.DeployServersUseCase
	servers        serverdomain.ServerRepository
	// bootMedia inserts ensure-boot-media into the ensure-os Job before each provision-os Task of
	// a Server with Boot Media enabled (decision 047). Its zero value adds nothing.
	bootMedia bootMediaPlanner
	// software plans and records the Managed Software a Slurm deployment composes: its container
	// runtime, the Platform's Grafana, and the Device Metrics Exporter (decisions 063 and 068). Nil
	// refuses such a deployment rather than skipping the feature.
	software *softwareapp.SoftwareService
	// ntp adds configure-ntp after each provision-os Task of the ensure-os Job and the clock-check
	// variables to the install step (decision 065). Its zero value adds no Task.
	ntp ntpStepPlanner
	// nodeExporter installs the Node Exporter on each Server the deployment provisions, after its
	// time Task, and on each Server an uninstall keeps. Its zero value adds no Task.
	nodeExporter nodeExporterPlanner
	// monitoring is how the configured Servers and the Site metrics store reach each other; a
	// deployment whose Platform Grafana or Omnistat needs an empty part of it is refused.
	monitoring platformMonitoringAccess
	// monitoringTokens issues the Platform's monitoring token for its Grafana when a deployment that
	// composes one is persisted. Required wherever deployments are persisted (buildPlatformOperations).
	monitoringTokens platformTokenIssuer
}

// platformTokenIssuer issues a Platform's monitoring token (monitoringapp.PlatformAccess.Issue):
// it stores only the token's hash, so the previous token stops working, and returns the token,
// which the caller seals as a Task secret and never keeps. Called inside the accepting store
// transaction, it must make no call outside the store.
type platformTokenIssuer interface {
	Issue(ctx context.Context, platformID string) (string, error)
}

// preparedPlatformDeployment is the plan the platform launchers' prepare steps return
// (platformdomain.PreparedWorkflow): the complete durable Workflow, with the Software Assignments
// a Slurm deployment composes and the Tasks of either type that receive the Platform's monitoring
// token. Only the launcher that prepared it persists it.
type preparedPlatformDeployment struct {
	workflow workflowapp.CreateWorkflowInput
	composed composedPlan
	// exporters is the Node Exporter install the ensure-os Job composes; its pending assignments
	// are written with the Workflow.
	exporters nodeExporterInstall
}

// OperationID is the preassigned Workflow id.
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
// deployment projection of every Server it provisions. A deployment that installs the Platform's
// Grafana, of either Platform type, first issues the Platform's monitoring token and seals it into
// that Grafana's install Task, so each accepted deploy rotates the token and a refused one leaves
// the current token working. The caller runs it inside the store transaction that also writes the
// Platform and its claims, so every failure here is returned rather than logged: any one of them
// rolls the whole deployment back.
func (l platformDeploymentLauncher) PersistDeployment(ctx context.Context, prepared platformdomain.PreparedWorkflow) (string, error) {
	plan, ok := prepared.(*preparedPlatformDeployment)
	if !ok || plan == nil {
		return "", fmt.Errorf("persist platform deployment: plan %T was not prepared by this launcher", prepared)
	}
	workflow := plan.workflow
	if steps := plan.composed.tokenSteps; len(steps) > 0 {
		if l.monitoringTokens == nil {
			return "", fmt.Errorf("persist platform deployment: the Platform's monitoring token cannot be issued in this process")
		}
		token, err := l.monitoringTokens.Issue(ctx, workflow.PlatformID)
		if err != nil {
			return "", fmt.Errorf("issue the Platform's monitoring token: %w", err)
		}
		workflow = withPlatformToken(workflow, steps, token)
	}
	created, err := l.orchestrations.Create(ctx, workflow)
	if err != nil {
		return "", err
	}
	if len(plan.composed.installs) > 0 {
		if err := l.recordComposedInstalls(ctx, plan.composed, created.ID); err != nil {
			return "", fmt.Errorf("record composed software assignments: %w", err)
		}
	}
	if install := plan.exporters.install; install != nil {
		if err := l.software.RecordComposedInstall(ctx, *install, created.ID); err != nil {
			return "", fmt.Errorf("record the Node Exporter's software assignments: %w", err)
		}
	}
	if err := persistInitialDeployments(ctx, l.servers, created.ID, plan.workflow.Steps); err != nil {
		return "", err
	}
	return created.ID, nil
}

// prepareKubernetes plans the k0s deployment: the ensure-os Job, the install and health-validation
// Tasks, the optional GPU Operator Job, and the Platform Grafana Job when the Platform has its own
// monitoring dashboards, under the Workflow id the deploy use case preassigned.
func (l platformDeploymentLauncher) prepareKubernetes(ctx context.Context, launch platformdomain.DeploymentLaunch) (*preparedPlatformDeployment, error) {
	provisionFirst := launch.MachinePreparation.Mode == platformdomain.MachinePreparationProvisionOS
	launch.TrustedVars = l.ntp.withCheckVars(launch.TrustedVars, launch.TimeSyncCheck)

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
		Executor: workflowdomain.RunnerKindHostAutomation, DependsOn: ensure.installDeps,
		Targets:    prepared.Targets,
		Parameters: map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars},
		Completion: []workflowdomain.Completion{{Type: kubeapi.CredentialCompletion}},
	})
	steps = append(steps, workflowdomain.Task{
		ID: "validate-platform", Kind: "validate-platform-health", Name: "Validate Platform health",
		Job:      jobConfigureK0s,
		Executor: workflowdomain.RunnerKindInternal, DependsOn: []string{installPlatformStepID},
		Targets: prepared.Targets,
	})
	last := []string{"validate-platform"}
	if gpuPrepared != nil {
		// The operator is installed only on a validated cluster, so its Job depends on the
		// health gate and the deployment succeeds only when the operator does (decision 062).
		steps = append(steps, gpuOperatorTask(gpuPrepared, launch.Platform.ID, operationID,
			last, "Install AMD GPU Operator"))
		last = []string{gpuOperatorStepID}
	}
	// The Platform Grafana comes last: it shows the GPU Operator's exporter when there is one.
	composed, err := l.planKubernetesGrafana(ctx, launch, operationID, provisionFirst, last)
	if err != nil {
		return nil, err
	}
	steps = append(steps, composed.steps...)

	workflow, err := l.deploymentWorkflowInput(ctx, operationID, workflowdomain.WorkflowKindDeployKubernetes,
		"Deploy k0s Platform "+launch.Platform.Name, launch, prepared, steps, ensure)
	if err != nil {
		return nil, err
	}
	return &preparedPlatformDeployment{workflow: workflow, composed: composed, exporters: ensure.exporters}, nil
}

// prepareSlurm plans the durable Slurm deployment Operation. It reuses the ensure-os Job to
// bring targets to a booted, SSH-reachable OS (with the user's Slurm image in provision_os
// mode) and then runs the hardcoded deploy-slurm playbook. Unlike k0s it adds no internal
// validate-platform-health Task: the deployed reader integration only exists once the
// install step's result is recorded, so cluster validation lives in the playbook itself and
// membership is filled in by the background sync. With the container runtime or Omnistat feature (decision 063)
// the Workflow also carries the composed software Jobs and the configure-omnistat Job after
// install-platform, under the Workflow id the deploy use case preassigned for the Omnistat
// record's fence. The composed Software Assignments are written by PersistDeployment.
func (l platformDeploymentLauncher) prepareSlurm(ctx context.Context, launch platformdomain.DeploymentLaunch) (*preparedPlatformDeployment, error) {
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
		Executor: workflowdomain.RunnerKindHostAutomation, DependsOn: ensure.installDeps,
		Targets:    prepared.Targets,
		Parameters: map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars},
		Completion: []workflowdomain.Completion{{Type: slurmapi.CredentialCompletion}},
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
	return &preparedPlatformDeployment{workflow: workflow, composed: composed, exporters: ensure.exporters}, nil
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
	// exporters is the Node Exporter install composed after the provisioned Servers' time Tasks;
	// its Tasks are already in steps.
	exporters nodeExporterInstall
}

// buildEnsureOS constructs the ensure-os Job shared by every platform deployment. In
// provision_os mode it provisions only targets that still need an OS (decision 072 convergence):
// a target already `deployed` in the same batch is reused as-is and only gets the SSH
// readiness gate, so one deploy can mix ready and deployed servers. In existing_os mode it
// adds a single readiness gate over the prepared targets. prepared is the install step's
// resolved target set, used for the existing_os gate.
//
// Time (decision 065) is part of an OS being ready, so each Server this Job provisions is followed
// by its own configure-ntp-<serverId>, exactly as in a Deploy OS Workflow, and the install step
// depends on it; reused and existing_os Servers are not reconfigured. The clock check belongs to
// each platform's install playbook, not to this Job. The same provisioned Servers then get the Node
// Exporter (nodeExporterPlanner), as in a Deploy OS Workflow.
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
	// The Node Exporter follows each provisioned Server's time Task in its own Job, which nothing
	// depends on, so the install step never waits for it.
	plan.exporters, err = l.nodeExporter.installTasks(ctx, operationID, launch.RequestedBy,
		softwareConfigureJob(nodeexporter.Kind), provisioned, afterTimeSync(provisioned, ntpTasks))
	if err != nil {
		return ensureOSPlan{}, err
	}
	plan.steps = append(plan.steps, plan.exporters.tasks...)
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
	// The Servers read here also name their provision-os Tasks.
	readyServers := make([]*serverdomain.Server, 0, len(launch.TargetServerIDs))
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
		readyServers = append(readyServers, server)
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
		for _, server := range readyServers {
			serverID := server.ID
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
				ID: stepID, Kind: "provision-os", Name: provisionOSTaskName(server),
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
// resource leases so deleting the platform frees its member servers. orchestrations is required.
type platformOperationCanceler struct {
	orchestrations *workflowapp.WorkflowService
}

// activeOperationsPageSize bounds one platform's active Operation listing. A platform never
// runs anywhere near this many concurrent durable Operations, so a single page is complete.
const activeOperationsPageSize = 100

func (c platformOperationCanceler) CancelActiveForPlatform(ctx context.Context, platformID string) error {
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
	// the hardcoded playbook, and user-facing labels.
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

	operationID := uuid.NewString()
	prepared, err := l.operations.PrepareAnsibleStep(ctx, workflowapp.CreateExecutionInput{
		Kind: uninstallKind, Intent: intent,
		TargetServerIDs: launch.TargetServerIDs, PlatformID: launch.Platform.ID,
		PlaybookName: uninstallPlaybook, RequestedBy: launch.RequestedBy,
	}, operationID, false)
	if err != nil {
		return "", err
	}

	steps := uninstallSteps(launch, prepared, operationID, stepName)
	exporters, err := l.uninstallNodeExporter(ctx, operationID, launch)
	if err != nil {
		return "", err
	}
	steps = append(steps, exporters.tasks...)

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
	l.nodeExporter.record(ctx, exporters, created.ID)
	return created.ID, nil
}

// uninstallNodeExporter makes sure each Server an uninstall keeps ends with the Node Exporter. The
// uninstall does not remove it; the plan installs it again after the uninstall-platform step, in a
// Job of its own, so a monitoring feature's uninstall cannot leave a Server without it, a Server
// that never had it gets it, and one that has it is left as it is (the install is idempotent). Its
// failure leaves the uninstall done. Releasing Servers wipes them, so a releasing uninstall
// composes nothing.
func (l platformDeploymentLauncher) uninstallNodeExporter(
	ctx context.Context, operationID string, launch platformdomain.UninstallLaunch,
) (nodeExporterInstall, error) {
	if launch.ReleaseServers {
		return nodeExporterInstall{}, nil
	}
	after := make(map[string]string, len(launch.TargetServerIDs))
	for _, serverID := range launch.TargetServerIDs {
		after[serverID] = uninstallPlatformStepID
	}
	return l.nodeExporter.installTasks(ctx, operationID, launch.RequestedBy,
		softwareConfigureJob(nodeexporter.Kind), l.restorableServers(ctx, launch.TargetServerIDs), after)
}

// restorableServers returns the targets the Node Exporter can be installed on once the Platform is
// gone: present, deployed, and unlocked. The others are left alone; a lookup failure only skips the
// Server.
func (l platformDeploymentLauncher) restorableServers(ctx context.Context, serverIDs []string) []string {
	restorable := make([]string, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		server, err := l.servers.FindByID(ctx, serverID)
		if err != nil {
			slog.Warn("skip the Node Exporter on an uninstalled Platform's Server", "serverId", serverID, "error", err)
			continue
		}
		if server.Absent || server.Provisioning == nil ||
			server.Provisioning.State != "deployed" || server.Provisioning.Locked {
			continue
		}
		restorable = append(restorable, serverID)
	}
	return restorable
}

// uninstallSteps composes the durable uninstall Tasks.
//
// When the servers are kept, it is the single ansible uninstall step (whose platform-uninstall
// completion clears projections). When the servers are released, it skips that
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
			Executor: workflowdomain.RunnerKindHostAutomation, Targets: prepared.Targets,
			Parameters: map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars},
			Completion: []workflowdomain.Completion{{Type: completionPlatformUninstall, Params: map[string]any{
				completionParamDeleteRecord: launch.DeleteRecord,
			}}},
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
	// the record when the user asked for it. No Node Exporter follows (a released host is wiped).
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
}

// uninstallChoice is what the user chose for a kept-servers uninstall, declared on the
// platform-uninstall completion that carries it out.
type uninstallChoice struct {
	deleteRecord bool
}

// uninstallChoiceOf reads the choice from a platform-uninstall completion's params.
func uninstallChoiceOf(completion workflowdomain.Completion) uninstallChoice {
	deleteRecord, _ := completion.Params[completionParamDeleteRecord].(bool)
	return uninstallChoice{deleteRecord: deleteRecord}
}

// recordPlatformGrafana records the Grafana URL a successful run reported in its result file
// (grafanaUrl) as the Platform's `Platform Grafana` Monitoring Link, replacing the links swallow
// recorded for the same Grafana under its earlier names. The composed Grafana install's
// platform-grafana-result completion calls it, and so does the omnistat-result completion a
// configure-omnistat Task may still declare. A missing URL fails the Task, because the playbook
// writes it only once Grafana serves every provisioned dashboard.
func (o platformDeploymentObserver) recordPlatformGrafana(ctx context.Context, platformID string, result workflowdomain.RunnerResult) error {
	grafanaURL := ""
	if result.Data != nil {
		grafanaURL = stringField(result.Data, grafanaResultURL)
	}
	if grafanaURL == "" {
		return fmt.Errorf("the Platform's Grafana installation returned no Grafana URL")
	}
	return o.platforms.RecordMonitoringLink(ctx, platformID,
		platformdomain.MonitoringLink{Name: platformGrafanaLinkName, URL: grafanaURL}, legacyGrafanaLinkNames)
}

// completeDeviceMetricsExporter records the GPU identity a successful Slurm run of
// deploy-device-metrics-exporter wrote to its result file. A missing or unknown identity is recorded
// as collapsed, never as distinct. The identity is fenced on the record id the result completion
// declares (the same id the State is fenced on), falling back to this Workflow; the State itself is
// projected by the device-metrics-exporter-state completion.
func (o platformDeploymentObserver) completeDeviceMetricsExporter(
	ctx context.Context,
	platformID, workflowID, recordID string,
	result workflowdomain.RunnerResult,
) error {
	identity := ""
	if result.Data != nil {
		identity = stringField(result.Data, deviceMetricsExporterResultGPUIdentity)
	}
	if recordID == "" {
		recordID = workflowID
	}
	return o.platforms.RecordDeviceMetricsExporterGPUIdentity(ctx, platformID, recordID, platformdomain.ParseGPUIdentity(identity))
}

func stringField(data map[string]any, key string) string {
	if value, ok := data[key].(string); ok {
		return value
	}
	return ""
}

// finishUninstall finishes a platform uninstall whose playbook succeeded: it removes the
// Platform's swallow-owned state (and the record when the request asked for it) and restores the
// exporters the uninstall removed. It returns the failure so the Task fails and can be retried.
func (o platformDeploymentObserver) finishUninstall(ctx context.Context, workflow *workflowdomain.Workflow, choice uninstallChoice) error {
	if err := o.platforms.CompleteUninstall(ctx, workflow.PlatformID, choice.deleteRecord); err != nil {
		return err
	}
	return nil
}
