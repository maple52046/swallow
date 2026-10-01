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

// InventorySource produces the same serverId-keyed inventory projection as the public
// dynamic-inventory endpoint, so an Ansible Task runs against swallow's own identifiers.
type InventorySource interface {
	Inventory(ctx context.Context, siteID string) (map[string]any, error)
}

// StepCompletionObserver records domain projections produced by successful Ansible
// Steps. The executor remains unaware of Platform internals.
type StepCompletionObserver interface {
	AnsibleStepSucceeded(ctx context.Context, execution *operationdomain.AnsibleExecution, result operationdomain.RunnerResult) error
}

// streamInterval bounds how often the executor tails a run's events into the durable event store
// and refreshes its progress. It is fast enough to feel live under the dashboard's polling and slow
// enough to keep the per-run read/write load negligible next to the ansible-runner subprocess.
const streamInterval = 2 * time.Second

// AnsibleQueueWorker is the only process allowed to own ansible-runner subprocesses.
type AnsibleQueueWorker struct {
	executions     operationdomain.AnsibleExecutionRepository
	configurations operationdomain.AutomationConfigurationRepository
	catalog        operationdomain.PlaybookCatalog
	runner         operationdomain.Runner
	// events is the durable stream of per-task results written while a run advances. It is
	// optional: a nil store, or a runner without IncrementalEventReader, simply disables live
	// streaming without affecting the run's outcome.
	events        operationdomain.AnsibleEventRepository
	inventory     InventorySource
	protection    serverdomain.MutationGuard
	leases        operationdomain.ResourceLeaseRepository
	observer      StepCompletionObserver
	secrets       operationdomain.OperationSecretRepository
	interval      time.Duration
	leaseDuration time.Duration
	owner         string
	parallelism   int
	semaphore     chan struct{}
	wait          sync.WaitGroup
}

