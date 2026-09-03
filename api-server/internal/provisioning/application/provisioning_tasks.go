package application

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// ProvisioningTaskItem is the safe operator-facing task history. The static-link
// snapshot stays internal because the Activity view only needs progress and errors.
type ProvisioningTaskItem struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	ServerID  string `json:"serverId"`
	Status    string `json:"status"`
	Phase     string `json:"phase"`
	Attempt   int    `json:"attempt"`
	Error     string `json:"error,omitempty"`
	RequestID string `json:"requestId,omitempty"`
	Retryable bool   `json:"retryable"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// ProvisioningTaskService exposes task history and retry without exposing leases
// or provider identifiers.
type ProvisioningTaskService struct {
	tasks   provisioningdomain.ProvisioningTaskRepository
	servers serverdomain.ServerRepository
}

// NewProvisioningTaskService creates the query and retry boundary.
func NewProvisioningTaskService(
	tasks provisioningdomain.ProvisioningTaskRepository,
	servers serverdomain.ServerRepository,
) *ProvisioningTaskService {
	return &ProvisioningTaskService{tasks: tasks, servers: servers}
}

// ListByServer returns newest task history after verifying the Server exists.
func (s *ProvisioningTaskService) ListByServer(
	ctx context.Context,
	serverID string,
) ([]ProvisioningTaskItem, error) {
	if _, err := s.servers.FindByID(ctx, serverID); err != nil {
		return nil, err
	}
	tasks, err := s.tasks.ListByServer(ctx, serverID)
	if err != nil {
		return nil, err
	}
	items := make([]ProvisioningTaskItem, len(tasks))
	for index, task := range tasks {
		items[index] = provisioningTaskItem(task)
	}
	return items, nil
}

// Get returns one task without its cleanup snapshot or lease.
func (s *ProvisioningTaskService) Get(
	ctx context.Context,
	id string,
) (*ProvisioningTaskItem, error) {
	task, err := s.tasks.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	item := provisioningTaskItem(task)
	return &item, nil
}

// Retry requeues failed cleanup and never repeats the original Release.
func (s *ProvisioningTaskService) Retry(
	ctx context.Context,
	id string,
) (*ProvisioningTaskItem, error) {
	task, err := s.tasks.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, err := s.servers.FindByID(ctx, task.ServerID); err != nil {
		return nil, err
	}
	if task.Status != provisioningdomain.ProvisioningTaskFailed ||
		task.Phase == provisioningdomain.ProvisioningTaskWaitingForRelease {
		return nil, provisioningdomain.ErrProvisioningTaskConflict
	}
	if err := s.tasks.Retry(ctx, id, time.Now().UTC()); err != nil {
		return nil, err
	}
	task, err = s.tasks.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	item := provisioningTaskItem(task)
	return &item, nil
}

func provisioningTaskItem(task *provisioningdomain.ProvisioningTask) ProvisioningTaskItem {
	return ProvisioningTaskItem{
		ID:        task.ID,
		Kind:      string(task.Kind),
		ServerID:  task.ServerID,
		Status:    string(task.Status),
		Phase:     string(task.Phase),
		Attempt:   task.Attempt,
		Error:     task.Error,
		RequestID: task.RequestID,
		CreatedAt: wire.Time(task.CreatedAt),
		Retryable: task.Status == provisioningdomain.ProvisioningTaskFailed &&
			task.Phase != provisioningdomain.ProvisioningTaskWaitingForRelease,
		UpdatedAt: wire.Time(task.UpdatedAt),
	}
}

// ProvisioningTaskWorker leases and advances release cleanup. A task is always
// persisted between provider transitions, so cancellation or process termination
// leaves recoverable work rather than an in-memory promise.
type ProvisioningTaskWorker struct {
	tasks         provisioningdomain.ProvisioningTaskRepository
	servers       serverdomain.ServerRepository
	providers     provisioningdomain.ProviderFactory
	owner         string
	interval      time.Duration
	leaseDuration time.Duration
}

// NewProvisioningTaskWorker creates a process-unique lease owner.
func NewProvisioningTaskWorker(
	tasks provisioningdomain.ProvisioningTaskRepository,
	servers serverdomain.ServerRepository,
	providers provisioningdomain.ProviderFactory,
	interval, leaseDuration time.Duration,
) *ProvisioningTaskWorker {
	return &ProvisioningTaskWorker{
		tasks: tasks, servers: servers, providers: providers,
		owner: uuid.NewString(), interval: interval, leaseDuration: leaseDuration,
	}
}

// Run processes due tasks until ctx is cancelled. Each tick drains all currently
// due work before sleeping, while repository leases prevent another process from
// executing the same cleanup concurrently.
func (w *ProvisioningTaskWorker) Run(ctx context.Context) {
	w.processAvailable(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processAvailable(ctx)
		}
	}
}

func (w *ProvisioningTaskWorker) processAvailable(ctx context.Context) {
	for {
		task, err := w.tasks.ClaimNext(
			ctx, w.owner, time.Now().UTC(), w.leaseDuration)
		if errors.Is(err, provisioningdomain.ErrNoProvisioningTask) {
			return
		}
		if err != nil {
			slog.Error("claim provisioning task", "error", err)
			return
		}
		if err := w.process(ctx, task); err != nil {
			slog.Error("process provisioning task",
				"taskId", task.ID, "serverId", task.ServerID, "error", err)
		}
	}
}

func (w *ProvisioningTaskWorker) process(
	ctx context.Context,
	task *provisioningdomain.ProvisioningTask,
) error {
	now := time.Now().UTC()
	server, err := w.servers.FindByID(ctx, task.ServerID)
	if err != nil {
		return w.fail(ctx, task, "The Server no longer exists.", now)
	}
	provider, err := w.providers.For(ctx, task.IntegrationID)
	if err != nil {
		return w.fail(ctx, task, provisioningTaskError(err), now)
	}
	machine, err := provider.GetMachine(ctx, task.ProviderMachineID)
	if err != nil {
		return w.fail(ctx, task, provisioningTaskError(err), now)
	}
	if machine.Status != provisioningdomain.MachineStatusReady {
		if machine.Status == provisioningdomain.MachineStatusFailed {
			return w.fail(ctx, task, "Release did not return the Server to Ready.", now)
		}
		task.Status = provisioningdomain.ProvisioningTaskPending
		task.Phase = provisioningdomain.ProvisioningTaskWaitingForReady
		task.NextRunAt = now.Add(w.interval)
		task.LeaseOwner = ""
		task.LeaseUntil = time.Time{}
		task.UpdatedAt = now
		return w.tasks.Save(ctx, task)
	}

	networkProvider, err := requireNetworkProvider(provider)
	if err != nil {
		return w.fail(ctx, task, provisioningTaskError(err), now)
	}
	task.Phase = provisioningdomain.ProvisioningTaskCleaningNetwork
	task.UpdatedAt = now
	if err := w.tasks.Save(ctx, task); err != nil {
		return err
	}
	for _, snapshot := range task.Snapshot {
		network, inspectErr := networkProvider.InspectNetwork(ctx, task.ProviderMachineID)
		if inspectErr != nil {
			return w.fail(ctx, task, provisioningTaskError(inspectErr), time.Now().UTC())
		}
		if !staticSnapshotStillMatches(network, snapshot) {
			continue
		}
		if _, unlinkErr := networkProvider.UnlinkNetwork(
			ctx,
			task.ProviderMachineID,
			snapshot.InterfaceID,
			snapshot.LinkID,
		); unlinkErr != nil {
			return w.fail(ctx, task, provisioningTaskError(unlinkErr), time.Now().UTC())
		}
	}

	machine, err = provider.GetMachine(ctx, task.ProviderMachineID)
	if err != nil {
		return w.fail(ctx, task, provisioningTaskError(err), time.Now().UTC())
	}
	updateProvisioningProjection(server, machine)
	server.Observed.Addresses = append([]string(nil), machine.IPAddresses...)
	if err := w.servers.Upsert(ctx, server); err != nil {
		return w.fail(ctx, task, "Cleanup completed but the Server projection could not be refreshed.", time.Now().UTC())
	}

	task.Status = provisioningdomain.ProvisioningTaskSucceeded
	task.Phase = provisioningdomain.ProvisioningTaskComplete
	task.Error = ""
	task.NextRunAt = time.Time{}
	task.LeaseOwner = ""
	task.LeaseUntil = time.Time{}
	task.UpdatedAt = time.Now().UTC()
	return w.tasks.Save(ctx, task)
}

func (w *ProvisioningTaskWorker) fail(
	ctx context.Context,
	task *provisioningdomain.ProvisioningTask,
	message string,
	now time.Time,
) error {
	task.Status = provisioningdomain.ProvisioningTaskFailed
	task.Error = message
	task.NextRunAt = time.Time{}
	task.LeaseOwner = ""
	task.LeaseUntil = time.Time{}
	task.UpdatedAt = now
	return w.tasks.Save(ctx, task)
}

func provisioningTaskError(err error) string {
	var providerErr *provisioningdomain.ProviderError
	if errors.As(err, &providerErr) && providerErr.Detail != "" {
		return providerErr.Detail
	}
	if errors.Is(err, provisioningdomain.ErrNetworkConfigurationUnsupported) {
		return "This provisioner cannot remove static IP bindings."
	}
	return "Provisioning cleanup could not be completed."
}

func staticSnapshotStillMatches(
	network *provisioningdomain.MachineNetwork,
	snapshot provisioningdomain.StaticNetworkLinkSnapshot,
) bool {
	for _, iface := range network.Interfaces {
		if iface.ID != snapshot.InterfaceID {
			continue
		}
		for _, link := range iface.Links {
			if link.ID == snapshot.LinkID &&
				link.State == provisioningdomain.NetworkStateStatic &&
				link.SubnetID == snapshot.SubnetID &&
				link.IPAddress == snapshot.IPAddress {
				return true
			}
		}
	}
	return false
}
