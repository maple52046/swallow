package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/operation/infra/temporalworkflow"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// The provisioner Tasks of the inspect-hardware Workflow (decision 053, contract
// server-enrollment.md).
//
// The enrollment wait polls at defaultEnrollmentSettlePoll and needs enrollmentSettledReadings
// consecutive settled readings, so a BMC that reports off for one instant during the enlistment
// reboot is not mistaken for the end of enlistment; it gives up after defaultEnrollmentSettleWait.
//
// Inspection makes up to defaultInspectionAttempts commissions. An attempt stalls when the provider
// records no event for defaultInspectionStallWait while inspecting: a healthy inspection records
// boot and script events every few minutes, and the slowest POST observed on the reference GPU
// server is about nine minutes. Without an event stream an attempt is bounded by
// defaultInspectionAttemptWait instead. Event times come from the provisioner's clock, which the
// co-located MAAS shares with swallow.
const (
	defaultEnrollmentSettleWait  = 20 * time.Minute
	defaultEnrollmentSettlePoll  = 15 * time.Second
	enrollmentSettledReadings    = 2
	defaultInspectionAttempts    = 3
	defaultInspectionStallWait   = 15 * time.Minute
	defaultInspectionAttemptWait = 45 * time.Minute
	// inspectionStartGrace is how long a commission may take to show up as inspecting before the
	// observed state is believed; it absorbs a projection read that raced the accepted request.
	inspectionStartGrace = 2 * time.Minute
	// inspectionAbortSettle bounds the wait for an aborted inspection to leave inspecting.
	inspectionAbortSettle = 3 * time.Minute
)

// waitEnrollmentSettled runs the wait-enrollment-settled Task: it holds the inspection until the
// provider's own enrollment of the Server has finished. A requested inspection and any retried
// run skip the wait, because an operator decided the Server may boot. A provisioner without the
// EnrollmentSettler capability has nothing to wait for. A provider read failure resets the
// confirmation count rather than failing, because enlistment often keeps the BMC busy; only the
// overall timeout asks for attention, and nothing has been commissioned at that point.
func (e providerStepExecutor) waitEnrollmentSettled(ctx context.Context, step temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	if skip, _ := step.Step.Parameters[skipEnrollmentWaitParameter].(bool); skip || step.Step.Attempt > 1 {
		return providerSucceeded()
	}
	serverID := firstTargetServer(step.Step)
	if serverID == "" {
		return providerFailed("invalid_step", "The enrollment wait has no target Server.", false).StepExecutionResult
	}
	server, provider, err := e.serverProvider(ctx, serverID)
	if err != nil {
		return normalizeProviderError(err, "enrollment")
	}
	settler, ok := provider.(provisioningdomain.EnrollmentSettler)
	if !ok {
		return providerSucceeded()
	}
	deadline := time.NewTimer(e.enrollmentSettleTimeout())
	defer deadline.Stop()
	ticker := time.NewTicker(e.enrollmentSettleInterval())
	defer ticker.Stop()
	readings := 0
	for {
		settled, err := settler.EnrollmentSettled(ctx, server.Source.ProviderMachineID)
		switch {
		case errors.Is(err, provisioningdomain.ErrMachineNotFound):
			return providerFailed("machine_not_found", "The provisioner no longer has the machine of "+server.DisplayName()+".", false).withStage("enrollment")
		case err == nil && settled:
			readings++
			if readings >= enrollmentSettledReadings {
				return providerSucceeded()
			}
		default:
			if err != nil {
				slog.Debug("enrollment settle reading failed", "serverId", serverID, "error", err)
			}
			readings = 0
		}
		select {
		case <-ctx.Done():
			return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskCanceled}
		case <-deadline.C:
			return providerAttention("enrollment_not_settled",
				fmt.Sprintf("The provisioner still reports %s as enrolling: it has not powered off after enrollment within %s. Check that the Server finished enrollment and that its BMC power settings are configured in the provisioner, then retry this Task to inspect it.",
					server.DisplayName(), e.enrollmentSettleTimeout()),
				"enrollment")
		case <-ticker.C:
		}
	}
}

