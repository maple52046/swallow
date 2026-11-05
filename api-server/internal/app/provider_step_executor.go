package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/maple52046/swallow/internal/adapters/executor/temporalworkflow"
	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	"github.com/maple52046/swallow/internal/shared/redact"
)

// deploymentSubmitter submits one frozen OS deployment intent after swallow's preflight. The
// worker binds DeployServersUseCase; tests substitute a fake that moves a fake provider's state.
type deploymentSubmitter interface {
	Execute(ctx context.Context, input provisioningapp.DeployServersInput) (*provisioningapp.DeployServersResult, error)
}

// releaseSubmitter asks the provisioner to release one Server. The worker binds
// ReleaseServerUseCase, which enforces the recovery policy and lock before the provider call.
type releaseSubmitter interface {
	ExecuteWithOptions(ctx context.Context, input provisioningapp.ReleaseServerInput) (*provisioningapp.ProvisioningStateItem, error)
}

// providerStepExecutor translates durable Step intent into swallow provisioning use
// cases, then observes provider state until the requested end state is proven.
type providerStepExecutor struct {
	deployments deploymentSubmitter
	release     releaseSubmitter
	refresh     *provisioningapp.RefreshServerUseCase
	servers     serverdomain.ServerRepository
	providers   provisioningdomain.ProviderFactory
	secrets     workflowdomain.OperationSecretRepository
	tasks       provisioningdomain.ProvisioningTaskRepository
	// cleanup advances a release's static IP cleanup from inside the release Task, under its lease.
	cleanup        *provisioningapp.ReleaseNetworkCleanup
	configurations workflowdomain.AutomationConfigurationRepository
	// protection is required: requireUnlocked reads the provider-owned Server Lock through it
	// before every host mutation.
	protection     serverdomain.MutationGuard
	poll           time.Duration
	readinessWait  time.Duration
	powerOnWait    time.Duration
	stageStallWait time.Duration
	// canceledDeploymentWait overrides defaultCanceledDeploymentAbortWait in tests.
	canceledDeploymentWait time.Duration
	// providerReadErrorWait overrides defaultProviderReadErrorWait, and releaseCleanupWait
	// defaultReleaseCleanupWait, in tests.
	providerReadErrorWait, releaseCleanupWait time.Duration
	sshProbe                                  func(context.Context, string, int) error
	// bootMedia recovers the deployment boot of a Boot Media Server that did not boot the ISO
	// after the provisioner's power-on (decision 047). Nil disables the watch.
	bootMedia bootMediaBootRecoverer
	// bootMediaMediaWait and bootMediaBootWait override bootMediaMediaCheck and
	// bootMediaBootCheck in tests.
	bootMediaMediaWait, bootMediaBootWait time.Duration
	// virtualMachines runs enroll-virtual-machine Tasks (decision 055). Nil refuses them.
	virtualMachines *virtualMachineEnroller
	// The inspect-hardware timings (inspection_steps.go) are overridden only in tests; zero uses
	// the production defaults there.
	enrollmentSettleWait, enrollmentSettlePoll                                           time.Duration
	inspectionStallWait, inspectionAttemptWait, inspectionStartWait, inspectionAbortWait time.Duration
	inspectionAttempts                                                                   int
}

const (
	defaultDeploymentReadinessWait = 20 * time.Minute
	defaultDeploymentPowerOnWait   = 10 * time.Minute
	// defaultDeploymentStageStallWait is how long a deploying Machine may go without a new
	// provider event before the Step asks for user attention. Healthy MAAS stages advance
	// within minutes (the longest, Configuring OS, typically under ten), so the window is wide
	// enough for a slow mirror yet far shorter than the two-hour observation timeout. It must
	// stay longer than defaultDeploymentPowerOnWait so a powered-off Machine keeps its more
	// specific power-on diagnosis.
	defaultDeploymentStageStallWait = 25 * time.Minute
	// defaultCanceledDeploymentAbortWait is how long returning a canceled deployment waits for the
	// provider to leave Deploying after the abort. MAAS applies an abort within seconds; the window
	// covers a slow provider while staying well inside the Workflow's 30-minute compensation cap.
	defaultCanceledDeploymentAbortWait = 10 * time.Minute
	// providerObservationWait bounds the deploy, release, and recover observations of one attempt.
	providerObservationWait = 2 * time.Hour
	// defaultProviderReadErrorWait is how long an observation tolerates provisioner reads that keep
	// failing — the provisioner unreachable, or an error swallow cannot classify — before it asks
	// for attention. A failed read says nothing about the Server and a provisioner restart takes
	// minutes, so the window is wide; it is still far shorter than the observations themselves, so
	// a provisioner that stays down is reported instead of being waited out for two hours.
	defaultProviderReadErrorWait = 10 * time.Minute
	// defaultReleaseCleanupWait bounds the static IP cleanup a Release finishes once its Server is
	// Ready. Each cleanup step is one provisioner call, so a cleanup unfinished after this long is
	// stuck, not slow.
	defaultReleaseCleanupWait = 30 * time.Minute
)

// Execute runs one provisioner Task attempt. A failure's diagnostic that names no target names the
// Task's Server: every provisioner Task acts on one.
func (e providerStepExecutor) Execute(ctx context.Context, input temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	return withServerTarget(e.execute(ctx, input), input.Step)
}

func (e providerStepExecutor) execute(ctx context.Context, input temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	switch input.Step.Kind {
	case "provision-os":
		serverID := firstTargetServer(input.Step)
		if serverID == "" {
			return e.deploy(ctx, input)
		}
		if err := e.setDeployment(ctx, input, serverID, serverdomain.DeploymentDeploying, nil, false); err != nil {
			return providerAttention("deployment_projection_unavailable", err.Error(), "deployment_projection")
		}
		result := e.deploy(ctx, input)
		if temporalworkflow.LeavesExternalWorkRunning(ctx) || isSuperseded(result) {
			return result
		}
		// A verification borrow must reach a terminal outcome so its Workflow returns the borrowed
		// Server; harden a retryable proving-deploy failure into a terminal one before it is
		// projected, so the verify Workflow does not park at requires_attention and strand the
		// Server. A normal deploy is unaffected.
		result = e.hardenVerificationResult(input.Step, result)
		if err := e.finishDeployment(ctx, input, serverID, result); err != nil {
			return providerAttention("deployment_projection_unavailable", err.Error(), "deployment_projection")
		}
		return result
	case "release-os":
		result := e.releaseServer(ctx, input)
		if result.Status == workflowdomain.TaskSucceeded {
			serverID := firstTargetServer(input.Step)
			if serverID != "" {
				if err := e.servers.SetDeployment(ctx, serverID, nil); err != nil {
					return providerAttention("deployment_projection_unavailable", err.Error(), "deployment_projection")
				}
			}
		}
		return result
	case "recover-server":
		result := e.recoverServer(ctx, input)
		if result.Status == workflowdomain.TaskSucceeded {
			serverID := firstTargetServer(input.Step)
			if serverID != "" {
				if err := e.servers.SetDeployment(ctx, serverID, nil); err != nil {
					return providerAttention("deployment_projection_unavailable", err.Error(), "deployment_projection")
				}
			}
		}
		return result
	case temporalworkflow.ReturnCanceledDeploymentKind:
		return e.returnCanceledDeployment(ctx, input)
	case waitEnrollmentTaskKind:
		return e.waitEnrollmentSettled(ctx, input)
	case inspectTaskKind:
		return e.inspectServer(ctx, input)
	case enrollVirtualMachineTaskKind:
		return e.enrollVirtualMachine(ctx, input)
	default:
		return providerFailed("unsupported_step", "The provider Step kind is not supported.", false).StepExecutionResult
	}
}

// hardenVerificationResult turns a retryable proving-deploy outcome into a terminal, non-retryable
// failure when the deploy is a verification borrow (VerificationRun). A verification must reach a
// terminal state so its Workflow's return-to-ready step runs and the borrowed Server is given back,
// instead of parking at requires_attention — which strands the Server behind a busy lock and shows
// a perpetual "verifying" on the OS Images list. A provider-unavailable outcome that never reached
// the Machine is left retryable: retrying it is safe and a transient provider outage must not be
// recorded as a proof failure. Non-verification deploys and success/attention-for-other-reasons are
// returned unchanged.
func (e providerStepExecutor) hardenVerificationResult(step workflowdomain.Task, result temporalworkflow.StepExecutionResult) temporalworkflow.StepExecutionResult {
	if result.Status != workflowdomain.TaskRequiresAttention && result.Status != workflowdomain.TaskFailed {
		return result
	}
	if result.Error != nil && result.Error.Code == "provider_unavailable" {
		return result
	}
	var input provisioningapp.DeployServersInput
	if err := decodeStepRequest(step.Parameters, &input); err != nil || !input.VerificationRun {
		return result
	}
	if result.Error != nil {
		result.Error.Retryable = false
	}
	result.Status = workflowdomain.TaskFailed
	return result
}

func (e providerStepExecutor) finishDeployment(ctx context.Context, input temporalworkflow.StepExecutionInput, serverID string, result temporalworkflow.StepExecutionResult) error {
	state := serverdomain.DeploymentDeploying
	switch result.Status {
	case workflowdomain.TaskSucceeded:
		state = serverdomain.DeploymentSucceeded
	case workflowdomain.TaskFailed:
		state = serverdomain.DeploymentFailed
	case workflowdomain.TaskRequiresAttention:
		state = serverdomain.DeploymentRequiresAttention
	case workflowdomain.TaskCanceled:
		state = serverdomain.DeploymentCanceled
	default:
		return nil
	}
	return e.setDeployment(ctx, input, serverID, state, result.Error, true)
}

func (e providerStepExecutor) setDeployment(ctx context.Context, input temporalworkflow.StepExecutionInput, serverID string, state serverdomain.DeploymentState, failure *workflowdomain.NormalizedError, finished bool) error {
	now := time.Now().UTC()
	startedAt := now
	server, err := e.servers.FindByID(ctx, serverID)
	if err != nil {
		return err
	}
	if current := server.Deployment; current != nil && current.OperationID == input.OperationID && current.StepID == input.Step.ID && current.Attempt == input.Step.Attempt {
		startedAt = current.StartedAt
	}
	code, stage, reason := "", "", ""
	if failure != nil {
		code = failure.Code
		stage = failure.Stage
		reason = failure.Message
	}
	var finishedAt *time.Time
	if finished {
		finishedAt = &now
	}
	return e.servers.SetDeployment(ctx, serverID, &serverdomain.DeploymentStatus{
		State:        state,
		OperationID:  input.OperationID,
		StepID:       input.Step.ID,
		Attempt:      input.Step.Attempt,
		Code:         code,
		Stage:        stage,
		StatusReason: reason,
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
		UpdatedAt:    now,
	})
}

