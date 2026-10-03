package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/operation/infra/temporalworkflow"
	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// providerStepExecutor translates durable Step intent into Swallow provisioning use
// cases, then observes provider state until the requested end state is proven.
type providerStepExecutor struct {
	deployments    *provisioningapp.DeployServersUseCase
	release        *provisioningapp.ReleaseServerUseCase
	refresh        *provisioningapp.RefreshServerUseCase
	servers        serverdomain.ServerRepository
	providers      provisioningdomain.ProviderFactory
	secrets        operationdomain.OperationSecretRepository
	tasks          provisioningdomain.ProvisioningTaskRepository
	configurations operationdomain.AutomationConfigurationRepository
	protection     serverdomain.MutationGuard
	poll           time.Duration
	readinessWait  time.Duration
	powerOnWait    time.Duration
	stageStallWait time.Duration
	sshProbe       func(context.Context, string, int) error
}

const (
	defaultDeploymentReadinessWait = 20 * time.Minute
	defaultDeploymentPowerOnWait   = 10 * time.Minute
	// defaultDeploymentStageStallWait is how long a deploying Machine may go without a new
	// provider event before the Step asks for operator attention. Healthy MAAS stages advance
	// within minutes (the longest, Configuring OS, typically under ten), so the window is wide
	// enough for a slow mirror yet far shorter than the two-hour observation timeout. It must
	// stay longer than defaultDeploymentPowerOnWait so a powered-off Machine keeps its more
	// specific power-on diagnosis.
	defaultDeploymentStageStallWait = 25 * time.Minute
)

