package app

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/operation/infra/temporalworkflow"
	platformapp "github.com/maple52046/swallow/internal/platform/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// platformUninstallFinalizer clears a Platform's projections after a successful uninstall.
// It is the completion hook for the release-and-uninstall path, whose workflow has no ansible
// step (the usual AnsibleStepSucceeded trigger), so this narrow port lets the internal
// complete-uninstall step run the same cleanup keyed only by platform id.
type platformUninstallFinalizer interface {
	CompleteUninstall(ctx context.Context, platformID string) error
}

// platformWorkflowStepExecutor implements Swallow-owned readiness and health phases.
// It exposes only normalized outcomes to Temporal; provider and socket details remain in
// the adapters that own them.
type platformWorkflowStepExecutor struct {
	servers            serverdomain.ServerRepository
	configurations     operationdomain.AutomationConfigurationRepository
	membership         *platformapp.MembershipSyncUseCase
	finalizer          platformUninstallFinalizer
	imageVerifications provisioningdomain.OSImageVerificationRepository
	poll               time.Duration
}

func (e platformWorkflowStepExecutor) Execute(ctx context.Context, input temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	switch input.Step.Kind {
	case "noop":
		return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
	case "wait-for-ssh":
		return e.waitForSSH(ctx, input)
	case "validate-platform-health":
		return e.validatePlatform(ctx, input.PlatformID, input.Step)
	case "complete-uninstall":
		return e.completeUninstall(ctx, input.PlatformID)
	case "record-image-verification":
		return e.recordImageVerification(ctx, input)
	case "record-image-verification-failure":
		return e.recordImageVerificationFailure(ctx, input)
	default:
		return internalStepFailed("unsupported_internal_step", "The internal Step kind is not supported.", false)
	}
}

// recordImageVerification is the finalize step of a verify-os-image Workflow: after the proving
// deploy succeeded, it writes the swallow-owned attestation that the image works for the deploy
// target. Separated from the deploy so the attestation write can retry without re-deploying. A
// write failure is retryable because the deploy already succeeded; only the record is missing.
func (e platformWorkflowStepExecutor) recordImageVerification(ctx context.Context, input temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	if e.imageVerifications == nil {
		return internalStepFailed("image_verification_unavailable", "Image verification recording is unavailable.", false)
	}
	params := input.Step.Parameters
	integrationID, _ := params["integrationId"].(string)
	imageID, _ := params["imageId"].(string)
	architecture, _ := params["architecture"].(string)
	targetRaw, _ := params["deployTarget"].(string)
	target, ok := provisioningdomain.ParseDeployTarget(targetRaw)
	if integrationID == "" || imageID == "" || architecture == "" || !ok {
		return internalStepFailed("image_verification_invalid", "The image verification Step is missing an image identity or deploy target.", false)
	}
	evidence := provisioningdomain.OSImageVerificationEvidence{
		VerifiedAt:  time.Now().UTC(),
		OperationID: input.OperationID,
		ServerID:    firstTargetServer(input.Step),
	}
	if err := e.imageVerifications.RecordTarget(ctx, integrationID, imageID, architecture, target, evidence); err != nil {
		return internalStepFailed("image_verification_write_failed", err.Error(), true)
	}
	return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
}

// recordImageVerificationFailure is the failure-path finalize step of a verify-os-image Workflow:
// it runs when the proving deploy failed and records the swallow-owned fact that the image did not
// deploy for the target, so the catalog can show a failed verification distinctly from a
// never-attempted one. The provider's reason lives on the failed provision Task; this records the
// Operation and Server for the operator to open, plus an optional short reason. A write failure is
// retryable: the failure fact is worth persisting so the operator is not misled into thinking the
// run never happened. This step succeeding does not mean the image is usable — it means the failure
// was recorded — so it returns TaskSucceeded and lets the return-to-ready step run next.
func (e platformWorkflowStepExecutor) recordImageVerificationFailure(ctx context.Context, input temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	if e.imageVerifications == nil {
		return internalStepFailed("image_verification_unavailable", "Image verification recording is unavailable.", false)
	}
	params := input.Step.Parameters
	integrationID, _ := params["integrationId"].(string)
	imageID, _ := params["imageId"].(string)
	architecture, _ := params["architecture"].(string)
	targetRaw, _ := params["deployTarget"].(string)
	reason, _ := params["reason"].(string)
	target, ok := provisioningdomain.ParseDeployTarget(targetRaw)
	if integrationID == "" || imageID == "" || architecture == "" || !ok {
		return internalStepFailed("image_verification_invalid", "The image verification Step is missing an image identity or deploy target.", false)
	}
	failure := provisioningdomain.OSImageVerificationFailure{
		FailedAt:    time.Now().UTC(),
		OperationID: input.OperationID,
		ServerID:    firstTargetServer(input.Step),
		Reason:      reason,
	}
	if err := e.imageVerifications.RecordFailedTarget(ctx, integrationID, imageID, architecture, target, failure); err != nil {
		return internalStepFailed("image_verification_write_failed", err.Error(), true)
	}
	return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
}