func (e providerStepExecutor) deploy(ctx context.Context, step temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	var input provisioningapp.DeployServersInput
	if err := decodeStepRequest(step.Step.Parameters, &input); err != nil {
		return providerFailed("invalid_step", err.Error(), false).StepExecutionResult
	}
	if input.Settings.ImageID == nil || strings.TrimSpace(*input.Settings.ImageID) == "" {
		return providerFailed("intent_snapshot_incomplete", "The Operation does not contain a frozen OS image. Create a new deployment Operation.", false).withStage("deployment_preflight")
	}
	input.Comment = fmt.Sprintf("Swallow operation %s step %s attempt %d", step.OperationID, step.Step.ID, step.Step.Attempt)
	if reference := step.Step.SecretRefs["userData"]; reference != "" {
		value, err := e.secrets.Resolve(ctx, reference)
		if err != nil {
			return providerFailed("secret_unavailable", "Cloud-init could not be resolved.", false).StepExecutionResult
		}
		text, ok := value.(string)
		if !ok {
			return providerFailed("secret_invalid", "Cloud-init has an invalid stored format.", false).StepExecutionResult
		}
		input.UserData.Value = text
	}
	serverID := firstTargetServer(step.Step)
	if serverID == "" {
		return providerFailed("invalid_step", "The provisioning Step has no Server target.", false).StepExecutionResult
	}
	if resumedStep(step) {
		state, stop := e.resumedDeploymentState(ctx, step, serverID)
		if stop != nil {
			return *stop
		}
		if state.State == string(provisioningdomain.MachineStatusDeployed) && imageMatches(state, input) {
			readiness, inspectErr := e.inspectDeploymentReadiness(ctx, serverID, step.SiteID)
			if inspectErr != nil {
				return providerAttention("deployment_readiness_unavailable", inspectErr.Error(), "ssh_readiness")
			}
			if readiness.reachable {
				return providerSucceeded()
			}
			// Releasing the installation to replay the intent is a user's retry to make; a
			// resumed or retried activity of the same attempt only observes.
			if len(readiness.addresses) == 0 && step.Step.Attempt > 1 {
				return e.recoverDeploymentWithoutAddress(ctx, step, serverID, input)
			}
			return e.observeDeploy(ctx, step, serverID, input, true)
		}
		if state.State == string(provisioningdomain.MachineStatusDeploying) {
			return e.observeDeploy(ctx, step, serverID, input, true)
		}
		machineState := provisioningdomain.MachineStatus(state.State)
		switch {
		case step.Step.Attempt > 1 && recoverableBeforeRedeploy(machineState):
			// A layout that needs a new inspection fails every disk deployment the same way;
			// refuse before Recover wipes the Machine only for the redeploy preflight to
			// refuse it, and end the Step so the Operation frees the Server for Inspect.
			if state.InspectionRequired && !deploysFromMemory(input) {
				return providerFailed("inspection_required",
					provisioningapp.InspectionRequiredMessage(state.InspectionRequiredReason), false).
					withStage("deployment_preflight")
			}
			// The user retried a deployment the provider left unusable. The deploy
			// preflight accepts only ready, so without returning the Machine to ready first
			// every retry was refused and parked again, forever.
			return e.recoverAndRedeploy(ctx, step, serverID, machineState, input)
		case step.Step.Attempt > 1 && machineState == provisioningdomain.MachineStatusDeployed:
			// Deployed with a different image (a match returned above). Replacing a running
			// OS is not something a retry may do on its own; end the Step so the Operation
			// frees the Server for an explicit Release.
			return providerFailed("deployment_image_mismatch",
				"The Server is already deployed with a different image. Release it, then deploy again.", false).
				withStage("deployment_preflight")
		case machineState == provisioningdomain.MachineStatusFailed || machineState == provisioningdomain.MachineStatusBroken:
			// An activity retry of the first attempt (worker turnover), not a user retry:
			// the deployment this attempt submitted failed while the worker was away. Report
			// it exactly as observation would, and leave recovery to an explicit retry.
			return e.deploymentFailed(ctx, step, serverID)
		case machineState == provisioningdomain.MachineStatusAllocated:
			// Same activity retry, with the submitted deployment reserved but not yet
			// installing: keep observing it rather than submitting a second deployment.
			return e.observeDeploy(ctx, step, serverID, input, true)
		}
	}
	if locked := e.requireUnlocked(ctx, serverID); locked != nil {
		return *locked
	}
	if cleared := e.clearReplacedDefaultUser(ctx, serverID); cleared != nil {
		return *cleared
	}
	result, err := e.deployments.Execute(ctx, input)
	if err != nil {
		return normalizeProviderError(err, "deployment_preflight")
	}
	if len(result.Failed) > 0 {
		failure := result.Failed[0]
		if failure.Code == "provider_unavailable" {
			return e.observeDeploy(ctx, step, serverID, input, false)
		}
		return providerFailed(failure.Code, failure.Message, failure.Code != "provider_rejected").withStage(failure.Stage)
	}
	return e.observeDeploy(ctx, step, serverID, input, true)
}

// observeDeploy follows a submitted deployment until swallow proves it (the frozen image deployed
// and SSH reachable), publishing each observation as the Task's wait observation. Failed
// provisioner reads are tolerated for providerReadErrorTimeout (providerWait); a stage the
// provisioner stops advancing asks for attention with the events it recorded about the machine.
func (e providerStepExecutor) observeDeploy(ctx context.Context, step temporalworkflow.StepExecutionInput, serverID string, input provisioningapp.DeployServersInput, accepted bool) temporalworkflow.StepExecutionResult {
	deadline := time.NewTimer(providerObservationWait)
	defer deadline.Stop()
	ticker := time.NewTicker(e.pollInterval())
	defer ticker.Stop()
	var readinessDeadline time.Time
	var deployingSince time.Time
	var allocatedSince time.Time
	var lastReadiness deploymentReadiness
	progress := e.newDeploymentProgress(ctx, step, serverID)
	watch := e.newBootMediaWatch(step)
	wait := e.newProviderWait(ctx, step, serverID, "deployment", providerObservationWait)
	for {
		state, err := e.refresh.Execute(ctx, serverID)
		if err != nil {
			if stop := wait.readFailed(ctx, err, time.Now()); stop != nil {
				return *stop
			}
		} else {
			observation := ""
			switch state.State {
			case string(provisioningdomain.MachineStatusDeploying):
				allocatedSince = time.Time{}
				now := time.Now()
				if deployingSince.IsZero() {
					deployingSince = now
				}
				if !now.Before(deployingSince.Add(e.powerOnTimeout())) {
					powerState, supported, powerErr := e.queryLivePower(ctx, serverID)
					if powerErr == nil && supported && powerState == provisioningdomain.PowerStateOff {
						return e.stalledDeployment(ctx, wait, providerAttention(
							"deployment_power_on_timeout",
							"The provisioner still reports Deploying, but the Server remained powered off for 10 minutes. Check the provider deployment and power workflow before retrying.",
							"deployment_power_on",
						), workflowdomain.FailurePermanent, e.powerOnTimeout(),
							"the provisioner reported Deploying while the Server's live power state stayed off",
							"Check why the provisioner did not power the Server on (its power configuration and the BMC), then retry this Task.")
					}
					// A live-on, unknown, or temporarily unavailable power reading is not
					// proof of a provider stall. Delay the next diagnostic query while the
					// outer two-hour deployment observation remains authoritative.
					deployingSince = now
				}
				if stalled := e.observeInstallProgress(ctx, step, serverID, &progress, now); stalled != nil {
					if stalled.Error == nil || stalled.Error.Code != "deployment_provider_stage_stall" {
						return *stalled
					}
					return e.stalledDeployment(ctx, wait, *stalled, workflowdomain.FailureTransient, e.stageStallTimeout(),
						"the provisioner recorded no new installation event within the stage-stall window",
						"Inspect the provider deployment where its last stage stopped (an unreachable package mirror is a common cause), then retry this Task; a retry observes again and accepts a deployment that finished.")
				}
				if watch.observe(ctx, serverID, progress, now) {
					// The host's boot started over; the stall window measures from the restart.
					progress.since = time.Now()
				}
				observation = providerStateObservation(wait.name, state, "waiting for the OS installation to finish.")
				if stage := progress.shown.eventType; stage != "" {
					observation = fmt.Sprintf("The provisioner reports %s as Deploying, at stage %s; waiting for the OS installation to finish.", wait.name, stage)
				}
			case string(provisioningdomain.MachineStatusAllocated):
				// ALLOCATED is the brief reserved state MAAS passes through on its way to
				// DEPLOYING. Lingering here after an accepted deploy means the provider reserved
				// the Machine but never started installing (for example a power or scheduling
				// stall), so surface it for user attention past a grace window instead of
				// silently waiting out the two-hour observation. The Machine is left allocated,
				// which is now a Recover/Release source, so the user can return it to ready.
				now := time.Now()
				if allocatedSince.IsZero() {
					allocatedSince = now
				} else if !now.Before(allocatedSince.Add(e.powerOnTimeout())) {
					return e.stalledDeployment(ctx, wait, providerAttention(
						"deployment_reserved_not_started",
						"The provisioner reserved the Server (allocated) but did not start the OS installation. Check the provider deployment before retrying.",
						"deployment",
					), workflowdomain.FailureTransient, e.powerOnTimeout(),
						"the provisioner kept the Server reserved (allocated) without starting the installation",
						"Check the provider deployment, then retry this Task, or Recover the Server to return it to Ready.")
				}
				observation = fmt.Sprintf("The provisioner reserved %s (allocated) and has not started the OS installation yet.", wait.name)
			case string(provisioningdomain.MachineStatusDeployed):
				deployingSince = time.Time{}
				allocatedSince = time.Time{}
				if imageMatches(state, input) {
					if readinessDeadline.IsZero() {
						// Record why the deployment sits in "verifying": the provider installed the
						// OS but swallow has not yet confirmed the host is reachable over SSH. Carrying
						// this as a non-terminal reason (empty code, so it is not a failure) gives the
						// UI something better than a silent, frozen "verifying" during the readiness
						// wait — which can be minutes for an image that boots from RAM but not disk.
						waiting := &workflowdomain.NormalizedError{
							Stage:   "ssh_readiness",
							Message: "The provider installed the OS; waiting for the host to become reachable over SSH before confirming the deployment.",
						}
						if err := e.setDeployment(ctx, step, serverID, serverdomain.DeploymentVerifying, waiting, false); err != nil {
							return providerAttention("deployment_projection_unavailable", err.Error(), "deployment_projection")
						}
						readinessDeadline = time.Now().Add(e.readinessTimeout())
					}
					readiness, inspectErr := e.inspectDeploymentReadiness(ctx, serverID, step.SiteID)
					if inspectErr != nil {
						return providerAttention("deployment_readiness_unavailable", inspectErr.Error(), "ssh_readiness")
					}
					lastReadiness = readiness
					if readiness.reachable {
						return providerSucceeded()
					}
					if !time.Now().Before(readinessDeadline) {
						return deploymentReadinessFailed(lastReadiness)
					}
					observation = deploymentReadinessObservation(wait.name, readiness)
				}
				if !accepted {
					return providerAttention("provider_outcome_unknown", "The provider response was lost and the Server is deployed with a different image. Verify MAAS before retrying.", "deployment")
				}
				if observation == "" {
					observation = fmt.Sprintf("The provisioner still reports %s as deployed with another image; waiting for the new deployment to start.", wait.name)
				}
			case string(provisioningdomain.MachineStatusFailed), string(provisioningdomain.MachineStatusBroken):
				return e.deploymentFailed(ctx, step, serverID)
			case string(provisioningdomain.MachineStatusReady):
				deployingSince = time.Time{}
				allocatedSince = time.Time{}
				if !accepted {
					return providerAttention("provider_outcome_unknown", "The provider response was lost before Swallow could prove whether deployment started. Verify MAAS before retrying.", "deployment")
				}
				observation = providerStateObservation(wait.name, state, "waiting for the deployment to start.")
			default:
				observation = providerStateObservation(wait.name, state, "waiting for it to reach Deployed.")
			}
			if stop := wait.observed(ctx, observation); stop != nil {
				return *stop
			}
		}
		select {
		case <-ctx.Done():
			if !temporalworkflow.LeavesExternalWorkRunning(ctx) {
				e.abort(serverID)
			}
			return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskCanceled}
		case <-deadline.C:
			if !readinessDeadline.IsZero() {
				return deploymentReadinessFailed(lastReadiness)
			}
			return wait.timedOut(providerAttention("provider_observation_timeout", "Timed out waiting for the Server to reach Deployed.", "deployment"),
				"Check the deployment in the provisioner, then retry this Task; a retry observes again before it acts.")
		case <-ticker.C:
		}
	}
}

