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
	sshProbe       func(context.Context, string, int) error
}

const (
	defaultDeploymentReadinessWait = 20 * time.Minute
	defaultDeploymentPowerOnWait   = 10 * time.Minute
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
		result := e.deploy(ctx, input)
		if temporalworkflow.IsActivityWorkerStopping(ctx) {
			return result
		}
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
	default:
		return providerFailed("unsupported_step", "The provider Step kind is not supported.", false).StepExecutionResult
	}
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
	var lastReadiness deploymentReadiness
	for {
		state, err := e.refresh.Execute(ctx, serverID)
		if err == nil {
			switch state.State {
			case string(provisioningdomain.MachineStatusDeploying):
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
			case string(provisioningdomain.MachineStatusDeployed):
				deployingSince = time.Time{}
				if imageMatches(state, input) {
					if readinessDeadline.IsZero() {
						if err := e.setDeployment(ctx, step, serverID, serverdomain.DeploymentVerifying, nil, false); err != nil {
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