// inspectionOutcomeKind classifies one inspection attempt.
type inspectionOutcomeKind int

const (
	// inspectionReady means the provider finished inspecting and the Server is ready.
	inspectionReady inspectionOutcomeKind = iota
	// inspectionFailedAttempt means the provider reported the inspection failed.
	inspectionFailedAttempt
	// inspectionStalledAttempt means the provider recorded no progress, which is what a Server
	// that never network-booted into the inspection looks like; swallow aborted it.
	inspectionStalledAttempt
	// inspectionStopped means the Task must stop now with result, without another attempt.
	inspectionStopped
)

// inspectionOutcome is one attempt's result. detail explains a failed or stalled attempt for the
// final attention message; result is the Task result to return for inspectionStopped.
type inspectionOutcome struct {
	kind   inspectionOutcomeKind
	detail string
	result temporalworkflow.StepExecutionResult
}

// inspectServer runs the inspect Task: bounded inspection attempts until the Server is ready
// (decision 053). Each attempt re-checks the Server Lock and issues Inspect. A failed or stalled
// attempt is followed by another until the attempts run out; the Task then asks for attention with
// a remediation that names Boot Media, leaving a stalled Server aborted back to New.
//
// The first attempt follows an inspection that is already running instead of issuing a second one
// the provider would refuse: an activity retry after a worker loss, or a provider that started
// inspecting on its own. An automatic inspection of a Server the provider has already brought to
// ready succeeds without inspecting again; a requested one re-inspects it.
func (e providerStepExecutor) inspectServer(ctx context.Context, step temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	serverID := firstTargetServer(step.Step)
	if serverID == "" {
		return providerFailed("invalid_step", "The inspection has no target Server.", false).StepExecutionResult
	}
	server, provider, err := e.serverProvider(ctx, serverID)
	if err != nil {
		return normalizeProviderError(err, "inspection")
	}
	validator, ok := provider.(provisioningdomain.HardwareValidator)
	if !ok {
		return providerFailed("inspection_unsupported", "This provisioner cannot inspect hardware.", false).withStage("inspection")
	}
	machineID := server.Source.ProviderMachineID
	initial := e.observedState(ctx, serverID)
	if origin, _ := step.Step.Parameters[inspectionOriginParameter].(string); origin == string(provisioningdomain.InspectionOriginAutomatic) &&
		initial == provisioningdomain.MachineStatusReady {
		return providerSucceeded()
	}
	attempts := e.inspectionAttemptLimit()
	var last inspectionOutcome
	for attempt := 1; attempt <= attempts; attempt++ {
		started := time.Now()
		resume := attempt == 1 && (initial == provisioningdomain.MachineStatusInspecting || initial == provisioningdomain.MachineStatusTesting)
		if !resume {
			if locked := e.requireUnlocked(ctx, serverID); locked != nil {
				return *locked
			}
			if _, err := validator.Inspect(ctx, machineID); err != nil {
				return normalizeProviderError(err, "inspection")
			}
		}
		outcome := e.observeInspection(ctx, serverID, validator, machineID, started)
		switch outcome.kind {
		case inspectionReady:
			return providerSucceeded()
		case inspectionStopped:
			return outcome.result
		}
		last = outcome
		slog.Info("hardware inspection attempt did not complete", "serverId", serverID, "attempt", attempt, "of", attempts, "reason", outcome.detail)
	}
	return inspectionExhausted(server.DisplayName(), attempts, last)
}

