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
	// overlays lets a deploy completion fill the mirrored OS image display name immediately, so
	// the fleet list shows the friendly name instead of the raw OS/release id until the next
	// reconcile pass. Optional: a nil repository skips that resolution (the name is still carried
	// forward and reconcile fills it), which the app-layer executor unit tests rely on.
	overlays provisioningdomain.OSImageOverlayRepository
}

func NewRefreshServerUseCase(
	servers serverdomain.ServerRepository,
	providers provisioningdomain.ProviderFactory,
	overlays provisioningdomain.OSImageOverlayRepository,
) *RefreshServerUseCase {
	return &RefreshServerUseCase{servers: servers, providers: providers, overlays: overlays}
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
	uc.fillDeployedImageName(ctx, server, machine, provider)
	if err := uc.servers.Upsert(ctx, server); err != nil {
		return nil, err
	}
	return item, nil
}

// fillDeployedImageName resolves the effective OS image display name and default user onto a
// freshly deployed machine the moment a deploy completes, instead of the raw OS/release id and no
// login user until the next reconcile pass. The default user matters here: wait-for-ssh runs right
// after the deploy and logs in as it. It only acts when the machine is deployed and the mirrored
// name is still blank — the fresh-deploy case, which updateProvisioningProjection guarantees for a
// redeploy to a different image — so the deploy-observe poll does not re-read the provider catalog
// on every tick. A catalog read failure still fills the built-in default user for the observed OS
// family; a nil overlay repository leaves the carried-forward values untouched.
func (uc *RefreshServerUseCase) fillDeployedImageName(
	ctx context.Context,
	server *serverdomain.Server,
	machine *provisioningdomain.Machine,
	provider provisioningdomain.OSProvisioningProvider,
) {
	if uc.overlays == nil || server.Provisioning == nil {
		return
	}
	if machine.Status != provisioningdomain.MachineStatusDeployed || server.Provisioning.DeployedImageName != "" {
		return
	}
	// A failed catalog read yields the zero resolver, whose resolve still returns the built-in
	// default user, so the bool is not needed here.
	resolver, _ := buildDeployedImageResolver(ctx, provider, uc.overlays, server.Source.IntegrationID)
	image := resolver.resolve(machine.OSSystem, machine.DistroSeries, machine.Architecture, true)
	server.Provisioning.DeployedImageName = image.Name
	server.Provisioning.DeployedImageDefaultUser = image.DefaultUser
}
