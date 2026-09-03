package application

import (
	"context"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// RefreshServerUseCase reads one machine directly from its provisioner and advances
// the Server provisioning projection and observed addresses. It is intended for bounded
// tracking after an accepted asynchronous lifecycle action, not as a replacement for
// inventory reconciliation, which still owns identity, hardware, and absence detection.
type RefreshServerUseCase struct {
	servers   serverdomain.ServerRepository
	providers provisioningdomain.ProviderFactory
}

func NewRefreshServerUseCase(
	servers serverdomain.ServerRepository,
	providers provisioningdomain.ProviderFactory,
) *RefreshServerUseCase {
	return &RefreshServerUseCase{servers: servers, providers: providers}
}

func (uc *RefreshServerUseCase) Execute(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	server, err := uc.servers.FindByID(ctx, serverID)
	if err != nil {
		return nil, err
	}

	provider, err := uc.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return nil, err
	}

	machine, err := provider.GetMachine(ctx, server.Source.ProviderMachineID)
	if err != nil {
		return nil, err
	}

	server.Observed.Addresses = append([]string(nil), machine.IPAddresses...)
	item := updateProvisioningProjection(server, machine)
	if err := uc.servers.Upsert(ctx, server); err != nil {
		return nil, err
	}
	return item, nil
}
