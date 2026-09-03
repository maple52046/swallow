package application

import (
	"context"
	"errors"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// DeleteServerUseCase permanently removes a Server and its backing provisioner Machine.
//
// Provider deletion always happens first so a successful local-only write cannot be
// undone by the next inventory reconciliation. Provider refusals and availability
// failures retain the projection. ErrMachineNotFound is the one exception: the external
// side is already absent, so deleting the stale projection completes the requested state.
// A local persistence failure after provider success is retryable because the next call
// observes ErrMachineNotFound and attempts the local delete again.
type DeleteServerUseCase struct {
	servers   serverdomain.ServerRepository
	providers provisioningdomain.ProviderFactory
}

// NewDeleteServerUseCase wires provider-first deletion through the two owning ports.
func NewDeleteServerUseCase(
	servers serverdomain.ServerRepository,
	providers provisioningdomain.ProviderFactory,
) *DeleteServerUseCase {
	return &DeleteServerUseCase{servers: servers, providers: providers}
}

// Execute deletes the provider Machine before removing its Server projection.
func (uc *DeleteServerUseCase) Execute(ctx context.Context, serverID string) error {
	server, err := uc.servers.FindByID(ctx, serverID)
	if err != nil {
		return err
	}

	provider, err := uc.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return err
	}
	remover, ok := provider.(provisioningdomain.MachineRemover)
	if !ok {
		return unsupported("machine removal")
	}
	if err := requireServerUnlocked(ctx, uc.servers, server, provider); err != nil &&
		!errors.Is(err, provisioningdomain.ErrMachineNotFound) {
		return err
	}

	err = remover.DeleteMachine(ctx, server.Source.ProviderMachineID)
	if err != nil && !errors.Is(err, provisioningdomain.ErrMachineNotFound) {
		return err
	}

	return uc.servers.Delete(ctx, server.ID)
}
