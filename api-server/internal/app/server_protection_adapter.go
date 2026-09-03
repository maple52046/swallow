package app

import (
	"context"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// providerServerMutationGuard is the anticorruption boundary for the provider-owned
// Machine lock. It deliberately reads the provider for every admission check so an
// external MAAS lock cannot be bypassed by a stale Server projection.
type providerServerMutationGuard struct {
	servers   serverdomain.ServerRepository
	providers provisioningdomain.ProviderFactory
}

func (g providerServerMutationGuard) RequireUnlocked(
	ctx context.Context,
	serverIDs []string,
) error {
	for _, serverID := range serverIDs {
		server, err := g.servers.FindByID(ctx, serverID)
		if err != nil {
			return err
		}
		provider, err := g.providers.For(ctx, server.Source.IntegrationID)
		if err != nil {
			return &serverdomain.ServerLockUnavailableError{Name: server.DisplayName()}
		}
		machine, err := provider.GetMachine(ctx, server.Source.ProviderMachineID)
		if err != nil {
			return &serverdomain.ServerLockUnavailableError{Name: server.DisplayName()}
		}

		if server.Provisioning != nil && server.Provisioning.Locked != machine.Locked {
			server.Provisioning.Locked = machine.Locked
			server.Provisioning.ObservedAt = time.Now().UTC()
			server.UpdatedAt = server.Provisioning.ObservedAt
			if err := g.servers.Upsert(ctx, server); err != nil {
				return err
			}
		}
		if machine.Locked {
			return &serverdomain.ServerLockedError{Name: server.DisplayName()}
		}
	}
	return nil
}
