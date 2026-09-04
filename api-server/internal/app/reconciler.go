package app

import (
	"context"
	"log"
	"time"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	platformapp "github.com/maple52046/swallow/internal/platform/application"
	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
)

// runReconciler polls every enabled provisioner on an interval until ctx is cancelled.
//
// This is one of only two background loops in the service, and both are periodic
// readers of external state. swallow executes nothing itself; a loop that reads is the
// most it needs. See docs/decisions/001-system-ownership-boundaries.md.
func runReconciler(ctx context.Context, reconcile *provisioningapp.ReconcileUseCase, interval time.Duration) {
	// Run once at startup so that a restart does not leave the projection stale for a
	// whole interval.
	reconcileOnce(ctx, reconcile)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("reconciler stopped")
			return
		case <-ticker.C:
			reconcileOnce(ctx, reconcile)
		}
	}
}

func reconcileOnce(ctx context.Context, reconcile *provisioningapp.ReconcileUseCase) {
	reports, err := reconcile.ExecuteAll(ctx)
	if err != nil {
		// Failing to list integrations at all, as opposed to one provisioner
		// failing, which is reported per-integration inside the report.
		log.Printf("reconciler: cannot list provisioners: %v", err)
		return
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
}

// runInventorySweep refreshes every provisioner's attached-hardware inventory (GPUs) on
// its own slower interval.
//
// Separate from the reconciler because attached hardware costs a call per machine and
// changes only at commissioning: polling it as often as lifecycle state would multiply
// the reconciler's request count for data that is nearly static.
func runInventorySweep(ctx context.Context, sweep *provisioningapp.InventorySweepUseCase, interval time.Duration) {
	sweepInventoryOnce(ctx, sweep)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("inventory sweep stopped")
			return
		case <-ticker.C:
			sweepInventoryOnce(ctx, sweep)
		}
	}
}

func sweepInventoryOnce(ctx context.Context, sweep *provisioningapp.InventorySweepUseCase) {
	reports, err := sweep.ExecuteAll(ctx)
	if err != nil {
		log.Printf("inventory sweep: cannot list provisioners: %v", err)
		return
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
}

// runMembershipSync reads every registered platform's membership on an interval.
//
// Separate from the provisioner reconciler because they read different systems and fail
// independently: an unreachable platform API must not stall the inventory projection.
func runMembershipSync(ctx context.Context, membership *platformapp.MembershipSyncUseCase, interval time.Duration) {
	syncMembershipOnce(ctx, membership)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("membership sync stopped")
			return
		case <-ticker.C:
			syncMembershipOnce(ctx, membership)
		}
	}
}

// runAutoExporterDeploy installs the Ansible exporters on newly deployed, ansible-owned
// servers on an interval. It shares the reconcile cadence: a server only becomes eligible
// after a reconcile pass has projected it as deployed, so there is nothing to do more
// often than that. Creation is deduplicated per server, so a rerun is cheap.
func runAutoExporterDeploy(ctx context.Context, autoDeploy *operationapp.AutoExporterDeployUseCase, interval time.Duration) {
	deployOnce := func() {
		if err := autoDeploy.Run(ctx); err != nil {
			log.Printf("auto-exporters: sweep failed: %v", err)
		}
	}

	deployOnce()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("auto-exporter deploy stopped")
			return
		case <-ticker.C:
			deployOnce()
		}
	}
}

func syncMembershipOnce(ctx context.Context, membership *platformapp.MembershipSyncUseCase) {
	reports, err := membership.ExecuteAll(ctx)
	if err != nil {
		log.Printf("membership sync: cannot list platforms: %v", err)
		return
	}

	for _, report := range reports {
		if report.Error != nil {
			log.Printf("membership sync: %s failed: %s", report.PlatformName, *report.Error)
			continue
		}
		// Unmatched members mean the platform contains machines swallow does not manage,
		// which is worth surfacing rather than quietly ignoring.
		if len(report.Unmatched) > 0 {
			log.Printf("membership sync: %s: %d members, %d matched, %d cleared, unmatched: %v",
				report.PlatformName, report.Members, report.Matched, report.Cleared, report.Unmatched)
			continue
		}
		if report.Matched > 0 || report.Cleared > 0 {
			log.Printf("membership sync: %s: %d members, %d matched, %d cleared",
				report.PlatformName, report.Members, report.Matched, report.Cleared)
		}
	}
}
