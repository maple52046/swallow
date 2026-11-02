package application

import (
	"context"
	"fmt"
	"strings"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// MachineActionsUseCase runs the provisioner actions that are neither deploy nor release:
// power, hardware validation, and state actions.
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

// NewMachineActionsUseCase wires the Server actions. Every dependency is required: work tells
// whether an unfinished Workflow owns a Server, which refuses Lock and the power actions.
func NewMachineActionsUseCase(
	servers serverdomain.ServerRepository,
	providers provisioningdomain.ProviderFactory,
	work ActiveServerWorkReader,
) *MachineActionsUseCase {
	return &MachineActionsUseCase{servers: servers, providers: providers, work: work}
}

// refuseActiveWork is a mutation conflict when an unfinished Workflow owns the Server: changing it
// underneath that Workflow (powering it off mid-deployment) would break the work the Workflow
// fences. action names the refused request for the message.
func (uc *MachineActionsUseCase) refuseActiveWork(ctx context.Context, server *serverdomain.Server, action string) error {
	work, err := uc.work.ActiveWork(ctx, server.ID)
	if err != nil {
		return err
	}
	if len(work.OperationIDs) > 0 {
		return lockConflict(server,
			"has active Workflow(s) "+strings.Join(work.OperationIDs, ", ")+". Wait for them to finish, or cancel them, before "+action+".")
	}
	return nil
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

// PowerOn powers the Server on through its provisioner. It is refused while the Server is locked
// or an unfinished Workflow owns it.
func (uc *MachineActionsUseCase) PowerOn(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	server, provider, err := uc.resolveMutable(ctx, serverID)
	if err != nil {
		return nil, err
	}
	if err := uc.refuseActiveWork(ctx, server, "powering it on"); err != nil {
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

// PowerOff powers the Server off through its provisioner. It is refused while the Server is locked
// or an unfinished Workflow owns it.
func (uc *MachineActionsUseCase) PowerOff(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	server, provider, err := uc.resolveMutable(ctx, serverID)
	if err != nil {
		return nil, err
	}
	if err := uc.refuseActiveWork(ctx, server, "powering it off"); err != nil {
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

// Test runs the provisioner's hardware tests on the Server. Like every hardware validation action
// it requires an unlocked Server and the HardwareValidator capability, and it mirrors the
// provider's accepted state onto the projection. Inspection is not one of these actions: it runs
// as the inspect-hardware Workflow.
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

// --- state actions ---

func (uc *MachineActionsUseCase) Lock(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	server, provider, err := uc.resolve(ctx, serverID)
	if err != nil {
		return nil, err
	}
	controller, ok := provider.(provisioningdomain.StateActionsController)
	if !ok {
		return nil, unsupported("state actions")
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
	if err := uc.refuseActiveWork(ctx, server, "locking it"); err != nil {
		return nil, err
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
	controller, ok := provider.(provisioningdomain.StateActionsController)
	if !ok {
		return nil, unsupported("state actions")
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
	return uc.stateAction(ctx, serverID, provisioningdomain.RecoveryIntentMarkBroken, func(c provisioningdomain.StateActionsController, id string) (*provisioningdomain.Machine, error) {
		return c.MarkBroken(ctx, id)
	})
}

func (uc *MachineActionsUseCase) MarkFixed(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	return uc.stateAction(ctx, serverID, provisioningdomain.RecoveryIntentMarkFixed, func(c provisioningdomain.StateActionsController, id string) (*provisioningdomain.Machine, error) {
		return c.MarkFixed(ctx, id)
	})
}

func (uc *MachineActionsUseCase) RescueMode(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	return uc.stateAction(ctx, serverID, provisioningdomain.RecoveryIntentRescueEnter, func(c provisioningdomain.StateActionsController, id string) (*provisioningdomain.Machine, error) {
		return c.EnterRescueMode(ctx, id)
	})
}

func (uc *MachineActionsUseCase) ExitRescueMode(ctx context.Context, serverID string) (*ProvisioningStateItem, error) {
	return uc.stateAction(ctx, serverID, provisioningdomain.RecoveryIntentRescueExit, func(c provisioningdomain.StateActionsController, id string) (*provisioningdomain.Machine, error) {
		return c.ExitRescueMode(ctx, id)
	})
}

// stateAction dispatches one state-action primitive after gating it on the Server's
// live provisioning state through the swallow-owned recovery policy. resolveMutable has
// already refreshed the projection from a live provider read (its lock check reads the
// Machine), so gating on server.Provisioning.State reflects the current provider state and
// lets swallow refuse with its own reason before the provisioner would reject the call.
func (uc *MachineActionsUseCase) stateAction(
	ctx context.Context, serverID string, intent provisioningdomain.RecoveryIntent,
	call func(provisioningdomain.StateActionsController, string) (*provisioningdomain.Machine, error),
) (*ProvisioningStateItem, error) {
	server, provider, err := uc.resolveMutable(ctx, serverID)
	if err != nil {
		return nil, err
	}
	controller, ok := provider.(provisioningdomain.StateActionsController)
	if !ok {
		return nil, unsupported("state actions")
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
