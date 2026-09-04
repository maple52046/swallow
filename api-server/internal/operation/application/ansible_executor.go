package application

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// StepCompletionObserver records domain projections produced by successful Ansible
// Steps. The executor remains unaware of Platform internals.
type StepCompletionObserver interface {
	AnsibleStepSucceeded(ctx context.Context, execution *operationdomain.AnsibleExecution, result operationdomain.RunnerResult) error
}

// AnsibleQueueWorker is the only process allowed to own ansible-runner subprocesses.
type AnsibleQueueWorker struct {
	executions     operationdomain.AnsibleExecutionRepository
	configurations operationdomain.AutomationConfigurationRepository
	catalog        operationdomain.PlaybookCatalog
	runner         operationdomain.Runner
	inventory      InventorySource
	protection     serverdomain.MutationGuard
	observer       StepCompletionObserver
	secrets        operationdomain.OperationSecretRepository
	interval       time.Duration
	leaseDuration  time.Duration
	owner          string
	parallelism    int
	semaphore      chan struct{}
	wait           sync.WaitGroup
}

func NewAnsibleQueueWorker(
	executions operationdomain.AnsibleExecutionRepository,
	configurations operationdomain.AutomationConfigurationRepository,
	catalog operationdomain.PlaybookCatalog,
	runner operationdomain.Runner,
	inventory InventorySource,
	protection serverdomain.MutationGuard,
	observer StepCompletionObserver,
	interval, leaseDuration time.Duration,
	parallelism int,
	secrets ...operationdomain.OperationSecretRepository,
) *AnsibleQueueWorker {
	if interval <= 0 {
		interval = time.Second
	}
	if leaseDuration <= 0 {
		leaseDuration = 90 * time.Second
	}
	if parallelism <= 0 {
		parallelism = 4
	}
	worker := &AnsibleQueueWorker{
		executions: executions, configurations: configurations, catalog: catalog,
		runner: runner, inventory: inventory, protection: protection, observer: observer,
		interval: interval, leaseDuration: leaseDuration, parallelism: parallelism,
		owner: uuid.NewString(), semaphore: make(chan struct{}, parallelism),
	}
	if len(secrets) > 0 {
		worker.secrets = secrets[0]
	}
	return worker
}

// Run recovers abandoned executions as attention-required, then claims queued work.
func (w *AnsibleQueueWorker) Run(ctx context.Context) {
	w.recover(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		w.claim(ctx)
		select {
		case <-ctx.Done():
			w.wait.Wait()
			return
		case <-ticker.C:
		}
	}
}

func (w *AnsibleQueueWorker) recover(ctx context.Context) {
	count, err := w.executions.MarkExpiredRequiresAttention(ctx, time.Now().UTC())
	if err != nil {
		slog.Error("recover Ansible executions", "error", err)
	} else if count > 0 {
		slog.Warn("Ansible executions require attention after executor loss", "count", count)
	}
}

func (w *AnsibleQueueWorker) claim(ctx context.Context) {
	w.recover(ctx)
	for len(w.semaphore) < w.parallelism {
		execution, err := w.executions.ClaimNext(ctx, w.owner, time.Now().UTC().Add(w.leaseDuration))
		if err != nil {
			slog.Error("claim Ansible execution", "error", err)
			return
		}
		if execution == nil {
			return
		}
		w.semaphore <- struct{}{}
		w.wait.Add(1)
		go func(item *operationdomain.AnsibleExecution) {
			defer func() { <-w.semaphore; w.wait.Done() }()
			w.execute(ctx, item)
		}(execution)
	}
}