// deploymentReadinessObservation is the wait observation of a deployment the provisioner finished
// while swallow waits for the host to answer on its SSH port.
func deploymentReadinessObservation(name string, readiness deploymentReadiness) string {
	if len(readiness.addresses) == 0 {
		return fmt.Sprintf("The provisioner installed the OS on %s; waiting for it to report the Server's address.", name)
	}
	return fmt.Sprintf("The provisioner installed the OS on %s; waiting for SSH port %d at %s.",
		name, readiness.port, strings.Join(readiness.addresses, ", "))
}

// stalledDeployment attaches to result, an attention for a deployment stage the provisioner stopped
// advancing, a diagnostic of class: the deployment wait's attempts, elapsed time and last
// observation, the stage's own window as the budget, what stalled as the cause, and the events the
// provisioner recorded about the machine during the attempt (DF-82).
func (e providerStepExecutor) stalledDeployment(
	ctx context.Context,
	wait *providerWait,
	result temporalworkflow.StepExecutionResult,
	class workflowdomain.FailureClass,
	window time.Duration,
	stalled, remediation string,
) temporalworkflow.StepExecutionResult {
	diagnostic := wait.progress.Diagnostic(class, fmt.Errorf("%w: %s for %s", workflowdomain.ErrWaitBudgetExhausted, stalled, window), time.Now())
	diagnostic.BudgetSeconds = int64(window / time.Second)
	diagnostic.Target, diagnostic.Remediation = serverTarget(wait.serverID), remediation
	diagnostic.ProviderEvents = e.providerEventsSince(ctx, wait.step, wait.serverID)
	return withDiagnostic(result, diagnostic)
}

type deploymentReadiness struct {
	serverName string
	addresses  []string
	port       int
	reachable  bool
}

// inspectDeploymentReadiness checks the latest provider projection without treating
// MAAS's narrower deployed state as a completed swallow deployment.
func (e providerStepExecutor) inspectDeploymentReadiness(ctx context.Context, serverID, siteID string) (deploymentReadiness, error) {
	server, err := e.servers.FindByID(ctx, serverID)
	if err != nil {
		return deploymentReadiness{}, err
	}
	port, err := e.deploymentSSHPort(ctx, siteID)
	if err != nil {
		return deploymentReadiness{}, err
	}
	readiness := deploymentReadiness{serverName: server.DisplayName(), port: port}
	for _, address := range server.Observed.Addresses {
		address = strings.TrimSpace(address)
		if address != "" {
			readiness.addresses = append(readiness.addresses, address)
		}
	}
	probe := e.sshProbe
	if probe == nil {
		probe = probeSSH
	}
	for _, address := range readiness.addresses {
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := probe(probeCtx, address, port)
		cancel()
		if err == nil {
			readiness.reachable = true
			break
		}
	}
	return readiness, nil
}

func (e providerStepExecutor) deploymentSSHPort(ctx context.Context, siteID string) (int, error) {
	if e.configurations == nil {
		return 22, nil
	}
	configuration, err := e.configurations.FindBySiteID(ctx, siteID)
	if errors.Is(err, workflowdomain.ErrAutomationConfigNotFound) {
		return 22, nil
	}
	if err != nil {
		return 0, err
	}
	if configuration.SSHPort > 0 {
		return configuration.SSHPort, nil
	}
	return 22, nil
}

func probeSSH(ctx context.Context, address string, port int) error {
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(address, fmt.Sprintf("%d", port)))
	if connection != nil {
		_ = connection.Close()
	}
	return err
}

func deploymentReadinessFailed(readiness deploymentReadiness) temporalworkflow.StepExecutionResult {
	if len(readiness.addresses) == 0 {
		message := fmt.Sprintf(
			"MAAS installed the requested OS on %s, but no provider address was observed. Swallow did not verify this deployment. Retry this Step to release and redeploy the Server with the same frozen network settings.",
			readiness.serverName,
		)
		return providerFailed("deployment_address_unavailable", message, true).withStage("ssh_readiness")
	}
	message := fmt.Sprintf(
		"MAAS installed the requested OS on %s, but SSH port %d was not reachable at %s. Swallow did not verify this deployment. Correct host or network access, then retry this Step to verify it again.",
		readiness.serverName, readiness.port, strings.Join(readiness.addresses, ", "),
	)
	return providerFailed("deployment_ssh_unreachable", message, true).withStage("ssh_readiness")
}

// resumedStep reports whether the attempt may already have started its provider work: an
// user's retry (a later attempt), an activity retry, or a later dispatch continuing the attempt.
// Such an attempt observes the provider before it acts. Only a later attempt may also replace what
// the provider holds.
func resumedStep(step temporalworkflow.StepExecutionInput) bool {
	return step.Step.Attempt > 1 || step.ActivityAttempt > 1 || step.Resumed
}

// recoverDeploymentWithoutAddress is entered only by an explicit failed-Step retry.
// MAAS exposes complete interface mutation only while Ready or Broken, so swallow
// releases the unusable installation before replaying the frozen deployment intent.
func (e providerStepExecutor) recoverDeploymentWithoutAddress(ctx context.Context, step temporalworkflow.StepExecutionInput, serverID string, input provisioningapp.DeployServersInput) temporalworkflow.StepExecutionResult {
	if e.release == nil || e.deployments == nil {
		return providerFailed("deployment_recovery_unavailable", "OS deployment recovery is unavailable.", false).withStage("deployment_recovery")
	}
	if locked := e.requireUnlocked(ctx, serverID); locked != nil {
		return *locked
	}
	_, err := e.release.ExecuteWithOptions(ctx, provisioningapp.ReleaseServerInput{
		ServerID:  serverID,
		Comment:   fmt.Sprintf("Recover Swallow operation %s step %s attempt %d", step.OperationID, step.Step.ID, step.Step.Attempt),
		RequestID: step.OperationID,
	})
	if err != nil {
		return normalizeProviderError(err, "deployment_recovery_release")
	}
	released := e.observeRelease(ctx, step, serverID, false, true)
	if released.Status != workflowdomain.TaskSucceeded {
		return released
	}
	if locked := e.requireUnlocked(ctx, serverID); locked != nil {
		return *locked
	}
	if cleared := e.clearReplacedDefaultUser(ctx, serverID); cleared != nil {
		return *cleared
	}
	result, err := e.deployments.Execute(ctx, input)
	if err != nil {
		return normalizeProviderError(err, "deployment_recovery_redeploy")
	}
	if len(result.Failed) > 0 {
		failure := result.Failed[0]
		return providerFailed(failure.Code, failure.Message, failure.Code != "provider_rejected").withStage("deployment_recovery_" + failure.Stage)
	}
	return e.observeDeploy(ctx, step, serverID, input, true)
}

// recoverableBeforeRedeploy reports the provider states an explicit deployment retry returns to
// ready before redeploying: the Recover sources other than deployed. A failed deploy leaves the
// Machine failed (or broken), a reservation that never started installing leaves it allocated,
// and rescue is a diagnostic detour; none of them can take a new deployment, and every one has a
// Recover plan. deployed is excluded because it may carry a running OS a retry must not discard.
func recoverableBeforeRedeploy(state provisioningdomain.MachineStatus) bool {
	switch state {
	case provisioningdomain.MachineStatusFailed, provisioningdomain.MachineStatusAllocated,
		provisioningdomain.MachineStatusBroken, provisioningdomain.MachineStatusRescue:
		return provisioningdomain.EvaluateRecovery(provisioningdomain.RecoveryIntentRecover, state).Allowed
	default:
		return false
	}
}

// recoverAndRedeploy is entered only by an explicit failed-Step retry (Attempt > 1) of a
// provision-os whose Machine the provider left unusable (see recoverableBeforeRedeploy). The
// user's retry is the request to redeploy, so the Step runs the same state-driven Recover
// the Recover Operation runs — without unbinding static IPs, so the frozen network intent still
// applies — and then replays the frozen deployment. It runs inside the deploying Operation, under
// its resource leases, because a separate Recover Operation would be refused while this one
// holds the Server. Recovery is never automatic: a first attempt or an activity retry never
// reaches it. Failures carry a "deployment_recovery_" stage so they read as part of the retry.
func (e providerStepExecutor) recoverAndRedeploy(
	ctx context.Context,
	step temporalworkflow.StepExecutionInput,
	serverID string,
	state provisioningdomain.MachineStatus,
	input provisioningapp.DeployServersInput,
) temporalworkflow.StepExecutionResult {
	if e.release == nil || e.deployments == nil {
		return providerFailed("deployment_recovery_unavailable", "OS deployment recovery is unavailable.", false).withStage("deployment_recovery")
	}
	recovering := &workflowdomain.NormalizedError{
		Stage:   "deployment_recovery",
		Message: fmt.Sprintf("The provisioner reports the Server as %s; returning it to Ready before redeploying.", state),
	}
	if err := e.setDeployment(ctx, step, serverID, serverdomain.DeploymentDeploying, recovering, false); err != nil {
		return providerAttention("deployment_projection_unavailable", err.Error(), "deployment_projection")
	}
	recovered := e.recoverToReady(ctx, step, serverID, provisioningapp.RecoverServerInput{
		ServerID:  serverID,
		Comment:   fmt.Sprintf("Redeploy Swallow operation %s step %s attempt %d", step.OperationID, step.Step.ID, step.Step.Attempt),
		RequestID: step.OperationID,
	})
	if recovered.Status != workflowdomain.TaskSucceeded {
		if recovered.Error != nil && !strings.HasPrefix(recovered.Error.Stage, "deployment_recovery") {
			recovered.Error.Stage = "deployment_recovery_" + recovered.Error.Stage
		}
		return recovered
	}
	if locked := e.requireUnlocked(ctx, serverID); locked != nil {
		return *locked
	}
	if cleared := e.clearReplacedDefaultUser(ctx, serverID); cleared != nil {
		return *cleared
	}
	result, err := e.deployments.Execute(ctx, input)
	if err != nil {
		return normalizeProviderError(err, "deployment_recovery_redeploy")
	}
	if len(result.Failed) > 0 {
		failure := result.Failed[0]
		if failure.Code == "provider_unavailable" {
			return e.observeDeploy(ctx, step, serverID, input, false)
		}
		return providerFailed(failure.Code, failure.Message, failure.Code != "provider_rejected").withStage("deployment_recovery_" + failure.Stage)
	}
	return e.observeDeploy(ctx, step, serverID, input, true)
}