// observeInspection follows one inspection attempt started at started.
func (e providerStepExecutor) observeInspection(
	ctx context.Context, serverID string, validator provisioningdomain.HardwareValidator, machineID string, started time.Time,
) inspectionOutcome {
	deadline := time.NewTimer(e.inspectionAttemptTimeout())
	defer deadline.Stop()
	ticker := time.NewTicker(e.pollInterval())
	defer ticker.Stop()
	progressSince := started
	sawInspecting := false
	for {
		state, err := e.refresh.Execute(ctx, serverID)
		if err == nil {
			now := time.Now()
			believed := sawInspecting || now.Sub(started) >= e.inspectionStartTimeout()
			switch provisioningdomain.MachineStatus(state.State) {
			case provisioningdomain.MachineStatusReady:
				return inspectionOutcome{kind: inspectionReady}
			case provisioningdomain.MachineStatusInspecting, provisioningdomain.MachineStatusTesting:
				// MAAS runs its commissioning tests as part of the inspection, so testing is progress.
				sawInspecting = true
				events, supported, eventErr := e.readMachineEvents(ctx, serverID)
				if !supported || eventErr != nil {
					break
				}
				if latest, ok := latestProviderEvent(events, started); ok && latest.at.After(progressSince) {
					progressSince = latest.at
				}
				if idle := now.Sub(progressSince); idle >= e.inspectionStallTimeout() {
					return e.abortStalledInspection(ctx, serverID, validator, machineID,
						fmt.Sprintf("no provider progress for %s after powering it on", idle.Round(time.Minute)))
				}
			case provisioningdomain.MachineStatusFailed, provisioningdomain.MachineStatusBroken:
				if believed {
					return inspectionOutcome{kind: inspectionFailedAttempt, detail: providerStateDetail(state.ProviderState, state.State)}
				}
			case provisioningdomain.MachineStatusNew:
				if sawInspecting {
					return inspectionInterrupted(state.State)
				}
				if believed {
					return inspectionOutcome{kind: inspectionFailedAttempt, detail: "the provisioner did not start the inspection"}
				}
			default:
				if believed {
					return inspectionInterrupted(state.State)
				}
			}
		}
		select {
		case <-ctx.Done():
			if !temporalworkflow.IsActivityWorkerStopping(ctx) {
				e.abort(serverID)
			}
			return inspectionOutcome{kind: inspectionStopped, result: temporalworkflow.StepExecutionResult{Status: operationdomain.TaskCanceled}}
		case <-deadline.C:
			return e.abortStalledInspection(ctx, serverID, validator, machineID,
				fmt.Sprintf("the inspection did not finish within %s", e.inspectionAttemptTimeout()))
		case <-ticker.C:
		}
	}
}

// abortStalledInspection aborts an attempt that made no progress and waits for the Server to leave
// inspecting. For MAAS the abort returns a Machine that was never inspected to New, which is the
// state the operator's next step (Boot Media, then Inspect) expects. An abort that does not settle
// stops the Task for attention rather than commissioning a Machine that is still inspecting.
func (e providerStepExecutor) abortStalledInspection(
	ctx context.Context, serverID string, validator provisioningdomain.HardwareValidator, machineID, detail string,
) inspectionOutcome {
	if _, err := validator.Abort(ctx, machineID); err != nil {
		slog.Warn("abort stalled hardware inspection", "serverId", serverID, "error", err)
	}
	deadline := time.NewTimer(e.inspectionAbortTimeout())
	defer deadline.Stop()
	ticker := time.NewTicker(e.pollInterval())
	defer ticker.Stop()
	for {
		switch e.observedState(ctx, serverID) {
		case provisioningdomain.MachineStatusInspecting, provisioningdomain.MachineStatusTesting, "":
		default:
			return inspectionOutcome{kind: inspectionStalledAttempt, detail: detail}
		}
		select {
		case <-ctx.Done():
			return inspectionOutcome{kind: inspectionStopped, result: temporalworkflow.StepExecutionResult{Status: operationdomain.TaskCanceled}}
		case <-deadline.C:
			return inspectionOutcome{kind: inspectionStopped, result: providerAttention("inspect_abort_unsettled",
				"swallow aborted a hardware inspection that made no progress ("+detail+"), but the provisioner still reports it as inspecting. Check the Server in the provisioner before retrying this Task.",
				"inspection")}
		case <-ticker.C:
		}
	}
}

