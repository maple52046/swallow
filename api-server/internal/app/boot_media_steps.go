package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/operation/infra/temporalworkflow"
	serverapp "github.com/maple52046/swallow/internal/server/application"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// ensureBootMediaTaskKind is the internal Task that re-applies a Server's Boot Media before its
// provision-os Task (decision 047). The kind is part of the published Workflow vocabulary
// (contract provisioning.md); renaming it breaks Workflows already persisted.
const ensureBootMediaTaskKind = "ensure-boot-media"

// bootMediaISOParameter is the provision-os Task parameter holding the Boot Media ISO URL frozen
// for the Workflow; it is present only for a Server with Boot Media enabled at acceptance.
const bootMediaISOParameter = "bootMediaIsoUrl"

// The provision-os observer of a Boot Media Server watches its deployment boot (decision 047),
// timed from its first Deploying reading: the provisioner powers the host on as it enters
// Deploying.
//
// bootMediaMediaCheck is when it checks, once, that the BMC still holds the ISO, mounting it again
// if not: AMI can drop a fresh mount at that power-on, and two minutes in is early in a server's
// POST (four to nine minutes to the network boot on the reference GPU server).
//
// bootMediaBootCheck is when, if the provisioner has recorded nothing since the deployment start
// (the host never network-booted into it), it re-applies Boot Media and restarts the host. It
// must exceed the slowest POST observed, and leave room before the stage-stall window for the
// restarted boot to reach the provisioner.
const (
	bootMediaMediaCheck = 2 * time.Minute
	bootMediaBootCheck  = 10 * time.Minute
)

// bootMediaEnsurer is the slice of the Boot Media use case the internal executor needs.
type bootMediaEnsurer interface {
	Ensure(ctx context.Context, serverID, isoURL string) (serverapp.EnsureOutcome, error)
}

// bootMediaBootRecoverer is the slice of the Boot Media use case the provision-os observer uses
// to recover a deployment boot that missed the ISO.
type bootMediaBootRecoverer interface {
	RecoverDeploymentBoot(ctx context.Context, serverID, isoURL string, notBooted bool) (bool, error)
}

// bootMediaWatch is one provision-os observation's Boot Media boot watch: a media check, then a
// boot check — the only one that restarts the host. It is inert for a Task without a frozen ISO
// URL or an executor without a recoverer.
type bootMediaWatch struct {
	recoverer              bootMediaBootRecoverer
	isoURL                 string
	mediaCheck, bootCheck  time.Duration
	since                  time.Time
	mediaChecked, finished bool
}

func (e providerStepExecutor) newBootMediaWatch(step temporalworkflow.StepExecutionInput) *bootMediaWatch {
	isoURL, _ := step.Step.Parameters[bootMediaISOParameter].(string)
	w := &bootMediaWatch{recoverer: e.bootMedia, isoURL: isoURL, mediaCheck: bootMediaMediaCheck, bootCheck: bootMediaBootCheck}
	if e.bootMediaMediaWait > 0 {
		w.mediaCheck = e.bootMediaMediaWait
	}
	if e.bootMediaBootWait > 0 {
		w.bootCheck = e.bootMediaBootWait
	}
	w.finished = e.bootMedia == nil || isoURL == ""
	return w
}

// observe runs the due check of a Deploying reading against the deployment's progress and
// reports whether it restarted the host. The first reading starts the clock, not the attempt's
// start, which precedes the ensure Task's mount and settle. The boot check needs the provider's
// event stream: without a recorded deployment-start event it cannot tell a host that never booted
// from a provider that reports nothing, so it does nothing. A failed check is logged, not
// surfaced: the deployment may still boot the ISO, and the observer's stage-stall detection
// reports one that never does.
func (w *bootMediaWatch) observe(ctx context.Context, serverID string, progress deploymentProgress, now time.Time) bool {
	if w.finished {
		return false
	}
	if w.since.IsZero() {
		w.since = now
	}
	elapsed := now.Sub(w.since)
	switch {
	case !w.mediaChecked && elapsed >= w.mediaCheck:
		w.mediaChecked = true
		w.recover(ctx, serverID, false, "boot media mounted again during the deployment boot")
	case w.mediaChecked && elapsed >= w.bootCheck:
		w.finished = true
		if awaitingNetworkBoot(progress) {
			return w.recover(ctx, serverID, true, "boot media re-applied and host restarted for the deployment boot")
		}
	}
	return false
}

func (w *bootMediaWatch) recover(ctx context.Context, serverID string, notBooted bool, done string) bool {
	changed, err := w.recoverer.RecoverDeploymentBoot(ctx, serverID, w.isoURL, notBooted)
	switch {
	case err != nil:
		slog.Warn("boot media deployment boot recovery failed", "serverId", serverID, "notBooted", notBooted, "error", err)
	case changed:
		slog.Info(done, "serverId", serverID)
	}
	return changed
}

