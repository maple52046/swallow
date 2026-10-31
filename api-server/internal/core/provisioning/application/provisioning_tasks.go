package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
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

// ReleaseCleanupRetrier retries the release Task of the Workflow that owns a failed network
// cleanup, so the cleanup runs again under that Workflow's resource lease and fencing rather than
// outside it.
type ReleaseCleanupRetrier interface {
	RetryReleaseCleanup(ctx context.Context, workflowID, serverID string) error
}

// ProvisioningTaskService exposes task history and retry without exposing provider identifiers.
type ProvisioningTaskService struct {
	tasks   provisioningdomain.ProvisioningTaskRepository
	servers serverdomain.ServerRepository
	retrier ReleaseCleanupRetrier
	guard   serverdomain.MutationGuard
}

// NewProvisioningTaskService creates the query and retry boundary; retrier carries a retry into
// the release Workflow that owns the cleanup.
func NewProvisioningTaskService(
	tasks provisioningdomain.ProvisioningTaskRepository,
	servers serverdomain.ServerRepository,
	retrier ReleaseCleanupRetrier,
	guard ...serverdomain.MutationGuard,
) *ProvisioningTaskService {
	service := &ProvisioningTaskService{tasks: tasks, servers: servers, retrier: retrier}
	if len(guard) > 0 {
		service.guard = guard[0]
	}
	return service
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

// Get returns one task without its cleanup snapshot.
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

// Retry retries a failed cleanup by retrying the release Task of the Workflow that owns it, and
// never repeats the original Release. The task changes once that Task runs again.
func (s *ProvisioningTaskService) Retry(
	ctx context.Context,
	id string,
) (*ProvisioningTaskItem, error) {
	task, err := s.tasks.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	server, err := s.servers.FindByID(ctx, task.ServerID)
	if err != nil {
		return nil, err
	}
	if s.guard != nil {
		if err := s.guard.RequireUnlocked(ctx, []string{server.ID}); err != nil {
			return nil, err
		}
	}
	if s.guard == nil && server.Provisioning != nil && server.Provisioning.Locked {
		return nil, &serverdomain.ServerLockedError{Name: server.DisplayName()}
	}
	if task.Status != provisioningdomain.ProvisioningTaskFailed ||
		task.Phase == provisioningdomain.ProvisioningTaskWaitingForRelease || task.RequestID == "" {
		return nil, provisioningdomain.ErrProvisioningTaskConflict
	}
	if s.retrier == nil {
		return nil, fmt.Errorf("%w: cleanup retry is unavailable", provisioningdomain.ErrProvisioningTaskConflict)
	}
	if err := s.retrier.RetryReleaseCleanup(ctx, task.RequestID, task.ServerID); err != nil {
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
			task.Phase != provisioningdomain.ProvisioningTaskWaitingForRelease && task.RequestID != "",
		UpdatedAt: wire.Time(task.UpdatedAt),
	}
}

// ReleaseNetworkCleanup removes the static IP bindings a release recorded once the Server is back
// to Ready. The release Task that holds the Server's resource lease advances it until the task is
// terminal, so the cleanup runs under the same lease and fencing as the release. A task is
// persisted between provider transitions, so an interrupted Task resumes it.
type ReleaseNetworkCleanup struct {
	tasks     provisioningdomain.ProvisioningTaskRepository
	servers   serverdomain.ServerRepository
	providers provisioningdomain.ProviderFactory
}

// NewReleaseNetworkCleanup creates the cleanup the release Task advances.
func NewReleaseNetworkCleanup(
	tasks provisioningdomain.ProvisioningTaskRepository,
	servers serverdomain.ServerRepository,
	providers provisioningdomain.ProviderFactory,
) *ReleaseNetworkCleanup {
	return &ReleaseNetworkCleanup{tasks: tasks, servers: servers, providers: providers}
}

// Retry returns a failed cleanup to pending for another pass; the release Task calls it on a
// retried attempt. A cleanup whose release was never accepted cannot be retried.
func (c *ReleaseNetworkCleanup) Retry(ctx context.Context, task *provisioningdomain.ProvisioningTask) error {
	if task.Phase == provisioningdomain.ProvisioningTaskWaitingForRelease {
		return provisioningdomain.ErrProvisioningTaskConflict
	}
	return c.tasks.Retry(ctx, task.ID, time.Now().UTC())
}

// Advance runs one pass of a pending cleanup and persists where it got to: waiting while the
// Server is not yet Ready, then removing every recorded static link that is still exactly as the
// release found it (an operator's change is preserved), then succeeded; any provider failure
// fails the task with an operator-facing reason.
func (c *ReleaseNetworkCleanup) Advance(
	ctx context.Context,
	task *provisioningdomain.ProvisioningTask,
) error {
	now := time.Now().UTC()
	server, err := c.servers.FindByID(ctx, task.ServerID)
	if err != nil {
		return c.fail(ctx, task, "The Server no longer exists.", now)
	}
	provider, err := c.providers.For(ctx, task.IntegrationID)
	if err != nil {
		return c.fail(ctx, task, provisioningTaskError(err), now)
	}
	machine, err := provider.GetMachine(ctx, task.ProviderMachineID)
	if err != nil {
		return c.fail(ctx, task, provisioningTaskError(err), now)
	}
	updateProvisioningProjection(server, machine)
	_ = c.servers.Upsert(ctx, server)
	if machine.Locked {
		task.Phase = provisioningdomain.ProvisioningTaskCleaningNetwork
		return c.fail(ctx, task,
			"Server is locked. Unlock it before retrying network cleanup.", now)
	}
	if machine.Status != provisioningdomain.MachineStatusReady {
		if machine.Status == provisioningdomain.MachineStatusFailed {
			return c.fail(ctx, task, "Release did not return the Server to Ready.", now)
		}
		task.Status = provisioningdomain.ProvisioningTaskPending
		task.Phase = provisioningdomain.ProvisioningTaskWaitingForReady
		task.UpdatedAt = now
		return c.tasks.Save(ctx, task)
	}

	networkProvider, err := requireNetworkProvider(provider)
	if err != nil {
		return c.fail(ctx, task, provisioningTaskError(err), now)
	}
	task.Status = provisioningdomain.ProvisioningTaskRunning
	task.Phase = provisioningdomain.ProvisioningTaskCleaningNetwork
	task.UpdatedAt = now
	if err := c.tasks.Save(ctx, task); err != nil {
		return err
	}
	for _, snapshot := range task.Snapshot {
		network, inspectErr := networkProvider.InspectNetwork(ctx, task.ProviderMachineID)
		if inspectErr != nil {
			return c.fail(ctx, task, provisioningTaskError(inspectErr), time.Now().UTC())
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
			return c.fail(ctx, task, provisioningTaskError(unlinkErr), time.Now().UTC())
		}
	}

	machine, err = provider.GetMachine(ctx, task.ProviderMachineID)
	if err != nil {
		return c.fail(ctx, task, provisioningTaskError(err), time.Now().UTC())
	}
	updateProvisioningProjection(server, machine)
	server.Observed.Addresses = append([]string(nil), machine.IPAddresses...)
	if err := c.servers.Upsert(ctx, server); err != nil {
		return c.fail(ctx, task, "Cleanup completed but the Server projection could not be refreshed.", time.Now().UTC())
	}

	task.Status = provisioningdomain.ProvisioningTaskSucceeded
	task.Phase = provisioningdomain.ProvisioningTaskComplete
	task.Error = ""
	task.UpdatedAt = time.Now().UTC()
	return c.tasks.Save(ctx, task)
}

func (c *ReleaseNetworkCleanup) fail(
	ctx context.Context,
	task *provisioningdomain.ProvisioningTask,
	message string,
	now time.Time,
) error {
	task.Status = provisioningdomain.ProvisioningTaskFailed
	task.Error = message
	task.UpdatedAt = now
	return c.tasks.Save(ctx, task)
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
