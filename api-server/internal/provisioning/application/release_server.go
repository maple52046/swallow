package application

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// releaseDispatchRecoveryDelay keeps a freshly persisted task out of the worker
// until the synchronous provider Release request has had time to return.
const releaseDispatchRecoveryDelay = 5 * time.Minute

// ReleaseServerInput carries provider-neutral release controls. Static cleanup is
// Swallow-owned follow-up and is not forwarded as a provider Release option.
type ReleaseServerInput struct {
	ServerID        string
	Erase           bool
	SecureErase     bool
	QuickErase      bool
	Comment         string
	UnbindStaticIPs bool
	RequestID       string
}

// ReleaseServerUseCase returns a Server to its provider pool and can persist
// release-time static cleanup before dispatching the asynchronous provider action.
type ReleaseServerUseCase struct {
	servers   serverdomain.ServerRepository
	providers provisioningdomain.ProviderFactory
	tasks     provisioningdomain.ProvisioningTaskRepository
}

// NewReleaseServerUseCase wires provider release and durable cleanup ownership.
func NewReleaseServerUseCase(
	servers serverdomain.ServerRepository,
	providers provisioningdomain.ProviderFactory,
	tasks provisioningdomain.ProvisioningTaskRepository,
) *ReleaseServerUseCase {
	return &ReleaseServerUseCase{servers: servers, providers: providers, tasks: tasks}
}

// Execute preserves the historical parameterless Release path.
func (uc *ReleaseServerUseCase) Execute(
	ctx context.Context,
	serverID string,
) (*ProvisioningStateItem, error) {
	return uc.ExecuteWithOptions(ctx, ReleaseServerInput{ServerID: serverID})
}

// ExecuteWithOptions persists requested cleanup before Release, so provider
// acceptance cannot leave untracked follow-up work after a process crash.
func (uc *ReleaseServerUseCase) ExecuteWithOptions(
	ctx context.Context,
	input ReleaseServerInput,
) (*ProvisioningStateItem, error) {
	if !input.Erase && (input.SecureErase || input.QuickErase) {
		return nil, fmt.Errorf(
			"%w: secureErase and quickErase require erase=true",
			provisioningdomain.ErrInvalidReleaseRequest,
		)
	}

	server, err := uc.servers.FindByID(ctx, input.ServerID)
	if err != nil {
		return nil, err
	}
	provider, err := uc.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return nil, err
	}
	if err := requireServerUnlocked(ctx, uc.servers, server, provider); err != nil {
		return nil, err
	}

	var task *provisioningdomain.ProvisioningTask
	if input.UnbindStaticIPs {
		networkProvider, networkErr := requireNetworkProvider(provider)
		if networkErr != nil {
			return nil, networkErr
		}
		network, networkErr := networkProvider.InspectNetwork(
			ctx, server.Source.ProviderMachineID)
		if networkErr != nil {
			return nil, networkErr
		}
		now := time.Now().UTC()
		task = &provisioningdomain.ProvisioningTask{
			ID:                uuid.NewString(),
			Kind:              provisioningdomain.ProvisioningTaskReleaseNetworkCleanup,
			ServerID:          server.ID,
			IntegrationID:     server.Source.IntegrationID,
			ProviderMachineID: server.Source.ProviderMachineID,
			Status:            provisioningdomain.ProvisioningTaskPending,
			Phase:             provisioningdomain.ProvisioningTaskWaitingForRelease,
			Snapshot:          staticLinkSnapshot(network),
			RequestID:         strings.TrimSpace(input.RequestID),
			NextRunAt:         now.Add(releaseDispatchRecoveryDelay),
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if err := uc.tasks.Create(ctx, task); err != nil {
			return nil, err
		}
	}

	comment := strings.TrimSpace(input.Comment)
	hasProviderOptions := input.Erase || input.SecureErase || input.QuickErase || comment != ""
	var machine *provisioningdomain.Machine
	if hasProviderOptions {
		releaser, ok := provider.(provisioningdomain.ConfigurableMachineReleaser)
		if !ok || !provider.Capabilities().ReleaseOptions {
			err = &provisioningdomain.ProviderError{
				Kind:   provisioningdomain.ProviderErrorRejected,
				Detail: "This provisioner does not support configurable release options.",
			}
		} else {
			machine, err = releaser.ReleaseWithOptions(ctx, provisioningdomain.ReleaseRequest{
				MachineID:   server.Source.ProviderMachineID,
				Erase:       input.Erase,
				SecureErase: input.SecureErase,
				QuickErase:  input.QuickErase,
				Comment:     comment,
			})
		}
	} else {
		machine, err = provider.Release(ctx, server.Source.ProviderMachineID)
	}
	if err != nil {
		if task != nil {
			task.Status = provisioningdomain.ProvisioningTaskFailed
			task.Error = "Release was not accepted. " + provisioningTaskError(err)
			task.NextRunAt = time.Time{}
			task.UpdatedAt = time.Now().UTC()
			_ = uc.tasks.Save(ctx, task)
		}
		return nil, err
	}

	item := applyProvisioningResult(ctx, uc.servers, server, machine)
	if task != nil {
		item.TaskID = task.ID
		now := time.Now().UTC()
		task.Status = provisioningdomain.ProvisioningTaskPending
		task.Phase = provisioningdomain.ProvisioningTaskWaitingForReady
		task.NextRunAt = now
		task.LeaseOwner = ""
		task.LeaseUntil = time.Time{}
		task.UpdatedAt = now
		if saveErr := uc.tasks.Save(ctx, task); saveErr != nil {
			// The persisted task remains recoverable after its dispatch delay.
			slog.Error("schedule release network cleanup", "taskId", task.ID, "error", saveErr)
		}
	}
	return item, nil
}

func staticLinkSnapshot(
	network *provisioningdomain.MachineNetwork,
) []provisioningdomain.StaticNetworkLinkSnapshot {
	snapshot := make([]provisioningdomain.StaticNetworkLinkSnapshot, 0)
	for _, iface := range network.Interfaces {
		for _, link := range iface.Links {
			if link.State != provisioningdomain.NetworkStateStatic {
				continue
			}
			snapshot = append(snapshot, provisioningdomain.StaticNetworkLinkSnapshot{
				InterfaceID: iface.ID,
				LinkID:      link.ID,
				SubnetID:    link.SubnetID,
				IPAddress:   link.IPAddress,
			})
		}
	}
	return snapshot
}
