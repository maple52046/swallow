package application

import (
	"context"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// ProvisioningStateItem reports the provisioning axis right after a lifecycle action.
//
// It is a snapshot of what the provisioner returned on accepting the request, not a
// completion report: deployment continues asynchronously and the reconciler is what
// tracks it to completion.
type ProvisioningStateItem struct {
	ServerID            string `json:"serverId"`
	State               string `json:"state"`
	ProviderState       string `json:"providerState"`
	PowerState          string `json:"powerState"`
	OSSystem            string `json:"osSystem"`
	DistroSeries        string `json:"distroSeries"`
	Ephemeral           bool   `json:"ephemeral"`
	HWEKernel           string `json:"hweKernel"`
	Locked              bool   `json:"locked"`
	CommissioningStatus string `json:"commissioningStatus"`
	TaskID              string `json:"taskId,omitempty"`
	TestingStatus       string `json:"testingStatus"`
	ObservedAt          string `json:"observedAt"`
}

type DeployServerInput struct {
	ServerID string
	// OSSystem may be empty when DistroSeries already identifies the OS family.
	OSSystem string
	// DistroSeries is the release to deploy, e.g. "jammy" or "ubuntu/22.04".
	DistroSeries string
	// UserData is optional cloud-init user data in plain text.
	UserData string
	Comment  string
	// Ephemeral asks for the OS to run from memory, leaving the disks untouched.
	//
	// Rejected rather than ignored when the provisioner cannot do it: an operator who
	// asks for "nothing is written to disk" and silently gets a disk installation has
	// had the opposite of their instruction carried out.
	Ephemeral bool
}

// DeployServerUseCase asks a server's own provisioner to install an operating system.
//
// The action is addressed by server ID, not by provider machine ID: callers know
// servers, and which provisioner to talk to is derived from the server's source. That
// is the identity mapping swallow exists to hold.
type DeployServerUseCase struct {
	servers   serverdomain.ServerRepository
	providers provisioningdomain.ProviderFactory
}

func NewDeployServerUseCase(
	servers serverdomain.ServerRepository,
	providers provisioningdomain.ProviderFactory,
) *DeployServerUseCase {
	return &DeployServerUseCase{servers: servers, providers: providers}
}

func (uc *DeployServerUseCase) Execute(ctx context.Context, input DeployServerInput) (*ProvisioningStateItem, error) {
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

	// Refused here rather than passed down, so that a provisioner which cannot deploy
	// from memory says so instead of installing to disk. This is the one deploy option
	// where being ignored produces the opposite of what was asked for.
	if input.Ephemeral && !provider.Capabilities().EphemeralDeploy {
		return nil, &provisioningdomain.ProviderError{
			Kind: provisioningdomain.ProviderErrorRejected,
			Detail: "This provisioner does not support ephemeral deployment, and " +
				"silently deploying to disk would be the opposite of what was requested.",
		}
	}

	machine, err := provider.Deploy(ctx, provisioningdomain.DeployRequest{
		MachineID:    server.Source.ProviderMachineID,
		OSSystem:     input.OSSystem,
		DistroSeries: input.DistroSeries,
		UserData:     input.UserData,
		Comment:      input.Comment,
		Ephemeral:    input.Ephemeral,
	})
	if err != nil {
		return nil, err
	}

	return applyProvisioningResult(ctx, uc.servers, server, machine), nil
}

// applyProvisioningResult writes the post-action state onto the server so that a
// caller reading the server back sees "deploying" immediately rather than the previous
// state until the next reconcile pass.
//
// A failure to persist is swallowed: the action already succeeded at the provisioner,
// and the reconciler will converge. Reporting a failure here would suggest the
// deployment did not start, which would be worse than a briefly stale axis.
func applyProvisioningResult(
	ctx context.Context,
	servers serverdomain.ServerRepository,
	server *serverdomain.Server,
	machine *provisioningdomain.Machine,
) *ProvisioningStateItem {
	server.Observed.Addresses = append([]string(nil), machine.IPAddresses...)
	item := updateProvisioningProjection(server, machine)
	_ = servers.Upsert(ctx, server)
	return item
}

func projectedEphemeral(machine *provisioningdomain.Machine) bool {
	if !machine.Ephemeral {
		return false
	}
	switch machine.Status {
	case provisioningdomain.MachineStatusDeploying,
		provisioningdomain.MachineStatusDeployed,
		provisioningdomain.MachineStatusReleasing:
		return true
	default:
		return false
	}
}

// updateProvisioningProjection applies a provider observation in memory. Callers choose
// whether persistence is best effort after an accepted action or required for a live
// refresh whose sole purpose is to advance the stored projection.
func updateProvisioningProjection(
	server *serverdomain.Server,
	machine *provisioningdomain.Machine,
) *ProvisioningStateItem {
	now := time.Now().UTC()

	server.Provisioning = &serverdomain.ProvisioningStatus{
		State:               string(machine.Status),
		ProviderState:       machine.ProviderStatus,
		PowerState:          string(machine.PowerState),
		OSSystem:            machine.OSSystem,
		DistroSeries:        machine.DistroSeries,
		Ephemeral:           projectedEphemeral(machine),
		HWEKernel:           machine.HWEKernel,
		Locked:              machine.Locked,
		CommissioningStatus: machine.CommissioningStatus,
		TestingStatus:       machine.TestingStatus,
		IntegrationID:       server.Source.IntegrationID,
		ObservedAt:          now,
	}
	server.UpdatedAt = now

	return &ProvisioningStateItem{
		ServerID:            server.ID,
		State:               server.Provisioning.State,
		ProviderState:       server.Provisioning.ProviderState,
		PowerState:          server.Provisioning.PowerState,
		OSSystem:            server.Provisioning.OSSystem,
		DistroSeries:        server.Provisioning.DistroSeries,
		Ephemeral:           server.Provisioning.Ephemeral,
		HWEKernel:           server.Provisioning.HWEKernel,
		Locked:              server.Provisioning.Locked,
		CommissioningStatus: server.Provisioning.CommissioningStatus,
		TestingStatus:       server.Provisioning.TestingStatus,
		ObservedAt:          now.Format(time.RFC3339),
	}
}