func (e providerStepExecutor) readinessTimeout() time.Duration {
	if e.readinessWait <= 0 {
		return defaultDeploymentReadinessWait
	}
	return e.readinessWait
}

func (e providerStepExecutor) powerOnTimeout() time.Duration {
	if e.powerOnWait <= 0 {
		return defaultDeploymentPowerOnWait
	}
	return e.powerOnWait
}

// stageStallTimeout is the provider-progress grace window for a deploying Machine; the field
// override exists for tests, production uses defaultDeploymentStageStallWait.
func (e providerStepExecutor) stageStallTimeout() time.Duration {
	if e.stageStallWait <= 0 {
		return defaultDeploymentStageStallWait
	}
	return e.stageStallWait
}

func (e providerStepExecutor) queryLivePower(ctx context.Context, serverID string) (provisioningdomain.PowerState, bool, error) {
	server, err := e.servers.FindByID(ctx, serverID)
	if err != nil {
		return provisioningdomain.PowerStateUnknown, false, err
	}
	provider, err := e.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return provisioningdomain.PowerStateUnknown, false, err
	}
	controller, ok := provider.(provisioningdomain.PowerController)
	if !ok {
		return provisioningdomain.PowerStateUnknown, false, nil
	}
	state, err := controller.QueryPowerState(ctx, server.Source.ProviderMachineID)
	return state, true, err
}

func (e providerStepExecutor) releaseServer(ctx context.Context, step temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	var input provisioningapp.ReleaseServerInput
	if err := decodeStepRequest(step.Step.Parameters, &input); err != nil {
		return providerFailed("invalid_step", err.Error(), false).StepExecutionResult
	}
	serverID := firstTargetServer(step.Step)
	input.ServerID = serverID
	input.RequestID = step.OperationID
	correlation := fmt.Sprintf("Swallow operation %s step %s attempt %d", step.OperationID, step.Step.ID, step.Step.Attempt)
	if input.Comment == "" {
		input.Comment = correlation
	} else {
		input.Comment = input.Comment + " | " + correlation
	}
	// Observe before acting on every attempt, not only on a retry (decision 061): a Release that
	// superseded other Workflows starts only after they end, and a canceled deployment returns its
	// Server to Ready before its Workflow ends, so a first attempt can find the Server already Ready
	// or Releasing. A retry resumes the Release this Step sent; a first attempt did not send one.
	resumed := resumedStep(step)
	if state, err := e.refresh.Execute(ctx, serverID); err == nil {
		switch provisioningdomain.MachineStatus(state.State) {
		case provisioningdomain.MachineStatusReady:
			if resumed {
				return e.observeReleaseCleanup(ctx, serverID, step.OperationID, input.UnbindStaticIPs, true)
			}
			return releaseFoundReady(input)
		case provisioningdomain.MachineStatusReleasing:
			if resumed {
				return e.observeRelease(ctx, step, serverID, input.UnbindStaticIPs, true)
			}
			if released := e.observeRelease(ctx, step, serverID, false, true); released.Status != workflowdomain.TaskSucceeded {
				return released
			}
			return releaseFoundReady(input)
		}
	}
	if locked := e.requireUnlocked(ctx, serverID); locked != nil {
		return *locked
	}
	_, err := e.release.ExecuteWithOptions(ctx, input)
	if err != nil {
		var providerErr *provisioningdomain.ProviderError
		if errors.As(err, &providerErr) && providerErr.Kind == provisioningdomain.ProviderErrorUnavailable {
			return e.observeRelease(ctx, step, serverID, input.UnbindStaticIPs, false)
		}
		return normalizeProviderError(err, "release")
	}
	return e.observeRelease(ctx, step, serverID, input.UnbindStaticIPs, true)
}

// releaseFoundReady settles a first Release attempt that found its Server already Ready without
// this Step releasing it. Ready is the Release's goal, so it succeeds; but the static IP bindings
// a cleanup would remove were never captured, so a request that asked for that cleanup fails,
// not retryable, saying the bindings are still there rather than reporting work that never ran.
func releaseFoundReady(input provisioningapp.ReleaseServerInput) temporalworkflow.StepExecutionResult {
	if input.UnbindStaticIPs {
		return providerFailed("release_cleanup_not_applied",
			"The Server was already Ready when this Release started, so Swallow did not release it and did not remove its static IP bindings. Remove them on the Server's Networking tab if they are no longer wanted.",
			false).withStage("network_cleanup")
	}
	return providerSucceeded()
}

// recoverServer runs a recover-server Step: it decodes the frozen Recover intent and converges
// the Step's target Server back to ready with recoverToReady.
func (e providerStepExecutor) recoverServer(ctx context.Context, step temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	var input provisioningapp.RecoverServerInput
	if err := decodeStepRequest(step.Step.Parameters, &input); err != nil {
		return providerFailed("invalid_step", err.Error(), false).StepExecutionResult
	}
	serverID := firstTargetServer(step.Step)
	if serverID == "" {
		return providerFailed("invalid_step", "The recovery Step has no Server target.", false).StepExecutionResult
	}
	return e.recoverToReady(ctx, step, serverID, input)
}

// returnCanceledDeployment runs the internal step a canceled Workflow issues for each
// provision-os the cancel interrupted: it returns that deployment's Server to Ready
// (decision 061). It acts only on what the canceled deployment left in flight. A Deploying
// Machine is aborted once and watched until the provider leaves Deploying (a Releasing one is
// watched until the Release ends), and an Allocated Machine, which is where MAAS parks an aborted
// deployment, is returned to Ready with the Recover plan: a plain Release, with static IP bindings
// kept and no disk erase. A Machine that is deployed, failed, broken, or in rescue holds the
// deployment's outcome and is left as it is; so is one already Ready.
//
// The result tells the Workflow what happened: succeeded means the Server was returned to Ready,
// skipped means nothing was done and WaitingReason says why, and any other status carries the
// reason in Error. The step never writes the Server's deployment result; once the Server is
// observed Ready, reconcile clears the canceled result.
func (e providerStepExecutor) returnCanceledDeployment(ctx context.Context, step temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	serverID := firstTargetServer(step.Step)
	if serverID == "" {
		return providerFailed("invalid_step", "The canceled deployment has no Server target.", false).StepExecutionResult
	}
	deadline := time.Now().Add(e.canceledDeploymentAbortTimeout())
	ticker := time.NewTicker(e.pollInterval())
	defer ticker.Stop()
	aborted := false
	for {
		state, err := e.refresh.Execute(ctx, serverID)
		if err == nil {
			switch machineState := provisioningdomain.MachineStatus(state.State); machineState {
			case provisioningdomain.MachineStatusDeploying:
				if !aborted {
					e.abort(serverID)
					aborted = true
				}
			case provisioningdomain.MachineStatusReleasing:
				// A Release (an explicit retry's Recover) is already returning it; keep watching.
			case provisioningdomain.MachineStatusAllocated:
				return e.recoverToReady(ctx, step, serverID, provisioningapp.RecoverServerInput{
					ServerID:  serverID,
					RequestID: step.OperationID,
					Comment: fmt.Sprintf("Return canceled deployment of Swallow operation %s step %s attempt %d",
						step.OperationID, step.Step.ID, step.Step.Attempt),
				})
			case provisioningdomain.MachineStatusReady:
				return canceledDeploymentLeft("The Server is already Ready.")
			default:
				return canceledDeploymentLeft(fmt.Sprintf(
					"The provisioner reports the Server as %s, which Swallow keeps as the outcome of the canceled deployment.",
					machineState))
			}
		}
		if !time.Now().Before(deadline) {
			return providerAttention("canceled_deployment_still_in_progress",
				"The provisioner still reports the canceled deployment as in progress. Check the provider, then Recover the Server.",
				"deployment_cancel")
		}
		select {
		case <-ctx.Done():
			return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskCanceled}
		case <-ticker.C:
		}
	}
}

// canceledDeploymentLeft reports a return-canceled-deployment step that did nothing, with the
// reason the Workflow records on its timeline.
func canceledDeploymentLeft(reason string) temporalworkflow.StepExecutionResult {
	return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskSkipped, WaitingReason: reason}
}

// canceledDeploymentAbortTimeout bounds how long a canceled deployment may still report Deploying
// or Releasing after the abort; the field override exists for tests.
func (e providerStepExecutor) canceledDeploymentAbortTimeout() time.Duration {
	if e.canceledDeploymentWait <= 0 {
		return defaultCanceledDeploymentAbortWait
	}
	return e.canceledDeploymentWait
}

