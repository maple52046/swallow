package application

import (
	"context"
	"fmt"
	"strings"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// MachineActionsUseCase runs the provisioner actions that are neither deploy nor release:
// power, hardware validation, and operator state changes.
//
// Every action is addressed by server ID and dispatched to the server's own provisioner,
// which is the identity mapping swallow exists to hold. Each action needs an optional
// provider capability; a provisioner that lacks it gets a clear refusal rather than a
// silently dropped request, so a caller can trust that a success means the thing happened.
type MachineActionsUseCase struct {
	servers   serverdomain.ServerRepository
	providers provisioningdomain.ProviderFactory
	work      ActiveServerWorkReader
}

func NewMachineActionsUseCase(
	servers serverdomain.ServerRepository,
	providers provisioningdomain.ProviderFactory,
	work ...ActiveServerWorkReader,
) *MachineActionsUseCase {
	uc := &MachineActionsUseCase{servers: servers, providers: providers}
	if len(work) > 0 {
		uc.work = work[0]
	}
	return uc
}

// PowerStateItem reports a machine's live power state after a query. Separate from
// ProvisioningStateItem because a query changes nothing and only the power state is
// meaningful to return.
type PowerStateItem struct {
	ServerID   string `json:"serverId"`
	PowerState string `json:"powerState"`
}

// resolve loads a server and its provider, the common first step of every action.
func (uc *MachineActionsUseCase) resolve(
	ctx context.Context, serverID string,
) (*serverdomain.Server, provisioningdomain.OSProvisioningProvider, error) {
	server, err := uc.servers.FindByID(ctx, serverID)
	if err != nil {
		return nil, nil, err
	}
	provider, err := uc.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return nil, nil, err
	}
	return server, provider, nil
}

func (uc *MachineActionsUseCase) resolveMutable(
	ctx context.Context, serverID string,
) (*serverdomain.Server, provisioningdomain.OSProvisioningProvider, error) {
	server, provider, err := uc.resolve(ctx, serverID)
	if err != nil {
		return nil, nil, err
	}
	if err := requireServerUnlocked(ctx, uc.servers, server, provider); err != nil {
		return nil, nil, err
	}
	return server, provider, nil
}

// unsupported is the refusal returned when a provisioner does not offer a capability. It
// is a rejection, not an internal error: the request was understood and declined.
func unsupported(action string) error {
	return &provisioningdomain.ProviderError{
		Kind:   provisioningdomain.ProviderErrorRejected,
		Detail: "This provisioner does not support " + action + ".",
	}
}

// --- power ---

func (uc *MachineActionsUseCase) PowerOn(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	server, provider, err := uc.resolveMutable(ctx, serverID)
	if err != nil {
		return nil, err
	}
	controller, ok := provider.(provisioningdomain.PowerController)
	if !ok {
		return nil, unsupported("power control")
	}
	machine, err := controller.PowerOn(ctx, server.Source.ProviderMachineID)
	if err != nil {
		return nil, err
	}
	return applyProvisioningResult(ctx, uc.servers, server, machine), nil
}

func (uc *MachineActionsUseCase) PowerOff(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	server, provider, err := uc.resolveMutable(ctx, serverID)
	if err != nil {
		return nil, err
	}
	controller, ok := provider.(provisioningdomain.PowerController)
	if !ok {
		return nil, unsupported("power control")
	}
	machine, err := controller.PowerOff(ctx, server.Source.ProviderMachineID)
	if err != nil {
		return nil, err
	}
	return applyProvisioningResult(ctx, uc.servers, server, machine), nil
}

func (uc *MachineActionsUseCase) QueryPower(ctx context.Context, serverID string) (*PowerStateItem, error) {
	server, provider, err := uc.resolve(ctx, serverID)
	if err != nil {
		return nil, err
	}
	controller, ok := provider.(provisioningdomain.PowerController)
	if !ok {
		return nil, unsupported("power control")
	}
	state, err := controller.QueryPowerState(ctx, server.Source.ProviderMachineID)
	if err != nil {
		return nil, err
	}
	return &PowerStateItem{ServerID: server.ID, PowerState: string(state)}, nil
}

// --- hardware validation ---

func (uc *MachineActionsUseCase) Commission(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	return uc.validate(ctx, serverID, func(v provisioningdomain.HardwareValidator, id string) (*provisioningdomain.Machine, error) {
		return v.Commission(ctx, id)
	})
}

func (uc *MachineActionsUseCase) Test(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	return uc.validate(ctx, serverID, func(v provisioningdomain.HardwareValidator, id string) (*provisioningdomain.Machine, error) {
		return v.Test(ctx, id)
	})
}

func (uc *MachineActionsUseCase) Abort(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	return uc.validate(ctx, serverID, func(v provisioningdomain.HardwareValidator, id string) (*provisioningdomain.Machine, error) {
		return v.Abort(ctx, id)
	})
}

func (uc *MachineActionsUseCase) OverrideFailedTesting(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	return uc.validate(ctx, serverID, func(v provisioningdomain.HardwareValidator, id string) (*provisioningdomain.Machine, error) {
		return v.OverrideFailedTesting(ctx, id)
	})
}

func (uc *MachineActionsUseCase) validate(
	ctx context.Context, serverID string,
	call func(provisioningdomain.HardwareValidator, string) (*provisioningdomain.Machine, error),
) (*ProvisioningStateItem, error) {
	server, provider, err := uc.resolveMutable(ctx, serverID)
	if err != nil {
		return nil, err
	}
	validator, ok := provider.(provisioningdomain.HardwareValidator)
	if !ok {
		return nil, unsupported("hardware validation")
	}
	machine, err := call(validator, server.Source.ProviderMachineID)
	if err != nil {
		return nil, err
	}
	return applyProvisioningResult(ctx, uc.servers, server, machine), nil
}

