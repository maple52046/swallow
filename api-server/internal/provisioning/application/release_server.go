package application

import (
	"context"
	"fmt"
	"strings"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// ReleaseServerInput carries provider-neutral release controls from the API. Secure
// and quick erase refine an enabled erase operation; they never imply erasure by
// themselves.
type ReleaseServerInput struct {
	ServerID    string
	Erase       bool
	SecureErase bool
	QuickErase  bool
	Comment     string
}

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
	return uc.ExecuteWithOptions(ctx, ReleaseServerInput{ServerID: serverID})
}

// ExecuteWithOptions releases a Server using optional disk-erasure controls while
// retaining the parameterless provider call for old clients and simpler adapters.
func (uc *ReleaseServerUseCase) ExecuteWithOptions(ctx context.Context, input ReleaseServerInput) (*ProvisioningStateItem, error) {
	if !input.Erase && (input.SecureErase || input.QuickErase) {
		return nil, fmt.Errorf("%w: secureErase and quickErase require erase=true", provisioningdomain.ErrInvalidReleaseRequest)
	}

	server, err := uc.servers.FindByID(ctx, input.ServerID)
	if err != nil {
		return nil, err
	}

	provider, err := uc.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return nil, err
	}

	comment := strings.TrimSpace(input.Comment)
	hasOptions := input.Erase || input.SecureErase || input.QuickErase || comment != ""
	var machine *provisioningdomain.Machine
	if hasOptions {
		releaser, ok := provider.(provisioningdomain.ConfigurableMachineReleaser)
		if !ok || !provider.Capabilities().ReleaseOptions {
			return nil, &provisioningdomain.ProviderError{
				Kind:   provisioningdomain.ProviderErrorRejected,
				Detail: "This provisioner does not support configurable release options.",
			}
		}
		machine, err = releaser.ReleaseWithOptions(ctx, provisioningdomain.ReleaseRequest{
			MachineID:   server.Source.ProviderMachineID,
			Erase:       input.Erase,
			SecureErase: input.SecureErase,
			QuickErase:  input.QuickErase,
			Comment:     comment,
		})
	} else {
		machine, err = provider.Release(ctx, server.Source.ProviderMachineID)
	}
	if err != nil {
		return nil, err
	}

	return applyProvisioningResult(ctx, uc.servers, server, machine), nil
}