// recoverToReady converges a Server whose provisioning axis is not usable back to ready by
// choosing the provider primitive for its observed state per the recovery policy: broken is
// cleared with Mark fixed, rescue is exited and then, if still not ready, released, and
// failed, allocated, or deployed is released. It is state-driven rather than plan-fixed so a
// Machine that changes state between primitives (rescue -> failed after exit) is re-evaluated.
// Each primitive is issued at most once so a stuck state fails for attention rather than
// looping. It is shared by the Recover Operation and by an explicit retry of a failed
// provision-os, which returns its Server to ready before redeploying; the caller owns the
// Server's deployment projection.
//
// Each observation is published as the Task's wait observation, and failed provisioner reads are
// tolerated for providerReadErrorTimeout (providerWait).
func (e providerStepExecutor) recoverToReady(ctx context.Context, step temporalworkflow.StepExecutionInput, serverID string, input provisioningapp.RecoverServerInput) temporalworkflow.StepExecutionResult {
	deadline := time.NewTimer(providerObservationWait)
	defer deadline.Stop()
	wait := e.newProviderWait(ctx, step, serverID, "recover", providerObservationWait)
	ticker := time.NewTicker(e.pollInterval())
	defer ticker.Stop()
	// Operator-state primitives (Mark fixed, exit rescue) are not instantaneous, and a failed
	// rescue transition can leave the Machine back in the rescue subsystem. Two failure modes
	// must be told apart: the Machine actively transitioning (MAAS "Entering/Exiting rescue
	// mode") is progress and must be waited out patiently, while a settled stuck state ("Rescue
	// mode" or "Failed to exit/enter rescue mode") needs the primitive (re)issued. Only settled
	// observations consume the retry budget, so slow but real hardware exits are not aborted.
	const recoverSettle = 60 * time.Second
	const maxExitAttempts = 5
	const maxMarkFixedAttempts = 3
	// A Machine that reports a settled failed rescue transition ("Failed to exit rescue mode")
	// will not leave rescue on its own, so recovery escalates to Mark broken (which the provider
	// still accepts from rescue) and then Mark fixes it to ready — the only path out for a
	// Machine whose exit-rescue and disk-erase both fail, and without a disk wipe. escalateGrace
	// bounds how long we wait for the escalation itself to take effect before pausing.
	const escalateGrace = 4 * time.Minute
	// Hard cap on total time in the rescue subsystem. A healthy exit leaves rescue (to a
	// non-rescue state) well within this window; a Machine that hangs in "Exiting rescue mode"
	// or keeps failing the transition exceeds it and is escalated to Mark broken. Without this
	// cap a provider that never settles the transition would hold the step until the 2h deadline.
	const rescueOverallGrace = 4 * time.Minute
	var exitAttempts, markFixedAttempts int
	var escalatedToBroken bool
	var escalatedAt, rescueSince, nextBrokenAt time.Time
	var nextExitAt, nextMarkFixedAt time.Time
	// The failed/deployed/allocated path returns to ready with a plain Release, but a Release can
	// itself fail — most importantly when a disk erase cannot complete ("Failed disk erasing"),
	// which leaves the Machine failed again with no progress on retry. When that happens, recovery
	// escalates to Mark broken -> Mark fixed, which returns the Machine to ready without a disk
	// erase, the same escape used for a stuck rescue. These track that one-shot escalation.
	var releaseAttempted, escalatedFailedToBroken bool
	var failedEscalatedAt, nextFailedBrokenAt time.Time
	for {
		state, err := e.refresh.Execute(ctx, serverID)
		if err != nil {
			if stop := wait.readFailed(ctx, err, time.Now()); stop != nil {
				return *stop
			}
		} else {
			now := time.Now()
			switch provisioningdomain.MachineStatus(state.State) {
			case provisioningdomain.MachineStatusReady:
				return providerSucceeded()
			case provisioningdomain.MachineStatusReleasing:
				// A Release is already converging this Server; follow it to Ready and reuse
				// the same static-IP cleanup contract as a direct Release.
				return e.observeRelease(ctx, step, serverID, input.UnbindStaticIPs, true)
			case provisioningdomain.MachineStatusFailed, provisioningdomain.MachineStatusDeployed,
				provisioningdomain.MachineStatusAllocated:
				// First, return to Ready with a plain Release (no erase). `allocated` (reserved but
				// not deployed) uses the same primitive as `failed`/`deployed`; without it a Server
				// parked at allocated — for example a verification borrow whose proving deploy never
				// finished — would loop here until the observation deadline instead of being released.
				if !releaseAttempted {
					releaseAttempted = true
					result := e.recoverViaRelease(ctx, step, serverID, input)
					if result.Status == workflowdomain.TaskSucceeded {
						return result
					}
					// Only a Release that ran and left the Machine failed (typically a disk erase that
					// cannot complete) is escalated; a locked/auth/timeout/cancel outcome is returned as
					// is so recovery does not mask an unrelated problem behind Mark broken.
					if result.Error == nil || result.Error.Code != "release_failed" {
						return result
					}
					break // Machine is still failed; escalate on the next iterations.
				}
				// Release already tried and the Machine is still failed: escalate to Mark broken, which
				// moves it to broken so the broken branch above Mark fixes it back to ready without a
				// disk erase. Bounded and one-shot, like the rescue escalation, so a truly stuck
				// provider pauses for attention rather than looping.
				if escalatedFailedToBroken {
					if now.Sub(failedEscalatedAt) >= escalateGrace {
						return providerAttention("recover_release_unsettled", "Release did not return the Server to Ready and Mark broken did not take effect. Inspect the provider before retrying.", "recover")
					}
					break // wait for Mark broken to move it to broken, then the broken branch runs
				}
				if !nextFailedBrokenAt.IsZero() && now.Before(nextFailedBrokenAt) {
					break // settle window from the last Mark broken attempt has not elapsed
				}
				ok, fatal := e.escalateMarkBroken(ctx, serverID)
				if fatal != nil {
					return *fatal
				}
				if ok {
					escalatedFailedToBroken = true
					failedEscalatedAt = now
				} else {
					nextFailedBrokenAt = now.Add(recoverSettle) // provider refused for now; retry
				}
			case provisioningdomain.MachineStatusBroken:
				if !nextMarkFixedAt.IsZero() && now.Before(nextMarkFixedAt) {
					break // still within the settle window from the last Mark fixed
				}
				if markFixedAttempts >= maxMarkFixedAttempts {
					return providerAttention("recover_mark_fixed_unsettled", "Mark fixed did not return the Server to Ready. Inspect the provider before retrying.", "recover")
				}
				if r := e.recoverOperatorPrimitive(ctx, serverID, provisioningdomain.RecoverPrimitiveMarkFixed); r != nil {
					return *r
				}
				markFixedAttempts++
				nextMarkFixedAt = now.Add(recoverSettle)
			case provisioningdomain.MachineStatusRescue:
				if rescueSince.IsZero() {
					rescueSince = now
				}
				// Escalate to Mark broken when the exit has settled into a failure, or when the
				// Machine has been anywhere in the rescue subsystem past the overall grace (it is
				// hung in a transition or repeatedly failing). Mark broken is best-effort: a
				// transient provider rejection is retried rather than failing the recovery.
				failedExit := rescueTransitionFailed(state.ProviderState)
				overGrace := now.Sub(rescueSince) >= rescueOverallGrace
				if !escalatedToBroken && (failedExit || overGrace) {
					if !nextBrokenAt.IsZero() && now.Before(nextBrokenAt) {
						break
					}
					ok, fatal := e.escalateMarkBroken(ctx, serverID)
					if fatal != nil {
						return *fatal
					}
					if ok {
						escalatedToBroken = true
						escalatedAt = now
					} else {
						nextBrokenAt = now.Add(recoverSettle) // provider refused for now; retry
					}
					break
				}
				if escalatedToBroken {
					if now.Sub(escalatedAt) >= escalateGrace {
						return providerAttention("recover_rescue_unsettled", "The Server did not leave rescue mode after escalation. Inspect the provider before retrying.", "recover")
					}
					break // wait for Mark broken to move it out of rescue, then the broken branch runs
				}
				if rescueTransitionInProgress(state.ProviderState) {
					break // actively entering/exiting rescue within grace: wait it out
				}
				if !nextExitAt.IsZero() && now.Before(nextExitAt) {
					break // settle window from the last exit attempt has not elapsed
				}
				if exitAttempts >= maxExitAttempts {
					break // grace-based escalation above will take over
				}
				if r := e.recoverOperatorPrimitive(ctx, serverID, provisioningdomain.RecoverPrimitiveExitRescue); r != nil {
					return *r
				}
				exitAttempts++
				nextExitAt = now.Add(recoverSettle)
			}
			if stop := wait.observed(ctx, providerStateObservation(wait.name, state, "returning it to Ready.")); stop != nil {
				return *stop
			}
		}
		select {
		case <-ctx.Done():
			return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskCanceled}
		case <-deadline.C:
			return wait.timedOut(providerAttention("provider_observation_timeout", "Timed out waiting for the Server to return to Ready.", "recover"),
				"Check the Server in the provisioner, then retry this Task; a retry observes again before it acts.")
		case <-ticker.C:
		}
	}
}

// rescueTransitionInProgress reports that the provider is actively moving a Machine into or
// out of rescue, as opposed to sitting in a settled rescue state. Both normalize to `rescue`,
// but only a settled state should have the exit primitive (re)issued; an in-flight transition
// is progress to wait out. It reads the provider's own label because the normalized state
// deliberately collapses these; the MAAS labels are stable ("Entering/Exiting rescue mode"),
// and a "failed" transition is treated as settled (needs a fresh exit), not in progress.
func rescueTransitionInProgress(providerState string) bool {
	label := strings.ToLower(providerState)
	if strings.Contains(label, "failed") {
		return false
	}
	return strings.Contains(label, "entering rescue") || strings.Contains(label, "exiting rescue")
}

// rescueTransitionFailed reports a settled failed rescue transition (the provider tried and
// failed to enter or exit rescue). Unlike an in-progress transition this will not resolve on
// its own, so recovery escalates rather than waiting. It reads the provider label because the
// normalized state deliberately collapses every rescue substate to `rescue`.
func rescueTransitionFailed(providerState string) bool {
	label := strings.ToLower(providerState)
	return strings.Contains(label, "failed") && strings.Contains(label, "rescue")
}

// recoverViaRelease performs the Release leg of a recovery and follows it to Ready. It
// re-checks the lock immediately before the mutation and reuses observeRelease so a
// recovery Release and a direct Release share one convergence and cleanup path.
func (e providerStepExecutor) recoverViaRelease(ctx context.Context, step temporalworkflow.StepExecutionInput, serverID string, input provisioningapp.RecoverServerInput) temporalworkflow.StepExecutionResult {
	if locked := e.requireUnlocked(ctx, serverID); locked != nil {
		return *locked
	}
	comment := fmt.Sprintf("Swallow recovery operation %s step %s attempt %d", step.OperationID, step.Step.ID, step.Step.Attempt)
	if input.Comment != "" {
		comment = input.Comment + " | " + comment
	}
	_, err := e.release.ExecuteWithOptions(ctx, provisioningapp.ReleaseServerInput{
		ServerID:        serverID,
		Comment:         comment,
		UnbindStaticIPs: input.UnbindStaticIPs,
		RequestID:       step.OperationID,
	})
	if err != nil {
		var providerErr *provisioningdomain.ProviderError
		if errors.As(err, &providerErr) && providerErr.Kind == provisioningdomain.ProviderErrorUnavailable {
			return e.observeRelease(ctx, step, serverID, input.UnbindStaticIPs, false)
		}
		return normalizeProviderError(err, "recover_release")
	}
	return e.observeRelease(ctx, step, serverID, input.UnbindStaticIPs, true)
}