func (e providerStepExecutor) Execute(ctx context.Context, input temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	switch input.Step.Kind {
	case "provision-os":
		serverID := firstTargetServer(input.Step)
		if serverID == "" {
			return e.deploy(ctx, input)
		}
		if err := e.setDeployment(ctx, input, serverID, serverdomain.DeploymentDeploying, nil, false); err != nil {
			return providerAttention("deployment_projection_unavailable", err.Error(), "deployment_projection")
		}
		// A Server Default User set on the Server belongs to the OS being replaced (decision 045).
		// Clear it before the deploy so the new OS's readiness check and every later run log in as
		// the new image's default user rather than an account that may not exist there.
		if err := e.servers.SetDefaultUser(ctx, serverID, ""); err != nil {
			return providerAttention("deployment_projection_unavailable", err.Error(), "deployment_projection")
		}
		result := e.deploy(ctx, input)
		if temporalworkflow.IsActivityWorkerStopping(ctx) {
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
		if result.Status == operationdomain.TaskSucceeded {
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
		if result.Status == operationdomain.TaskSucceeded {
			serverID := firstTargetServer(input.Step)
			if serverID != "" {
				if err := e.servers.SetDeployment(ctx, serverID, nil); err != nil {
					return providerAttention("deployment_projection_unavailable", err.Error(), "deployment_projection")
				}
			}
		}
		return result
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
func (e providerStepExecutor) hardenVerificationResult(step operationdomain.Task, result temporalworkflow.StepExecutionResult) temporalworkflow.StepExecutionResult {
	if result.Status != operationdomain.TaskRequiresAttention && result.Status != operationdomain.TaskFailed {
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
	result.Status = operationdomain.TaskFailed
	return result
}

func (e providerStepExecutor) finishDeployment(ctx context.Context, input temporalworkflow.StepExecutionInput, serverID string, result temporalworkflow.StepExecutionResult) error {
	state := serverdomain.DeploymentDeploying
	switch result.Status {
	case operationdomain.TaskSucceeded:
		state = serverdomain.DeploymentSucceeded
	case operationdomain.TaskFailed:
		state = serverdomain.DeploymentFailed
	case operationdomain.TaskRequiresAttention:
		state = serverdomain.DeploymentRequiresAttention
	case operationdomain.TaskCanceled:
		state = serverdomain.DeploymentCanceled
	default:
		return nil
	}
	return e.setDeployment(ctx, input, serverID, state, result.Error, true)
}

func (e providerStepExecutor) setDeployment(ctx context.Context, input temporalworkflow.StepExecutionInput, serverID string, state serverdomain.DeploymentState, failure *operationdomain.NormalizedError, finished bool) error {
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
	if step.Step.Attempt > 1 || step.ActivityAttempt > 1 {
		state, err := e.refresh.Execute(ctx, serverID)
		if err == nil && state.State == string(provisioningdomain.MachineStatusDeployed) && imageMatches(state, input) {
			readiness, inspectErr := e.inspectDeploymentReadiness(ctx, serverID, step.SiteID)
			if inspectErr != nil {
				return providerAttention("deployment_readiness_unavailable", inspectErr.Error(), "ssh_readiness")
			}
			if readiness.reachable {
				return providerSucceeded()
			}
			if len(readiness.addresses) == 0 {
				return e.recoverDeploymentWithoutAddress(ctx, step, serverID, input)
			}
			return e.observeDeploy(ctx, step, serverID, input, true)
		}
		if err == nil && state.State == string(provisioningdomain.MachineStatusDeploying) {
			return e.observeDeploy(ctx, step, serverID, input, true)
		}
	}
	if locked := e.requireUnlocked(ctx, serverID); locked != nil {
		return *locked
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

func (e providerStepExecutor) observeDeploy(ctx context.Context, step temporalworkflow.StepExecutionInput, serverID string, input provisioningapp.DeployServersInput, accepted bool) temporalworkflow.StepExecutionResult {
	deadline := time.NewTimer(2 * time.Hour)
	defer deadline.Stop()
	ticker := time.NewTicker(e.pollInterval())
	defer ticker.Stop()
	var readinessDeadline time.Time
	var deployingSince time.Time
	var allocatedSince time.Time
	var lastReadiness deploymentReadiness
	progress := e.newDeploymentProgress(ctx, step, serverID)
	for {
		state, err := e.refresh.Execute(ctx, serverID)
		if err == nil {
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
						return providerAttention(
							"deployment_power_on_timeout",
							"The provisioner still reports Deploying, but the Server remained powered off for 10 minutes. Check the provider deployment and power workflow before retrying.",
							"deployment_power_on",
						)
					}
					// A live-on, unknown, or temporarily unavailable power reading is not
					// proof of a provider stall. Delay the next diagnostic query while the
					// outer two-hour deployment observation remains authoritative.
					deployingSince = now
				}
				if stalled := e.observeInstallProgress(ctx, step, serverID, &progress, now); stalled != nil {
					return *stalled
				}
			case string(provisioningdomain.MachineStatusAllocated):
				// ALLOCATED is the brief reserved state MAAS passes through on its way to
				// DEPLOYING. Lingering here after an accepted deploy means the provider reserved
				// the Machine but never started installing (for example a power or scheduling
				// stall), so surface it for operator attention past a grace window instead of
				// silently waiting out the two-hour observation. The Machine is left allocated,
				// which is now a Recover/Release source, so the operator can return it to ready.
				now := time.Now()
				if allocatedSince.IsZero() {
					allocatedSince = now
				} else if !now.Before(allocatedSince.Add(e.powerOnTimeout())) {
					return providerAttention(
						"deployment_reserved_not_started",
						"The provisioner reserved the Server (allocated) but did not start the OS installation. Check the provider deployment before retrying.",
						"deployment",
					)
				}
			case string(provisioningdomain.MachineStatusDeployed):
				deployingSince = time.Time{}
				allocatedSince = time.Time{}
				if imageMatches(state, input) {
					if readinessDeadline.IsZero() {
						// Record why the deployment sits in "verifying": the provider installed the
						// OS but Swallow has not yet confirmed the host is reachable over SSH. Carrying
						// this as a non-terminal reason (empty code, so it is not a failure) gives the
						// UI something better than a silent, frozen "verifying" during the readiness
						// wait — which can be minutes for an image that boots from RAM but not disk.
						waiting := &operationdomain.NormalizedError{
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
				}
				if !accepted {
					return providerAttention("provider_outcome_unknown", "The provider response was lost and the Server is deployed with a different image. Verify MAAS before retrying.", "deployment")
				}
			case string(provisioningdomain.MachineStatusFailed), string(provisioningdomain.MachineStatusBroken):
				return providerFailed("deployment_failed", "The provisioner reported that OS deployment failed.", true).withStage("deployment")
			case string(provisioningdomain.MachineStatusReady):
				deployingSince = time.Time{}
				allocatedSince = time.Time{}
				if !accepted {
					return providerAttention("provider_outcome_unknown", "The provider response was lost before Swallow could prove whether deployment started. Verify MAAS before retrying.", "deployment")
				}
			}
		}
		select {
		case <-ctx.Done():
			if !temporalworkflow.IsActivityWorkerStopping(ctx) {
				e.abort(serverID)
			}
			return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskCanceled}
		case <-deadline.C:
			if !readinessDeadline.IsZero() {
				return deploymentReadinessFailed(lastReadiness)
			}
			return providerAttention("provider_observation_timeout", "Timed out waiting for the Server to reach Deployed.", "deployment")
		case <-ticker.C:
		}
	}
}

type deploymentReadiness struct {
	serverName string
	addresses  []string
	port       int
	reachable  bool
}

// inspectDeploymentReadiness checks the latest provider projection without treating
// MAAS's narrower deployed state as a completed Swallow deployment.
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
	if errors.Is(err, operationdomain.ErrAutomationConfigNotFound) {
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

// recoverDeploymentWithoutAddress is entered only by an explicit failed-Step retry.
// MAAS exposes complete interface mutation only while Ready or Broken, so Swallow
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
	released := e.observeRelease(ctx, serverID, step.OperationID, false, true)
	if released.Status != operationdomain.TaskSucceeded {
		return released
	}
	if locked := e.requireUnlocked(ctx, serverID); locked != nil {
		return *locked
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
	if step.Step.Attempt > 1 || step.ActivityAttempt > 1 {
		state, err := e.refresh.Execute(ctx, serverID)
		if err == nil && state.State == string(provisioningdomain.MachineStatusReady) {
			return e.observeReleaseCleanup(ctx, serverID, step.OperationID, input.UnbindStaticIPs, true)
		}
		if err == nil && state.State == string(provisioningdomain.MachineStatusReleasing) {
			return e.observeRelease(ctx, serverID, step.OperationID, input.UnbindStaticIPs, true)
		}
	}
	if locked := e.requireUnlocked(ctx, serverID); locked != nil {
		return *locked
	}
	_, err := e.release.ExecuteWithOptions(ctx, input)
	if err != nil {
		var providerErr *provisioningdomain.ProviderError
		if errors.As(err, &providerErr) && providerErr.Kind == provisioningdomain.ProviderErrorUnavailable {
			return e.observeRelease(ctx, serverID, step.OperationID, input.UnbindStaticIPs, false)
		}
		return normalizeProviderError(err, "release")
	}
	return e.observeRelease(ctx, serverID, step.OperationID, input.UnbindStaticIPs, true)
}

// recoverServer converges a Server whose provisioning axis is not usable back to ready by
// choosing the provider primitive for its observed state per the recovery policy: broken is
// cleared with Mark fixed, rescue is exited and then, if still not ready, released, and
// failed or deployed is released. It is state-driven rather than plan-fixed so a Machine
// that changes state between primitives (rescue -> failed after exit) is re-evaluated. Each
// primitive is issued at most once so a stuck state fails for attention rather than looping.
func (e providerStepExecutor) recoverServer(ctx context.Context, step temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	var input provisioningapp.RecoverServerInput
	if err := decodeStepRequest(step.Step.Parameters, &input); err != nil {
		return providerFailed("invalid_step", err.Error(), false).StepExecutionResult
	}
	serverID := firstTargetServer(step.Step)
	if serverID == "" {
		return providerFailed("invalid_step", "The recovery Step has no Server target.", false).StepExecutionResult
	}
	deadline := time.NewTimer(2 * time.Hour)
	defer deadline.Stop()
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
		if err == nil {
			now := time.Now()
			switch provisioningdomain.MachineStatus(state.State) {
			case provisioningdomain.MachineStatusReady:
				return providerSucceeded()
			case provisioningdomain.MachineStatusReleasing:
				// A Release is already converging this Server; follow it to Ready and reuse
				// the same static-IP cleanup contract as a direct Release.
				return e.observeRelease(ctx, serverID, step.OperationID, input.UnbindStaticIPs, true)
			case provisioningdomain.MachineStatusFailed, provisioningdomain.MachineStatusDeployed,
				provisioningdomain.MachineStatusAllocated:
				// First, return to Ready with a plain Release (no erase). `allocated` (reserved but
				// not deployed) uses the same primitive as `failed`/`deployed`; without it a Server
				// parked at allocated — for example a verification borrow whose proving deploy never
				// finished — would loop here until the observation deadline instead of being released.
				if !releaseAttempted {
					releaseAttempted = true
					result := e.recoverViaRelease(ctx, step, serverID, input)
					if result.Status == operationdomain.TaskSucceeded {
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
		}
		select {
		case <-ctx.Done():
			return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskCanceled}
		case <-deadline.C:
			return providerAttention("provider_observation_timeout", "Timed out waiting for the Server to return to Ready.", "recover")
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
			return e.observeRelease(ctx, serverID, step.OperationID, input.UnbindStaticIPs, false)
		}
		return normalizeProviderError(err, "recover_release")
	}
	return e.observeRelease(ctx, serverID, step.OperationID, input.UnbindStaticIPs, true)
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
	controller, ok := provider.(provisioningdomain.OperatorStateController)
	if !ok {
		result := providerFailed("recover_unsupported", "This provisioner cannot recover the Server through operator-state actions.", false).withStage("recover")
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

// recoverOperatorPrimitive issues one operator-state provider action (Mark fixed or exit
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
	controller, ok := provider.(provisioningdomain.OperatorStateController)
	if !ok {
		result := providerFailed("recover_unsupported", "This provisioner cannot recover the Server through operator-state actions.", false).withStage("recover")
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

func (e providerStepExecutor) observeRelease(ctx context.Context, serverID, operationID string, cleanupRequested, accepted bool) temporalworkflow.StepExecutionResult {
	deadline := time.NewTimer(2 * time.Hour)
	defer deadline.Stop()
	ticker := time.NewTicker(e.pollInterval())
	defer ticker.Stop()
	for {
		state, err := e.refresh.Execute(ctx, serverID)
		if err == nil {
			switch state.State {
			case string(provisioningdomain.MachineStatusReady):
				return e.observeReleaseCleanup(ctx, serverID, operationID, cleanupRequested, false)
			case string(provisioningdomain.MachineStatusFailed), string(provisioningdomain.MachineStatusBroken):
				return providerFailed("release_failed", "The provisioner reported that release failed.", true).withStage("release")
			case string(provisioningdomain.MachineStatusDeployed):
				if !accepted {
					return providerAttention("provider_outcome_unknown", "The provider response was lost before Swallow could prove whether release started. Verify MAAS before retrying.", "release")
				}
			}
		}
		select {
		case <-ctx.Done():
			if !temporalworkflow.IsActivityWorkerStopping(ctx) {
				e.abort(serverID)
			}
			return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskCanceled}
		case <-deadline.C:
			return providerAttention("provider_observation_timeout", "Timed out waiting for the Server to return to Ready.", "release")
		case <-ticker.C:
		}
	}
}

func (e providerStepExecutor) observeReleaseCleanup(ctx context.Context, serverID, operationID string, cleanupRequested, retryFailed bool) temporalworkflow.StepExecutionResult {
	if !cleanupRequested {
		return providerSucceeded()
	}
	ticker := time.NewTicker(e.pollInterval())
	defer ticker.Stop()
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
				if err := e.tasks.Retry(ctx, cleanup.ID, time.Now().UTC()); err != nil {
					return providerFailed("cleanup_retry_failed", "Static IP cleanup could not be queued for retry.", true).withStage("network_cleanup")
				}
				retryFailed = false
				continue
			}
			return providerFailed("network_cleanup_failed", cleanup.Error, cleanup.Phase != provisioningdomain.ProvisioningTaskWaitingForRelease).withStage("network_cleanup")
		}
		select {
		case <-ctx.Done():
			return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskCanceled}
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

func firstTargetServer(step operationdomain.Task) string {
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

type providerResult struct {
	temporalworkflow.StepExecutionResult
}

func providerSucceeded() temporalworkflow.StepExecutionResult {
	return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
}

func providerFailed(code, message string, retryable bool) providerResult {
	if strings.TrimSpace(message) == "" {
		message = "The provider Step failed."
	}
	return providerResult{temporalworkflow.StepExecutionResult{Status: operationdomain.TaskFailed,
		Error: &operationdomain.NormalizedError{Code: code, Message: message, Retryable: retryable}}}
}

func providerAttention(code, message, stage string) temporalworkflow.StepExecutionResult {
	return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskRequiresAttention,
		Error: &operationdomain.NormalizedError{Code: code, Message: message, Retryable: true, Stage: stage}}
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
// The Step pauses for operator attention (retryable) rather than failing terminally, so
// the operator can unlock and retry the same Step.
func (e providerStepExecutor) requireUnlocked(ctx context.Context, serverID string) *temporalworkflow.StepExecutionResult {
	if e.protection == nil || serverID == "" {
		return nil
	}
	if err := e.protection.RequireUnlocked(ctx, []string{serverID}); err != nil {
		result := providerAttention("target_locked", err.Error(), "lock_precheck")
		return &result
	}
	return nil
}

func normalizeProviderError(err error, stage string) temporalworkflow.StepExecutionResult {
	// The Deployment Key could not be registered before any provider write, so the outcome is
	// known (nothing was deployed) and retrying once the provisioner recovers is safe.
	if errors.Is(err, provisioningdomain.ErrSSHKeyRegistration) {
		return providerAttention("ssh_key_registration_failed", err.Error(), stage)
	}
	// Reached only if the Deployment Key disappeared after acceptance; nothing was written to the
	// provider, so the operator can create the key and retry the Task.
	if errors.Is(err, provisioningdomain.ErrDeploymentKeyMissing) {
		return providerAttention("deployment_key_missing", err.Error(), stage)
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
	// transport failed. Reconcile under operator attention instead of auto-retrying,
	// which could duplicate a deploy or release side effect.
	return providerAttention("provider_outcome_unknown", err.Error(), stage)
}
