package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// InventorySource uses the same application projection as the public dynamic inventory.
type InventorySource interface {
	Inventory(ctx context.Context, siteID string) (map[string]any, error)
}

// CompletionObserver is notified when an operation finishes successfully, with whatever
// the run captured. It lets another context react to an operation whose intent it owns —
// recording the credential a cluster deployment produced, for instance — without the
// operation context depending on that context. A failure to react is the observer's to
// log; it does not change the operation's own outcome, which the run already decided.
type CompletionObserver interface {
	OperationSucceeded(ctx context.Context, operation *operationdomain.ExecutionOperation, result operationdomain.RunnerResult)
}

// Dispatcher claims pending operations and executes different sites concurrently.
type Dispatcher struct {
	operations     operationdomain.ExecutionRepository
	leases         operationdomain.SiteLeaseRepository
	configurations operationdomain.AutomationConfigurationRepository
	catalog        operationdomain.PlaybookCatalog
	runner         operationdomain.Runner
	inventory      InventorySource
	observer       CompletionObserver
	protection     serverdomain.MutationGuard
	interval       time.Duration
	leaseDuration  time.Duration
	owner          string
}

// NewDispatcher constructs an embedded dispatcher instance. observer may be nil when no
// context needs to react to successful operations.
func NewDispatcher(
	operations operationdomain.ExecutionRepository,
	leases operationdomain.SiteLeaseRepository,
	configurations operationdomain.AutomationConfigurationRepository,
	catalog operationdomain.PlaybookCatalog,
	runner operationdomain.Runner,
	inventory InventorySource,
	observer CompletionObserver,
	interval, leaseDuration time.Duration,
	protection ...serverdomain.MutationGuard,
) *Dispatcher {
	dispatcher := &Dispatcher{
		operations: operations, leases: leases, configurations: configurations,
		catalog: catalog, runner: runner, inventory: inventory, observer: observer,
		interval: interval, leaseDuration: leaseDuration, owner: uuid.NewString(),
	}
	if len(protection) > 0 {
		dispatcher.protection = protection[0]
	}
	return dispatcher
}