// escalateMarkBroken issues Mark broken as a recovery escalation and reports whether it was
// accepted. It is best-effort: a provider that transiently refuses Mark broken (for example
// during a rescue transition) or is briefly unavailable yields ok=false with a nil result so
// the caller retries, rather than failing the whole recovery. A non-nil result means the Step
// must stop (missing capability, auth failure, or an unknown provider outcome).
func (e providerStepExecutor) escalateMarkBroken(ctx context.Context, serverID string) (bool, *temporalworkflow.StepExecutionResult) {
	if locked := e.requireUnlocked(ctx, serverID); locked != nil {
		return false, locked
	}
	server, err := e.servers.FindByID(ctx, serverID)
	if err != nil {
		result := normalizeProviderError(err, "recover")
		return false, &result
	}
	provider, err := e.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		result := normalizeProviderError(err, "recover")
		return false, &result
	}
	controller, ok := provider.(provisioningdomain.StateActionsController)
	if !ok {
		result := providerFailed("recover_unsupported", "This provisioner cannot recover the Server through state actions.", false).withStage("recover")
		return false, &result
	}
	if _, err := controller.MarkBroken(ctx, server.Source.ProviderMachineID); err != nil {
		var providerErr *provisioningdomain.ProviderError
		if errors.As(err, &providerErr) &&
			(providerErr.Kind == provisioningdomain.ProviderErrorRejected || providerErr.Kind == provisioningdomain.ProviderErrorUnavailable) {
			return false, nil // transient refusal; caller retries after a backoff
		}
		result := normalizeProviderError(err, "recover")
		return false, &result
	}
	return true, nil
}

// recoverOperatorPrimitive issues one state-action provider action (Mark fixed or exit
// rescue) during recovery. It re-checks the lock first and returns a non-nil result only
// when the recovery Step must stop; a nil result means the primitive was issued and the
// caller should keep observing.
func (e providerStepExecutor) recoverOperatorPrimitive(ctx context.Context, serverID string, primitive provisioningdomain.RecoverPrimitive) *temporalworkflow.StepExecutionResult {
	if locked := e.requireUnlocked(ctx, serverID); locked != nil {
		return locked
	}
	server, err := e.servers.FindByID(ctx, serverID)
	if err != nil {
		result := normalizeProviderError(err, "recover")
		return &result
	}
	provider, err := e.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		result := normalizeProviderError(err, "recover")
		return &result
	}
	controller, ok := provider.(provisioningdomain.StateActionsController)
	if !ok {
		result := providerFailed("recover_unsupported", "This provisioner cannot recover the Server through state actions.", false).withStage("recover")
		return &result
	}
	switch primitive {
	case provisioningdomain.RecoverPrimitiveMarkFixed:
		_, err = controller.MarkFixed(ctx, server.Source.ProviderMachineID)
	case provisioningdomain.RecoverPrimitiveMarkBroken:
		_, err = controller.MarkBroken(ctx, server.Source.ProviderMachineID)
	case provisioningdomain.RecoverPrimitiveExitRescue:
		_, err = controller.ExitRescueMode(ctx, server.Source.ProviderMachineID)
	default:
		result := providerFailed("recover_unsupported", "Unsupported recovery primitive.", false).withStage("recover")
		return &result
	}
	if err != nil {
		result := normalizeProviderError(err, "recover")
		return &result
	}
	return nil
}

// observeRelease follows a Release of the step's Server until the provisioner reports it Ready and
// its requested static IP cleanup finished, publishing each observation as the Task's wait
// observation. Failed provisioner reads are tolerated for providerReadErrorTimeout (providerWait).
func (e providerStepExecutor) observeRelease(ctx context.Context, step temporalworkflow.StepExecutionInput, serverID string, cleanupRequested, accepted bool) temporalworkflow.StepExecutionResult {
	deadline := time.NewTimer(providerObservationWait)
	defer deadline.Stop()
	ticker := time.NewTicker(e.pollInterval())
	defer ticker.Stop()
	wait := e.newProviderWait(ctx, step, serverID, "release", providerObservationWait)
	for {
		state, err := e.refresh.Execute(ctx, serverID)
		if err != nil {
			if stop := wait.readFailed(ctx, err, time.Now()); stop != nil {
				return *stop
			}
		} else {
			switch state.State {
			case string(provisioningdomain.MachineStatusReady):
				return e.observeReleaseCleanup(ctx, serverID, step.OperationID, cleanupRequested, false)
			case string(provisioningdomain.MachineStatusFailed), string(provisioningdomain.MachineStatusBroken):
				return e.releaseFailed(ctx, step, serverID)
			case string(provisioningdomain.MachineStatusDeployed):
				if !accepted {
					return providerAttention("provider_outcome_unknown", "The provider response was lost before Swallow could prove whether release started. Verify MAAS before retrying.", "release")
				}
			}
			if stop := wait.observed(ctx, providerStateObservation(wait.name, state, "waiting for it to return to Ready.")); stop != nil {
				return *stop
			}
		}
		select {
		case <-ctx.Done():
			if !temporalworkflow.LeavesExternalWorkRunning(ctx) {
				e.abort(serverID)
			}
			return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskCanceled}
		case <-deadline.C:
			return wait.timedOut(providerAttention("provider_observation_timeout", "Timed out waiting for the Server to return to Ready.", "release"),
				"Check the Release in the provisioner, then retry this Task; a retry observes again before it acts.")
		case <-ticker.C:
		}
	}
}

// observeReleaseCleanup finishes the static IP cleanup a Release recorded for operationID once the
// Server is Ready, advancing it from inside the release Task. It is bounded by
// releaseCleanupTimeout: each cleanup step is one provisioner call, so a cleanup that has not
// finished by then asks for attention with the cleanup's last phase instead of holding the Task.
func (e providerStepExecutor) observeReleaseCleanup(ctx context.Context, serverID, operationID string, cleanupRequested, retryFailed bool) temporalworkflow.StepExecutionResult {
	if !cleanupRequested {
		return providerSucceeded()
	}
	ticker := time.NewTicker(e.pollInterval())
	defer ticker.Stop()
	deadline := time.NewTimer(e.releaseCleanupTimeout())
	defer deadline.Stop()
	wait := workflowdomain.NewWaitProgress(time.Now(), e.releaseCleanupTimeout())
	for {
		tasks, err := e.tasks.ListByServer(ctx, serverID)
		if err != nil {
			return providerAttention("cleanup_observation_failed", "Static IP cleanup could not be observed.", "network_cleanup")
		}
		var cleanup *provisioningdomain.ProvisioningTask
		for _, task := range tasks {
			if task.Kind == provisioningdomain.ProvisioningTaskReleaseNetworkCleanup && task.RequestID == operationID {
				cleanup = task
				break
			}
		}
		if cleanup == nil {
			return providerAttention("cleanup_task_missing", "Release reached Ready, but its static IP cleanup task is unavailable.", "network_cleanup")
		}
		switch cleanup.Status {
		case provisioningdomain.ProvisioningTaskSucceeded:
			return providerSucceeded()
		case provisioningdomain.ProvisioningTaskFailed:
			if retryFailed && cleanup.Phase != provisioningdomain.ProvisioningTaskWaitingForRelease {
				if err := e.cleanup.Retry(ctx, cleanup); err != nil {
					return providerFailed("cleanup_retry_failed", "Static IP cleanup could not be retried.", true).withStage("network_cleanup")
				}
				retryFailed = false
				continue
			}
			return providerFailed("network_cleanup_failed", cleanup.Error, cleanup.Phase != provisioningdomain.ProvisioningTaskWaitingForRelease).withStage("network_cleanup")
		default:
			if err := e.cleanup.Advance(ctx, cleanup); err != nil {
				return providerAttention("cleanup_observation_failed", "Static IP cleanup could not be recorded.", "network_cleanup")
			}
			if cleanup.Status == provisioningdomain.ProvisioningTaskSucceeded || cleanup.Status == provisioningdomain.ProvisioningTaskFailed {
				continue
			}
		}
		wait.Observe(fmt.Sprintf("The static IP cleanup is %s in phase %s.", cleanup.Status, cleanup.Phase), time.Now())
		select {
		case <-ctx.Done():
			return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskCanceled}
		case <-deadline.C:
			now := time.Now()
			diagnostic := wait.Diagnostic(workflowdomain.FailureTransient,
				fmt.Errorf("%w; the cleanup was %s in phase %s", workflowdomain.ErrWaitBudgetExhausted, cleanup.Status, cleanup.Phase), now)
			diagnostic.Target = serverTarget(serverID)
			diagnostic.Remediation = "Check the Server's static IP bindings on its Networking tab and in the provisioner, then retry this Task to finish the cleanup."
			return withDiagnostic(providerAttention("network_cleanup_timeout",
				fmt.Sprintf("The Server is Ready, but its static IP cleanup did not finish within %s.", e.releaseCleanupTimeout()),
				"network_cleanup"), diagnostic)
		case <-ticker.C:
		}
	}
}

func (e providerStepExecutor) abort(serverID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	server, err := e.servers.FindByID(ctx, serverID)
	if err != nil {
		return
	}
	provider, err := e.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return
	}
	if actions, ok := provider.(provisioningdomain.HardwareValidator); ok {
		_, _ = actions.Abort(ctx, server.Source.ProviderMachineID)
	}
}

func (e providerStepExecutor) pollInterval() time.Duration {
	if e.poll <= 0 {
		return 5 * time.Second
	}
	return e.poll
}

func decodeStepRequest(parameters map[string]any, target any) error {
	request, ok := parameters["request"]
	if !ok {
		return errors.New("the provider Step has no request snapshot")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return errors.New("the provider Step request could not be decoded")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return errors.New("the provider Step request has an invalid format")
	}
	return nil
}

func firstTargetServer(step workflowdomain.Task) string {
	for _, target := range step.Targets {
		if target.Kind == "server" {
			return target.ID
		}
	}
	return ""
}

func imageMatches(state *provisioningapp.ProvisioningStateItem, input provisioningapp.DeployServersInput) bool {
	if input.Settings.ImageID == nil || strings.TrimSpace(*input.Settings.ImageID) == "" {
		return false
	}
	wanted := strings.ToLower(strings.TrimSpace(*input.Settings.ImageID))
	actual := strings.ToLower(strings.TrimSpace(state.DistroSeries))
	imageMatches := actual == wanted || strings.HasSuffix(wanted, "/"+actual)
	if !imageMatches {
		return false
	}
	return input.Settings.Ephemeral == nil || state.Ephemeral == *input.Settings.Ephemeral
}

// installationFailureReadWait bounds the best-effort installation log read on a failed
// deployment, so a slow provisioner cannot delay the Step outcome.
const installationFailureReadWait = 30 * time.Second