// awaitingNetworkBoot reports whether the provider's newest event of this attempt is still the
// deployment start — for MAAS the "Deploying" event, followed by "Performing PXE boot" once the
// host network-boots into the deployment.
func awaitingNetworkBoot(progress deploymentProgress) bool {
	return progress.shown.eventType != "" && providerEventStage(progress.shown.eventType) == "deploying"
}

// ensureBootMedia runs one ensure-boot-media Task. A BMC failure is retryable: the BMC may be
// busy or briefly unreachable, and the operator can retry the Task once it answers; the dependent
// provision-os Task does not run until this succeeds, because deploying a Server whose BMC will
// not boot the ISO cannot reach the provisioner.
func (e platformWorkflowStepExecutor) ensureBootMedia(ctx context.Context, input temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	if e.bootMedia == nil {
		return internalStepFailed("boot_media_unavailable", "Boot Media is unavailable in this worker.", false)
	}
	serverID := firstTargetServer(input.Step)
	isoURL, _ := input.Step.Parameters["isoUrl"].(string)
	if serverID == "" {
		return internalStepFailed("boot_media_invalid", "The ensure-boot-media Task has no target Server.", false)
	}
	if isoURL == "" {
		return internalStepFailed("boot_media_not_configured",
			"Boot Media is enabled on the Server, but it had no served Boot ISO when the deployment was accepted. Choose a Boot ISO for the Server's Boot Media, then deploy again.", false)
	}
	if _, err := e.bootMedia.Ensure(ctx, serverID, isoURL); err != nil {
		return internalStepFailed("boot_media_ensure_failed", err.Error(), true)
	}
	return temporalworkflow.StepExecutionResult{Status: operationdomain.TaskSucceeded, Progress: 100}
}

// bootMediaPlanner adds ensure-boot-media Tasks to Workflows that deploy an OS (decisions 047
// and 049). The decision is made when the Workflow is created, from the Server's setting at that
// moment; the Task re-reads the setting when it runs and does nothing if Boot Media was disabled
// since.
type bootMediaPlanner struct {
	servers serverdomain.ServerRepository
	isos    serverdomain.BootISOResolver
}

// withEnsureTasks returns steps with an ensure-boot-media Task inserted before every provision-os
// Task whose Server has Boot Media enabled. The new Task joins the provision Task's Job, targets
// the same Server, carries the URL of that Server's chosen Boot ISO frozen for the Workflow's
// lifetime, and the provision Task gains a dependency on it. A Server whose Boot ISO is not
// served (none chosen, deleted, file missing) gets an empty URL, which fails its ensure Task as
// boot_media_not_configured instead of deploying a host that cannot reach the provisioner. Steps
// of Servers without Boot Media are unchanged, so the Workflow is exactly what it was before Boot
// Media existed. A zero planner returns steps unchanged; an unreadable Server is an error so a
// deploy cannot silently skip its Boot Media.
func (p bootMediaPlanner) withEnsureTasks(ctx context.Context, steps []operationdomain.Task) ([]operationdomain.Task, error) {
	if p.servers == nil {
		return steps, nil
	}
	out := make([]operationdomain.Task, 0, len(steps))
	for _, step := range steps {
		serverID := firstTargetServer(step)
		if step.Kind != "provision-os" || serverID == "" {
			out = append(out, step)
			continue
		}
		server, err := p.servers.FindByID(ctx, serverID)
		if err != nil {
			return nil, fmt.Errorf("read Boot Media of Server %s: %w", serverID, err)
		}
		if server.BootMedia == nil || !server.BootMedia.Enabled {
			out = append(out, step)
			continue
		}
		ensureID := ensureBootMediaTaskKind + "-" + serverID
		isoURL := ""
		if p.isos != nil && server.BootMedia.ISOID != "" {
			image, err := p.isos.Resolve(ctx, server.BootMedia.ISOID)
			switch {
			case err == nil:
				isoURL = image.URL
			case errors.Is(err, serverdomain.ErrBootISOUnknown), errors.Is(err, serverdomain.ErrBootMediaNotConfigured):
			default:
				return nil, fmt.Errorf("resolve Boot ISO of Server %s: %w", serverID, err)
			}
		}
		out = append(out, operationdomain.Task{
			ID: ensureID, Kind: ensureBootMediaTaskKind, Name: "Ensure Boot Media on " + server.DisplayName(),
			Job: step.Job, Executor: operationdomain.RunnerKindInternal,
			Targets:    []operationdomain.ResourceReference{{Kind: "server", ID: serverID}},
			Parameters: map[string]any{"isoUrl": isoURL},
		})
		step.DependsOn = append(append([]string(nil), step.DependsOn...), ensureID)
		// The provision Task carries the same frozen URL so its boot watch (see
		// bootMediaBootRecoverer) can remount the ISO if the BMC drops it at the power-on.
		parameters := make(map[string]any, len(step.Parameters)+1)
		for key, value := range step.Parameters {
			parameters[key] = value
		}
		parameters[bootMediaISOParameter] = isoURL
		step.Parameters = parameters
		out = append(out, step)
	}
	return out, nil
}