// completeUninstall finalizes a release-and-uninstall Operation by clearing the Platform's
// projections (membership, owned credential Integration, sync). It runs after every release
// step succeeds, replacing the ansible step's AnsibleStepSucceeded cleanup that this path
// omits. A missing finalizer or platform id is a non-retryable failure; the cleanup itself is
// idempotent, so a retried step is safe.
func (e platformWorkflowStepExecutor) completeUninstall(ctx context.Context, platformID string) temporalworkflow.StepExecutionResult {
	if e.finalizer == nil || strings.TrimSpace(platformID) == "" {
		return internalStepFailed("platform_finalize_unavailable", "Platform uninstall finalization is unavailable.", false)
	}
	if err := e.finalizer.CompleteUninstall(ctx, platformID); err != nil {
		return internalStepFailed("platform_finalize_failed", err.Error(), true)
	}
	return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
}

func (e platformWorkflowStepExecutor) waitForSSH(ctx context.Context, input temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	port := 22
	if e.configurations != nil {
		configuration, err := e.configurations.FindBySiteID(ctx, input.SiteID)
		if err != nil {
			return internalStepFailed("ssh_configuration_unavailable", err.Error(), true)
		}
		if configuration.SSHPort > 0 {
			port = configuration.SSHPort
		}
	}
	poll := e.pollInterval()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	deadline := time.NewTimer(20 * time.Minute)
	defer deadline.Stop()

	for {
		missingAddresses, unreachable, err := e.unreachableTargets(ctx, input.Step, port)
		if err != nil {
			return internalStepFailed("ssh_readiness_unavailable", err.Error(), true)
		}
		if len(missingAddresses) == 0 && len(unreachable) == 0 {
			return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
		}
		select {
		case <-ctx.Done():
			return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskCanceled}
		case <-deadline.C:
			failure := internalStepFailed("ssh_readiness_timeout", sshReadinessTimeoutMessage(missingAddresses, unreachable, port), true)
			failure.Error.Stage = "ssh_readiness"
			return failure
		case <-ticker.C:
		}
	}
}

// unreachableTargets probes the latest Server projections and keeps absent provider
// addresses separate from failed TCP probes. Calls are bounded to eight concurrent dials;
// repository failures abort the observation instead of being misreported as host failures.
func (e platformWorkflowStepExecutor) unreachableTargets(ctx context.Context, step operationdomain.Task, port int) ([]string, []string, error) {
	type probe struct {
		name           string
		reachable      bool
		missingAddress bool
		err            error
	}
	results := make(chan probe, len(step.Targets))
	semaphore := make(chan struct{}, 8)
	var wait sync.WaitGroup
	for _, target := range step.Targets {
		if target.Kind != "server" {
			continue
		}
		server, err := e.servers.FindByID(ctx, target.ID)
		if err != nil {
			return nil, nil, err
		}
		name := server.DisplayName()
		address := server.PrimaryAddress()
		if address == "" {
			results <- probe{name: name, missingAddress: true}
			continue
		}
		if server.Absent || server.Provisioning == nil || server.Provisioning.State != "deployed" {
			results <- probe{name: name}
			continue
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				results <- probe{name: name, err: ctx.Err()}
				return
			}
			probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			connection, dialErr := (&net.Dialer{}).DialContext(probeCtx, "tcp", net.JoinHostPort(address, fmt.Sprintf("%d", port)))
			if connection != nil {
				_ = connection.Close()
			}
			results <- probe{name: name, reachable: dialErr == nil}
		}()
	}
	wait.Wait()
	close(results)
	missingAddresses := []string{}
	unreachable := []string{}
	for result := range results {
		if result.err != nil && ctx.Err() != nil {
			return nil, nil, result.err
		}
		if result.missingAddress {
			missingAddresses = append(missingAddresses, result.name)
		} else if !result.reachable {
			unreachable = append(unreachable, result.name)
		}
	}
	sort.Strings(missingAddresses)
	sort.Strings(unreachable)
	return missingAddresses, unreachable, nil
}

