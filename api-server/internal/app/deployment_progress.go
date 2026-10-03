package app

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/operation/infra/temporalworkflow"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// deploymentProgressEventLimit bounds each event read during deploy observation. Only the newest
// event matters, so a small window keeps the per-poll provider query cheap while still tolerating
// a burst of same-second events.
const deploymentProgressEventLimit = 20

// providerEvent is one provisioner event reduced to what progress tracking needs: its
// provider-authored type (for MAAS a human-readable stage such as "Configuring OS") and when the
// provider recorded it.
type providerEvent struct {
	eventType string
	at        time.Time
}

// deploymentProgress tracks provider-reported OS installation progress for one observeDeploy call.
//
// A provisioner such as MAAS reports a Machine only as "Deploying" until installation finishes, so
// the Machine status cannot tell a slow installation from one that will never finish — for example
// curtin waiting forever on an unreachable package mirror while the Machine stays powered on. The
// provider's event stream does record each installation stage, so the tracker surfaces the newest
// event as the non-terminal deployment reason and reports a stall when no newer event arrives
// within the grace window.
//
// Progress is measured from provider event timestamps rather than from when this process first
// looked, so a worker restart or activity retry resumes the same measurement instead of granting a
// fresh grace window. notBefore excludes events from an earlier provider lifecycle (a previous
// deploy or release of the same Machine) so they are never mistaken for this deployment's progress.
type deploymentProgress struct {
	// notBefore is when this deployment attempt started; older events are ignored.
	notBefore time.Time
	// since is the time of the newest event at or after notBefore, or notBefore when the provider
	// has recorded none yet. A stall is measured from it.
	since time.Time
	// shown is the event last projected onto the Server, so the projection is rewritten only when
	// the provider advances instead of on every poll. Zero means nothing has been projected.
	shown providerEvent
}

// newDeploymentProgress anchors progress tracking to the current attempt's start. The Server's
// deployment projection keeps StartedAt across activity retries of the same Operation, Step, and
// attempt (see setDeployment), which is what makes the measurement survive worker restarts. When
// no matching projection exists the observation start is used, which only delays stall detection.
func (e providerStepExecutor) newDeploymentProgress(ctx context.Context, step temporalworkflow.StepExecutionInput, serverID string) deploymentProgress {
	start := time.Now()
	if e.servers != nil {
		server, err := e.servers.FindByID(ctx, serverID)
		if err == nil && server.Deployment != nil {
			current := server.Deployment
			if current.OperationID == step.OperationID && current.StepID == step.Step.ID &&
				current.Attempt == step.Step.Attempt && !current.StartedAt.IsZero() {
				start = current.StartedAt
			}
		}
	}
	return deploymentProgress{notBefore: start, since: start}
}

// observeInstallProgress reads the provider's event stream once while the Machine is deploying,
// projects a newer event onto the Server as the deployment's non-terminal reason, and returns a
// requires-attention result once no new event has arrived for the stage-stall grace window.
//
// It returns nil while the deployment should keep being observed. A provider without an event
// stream, or a failed event read, yields nil without evaluating a stall: missing data is not
// evidence of a stall, and the power-on diagnosis and the outer two-hour observation timeout stay
// authoritative. A powered-off Machine is diagnosed earlier by the power-on check, whose window is
// shorter than the stall window. The stall does not abort the provider deployment; the operator
// decides whether to retry, release, or let it finish.
func (e providerStepExecutor) observeInstallProgress(
	ctx context.Context,
	step temporalworkflow.StepExecutionInput,
	serverID string,
	progress *deploymentProgress,
	now time.Time,
) *temporalworkflow.StepExecutionResult {
	events, supported, err := e.readMachineEvents(ctx, serverID)
	if !supported || err != nil {
		return nil
	}
	if latest, ok := latestProviderEvent(events, progress.notBefore); ok {
		if latest.at.After(progress.since) {
			progress.since = latest.at
		}
		if latest != progress.shown {
			// Code stays empty so clients read this as a progress reason, not a failure; the
			// same convention the verifying projection uses for its SSH-readiness wait.
			reason := &operationdomain.NormalizedError{
				Stage:   providerEventStage(latest.eventType),
				Message: fmt.Sprintf("Provider stage: %s (since %s).", latest.eventType, latest.at.UTC().Format(time.RFC3339)),
			}
			if err := e.setDeployment(ctx, step, serverID, serverdomain.DeploymentDeploying, reason, false); err != nil {
				result := providerAttention("deployment_projection_unavailable", err.Error(), "deployment_projection")
				return &result
			}
			progress.shown = latest
		}
	}
	if now.Sub(progress.since) >= e.stageStallTimeout() {
		result := deploymentStageStalled(*progress, now)
		return &result
	}
	return nil
}

