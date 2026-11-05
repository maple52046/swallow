package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"

	"github.com/maple52046/swallow/config"
	"github.com/maple52046/swallow/internal/core/convergence"
	platformapp "github.com/maple52046/swallow/internal/core/platform/application"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	serverapp "github.com/maple52046/swallow/internal/core/server/application"
	"github.com/maple52046/swallow/internal/shared/redact"
)

// observationPasses are the worker's periodic passes that read external systems into swallow's
// observed state: the provisioners' machines (reconcile), their attached hardware (inventory
// sweep), the Platforms' members (membership sync), and the BMCs' Redfish capability. Each runs
// under a singleton lease (convergence.Periodic), so one process at a time makes a pass, and none
// runs in the API process. A pass changes nothing in the system it reads.
func observationPasses(
	cfg config.APIConfig,
	reconcile *provisioningapp.ReconcileUseCase,
	inventory *provisioningapp.InventorySweepUseCase,
	membership *platformapp.MembershipSyncUseCase,
	bootMedia *serverapp.BootMediaUseCase,
) []convergence.Periodic {
	return []convergence.Periodic{
		{Name: "reconcile", Interval: cfg.ReconcileInterval, Run: func(ctx context.Context) error {
			return reconcileOnce(ctx, reconcile)
		}},
		// Attached hardware costs a call per machine and changes only at commissioning, so it is
		// read on its own slower interval.
		{Name: "inventory-sweep", Interval: cfg.InventoryInterval, Run: func(ctx context.Context) error {
			return sweepInventoryOnce(ctx, inventory)
		}},
		// A separate pass from reconcile because the two read different systems and fail
		// independently: an unreachable Platform API must not stall the machine projection.
		{Name: "membership-sync", Interval: cfg.ReconcileInterval, Run: func(ctx context.Context) error {
			return syncMembershipOnce(ctx, membership)
		}},
		// The enrollment-time Redfish detection (decision 047): a Server reconcile just projected is
		// probed on the next pass, a few BMCs in parallel, failures logged per Server.
		{Name: "redfish-capability-sweep", Interval: cfg.RedfishProbeInterval, Run: func(ctx context.Context) error {
			_, err := bootMedia.ProbeStale(ctx, cfg.RedfishProbeMaxAge, 4)
			return err
		}},
	}
}

// reconcileOnce reads every enabled provisioner's machines into the Server projection. It returns
// an error only when the provisioners cannot be listed at all; one provisioner failing is logged
// in its report and does not stop the others. See decision 069.
func reconcileOnce(ctx context.Context, reconcile *provisioningapp.ReconcileUseCase) error {
	reports, err := reconcile.ExecuteAll(ctx)
	if err != nil {
		return fmt.Errorf("reconciler: list provisioners: %w", err)
	}

	for _, report := range reports {
		if report.Error != nil {
			log.Printf("reconciler: %s (%s) failed: %s",
				report.IntegrationName, report.IntegrationID, *report.Error)
			continue
		}

		// Conflicts are logged even at zero counts elsewhere, because they are the
		// part that needs a human and would otherwise only appear if someone asked.
		if len(report.Conflicts) > 0 {
			log.Printf("reconciler: %s: %d machines, +%d ~%d relinked=%d absent=%d, %d CONFLICTS need attention",
				report.IntegrationName, report.Machines, report.Created, report.Updated,
				report.Relinked, report.MarkedAbsent, len(report.Conflicts))
			for _, conflict := range report.Conflicts {
				log.Printf("reconciler: conflict on machine %s (%s): %s",
					conflict.ProviderMachineID, conflict.Hostname, conflict.Reason)
			}
			continue
		}

		// Only log a quiet pass when it changed something, so that a steady fleet
		// does not fill the log with identical lines.
		if report.Created > 0 || report.Relinked > 0 || report.MarkedAbsent > 0 {
			log.Printf("reconciler: %s: %d machines, +%d ~%d relinked=%d absent=%d",
				report.IntegrationName, report.Machines, report.Created, report.Updated,
				report.Relinked, report.MarkedAbsent)
		}
	}
	return nil
}

// sweepInventoryOnce refreshes every provisioner's attached-hardware inventory (GPUs). It returns
// an error only when the provisioners cannot be listed.
func sweepInventoryOnce(ctx context.Context, sweep *provisioningapp.InventorySweepUseCase) error {
	reports, err := sweep.ExecuteAll(ctx)
	if err != nil {
		return fmt.Errorf("inventory sweep: list provisioners: %w", err)
	}

	for _, report := range reports {
		if report.Error != nil {
			log.Printf("inventory sweep: %s failed: %s", report.IntegrationName, *report.Error)
			continue
		}
		if report.Updated > 0 || report.Skipped > 0 {
			log.Printf("inventory sweep: %s: %d servers, %d updated, %d skipped",
				report.IntegrationName, report.Servers, report.Updated, report.Skipped)
		}
	}
	return nil
}

// syncMembershipOnce reads every registered Platform's membership. It returns an error only when
// the Platforms cannot be listed. Each report is one structured log line keyed by platformId and
// integrationId; a failure's error is the redacted sentence membership sync recorded as the
// Platform's sync.lastError, with the platform API failure's kind, transport failure, endpoint,
// and operation as their own attributes when the cause is one.
func syncMembershipOnce(ctx context.Context, membership *platformapp.MembershipSyncUseCase) error {
	reports, err := membership.ExecuteAll(ctx)
	if err != nil {
		return fmt.Errorf("membership sync: list platforms: %w", err)
	}

	for _, report := range reports {
		attrs := []any{"platformId", report.PlatformID, "platformName", report.PlatformName, "integrationId", report.IntegrationID}
		if report.Error != nil {
			attrs = append(attrs, "error", *report.Error)
			var readerErr *platformdomain.ReaderError
			if errors.As(report.Err, &readerErr) {
				attrs = append(attrs, "readerErrorKind", string(readerErr.Kind), "endpoint", redact.URL(readerErr.Endpoint),
					"operation", readerErr.Operation)
				if readerErr.Transport != "" {
					attrs = append(attrs, "transport", string(readerErr.Transport))
				}
			}
			slog.Warn("platform membership sync failed", attrs...)
			continue
		}
		attrs = append(attrs, "members", report.Members, "matched", report.Matched, "cleared", report.Cleared)
		// Unmatched members mean the platform contains machines swallow does not manage,
		// which is worth surfacing rather than quietly ignoring.
		if len(report.Unmatched) > 0 {
			slog.Info("platform membership synced with unmatched members", append(attrs, "unmatched", report.Unmatched)...)
			continue
		}
		if report.Matched > 0 || report.Cleared > 0 {
			slog.Info("platform membership synced", attrs...)
		}
	}
	return nil
}