// sshReadinessTimeoutMessage separates missing provider observations from a closed or
// unreachable SSH port so operators know whether to inspect DHCP/addressing or the host.
func sshReadinessTimeoutMessage(missingAddresses, unreachable []string, port int) string {
	parts := make([]string, 0, 2)
	if len(missingAddresses) > 0 {
		parts = append(parts, fmt.Sprintf("No provider address was observed after OS deployment for: %s.", strings.Join(missingAddresses, ", ")))
	}
	if len(unreachable) > 0 {
		parts = append(parts, fmt.Sprintf("SSH port %d did not become reachable for: %s.", port, strings.Join(unreachable, ", ")))
	}
	return strings.Join(parts, " ")
}

func (e platformWorkflowStepExecutor) validatePlatform(ctx context.Context, platformID string, step operationdomain.Task) temporalworkflow.StepExecutionResult {
	if strings.TrimSpace(platformID) == "" || e.membership == nil {
		return internalStepFailed("platform_validation_unavailable", "Platform health validation is unavailable.", false)
	}
	ticker := time.NewTicker(e.pollInterval())
	defer ticker.Stop()
	deadline := time.NewTimer(10 * time.Minute)
	defer deadline.Stop()
	lastReason := "The Platform API has not reported membership yet."
	for {
		report, err := e.membership.Execute(ctx, platformID)
		if err != nil {
			lastReason = err.Error()
		} else if report.Error != nil {
			lastReason = *report.Error
		} else {
			// Validate the Operation's own target Servers, not a global matched count.
			// A platform can carry unrelated members, and a global count can reach the
			// expected total while a specific target never joined; that would falsely
			// report this deployment healthy.
			joined, missing, checkErr := e.targetsJoined(ctx, platformID, step)
			if checkErr != nil {
				lastReason = checkErr.Error()
			} else if len(missing) == 0 {
				return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
			} else {
				lastReason = fmt.Sprintf("Platform membership is missing %d of %d target Servers: %s.",
					len(missing), joined+len(missing), strings.Join(missing, ", "))
			}
		}
		select {
		case <-ctx.Done():
			return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskCanceled}
		case <-deadline.C:
			return internalStepFailed("platform_health_timeout", lastReason, true)
		case <-ticker.C:
		}
	}
}

// targetsJoined reports how many of the Operation's target Servers now carry a membership
// projection naming this Platform, and which are still missing. It reads each target's own
// projection (written by the membership sync) rather than trusting an aggregate count.
func (e platformWorkflowStepExecutor) targetsJoined(ctx context.Context, platformID string, step operationdomain.Task) (int, []string, error) {
	joined := 0
	missing := []string{}
	for _, target := range step.Targets {
		if target.Kind != "server" {
			continue
		}
		server, err := e.servers.FindByID(ctx, target.ID)
		if err != nil {
			return 0, nil, err
		}
		if server.Membership != nil && server.Membership.PlatformID == platformID {
			joined++
		} else {
			missing = append(missing, server.DisplayName())
		}
	}
	sort.Strings(missing)
	return joined, missing, nil
}

func (e platformWorkflowStepExecutor) pollInterval() time.Duration {
	if e.poll <= 0 {
		return 5 * time.Second
	}
	return e.poll
}

func internalStepFailed(code, message string, retryable bool) temporalworkflow.StepExecutionResult {
	return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskFailed, Error: &operationdomain.NormalizedError{
		Code: code, Message: message, Retryable: retryable,
	}}
}