// --- operator state ---

func (uc *MachineActionsUseCase) Lock(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	server, provider, err := uc.resolve(ctx, serverID)
	if err != nil {
		return nil, err
	}
	controller, ok := provider.(provisioningdomain.OperatorStateController)
	if !ok {
		return nil, unsupported("operator state changes")
	}
	machine, err := provider.GetMachine(ctx, server.Source.ProviderMachineID)
	if err != nil {
		return nil, &serverdomain.ServerLockUnavailableError{Name: server.DisplayName()}
	}
	updateProvisioningProjection(server, machine)
	if err := uc.servers.Upsert(ctx, server); err != nil {
		return nil, err
	}
	if machine.Locked {
		return nil, &serverdomain.ServerLockedError{Name: server.DisplayName()}
	}
	if providerStateBlocksLock(machine.Status) {
		return nil, lockConflict(server,
			fmt.Sprintf("is currently %s. Wait for the provider lifecycle action to finish before locking it.", machine.Status))
	}
	if machine.Status != provisioningdomain.MachineStatusDeployed {
		return nil, lockConflict(server,
			fmt.Sprintf("is %s. Lock is available only when the Server is deployed.", machine.Status))
	}
	if uc.work != nil {
		work, workErr := uc.work.ActiveWork(ctx, server.ID)
		if workErr != nil {
			return nil, workErr
		}
		if len(work.OperationIDs) > 0 {
			return nil, lockConflict(server,
				"has active Operation(s) "+strings.Join(work.OperationIDs, ", ")+". Wait for them to finish before locking it.")
		}
		if len(work.TaskIDs) > 0 {
			return nil, lockConflict(server,
				"has active Provisioning Task(s) "+strings.Join(work.TaskIDs, ", ")+". Wait for them to finish before locking it.")
		}
	}
	updated, err := controller.Lock(ctx, server.Source.ProviderMachineID)
	if err != nil {
		return nil, err
	}
	item := updateProvisioningProjection(server, updated)
	if err := uc.servers.Upsert(ctx, server); err != nil {
		return nil, err
	}
	return item, nil
}

func (uc *MachineActionsUseCase) Unlock(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	server, provider, err := uc.resolve(ctx, serverID)
	if err != nil {
		return nil, err
	}
	controller, ok := provider.(provisioningdomain.OperatorStateController)
	if !ok {
		return nil, unsupported("operator state changes")
	}
	machine, err := controller.Unlock(ctx, server.Source.ProviderMachineID)
	if err != nil {
		return nil, err
	}
	item := updateProvisioningProjection(server, machine)
	if err := uc.servers.Upsert(ctx, server); err != nil {
		return nil, err
	}
	return item, nil
}

func (uc *MachineActionsUseCase) MarkBroken(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	return uc.operatorState(ctx, serverID, provisioningdomain.RecoveryIntentMarkBroken, func(c provisioningdomain.OperatorStateController, id string) (*provisioningdomain.Machine, error) {
		return c.MarkBroken(ctx, id)
	})
}

func (uc *MachineActionsUseCase) MarkFixed(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	return uc.operatorState(ctx, serverID, provisioningdomain.RecoveryIntentMarkFixed, func(c provisioningdomain.OperatorStateController, id string) (*provisioningdomain.Machine, error) {
		return c.MarkFixed(ctx, id)
	})
}

func (uc *MachineActionsUseCase) RescueMode(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	return uc.operatorState(ctx, serverID, provisioningdomain.RecoveryIntentRescueEnter, func(c provisioningdomain.OperatorStateController, id string) (*provisioningdomain.Machine, error) {
		return c.EnterRescueMode(ctx, id)
	})
}

func (uc *MachineActionsUseCase) ExitRescueMode(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	return uc.operatorState(ctx, serverID, provisioningdomain.RecoveryIntentRescueExit, func(c provisioningdomain.OperatorStateController, id string) (*provisioningdomain.Machine, error) {
		return c.ExitRescueMode(ctx, id)
	})
}

// operatorState dispatches one operator-state primitive after gating it on the Server's
// live provisioning state through the Swallow-owned recovery policy. resolveMutable has
// already refreshed the projection from a live provider read (its lock check reads the
// Machine), so gating on server.Provisioning.State reflects the current provider state and
// lets Swallow refuse with its own reason before the provisioner would reject the call.
func (uc *MachineActionsUseCase) operatorState(
	ctx context.Context, serverID string, intent provisioningdomain.RecoveryIntent,
	call func(provisioningdomain.OperatorStateController, string) (*provisioningdomain.Machine, error),
) (*ProvisioningStateItem, error) {
	server, provider, err := uc.resolveMutable(ctx, serverID)
	if err != nil {
		return nil, err
	}
	controller, ok := provider.(provisioningdomain.OperatorStateController)
	if !ok {
		return nil, unsupported("operator state changes")
	}
	state := provisioningdomain.MachineStatusUnknown
	if server.Provisioning != nil {
		state = provisioningdomain.MachineStatus(server.Provisioning.State)
	}
	if decision := provisioningdomain.EvaluateRecovery(intent, state); !decision.Allowed {
		return nil, lockConflict(server, decision.Reason)
	}
	machine, err := call(controller, server.Source.ProviderMachineID)
	if err != nil {
		return nil, err
	}
	return applyProvisioningResult(ctx, uc.servers, server, machine), nil
}
