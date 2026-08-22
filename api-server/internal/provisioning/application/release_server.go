package application

import (
	"context"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// ReleaseServerUseCase returns a server to its provisioner's available pool, which is
// what makes an already-deployed machine deployable again.
//
// It does not remove the server: the physical machine still exists and swallow still
// manages it. Only the provisioning axis changes.
type ReleaseServerUseCase struct {
	servers   serverdomain.ServerRepository
	providers provisioningdomain.ProviderFactory
}

func NewReleaseServerUseCase(
	servers serverdomain.ServerRepository,
	providers provisioningdomain.ProviderFactory,
) *ReleaseServerUseCase {
	return &ReleaseServerUseCase{servers: servers, providers: providers}
}

func (uc *ReleaseServerUseCase) Execute(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	server, err := uc.servers.FindByID(ctx, serverID)
	if err != nil {
		return nil, err
	}

	provider, err := uc.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return nil, err
	}

	machine, err := provider.Release(ctx, server.Source.ProviderMachineID)
	if err != nil {
		return nil, err
	}

	return applyProvisioningResult(ctx, uc.servers, server, machine), nil
}