// inspectionInterrupted stops the Task when the Server left inspecting without swallow aborting
// it, typically an operator abort or another action in the provisioner: swallow does not
// commission again over an operator's decision.
func inspectionInterrupted(state string) inspectionOutcome {
	return inspectionOutcome{kind: inspectionStopped, result: providerAttention("inspect_interrupted",
		"The Server left hardware inspection without swallow aborting it (it is now "+state+"). Retry this Task to inspect it again.",
		"inspection")}
}

// inspectionExhausted is the attention result after the last attempt. It is retryable: a retry
// re-runs the whole ensure-inspected Job, applying Boot Media enabled in the meantime.
func inspectionExhausted(name string, attempts int, last inspectionOutcome) temporalworkflow.StepExecutionResult {
	if last.kind == inspectionStalledAttempt {
		return providerAttention("inspect_pxe_unreached",
			fmt.Sprintf("Hardware inspection of %s did not complete in %d attempts: %s. The Server probably could not network-boot into the provisioner. If its network is not served by the provisioner's DHCP (an external network), build a Boot ISO and enable Boot Media on the Server, then retry this Task.",
				name, attempts, last.detail),
			"inspection")
	}
	return providerAttention("inspect_failed",
		fmt.Sprintf("The provisioner reported that hardware inspection of %s failed in %d attempts (%s). Check the inspection results in the provisioner, then retry this Task.",
			name, attempts, last.detail),
		"inspection")
}

func providerStateDetail(providerState, state string) string {
	if strings.TrimSpace(providerState) != "" {
		return providerState
	}
	return state
}

// observedState reads the Server's provisioning state live, or "" when it cannot be read.
func (e providerStepExecutor) observedState(ctx context.Context, serverID string) provisioningdomain.MachineStatus {
	state, err := e.refresh.Execute(ctx, serverID)
	if err != nil {
		return ""
	}
	return provisioningdomain.MachineStatus(state.State)
}

// serverProvider loads a Task's Server and its provisioner.
func (e providerStepExecutor) serverProvider(ctx context.Context, serverID string) (*serverdomain.Server, provisioningdomain.OSProvisioningProvider, error) {
	server, err := e.servers.FindByID(ctx, serverID)
	if err != nil {
		return nil, nil, err
	}
	provider, err := e.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return nil, nil, err
	}
	return server, provider, nil
}

// The timing accessors below fall back to the production defaults; the fields exist for tests.

func (e providerStepExecutor) enrollmentSettleTimeout() time.Duration {
	if e.enrollmentSettleWait > 0 {
		return e.enrollmentSettleWait
	}
	return defaultEnrollmentSettleWait
}

func (e providerStepExecutor) enrollmentSettleInterval() time.Duration {
	if e.enrollmentSettlePoll > 0 {
		return e.enrollmentSettlePoll
	}
	return defaultEnrollmentSettlePoll
}

func (e providerStepExecutor) inspectionAttemptLimit() int {
	if e.inspectionAttempts > 0 {
		return e.inspectionAttempts
	}
	return defaultInspectionAttempts
}

func (e providerStepExecutor) inspectionStallTimeout() time.Duration {
	if e.inspectionStallWait > 0 {
		return e.inspectionStallWait
	}
	return defaultInspectionStallWait
}

func (e providerStepExecutor) inspectionAttemptTimeout() time.Duration {
	if e.inspectionAttemptWait > 0 {
		return e.inspectionAttemptWait
	}
	return defaultInspectionAttemptWait
}

func (e providerStepExecutor) inspectionStartTimeout() time.Duration {
	if e.inspectionStartWait > 0 {
		return e.inspectionStartWait
	}
	return inspectionStartGrace
}

func (e providerStepExecutor) inspectionAbortTimeout() time.Duration {
	if e.inspectionAbortWait > 0 {
		return e.inspectionAbortWait
	}
	return inspectionAbortSettle
}
