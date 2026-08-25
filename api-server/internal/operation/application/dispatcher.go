package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// InventorySource uses the same application projection as the public dynamic inventory.
type InventorySource interface {
	Inventory(ctx context.Context, siteID string) (map[string]any, error)
}

// Dispatcher claims pending operations and executes different sites concurrently.
type Dispatcher struct {
	operations     operationdomain.ExecutionRepository
	leases         operationdomain.SiteLeaseRepository
	configurations operationdomain.AutomationConfigurationRepository
	catalog        operationdomain.PlaybookCatalog
	runner         operationdomain.Runner
	inventory      InventorySource
	interval       time.Duration
	leaseDuration  time.Duration
	owner          string
}

// NewDispatcher constructs an embedded dispatcher instance.
func NewDispatcher(
	operations operationdomain.ExecutionRepository,
	leases operationdomain.SiteLeaseRepository,
	configurations operationdomain.AutomationConfigurationRepository,
	catalog operationdomain.PlaybookCatalog,
	runner operationdomain.Runner,
	inventory InventorySource,
	interval, leaseDuration time.Duration,
) *Dispatcher {
	return &Dispatcher{
		operations: operations, leases: leases, configurations: configurations,
		catalog: catalog, runner: runner, inventory: inventory,
		interval: interval, leaseDuration: leaseDuration, owner: uuid.NewString(),
	}
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

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	result := make(chan error, 1)
	go func() {
		result <- d.runner.Run(runCtx, operationdomain.RunnerInput{
			Operation: operation, Configuration: configuration, Credential: credential,
			Inventory: inventory, PlaybookPath: playbookPath,
		})
	}()

	heartbeat := time.NewTicker(d.leaseDuration / 3)
	defer heartbeat.Stop()
	for {
		select {
		case runErr := <-result:
			if runErr != nil {
				status := operationdomain.StatusFailed
				if runCtx.Err() != nil {
					status = operationdomain.StatusIndeterminate
				}
				d.finish(operation, owner, status, runErr)
			} else {
				d.finish(operation, owner, operationdomain.StatusSucceeded, nil)
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
