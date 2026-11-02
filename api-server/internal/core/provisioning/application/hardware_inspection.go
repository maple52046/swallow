package application

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	"github.com/maple52046/swallow/internal/core/store"
)

// SettingAutoInspect is the provisioner Integration setting that turns automatic hardware
// inspection of newly enrolled Servers off when it is "false" (contract sites-integrations.md).
// Any other value, or none, leaves it on, so Integrations created before the key existed keep the
// documented default.
const SettingAutoInspect = "autoInspect"

// AutoInspectWindow bounds automatic inspection to Servers swallow first observed this recently.
// It keeps an upgrade, or a re-enabled Integration, from powering on Machines users left New on
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
	// RequestedBy is the user, or "system" for the automatic inspection.
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
		item.InspectionRequired, item.InspectionRequiredReason = p.InspectionRequired, p.InspectionRequiredReason
		if !p.ObservedAt.IsZero() {
			item.ObservedAt = p.ObservedAt.UTC().Format(time.RFC3339)
		}
	}
	return item
}

// AutoInspectController converges a provisioner Integration's autoInspect policy (decision 053):
// a Server that network-boot enrollment just brought into an enabled provisioner whose autoInspect
// is not off is inspected once, by an inspect-hardware Workflow, so a user no longer presses
// Commission in the provisioner's UI. The setting is the user's declared intent; the controller
// starts the Workflow that realizes it.
//
// It is deliberately conservative: only present, unlocked Servers in `new` first observed within
// AutoInspectWindow, and at most one inspect-hardware Workflow per Server ever, so a failed or
// canceled inspection is left for a user instead of being retried. A Server enrolled with its OS
// kept is `deployed` and never matches. It is keyed by Server: a Server's change wakes it, an
// Integration's change re-examines every candidate, and the periodic resync covers a Server that
// was busy or locked when it last looked.
type AutoInspectController struct {
	integrations sitedomain.IntegrationRepository
	servers      serverdomain.ServerRepository
	inspections  HardwareInspectionLauncher
	now          func() time.Time
}

// NewAutoInspectController wires the automatic inspection controller. Every dependency is required.
func NewAutoInspectController(
	integrations sitedomain.IntegrationRepository,
	servers serverdomain.ServerRepository,
	inspections HardwareInspectionLauncher,
) *AutoInspectController {
	return &AutoInspectController{integrations: integrations, servers: servers, inspections: inspections, now: time.Now}
}

// Name implements convergence.Controller.
func (c *AutoInspectController) Name() string { return "auto-inspect" }

// Collections implements convergence.Controller: a Server appears or changes state in `servers`,
// and an Integration turns autoInspect on or is enabled in `integrations`.
func (c *AutoInspectController) Collections() []string { return []string{"servers", "integrations"} }

// KeysForChange implements convergence.Controller: a changed Server is its own key, a deleted one
// needs nothing, and a changed Integration re-examines every candidate.
func (c *AutoInspectController) KeysForChange(ctx context.Context, change store.Change) ([]string, error) {
	if change.Collection == "integrations" {
		return c.Keys(ctx)
	}
	if change.Operation == store.ChangeDelete {
		return nil, nil
	}
	return []string{change.Key}, nil
}

// Keys implements convergence.Controller with the `new` Servers of every enabled provisioner whose
// autoInspect is on.
func (c *AutoInspectController) Keys(ctx context.Context) ([]string, error) {
	integrations, err := c.integrations.List(ctx, sitedomain.IntegrationFilter{
		Kind:        sitedomain.IntegrationKindProvisioner,
		EnabledOnly: true,
	})
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, integration := range integrations {
		if !AutoInspectEnabled(integration) {
			continue
		}
		// Unpaginated: one provisioner's New Servers, which are few.
		result, err := c.servers.List(ctx, serverdomain.ListFilter{
			IntegrationID:     integration.ID,
			ProvisioningState: string(provisioningdomain.MachineStatusNew),
		})
		if err != nil {
			return nil, err
		}
		for _, server := range result.Servers {
			keys = append(keys, server.ID)
		}
	}
	return keys, nil
}

// Reconcile implements convergence.Controller: it starts the automatic inspection of one Server
// when the policy applies and the Server never had an inspection. A busy, locked, or since-changed
// Server is an expected reason not to act now and is logged at debug level; only failing to read
// the Server, its Integration, or its inspection history is an error, which the runtime retries.
func (c *AutoInspectController) Reconcile(ctx context.Context, request convergence.Request) error {
	server, err := c.servers.FindByID(ctx, request.Key)
	if errors.Is(err, serverdomain.ErrServerNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !autoInspectCandidate(server, c.now().Add(-AutoInspectWindow)) {
		return nil
	}
	integration, err := c.integrations.FindByID(ctx, server.Source.IntegrationID)
	if errors.Is(err, sitedomain.ErrIntegrationNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if integration.Kind != sitedomain.IntegrationKindProvisioner || !integration.Enabled || !AutoInspectEnabled(integration) {
		return nil
	}
	inspected, err := c.inspections.HasInspection(ctx, server.ID)
	if err != nil || inspected {
		return err
	}
	accepted, err := c.inspections.LaunchInspection(ctx, InspectionRequest{
		ServerID: server.ID, Origin: provisioningdomain.InspectionOriginAutomatic, RequestedBy: "system",
	})
	if err != nil {
		slog.Debug("auto-inspect: skipped server", "serverId", server.ID, "reason", err)
		return nil
	}
	slog.Info("auto-inspect: started hardware inspection", "serverId", server.ID, "workflowId", accepted.WorkflowID)
	return nil
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