// Run recovers expired work, then continuously looks for pending intent.
func (d *Dispatcher) Run(ctx context.Context) {
	d.recoverExpired(ctx)
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		d.dispatch(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) recoverExpired(ctx context.Context) {
	now := time.Now().UTC()
	if count, err := d.operations.MarkExpiredIndeterminate(ctx, now); err != nil {
		slog.Error("recover expired operations", "error", err)
	} else if count > 0 {
		slog.Warn("marked expired operations indeterminate", "count", count)
	}
	if err := d.leases.ReleaseExpired(ctx, now); err != nil {
		slog.Error("release expired site leases", "error", err)
	}
}

func (d *Dispatcher) dispatch(ctx context.Context) {
	d.recoverExpired(ctx)
	result, err := d.operations.List(ctx, operationdomain.ExecutionListFilter{
		Status: operationdomain.StatusPending, Limit: 100,
	})
	if err != nil {
		slog.Error("list pending operations", "error", err)
		return
	}
	for _, operation := range result.Operations {
		owner := d.owner + ":" + operation.ID
		expiresAt := time.Now().UTC().Add(d.leaseDuration)
		acquired, err := d.leases.Acquire(ctx, operation.SiteID, owner, expiresAt)
		if err != nil {
			slog.Error("acquire operation site lease", "operationId", operation.ID, "error", err)
			continue
		}
		if !acquired {
			continue
		}
		claimed, err := d.operations.Claim(ctx, operation.ID, owner, expiresAt)
		if err != nil || !claimed {
			_ = d.leases.Release(ctx, operation.SiteID, owner)
			if err != nil {
				slog.Error("claim operation", "operationId", operation.ID, "error", err)
			}
			continue
		}
		operation.Execution.Status = operationdomain.StatusRunning
		now := time.Now().UTC()
		operation.Execution.StartedAt = &now
		operation.Execution.LeaseOwner = owner
		operation.Execution.LeaseExpiresAt = &expiresAt
		go d.execute(ctx, operation, owner)
	}
}

func (d *Dispatcher) execute(ctx context.Context, operation *operationdomain.ExecutionOperation, owner string) {
	defer func() {
		if err := d.leases.Release(context.Background(), operation.SiteID, owner); err != nil {
			slog.Error("release operation site lease", "operationId", operation.ID, "error", err)
		}
	}()
	if d.protection != nil {
		if err := d.protection.RequireUnlocked(ctx, operation.TargetServerIDs); err != nil {
			d.finish(operation, owner, operationdomain.StatusFailed, err)
			return
		}
	}
	configuration, err := d.configurations.FindBySiteID(ctx, operation.SiteID)
	if err != nil {
		d.finish(operation, owner, operationdomain.StatusFailed, err)
		return
	}
	credential, err := d.configurations.Credential(ctx, operation.SiteID)
	if err != nil {
		d.finish(operation, owner, operationdomain.StatusFailed, err)
		return
	}
	playbookPath, err := d.catalog.Resolve(operation.Execution.Playbook)
	if err != nil {
		d.finish(operation, owner, operationdomain.StatusFailed, err)
		return
	}
	inventory, err := d.inventory.Inventory(ctx, operation.SiteID)
	if err != nil {
		d.finish(operation, owner, operationdomain.StatusFailed, err)
		return
	}
	secretVars, err := d.operations.SecretVars(ctx, operation.ID)
	if err != nil {
		d.finish(operation, owner, operationdomain.StatusFailed, err)
		return
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	type runOutcome struct {
		result operationdomain.RunnerResult
		err    error
	}
	done := make(chan runOutcome, 1)
	go func() {
		result, runErr := d.runner.Run(runCtx, operationdomain.RunnerInput{
			Operation: operation, Configuration: configuration, Credential: credential,
			Inventory: inventory, PlaybookPath: playbookPath, SecretVars: secretVars,
		})
		done <- runOutcome{result: result, err: runErr}
	}()

	heartbeat := time.NewTicker(d.leaseDuration / 3)
	defer heartbeat.Stop()
	for {
		select {
		case outcome := <-done:
			if outcome.err != nil {
				status := operationdomain.StatusFailed
				if runCtx.Err() != nil {
					status = operationdomain.StatusIndeterminate
				}
				d.finish(operation, owner, status, outcome.err)
			} else {
				d.finish(operation, owner, operationdomain.StatusSucceeded, nil)
				d.notifySuccess(operation, outcome.result)
			}
			return
		case <-heartbeat.C:
			expiresAt := time.Now().UTC().Add(d.leaseDuration)
			operation.Execution.LeaseExpiresAt = &expiresAt
			if err := d.leases.Renew(ctx, operation.SiteID, owner, expiresAt); err != nil {
				cancelRun()
				d.finish(operation, owner, operationdomain.StatusIndeterminate,
					fmt.Errorf("renew site lease: %w", err))
				return
			}
			if err := d.operations.UpdateExecution(ctx, operation.ID, owner, operation.Execution); err != nil {
				cancelRun()
				d.finish(operation, owner, operationdomain.StatusIndeterminate,
					fmt.Errorf("renew operation lease: %w", err))
				return
			}
		case <-ctx.Done():
			cancelRun()
			d.finish(operation, owner, operationdomain.StatusIndeterminate, ctx.Err())
			return
		}
	}
}

// notifySuccess lets an observer react to a successful operation. Its failure is logged
// and does not change the operation's outcome: the run already succeeded, and a follow-on
// such as recording a cluster credential can be recovered by retrying the operation.
func (d *Dispatcher) notifySuccess(operation *operationdomain.ExecutionOperation, result operationdomain.RunnerResult) {
	if d.observer == nil {
		return
	}
	// A fresh context: the dispatcher's ctx may already be cancelled at shutdown, but the
	// operation succeeded and its side effects should still be recorded.
	d.observer.OperationSucceeded(context.Background(), operation, result)
}

func (d *Dispatcher) finish(operation *operationdomain.ExecutionOperation, owner string, status operationdomain.Status, runErr error) {
	now := time.Now().UTC()
	operation.Execution.Status = status
	operation.Execution.FinishedAt = &now
	operation.Execution.LeaseExpiresAt = nil
	operation.Execution.LeaseOwner = ""
	if runErr != nil {
		operation.Execution.StatusReason = fmt.Sprintf("%v", runErr)
	}
	if err := d.operations.UpdateExecution(context.Background(), operation.ID, owner, operation.Execution); err != nil {
		slog.Error("finish operation", "operationId", operation.ID, "status", status, "error", err)
	}
}