// deploymentFailed is the retryable deployment_failed outcome of a deployment the provisioner
// failed (DF-82). Its message names the provisioner's error description and this attempt's
// installation error line (deploymentFailureMessage); its diagnostic carries both as the cause, and
// the events the provisioner recorded about the machine during the attempt, which is the evidence
// a user reads before retrying. The failure is classified permanent: an installation fails the same
// way until its cause (the image, the storage layout, a package mirror) is fixed.
func (e providerStepExecutor) deploymentFailed(ctx context.Context, step temporalworkflow.StepExecutionInput, serverID string) temporalworkflow.StepExecutionResult {
	message, cause := e.deploymentFailureMessage(ctx, step, serverID)
	diagnostic := workflowdomain.NewTaskDiagnostic(workflowdomain.FailurePermanent, nil)
	diagnostic.Cause = redact.Text(cause)
	diagnostic.Target = serverTarget(serverID)
	diagnostic.ProviderEvents = e.providerEventsSince(ctx, step, serverID)
	diagnostic.Remediation = "Read the installation error and the provisioner's events, fix their cause (the image, the storage layout, the package mirror), then retry this Task: it returns the Server to Ready and deploys it again."
	return withDiagnostic(providerFailed("deployment_failed", message, true).withStage("deployment"), diagnostic)
}

// deploymentFailureMessage names why the provisioner failed the OS deployment: its error
// description, then the most specific line of this attempt's installation log, each when
// available. Either read failing only shortens the message; the Step outcome never changes. cause
// is the same explanation for a diagnostic, empty when the provisioner explained nothing.
func (e providerStepExecutor) deploymentFailureMessage(ctx context.Context, step temporalworkflow.StepExecutionInput, serverID string) (message, cause string) {
	const generic = "The provisioner reported that OS deployment failed"
	server, err := e.servers.FindByID(ctx, serverID)
	if err != nil {
		return generic + ".", ""
	}
	message = generic
	var causes []string
	if description := provisioningErrorDescription(server); description != "" {
		message += ": " + description
		causes = append(causes, "provisioner error description: "+description)
	}
	message += "."
	if line := e.installationFailure(ctx, step, server); line != "" {
		message += " Installation error: " + line
		causes = append(causes, "installation error: "+line)
	}
	return message, strings.Join(causes, "; ")
}

// releaseFailed is the retryable release_failed outcome of a Release the provisioner failed
// (DF-82): its message names the provisioner's error description, and its diagnostic carries it
// with the events the provisioner recorded about the machine during the attempt. It is classified
// permanent: the usual cause, a disk erase that cannot complete, fails the same way again.
func (e providerStepExecutor) releaseFailed(ctx context.Context, step temporalworkflow.StepExecutionInput, serverID string) temporalworkflow.StepExecutionResult {
	message := "The provisioner reported that release failed."
	diagnostic := workflowdomain.NewTaskDiagnostic(workflowdomain.FailurePermanent, nil)
	if server, err := e.servers.FindByID(ctx, serverID); err == nil {
		if description := provisioningErrorDescription(server); description != "" {
			message = "The provisioner reported that release failed: " + description + "."
			diagnostic.Cause = redact.Text("provisioner error description: " + description)
		}
	}
	diagnostic.Target = serverTarget(serverID)
	diagnostic.ProviderEvents = e.providerEventsSince(ctx, step, serverID)
	diagnostic.Remediation = "Read the provisioner's error and events (a disk erase that cannot complete is the usual cause), then retry this Task, or Recover the Server, which returns it to Ready without erasing its disks."
	return withDiagnostic(providerFailed("release_failed", message, true).withStage("release"), diagnostic)
}

// provisioningErrorDescription is the provisioner's error description the Server's last refresh
// mirrored, without a trailing period, or "".
func provisioningErrorDescription(server *serverdomain.Server) string {
	if server.Provisioning == nil {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(server.Provisioning.ErrorDescription), ".")
}

// providerEventsSince reads the events the provisioner recorded about the Server's machine since
// the attempt started (every event it returns when the start is unknown), oldest first, for a
// diagnostic's evidence. It is best-effort and bounded by installationFailureReadWait: a
// provisioner without an event stream, or a read that fails, yields none. An event whose time
// cannot be parsed is kept only when there is no start to compare it with.
func (e providerStepExecutor) providerEventsSince(ctx context.Context, step temporalworkflow.StepExecutionInput, serverID string) []workflowdomain.ProviderEvent {
	if e.servers == nil || e.providers == nil {
		return nil
	}
	readCtx, cancel := context.WithTimeout(ctx, installationFailureReadWait)
	defer cancel()
	events, supported, err := e.readMachineEvents(readCtx, serverID)
	if !supported || err != nil {
		return nil
	}
	var since time.Time
	if step.Step.StartedAt != nil {
		since = *step.Step.StartedAt
	}
	evidence := make([]workflowdomain.ProviderEvent, 0, len(events))
	for _, event := range events {
		item := workflowdomain.ProviderEvent{Type: strings.TrimSpace(event.Type), Description: strings.TrimSpace(event.Description)}
		at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(event.OccurredAt))
		switch {
		case err == nil && at.Before(since):
			continue
		case err == nil:
			item.At = &at
		case !since.IsZero():
			continue
		}
		evidence = append(evidence, item)
	}
	slices.SortStableFunc(evidence, func(a, b workflowdomain.ProviderEvent) int {
		switch {
		case a.At == nil && b.At == nil:
			return 0
		case a.At == nil:
			return -1
		case b.At == nil:
			return 1
		default:
			return a.At.Compare(*b.At)
		}
	})
	return evidence
}

// installationFailure reads the error line of this attempt's failed installation when the
// provisioner can explain one. An installation that started before the Step attempt belongs to
// an earlier deployment and is never reported as this one's cause.
func (e providerStepExecutor) installationFailure(ctx context.Context, step temporalworkflow.StepExecutionInput, server *serverdomain.Server) string {
	if e.providers == nil {
		return ""
	}
	provider, err := e.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return ""
	}
	reader, ok := provider.(provisioningdomain.InstallationFailureReader)
	if !ok {
		return ""
	}
	var since time.Time
	if step.Step.StartedAt != nil {
		since = *step.Step.StartedAt
	}
	readCtx, cancel := context.WithTimeout(ctx, installationFailureReadWait)
	defer cancel()
	line, err := reader.LatestInstallationFailure(readCtx, server.Source.ProviderMachineID, since)
	if err != nil {
		return ""
	}
	return line
}

// clearReplacedDefaultUser clears the Server Default User right before a deployment is submitted:
// an account set on the Server belongs to the OS that deployment replaces (decision 039), and the
// new OS's readiness check and every later run must log in as the new image's default user. It is
// never called when a Step only observes an OS that already converged, so a rerun or Job retry of
// a deployed Server keeps the user's account. A projection write failure asks for attention.
func (e providerStepExecutor) clearReplacedDefaultUser(ctx context.Context, serverID string) *temporalworkflow.StepExecutionResult {
	if err := e.servers.SetDefaultUser(ctx, serverID, ""); err != nil {
		attention := providerAttention("deployment_projection_unavailable", err.Error(), "deployment_projection")
		return &attention
	}
	return nil
}

// deploysFromMemory reports a frozen memory (RAM) deployment; a frozen intent without the
// setting resolves to a disk deployment, as the deployment preflight does.
func deploysFromMemory(input provisioningapp.DeployServersInput) bool {
	return input.Settings.Ephemeral != nil && *input.Settings.Ephemeral
}

type providerResult struct {
	temporalworkflow.StepExecutionResult
}

func providerSucceeded() temporalworkflow.StepExecutionResult {
	return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskSucceeded, Progress: 100}
}

func providerFailed(code, message string, retryable bool) providerResult {
	if strings.TrimSpace(message) == "" {
		message = "The provider Step failed."
	}
	return providerResult{temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskFailed,
		Error: &workflowdomain.NormalizedError{Code: code, Message: message, Retryable: retryable}}}
}

func providerAttention(code, message, stage string) temporalworkflow.StepExecutionResult {
	return temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskRequiresAttention,
		Error: &workflowdomain.NormalizedError{Code: code, Message: message, Retryable: true, Stage: stage}}
}

func (r providerResult) withStage(stage string) temporalworkflow.StepExecutionResult {
	if r.Error != nil {
		r.Error.Stage = stage
	}
	return r.StepExecutionResult
}

// requireUnlocked re-checks the provider-owned Server Lock immediately before a host
// mutation. An acceptance-time lock check can go stale while an Operation waits in a
// durable queue, so every side-effecting provider call revalidates that the target is
// unlocked. It fails closed: a lock read that cannot be confirmed blocks the mutation.
// The Step pauses for user attention (retryable) rather than failing terminally, so
// the user can unlock and retry the same Step.
func (e providerStepExecutor) requireUnlocked(ctx context.Context, serverID string) *temporalworkflow.StepExecutionResult {
	if serverID == "" {
		return nil
	}
	if err := e.protection.RequireUnlocked(ctx, []string{serverID}); err != nil {
		result := providerAttention("target_locked", err.Error(), "lock_precheck")
		return &result
	}
	return nil
}

// normalizeProviderError maps a failed provisioner call onto the Task's outcome, with the
// diagnostic providerErrorDiagnostic builds from the error.
func normalizeProviderError(err error, stage string) temporalworkflow.StepExecutionResult {
	return withDiagnostic(providerErrorResult(err, stage), providerErrorDiagnostic(err))
}

// providerErrorResult is the code, message, status, and retryability of a failed provisioner call.
func providerErrorResult(err error, stage string) temporalworkflow.StepExecutionResult {
	// The Deployment Key could not be registered before any provider write, so the outcome is
	// known (nothing was deployed) and retrying once the provisioner recovers is safe.
	if errors.Is(err, provisioningdomain.ErrSSHKeyRegistration) {
		return providerAttention("ssh_key_registration_failed", err.Error(), stage)
	}
	// Reached only if the Deployment Key disappeared after acceptance; nothing was written to the
	// provider, so the user can create the key and retry the Task.
	if errors.Is(err, provisioningdomain.ErrDeploymentKeyMissing) {
		return providerAttention("deployment_key_missing", err.Error(), stage)
	}
	// swallow's own deployment preflight refused the request before any provider write (a target
	// that is not ready, an image that is not staged or verified, a network conflict), so the
	// outcome is known. Reporting it as an unknown outcome invited the user to retry the same
	// refusal forever; ending the Step lets the Operation finish and free its Servers so the cause
	// can be fixed and the work started again.
	if errors.Is(err, provisioningdomain.ErrDeploymentBatchConflict) {
		return providerFailed("deployment_preflight_rejected", err.Error(), false).withStage(stage)
	}
	var providerErr *provisioningdomain.ProviderError
	if errors.As(err, &providerErr) {
		switch providerErr.Kind {
		case provisioningdomain.ProviderErrorAuth:
			return providerFailed("provider_auth", providerErr.Detail, false).withStage(stage)
		case provisioningdomain.ProviderErrorUnavailable:
			return providerAttention("provider_unavailable", providerErr.Detail, stage)
		default:
			return providerFailed("provider_rejected", providerErr.Detail, false).withStage(stage)
		}
	}
	// An unclassified error from a mutating provider call is an unknown outcome, not a
	// safe retry: the request may have reached the provider and taken effect before the
	// transport failed. Reconcile under user attention instead of auto-retrying,
	// which could duplicate a deploy or release side effect.
	return providerAttention("provider_outcome_unknown", err.Error(), stage)
}

