package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// ActiveServerWork is the cross-context minimum needed to decide whether Lock can be
// accepted without racing already accepted work.
type ActiveServerWork struct {
	OperationIDs []string
	TaskIDs      []string
}

// ActiveServerWorkReader reads unfinished work without exposing foreign aggregates.
type ActiveServerWorkReader interface {
	ActiveWork(ctx context.Context, serverID string) (ActiveServerWork, error)
}

func requireServerUnlocked(
	ctx context.Context,
	servers serverdomain.ServerRepository,
	server *serverdomain.Server,
	provider provisioningdomain.OSProvisioningProvider,
) error {
	machine, err := provider.GetMachine(ctx, server.Source.ProviderMachineID)
	if err != nil {
		if errors.Is(err, provisioningdomain.ErrMachineNotFound) {
			return err
		}
		return &serverdomain.ServerLockUnavailableError{Name: server.DisplayName()}
	}
	updateProvisioningProjection(server, machine)
	if err := servers.Upsert(ctx, server); err != nil {
		return err
	}
	if machine.Locked {
		return &serverdomain.ServerLockedError{Name: server.DisplayName()}
	}
	return nil
}

func lockConflict(server *serverdomain.Server, reason string) error {
	return fmt.Errorf("%w: Server %q %s", provisioningdomain.ErrServerMutationConflict,
		server.DisplayName(), strings.TrimSpace(reason))
}

func providerStateBlocksLock(state provisioningdomain.MachineStatus) bool {
	switch state {
	case provisioningdomain.MachineStatusInspecting,
		provisioningdomain.MachineStatusDeploying,
		provisioningdomain.MachineStatusReleasing,
		provisioningdomain.MachineStatusTesting:
		return true
	default:
		return false
	}
}