func NewAnsibleQueueWorker(
	executions operationdomain.AnsibleExecutionRepository,
	configurations operationdomain.AutomationConfigurationRepository,
	catalog operationdomain.PlaybookCatalog,
	runner operationdomain.Runner,
	events operationdomain.AnsibleEventRepository,
	inventory InventorySource,
	protection serverdomain.MutationGuard,
	leases operationdomain.ResourceLeaseRepository,
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
		runner: runner, events: events, inventory: inventory, protection: protection, leases: leases, observer: observer,
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
	// The provider-owned Server Lock and the Swallow resource lease are independent
	// protections; both must hold before this executor mutates a host. A lost fencing
	// token means the owning workflow no longer has exclusivity, so refuse to start and
	// require operator attention rather than run against an un-owned resource.
	if err := w.validateLeases(ctx, execution); err != nil {
		finish(operationdomain.AnsibleRequiresAttention, err)
		return
	}
	var err error
	var configuration *operationdomain.AutomationConfiguration
	if execution.Configuration != nil {
		configuration = &operationdomain.AutomationConfiguration{
			SiteID: execution.SiteID, Enabled: true,
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

	// Live streaming is best-effort and optional: it needs both an event store and a runner that
	// can read its own events incrementally. When either is absent the run behaves exactly as
	// before, just without live progress.
	reader, canStream := w.runner.(operationdomain.IncrementalEventReader)
	canStream = canStream && w.events != nil
	stream := &runProgressState{}
	streamTicker := time.NewTicker(streamInterval)
	defer streamTicker.Stop()

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
			// Final flush so the last task results and current-task label land before the
			// terminal status is written and the dashboard stops polling.
			if canStream {
				w.streamProgress(ctx, execution, reader, stream)
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
		case <-streamTicker.C:
			if canStream {
				w.streamProgress(ctx, execution, reader, stream)
			}
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
			// Re-check fencing on every renew tick: if the workflow's resource lease was
			// taken over while this runner was executing, stop and require attention
			// instead of continuing to mutate a host we no longer exclusively own.
			if err := w.validateLeases(ctx, execution); err != nil {
				cancel()
				finish(operationdomain.AnsibleRequiresAttention, err)
				return
			}
			if err := w.executions.Renew(ctx, execution.ID, w.owner, time.Now().UTC().Add(w.leaseDuration)); err != nil {
				// A failed renew means this executor may have lost ownership (a fencing
				// conflict) or the store is unreachable. Either way the runner outcome is
				// now unknown, so stop the subprocess and hand the execution to an operator
				// rather than silently leaving it "running" until the lease expires. If
				// another executor already owns it, Finish is a no-op under the owner guard.
				cancel()
				finish(operationdomain.AnsibleRequiresAttention, fmt.Errorf("renew execution lease: %w", err))
				return
			}
		case <-ctx.Done():
			cancel()
			return
		}
	}
}

// runProgressState accumulates a run's live progress across streaming ticks. lastSeq is the event
// ordinal cursor; prog carries the cumulative counts and current play/task. It advances only after
// new events are durably appended, so a failed append is retried on the next tick without losing or
// double-counting events.
type runProgressState struct {
	lastSeq int
	prog    operationdomain.AnsibleRunProgress
}

// apply folds a delta into the accumulator: it advances the cursor, carries forward the newest
// play/task, and counts each host result. A changed result also counts as ok, matching Ansible's
// recap where "changed" is a subset of "ok".
func (s *runProgressState) apply(delta operationdomain.RunProgressDelta) {
	if delta.CurrentPlay != "" {
		s.prog.CurrentPlay = delta.CurrentPlay
	}
	if delta.CurrentTask != "" {
		s.prog.CurrentTask = delta.CurrentTask
	}
	for _, event := range delta.Events {
		s.prog.Total++
		switch event.Status {
		case "ok":
			s.prog.OK++
		case "failed":
			s.prog.Failed++
		case "unreachable":
			s.prog.Unreachable++
		case "skipped":
			s.prog.Skipped++
		}
		if event.Changed {
			s.prog.Changed++
		}
	}
	if delta.LastSeq > s.lastSeq {
		s.lastSeq = delta.LastSeq
	}
}

// streamProgress tails the run's new events into the durable store and refreshes its progress. It
// is best-effort: a read or write failure is logged and retried on the next tick rather than
// affecting the run. The cursor and counts advance only after the new events are appended, so an
// append failure re-reads and re-appends the same window (idempotent by (runId, seq)) instead of
// dropping or double-counting events.
func (w *AnsibleQueueWorker) streamProgress(
	ctx context.Context,
	execution *operationdomain.AnsibleExecution,
	reader operationdomain.IncrementalEventReader,
	state *runProgressState,
) {
	delta, err := reader.EventsSince(execution.RunID, state.lastSeq)
	if err != nil {
		slog.Warn("read Ansible run events", "executionId", execution.ID, "runId", execution.RunID, "error", err)
		return
	}
	if len(delta.Events) > 0 {
		if appendErr := w.events.AppendEvents(ctx, delta.Events); appendErr != nil {
			slog.Warn("append Ansible run events", "executionId", execution.ID, "runId", execution.RunID, "error", appendErr)
			return
		}
	}
	state.apply(delta)
	if err := w.executions.UpdateProgress(ctx, execution.ID, w.owner, state.prog); err != nil {
		slog.Warn("update Ansible run progress", "executionId", execution.ID, "error", err)
	}
}

// validateLeases confirms every workflow-held resource lease frozen onto the execution is
// still current. It is a no-op when no lease repository or leases are present (legacy v2
// executions carry none), so it never blocks the compatibility drain path.
func (w *AnsibleQueueWorker) validateLeases(ctx context.Context, execution *operationdomain.AnsibleExecution) error {
	if w.leases == nil {
		return nil
	}
	for _, lease := range execution.ResourceLeases {
		if err := w.leases.Validate(ctx, lease); err != nil {
			return fmt.Errorf("resource lease %s is no longer current: %w", lease.ResourceKey, err)
		}
	}
	return nil
}