// providerErrorDiagnostic explains a failed provisioner call for a Task diagnostic: its Failure
// Class, the redacted cause, and for a ProviderError the endpoint and operation the adapter
// recorded. Rejected credentials, a refused request, a missing provisioner Integration or
// credential, a machine the provisioner no longer has, and swallow's own refusals before any
// provisioner write repeat until something changes (permanent); an unreachable provisioner is
// expected to clear (transient); anything else is unknown. The caller adds the target.
func providerErrorDiagnostic(err error) *workflowdomain.TaskDiagnostic {
	class := workflowdomain.FailureUnknown
	var providerErr *provisioningdomain.ProviderError
	switch {
	case errors.As(err, &providerErr) && providerErr.Kind == provisioningdomain.ProviderErrorUnavailable:
		class = workflowdomain.FailureTransient
	case errors.As(err, &providerErr),
		errors.Is(err, sitedomain.ErrCredentialNotSet), errors.Is(err, sitedomain.ErrIntegrationNotFound),
		errors.Is(err, provisioningdomain.ErrIntegrationNotProvisioner), errors.Is(err, provisioningdomain.ErrMachineNotFound),
		errors.Is(err, provisioningdomain.ErrDeploymentKeyMissing), errors.Is(err, provisioningdomain.ErrDeploymentBatchConflict):
		class = workflowdomain.FailurePermanent
	}
	diagnostic := workflowdomain.NewTaskDiagnostic(class, nil)
	diagnostic.Cause = causeChain(err)
	if providerErr != nil {
		diagnostic.Endpoint, diagnostic.Operation = providerErr.Endpoint, providerErr.Operation
	}
	switch {
	case providerCredentialRejected(err):
		diagnostic.Remediation = "Correct the credential of the Server's provisioner Integration, then retry this Task."
	case class == workflowdomain.FailureTransient:
		diagnostic.Remediation = "Check that the provisioner is running and reachable from swallow's worker, then retry this Task."
	}
	return diagnostic
}

// providerCredentialRejected reports a provisioner that rejected swallow's credential, or an
// Integration that holds none.
func providerCredentialRejected(err error) bool {
	var providerErr *provisioningdomain.ProviderError
	return errors.As(err, &providerErr) && providerErr.Kind == provisioningdomain.ProviderErrorAuth ||
		errors.Is(err, sitedomain.ErrCredentialNotSet)
}

// providerWait is the bookkeeping of one loop that observes a Server's provisioner state (decision
// 077). It publishes what the loop observes as the Task's wait observation and classifies the
// loop's failed reads: a permanent one (providerErrorDiagnostic) ends the loop at once, and any
// other is tolerated until reads have failed for providerReadErrorTimeout in a row, because a
// failed read says nothing about the Server. The loop keeps its own state machine and deadline. A
// providerWait belongs to one loop on one goroutine.
type providerWait struct {
	step     temporalworkflow.StepExecutionInput
	serverID string
	// name is the Server's display name in observations and messages.
	name  string
	stage string
	// progress follows the whole loop within its budget; reads follows the current run of
	// consecutive failed reads and is nil while reads succeed.
	progress    *workflowdomain.WaitProgress
	reads       *workflowdomain.WaitProgress
	readTimeout time.Duration
}

// newProviderWait starts the bookkeeping of a loop over serverID's provisioner state that may run
// for budget; stage is the stage of the attention results it builds.
func (e providerStepExecutor) newProviderWait(ctx context.Context, step temporalworkflow.StepExecutionInput, serverID, stage string, budget time.Duration) *providerWait {
	name := serverID
	if e.servers != nil {
		if server, err := e.servers.FindByID(ctx, serverID); err == nil {
			name = server.DisplayName()
		}
	}
	return &providerWait{
		step: step, serverID: serverID, name: name, stage: stage,
		progress: workflowdomain.NewWaitProgress(time.Now(), budget), readTimeout: e.providerReadErrorTimeout(),
	}
}

// resumedDeploymentState reads serverID's provisioner state for a resumed provision-os attempt,
// which observes before it acts. A failed read is tolerated as an observation loop's is: the
// earlier activity of this attempt may already have submitted the deployment, so acting without
// a read would submit it twice. The result is set when the read could not be made (attention, a
// superseded run, or cancellation).
func (e providerStepExecutor) resumedDeploymentState(ctx context.Context, step temporalworkflow.StepExecutionInput, serverID string) (*provisioningapp.ProvisioningStateItem, *temporalworkflow.StepExecutionResult) {
	wait := e.newProviderWait(ctx, step, serverID, "deployment", 0)
	ticker := time.NewTicker(e.pollInterval())
	defer ticker.Stop()
	for {
		state, err := e.refresh.Execute(ctx, serverID)
		if err == nil {
			return state, nil
		}
		if stop := wait.readFailed(ctx, err, time.Now()); stop != nil {
			return nil, stop
		}
		select {
		case <-ctx.Done():
			return nil, &temporalworkflow.StepExecutionResult{Status: workflowdomain.TaskCanceled}
		case <-ticker.C:
		}
	}
}

// observed records a successful read, which ends a run of failed reads, and publishes observation.
// It returns the result that ends the loop when a later dispatch superseded the run, so the loop
// stops before its next provisioner call; nil otherwise.
func (w *providerWait) observed(ctx context.Context, observation string) *temporalworkflow.StepExecutionResult {
	w.reads = nil
	return w.publish(ctx, observation, time.Now())
}

// readFailed records the failed read err at now and returns the result that ends the loop, or nil
// to keep observing. A permanent failure asks for attention at once: the deployment or release the
// provisioner runs is not known to have failed, but swallow cannot observe it until a user fixes
// the cause. A read that failed because ctx ended is not a provisioner failure; the loop's select
// handles the cancel.
func (w *providerWait) readFailed(ctx context.Context, err error, now time.Time) *temporalworkflow.StepExecutionResult {
	if ctx.Err() != nil {
		return nil
	}
	diagnostic := providerErrorDiagnostic(err)
	diagnostic.Target = serverTarget(w.serverID)
	detail := providerReadDetail(err)
	if diagnostic.FailureClass == workflowdomain.FailurePermanent {
		code := "provider_observation_failed"
		diagnostic.Remediation = "Check the Server's provisioner Integration and its machine in the provisioner, then retry this Task; it observes the Server again before it acts."
		if providerCredentialRejected(err) {
			code = "provider_auth"
			diagnostic.Remediation = "Correct the credential of the Server's provisioner Integration, then retry this Task; it observes the Server again before it acts."
		}
		result := withDiagnostic(providerAttention(code, fmt.Sprintf("Swallow cannot observe %s in the provisioner: %s", w.name, detail), w.stage), diagnostic)
		return &result
	}
	if w.reads == nil {
		w.reads = workflowdomain.NewWaitProgress(now, w.readTimeout)
	}
	observation := fmt.Sprintf("Swallow cannot read %s from the provisioner: %s", w.name, detail)
	w.reads.Observe(observation, now)
	if w.reads.Exhausted(now) {
		exhausted := w.reads.Diagnostic(diagnostic.FailureClass, nil, now)
		exhausted.Cause = workflowdomain.ErrWaitBudgetExhausted.Error() + "; last read error: " + diagnostic.Cause
		exhausted.Endpoint, exhausted.Operation, exhausted.Target = diagnostic.Endpoint, diagnostic.Operation, diagnostic.Target
		exhausted.Remediation = "Check that the provisioner is running and reachable from swallow's worker, then retry this Task; it observes the Server again before it acts."
		result := withDiagnostic(providerAttention("provider_unavailable",
			fmt.Sprintf("Swallow could not read %s from the provisioner for %s: %s", w.name, w.readTimeout, detail), w.stage), exhausted)
		return &result
	}
	return w.publish(ctx, observation, now)
}

// timedOut attaches to result the diagnostic of the loop's own budget running out: the last
// observation, the attempts, the elapsed time, and the budget. The provisioner was still working
// when the budget ended, so the class is transient.
func (w *providerWait) timedOut(result temporalworkflow.StepExecutionResult, remediation string) temporalworkflow.StepExecutionResult {
	diagnostic := w.progress.Diagnostic(workflowdomain.FailureTransient, workflowdomain.ErrWaitBudgetExhausted, time.Now())
	diagnostic.Target, diagnostic.Remediation = serverTarget(w.serverID), remediation
	return withDiagnostic(result, diagnostic)
}

// publish counts observation in the loop's wait and publishes it; see observed for the result.
func (w *providerWait) publish(ctx context.Context, observation string, now time.Time) *temporalworkflow.StepExecutionResult {
	if publishWaiting(ctx, w.step, w.progress.Observe(observation, now)) {
		result := supersededResult()
		return &result
	}
	return nil
}

// providerStateObservation is the wait observation of one provisioner read: the provisioner's own
// label of the state when it has one ("Failed disk erasing"), else swallow's normalized state, and
// what the Task waits for.
func providerStateObservation(name string, state *provisioningapp.ProvisioningStateItem, waitingFor string) string {
	return fmt.Sprintf("The provisioner reports %s as %s; %s", name, providerStateDetail(state.ProviderState, state.State), waitingFor)
}

// providerReadDetail is what a user reads of a failed provisioner read: the adapter's Detail of a
// ProviderError, which carries no credential or internal URL, else the redacted error.
func providerReadDetail(err error) string {
	var providerErr *provisioningdomain.ProviderError
	if errors.As(err, &providerErr) && strings.TrimSpace(providerErr.Detail) != "" {
		return strings.TrimSpace(providerErr.Detail)
	}
	return redact.Error(err)
}

// withDiagnostic attaches diagnostic to the error of a failed or attention result.
func withDiagnostic(result temporalworkflow.StepExecutionResult, diagnostic *workflowdomain.TaskDiagnostic) temporalworkflow.StepExecutionResult {
	if result.Error != nil {
		result.Error.Diagnostic = diagnostic
	}
	return result
}

// withServerTarget names the step's Server as the target of a diagnostic that names none.
func withServerTarget(result temporalworkflow.StepExecutionResult, step workflowdomain.Task) temporalworkflow.StepExecutionResult {
	if result.Error == nil || result.Error.Diagnostic == nil || result.Error.Diagnostic.Target != nil {
		return result
	}
	if serverID := firstTargetServer(step); serverID != "" {
		result.Error.Diagnostic.Target = serverTarget(serverID)
	}
	return result
}

func serverTarget(serverID string) *workflowdomain.ResourceReference {
	return &workflowdomain.ResourceReference{Kind: "server", ID: serverID}
}

func (e providerStepExecutor) providerReadErrorTimeout() time.Duration {
	if e.providerReadErrorWait > 0 {
		return e.providerReadErrorWait
	}
	return defaultProviderReadErrorWait
}

func (e providerStepExecutor) releaseCleanupTimeout() time.Duration {
	if e.releaseCleanupWait > 0 {
		return e.releaseCleanupWait
	}
	return defaultReleaseCleanupWait
}