// readMachineEvents reads the newest provider events for the Server's Machine. supported is false
// when the Server's provisioner keeps no event stream, which callers treat as "no progress data"
// rather than as an error.
func (e providerStepExecutor) readMachineEvents(ctx context.Context, serverID string) ([]provisioningdomain.MachineEvent, bool, error) {
	server, err := e.servers.FindByID(ctx, serverID)
	if err != nil {
		return nil, false, err
	}
	provider, err := e.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return nil, false, err
	}
	reader, ok := provider.(provisioningdomain.MachineEventReader)
	if !ok {
		return nil, false, nil
	}
	events, err := reader.ListMachineEvents(ctx, server.Source.ProviderMachineID, deploymentProgressEventLimit)
	return events, true, err
}

// latestProviderEvent returns the newest event recorded at or after notBefore. Events whose
// timestamp cannot be parsed are skipped rather than guessed at: the provider adapter keeps an
// unknown raw value for display, but ordering progress on it could invent or hide a stall.
func latestProviderEvent(events []provisioningdomain.MachineEvent, notBefore time.Time) (providerEvent, bool) {
	var latest providerEvent
	found := false
	for _, event := range events {
		eventType := strings.TrimSpace(event.Type)
		if eventType == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(event.OccurredAt))
		if err != nil || at.Before(notBefore) {
			continue
		}
		if !found || at.After(latest.at) {
			latest = providerEvent{eventType: eventType, at: at}
			found = true
		}
	}
	return latest, found
}

// providerEventStage normalizes a provider event type into the stable snake_case stage carried on
// the deployment projection and on a stall diagnosis, for example "Configuring OS" becomes
// "configuring_os". The human-readable type stays in the status reason.
func providerEventStage(eventType string) string {
	var stage strings.Builder
	pendingSeparator := false
	for _, r := range strings.ToLower(eventType) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if pendingSeparator && stage.Len() > 0 {
				stage.WriteByte('_')
			}
			pendingSeparator = false
			stage.WriteRune(r)
			continue
		}
		pendingSeparator = true
	}
	if stage.Len() == 0 {
		return "deployment_progress"
	}
	return stage.String()
}

// deploymentStageStalled builds the requires-attention diagnosis for a deployment whose provider
// stopped recording progress. It is retryable: a retry re-observes before writing, so a deployment
// that eventually finishes is accepted, and one still stuck gets a fresh grace window.
func deploymentStageStalled(progress deploymentProgress, now time.Time) temporalworkflow.StepExecutionResult {
	idle := strings.TrimSuffix(now.Sub(progress.since).Round(time.Minute).String(), "0s")
	since := progress.since.UTC().Format(time.RFC3339)
	if progress.shown.eventType == "" {
		return providerAttention(
			"deployment_provider_stage_stall",
			fmt.Sprintf("The provisioner still reports Deploying, but it has recorded no installation progress for %s (since %s). Inspect the provider deployment before retrying.", idle, since),
			"deployment_progress",
		)
	}
	return providerAttention(
		"deployment_provider_stage_stall",
		fmt.Sprintf("The provisioner still reports Deploying, but its latest stage %q has not advanced for %s (since %s). The installer may be waiting on a package mirror or network resource it cannot reach; inspect the provider deployment before retrying.", progress.shown.eventType, idle, since),
		providerEventStage(progress.shown.eventType),
	)
}
