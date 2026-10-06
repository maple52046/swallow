package application

import (
	"context"
	"log/slog"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// SettingAutoInspect is the provisioner Integration setting that turns automatic hardware
// inspection of newly enrolled Servers off when it is "false" (contract sites-integrations.md).
// Any other value, or none, leaves it on, so Integrations created before the key existed keep the
// documented default.
const SettingAutoInspect = "autoInspect"

// AutoInspectWindow bounds automatic inspection to Servers swallow first observed this recently.
// It keeps an upgrade, or a re-enabled Integration, from powering on Machines operators left New on
// purpose long ago; an older New Server is inspected only on request (decision 053).
const AutoInspectWindow = 24 * time.Hour

// AutoInspectEnabled reports whether integration starts hardware inspection automatically.
func AutoInspectEnabled(integration *sitedomain.Integration) bool {
	return integration.Settings[SettingAutoInspect] != "false"
}

// InspectionRequest is the intent to inspect one Server's hardware through an inspect-hardware
// Workflow.
type InspectionRequest struct {
	ServerID string
	Origin   provisioningdomain.InspectionOrigin
	// RequestedBy is the operator, or "system" for the automatic sweep.
	RequestedBy string
	RequestID   string
}

// InspectionAccepted reports the Workflow an inspection request started or resumed, together with
// the Server's provisioning snapshot as stored when it was accepted (inspection has not started
// yet). Resumed is true when the request retried a Workflow that was waiting for attention.
type InspectionAccepted struct {
	ProvisioningStateItem
	WorkflowID string `json:"workflowId"`
	Resumed    bool   `json:"resumed"`
}

// HardwareInspectionLauncher starts hardware inspection as a durable inspect-hardware Workflow
// without making the provisioning context depend on Temporal or Workflow persistence.
//
// Implementations gate the Server (present, unlocked, EvaluateInspection) and refuse with
// ErrInspectionNotAllowed or the Workflow busy error; a requested inspection of a Server whose
// inspect-hardware Workflow waits for attention retries that Workflow instead of starting another.
// An automatic request never resumes and never starts for a Server that is not new.
type HardwareInspectionLauncher interface {
	LaunchInspection(ctx context.Context, request InspectionRequest) (*InspectionAccepted, error)
	// HasInspection reports whether the Server ever had an inspect-hardware Workflow, in any
	// status, so automatic inspection is one-shot per Server.
	HasInspection(ctx context.Context, serverID string) (bool, error)
}

// ProvisioningStateItemOf is the provisioning snapshot of a Server as stored, for responses that
// accept work without calling the provider.
func ProvisioningStateItemOf(server *serverdomain.Server) ProvisioningStateItem {
	item := ProvisioningStateItem{ServerID: server.ID}
	if p := server.Provisioning; p != nil {
		item.State, item.ProviderState, item.PowerState = p.State, p.ProviderState, p.PowerState
		item.OSSystem, item.DistroSeries, item.Ephemeral = p.OSSystem, p.DistroSeries, p.Ephemeral
		item.HWEKernel, item.Locked = p.HWEKernel, p.Locked
		item.CommissioningStatus, item.TestingStatus = p.CommissioningStatus, p.TestingStatus
		if !p.ObservedAt.IsZero() {
			item.ObservedAt = p.ObservedAt.UTC().Format(time.RFC3339)
		}
	}
	return item
}

// AutoInspectUseCase starts hardware inspection for Servers that network-boot enrollment just
// brought into a provisioner (decision 053), so an operator no longer presses Commission in the
// provisioner's UI.
//
// It is a policy sweep beside the reconciler, which stays a read-only projection. It is
// deliberately conservative: only enabled provisioners whose autoInspect setting is not off, only
// present, unlocked Servers in `new` first observed within AutoInspectWindow, and at most one
// inspect-hardware Workflow per Server ever, so a failed or canceled inspection is left for an
// operator instead of being retried in a loop. A Server enrolled with its OS kept is `deployed` and
// never matches.
type AutoInspectUseCase struct {
	integrations sitedomain.IntegrationRepository
	servers      serverdomain.ServerRepository
	inspections  HardwareInspectionLauncher
	now          func() time.Time
}

// NewAutoInspectUseCase wires the automatic inspection sweep.
func NewAutoInspectUseCase(
	integrations sitedomain.IntegrationRepository,
	servers serverdomain.ServerRepository,
	inspections HardwareInspectionLauncher,
) *AutoInspectUseCase {
	return &AutoInspectUseCase{integrations: integrations, servers: servers, inspections: inspections, now: time.Now}
}

// Run makes one sweep and returns how many inspections it started. It is safe on an interval:
// creation is one-shot per Server, and a busy, locked, or refused Server is skipped and logged
// at debug level rather than failing the sweep. Only failing to list Integrations is an error.
func (uc *AutoInspectUseCase) Run(ctx context.Context) (int, error) {
	integrations, err := uc.integrations.List(ctx, sitedomain.IntegrationFilter{
		Kind:        sitedomain.IntegrationKindProvisioner,
		EnabledOnly: true,
	})
	if err != nil {
		return 0, err
	}
	started := 0
	cutoff := uc.now().Add(-AutoInspectWindow)
	for _, integration := range integrations {
		if !AutoInspectEnabled(integration) {
			continue
		}
		// Unpaginated: a complete sweep of one provisioner's New Servers, which is small.
		result, err := uc.servers.List(ctx, serverdomain.ListFilter{
			IntegrationID:     integration.ID,
			ProvisioningState: string(provisioningdomain.MachineStatusNew),
		})
		if err != nil {
			slog.Warn("auto-inspect: list servers", "integrationId", integration.ID, "error", err)
			continue
		}
		for _, server := range result.Servers {
			if !autoInspectCandidate(server, cutoff) {
				continue
			}
			inspected, err := uc.inspections.HasInspection(ctx, server.ID)
			if err != nil {
				slog.Warn("auto-inspect: inspection history", "serverId", server.ID, "error", err)
				continue
			}
			if inspected {
				continue
			}
			accepted, err := uc.inspections.LaunchInspection(ctx, InspectionRequest{
				ServerID: server.ID, Origin: provisioningdomain.InspectionOriginAutomatic, RequestedBy: "system",
			})
			if err != nil {
				// A busy, locked, or since-changed Server is an expected reason not to act now.
				slog.Debug("auto-inspect: skipped server", "serverId", server.ID, "reason", err)
				continue
			}
			started++
			slog.Info("auto-inspect: started hardware inspection", "serverId", server.ID, "workflowId", accepted.WorkflowID)
		}
	}
	return started, nil
}

// autoInspectCandidate applies the projection-only part of the automatic policy; the launcher
// re-checks state and the live lock before it creates anything.
func autoInspectCandidate(server *serverdomain.Server, cutoff time.Time) bool {
	if server.Absent || server.Provisioning == nil {
		return false
	}
	if server.Provisioning.State != string(provisioningdomain.MachineStatusNew) || server.Provisioning.Locked {
		return false
	}
	return !server.CreatedAt.Before(cutoff)
}