func (w *AnsibleQueueWorker) execute(ctx context.Context, execution *operationdomain.AnsibleExecution) {
	finish := func(status operationdomain.AnsibleExecutionStatus, err error) {
		reason := ""
		if err != nil {
			reason = err.Error()
		}
		if finishErr := w.executions.Finish(context.Background(), execution.ID, w.owner, status, reason); finishErr != nil {
			slog.Error("finish Ansible execution", "executionId", execution.ID, "error", finishErr)
		}
	}
	if w.protection != nil {
		if err := w.protection.RequireUnlocked(ctx, execution.TargetServerIDs); err != nil {
			finish(operationdomain.AnsibleFailed, err)
			return
		}
	}
	var err error
	var configuration *operationdomain.AutomationConfiguration
	if execution.Configuration != nil {
		configuration = &operationdomain.AutomationConfiguration{
			SiteID: execution.SiteID, Enabled: true, HasCredential: true,
			SSHUser: execution.Configuration.SSHUser, SSHPort: execution.Configuration.SSHPort,
			KnownHosts: execution.Configuration.KnownHosts,
		}
	} else {
		configuration, err = w.configurations.FindBySiteID(ctx, execution.SiteID)
		if err != nil {
			finish(operationdomain.AnsibleFailed, err)
			return
		}
	}
	credential, err := w.configurations.Credential(ctx, execution.SiteID)
	if err != nil {
		finish(operationdomain.AnsibleFailed, err)
		return
	}
	playbookPath, err := w.catalog.Resolve(execution.Playbook)
	if err != nil {
		finish(operationdomain.AnsibleFailed, err)
		return
	}
	inventory := execution.Inventory
	if inventory == nil {
		inventory, err = w.inventory.Inventory(ctx, execution.SiteID)
		if err != nil {
			finish(operationdomain.AnsibleFailed, err)
			return
		}
	}

	secretVars := map[string]any{}
	for name, reference := range execution.SecretRefs {
		if w.secrets == nil {
			finish(operationdomain.AnsibleFailed, fmt.Errorf("secret resolver is unavailable"))
			return
		}
		value, resolveErr := w.secrets.Resolve(ctx, reference)
		if resolveErr != nil {
			finish(operationdomain.AnsibleFailed, fmt.Errorf("resolve secret reference: %w", resolveErr))
			return
		}
		secretVars[name] = value
	}

	runOperation := &operationdomain.ExecutionOperation{
		ID: execution.OperationID, SiteID: execution.SiteID,
		TargetServerIDs: execution.TargetServerIDs, ExtraVars: execution.ExtraVars,
		Execution: operationdomain.Execution{RunID: execution.RunID, Playbook: execution.Playbook, Status: operationdomain.StatusRunning},
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type outcome struct {
		result operationdomain.RunnerResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, runErr := w.runner.Run(runCtx, operationdomain.RunnerInput{
			Operation: runOperation, Configuration: configuration, Credential: credential,
			Inventory: inventory, PlaybookPath: playbookPath, SecretVars: secretVars,
		})
		done <- outcome{result: result, err: runErr}
	}()

	ticker := time.NewTicker(w.leaseDuration / 3)
	defer ticker.Stop()
	for {
		select {
		case completed := <-done:
			if ctx.Err() != nil {
				// Process ownership was lost with this executor. Leave the lease to expire;
				// the next executor will require explicit operator attention.
				return
			}
			latest, findErr := w.executions.FindByID(ctx, execution.ID)
			if findErr == nil && latest.CancelRequested {
				finish(operationdomain.AnsibleCanceled, nil)
				return
			}
			if completed.err != nil {
				finish(operationdomain.AnsibleFailed, completed.err)
				return
			}
			if w.observer != nil {
				if observeErr := w.observer.AnsibleStepSucceeded(context.Background(), execution, completed.result); observeErr != nil {
					finish(operationdomain.AnsibleFailed, fmt.Errorf("complete Ansible Step projections: %w", observeErr))
					return
				}
			}
			finish(operationdomain.AnsibleSucceeded, nil)
			return
		case <-ticker.C:
			latest, findErr := w.executions.FindByID(ctx, execution.ID)
			if findErr != nil {
				cancel()
				finish(operationdomain.AnsibleRequiresAttention, fmt.Errorf("observe execution: %w", findErr))
				return
			}
			if latest.CancelRequested {
				cancel()
				continue
			}
			if err := w.executions.Renew(ctx, execution.ID, w.owner, time.Now().UTC().Add(w.leaseDuration)); err != nil {
				cancel()
				return
			}
		case <-ctx.Done():
			cancel()
			return
		}
	}
}
