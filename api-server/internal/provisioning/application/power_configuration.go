package application

import (
	"context"
	"fmt"
	"strings"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// PowerDriverOptionItem is one driver a Power Configuration write may choose, with its family.
type PowerDriverOptionItem struct {
	Driver string `json:"driver"`
	Family string `json:"family"`
}

// PowerConfigurationItem is a Server's Power Configuration as the API reports it
// (server-detail-actions.md, decision 054). It never carries the password: PasswordSet says only
// whether the provisioner holds one, and Address has URL passwords, queries, and fragments removed.
type PowerConfigurationItem struct {
	ServerID       string                  `json:"serverId"`
	Driver         string                  `json:"driver"`
	Family         string                  `json:"family"`
	Control        string                  `json:"control"`
	Address        string                  `json:"address"`
	PowerID        string                  `json:"powerId"`
	Username       string                  `json:"username"`
	PasswordSet    bool                    `json:"passwordSet"`
	Editable       bool                    `json:"editable"`
	ReadOnlyReason string                  `json:"readOnlyReason"`
	Drivers        []PowerDriverOptionItem `json:"drivers"`
}

// SetPowerConfigurationInput is an operator's replacement Power Configuration. Password follows
// provisioningdomain.PowerConfigurationChange: nil keeps the stored password for an unchanged
// driver and clears it when the driver changes; a pointer to "" clears it.
type SetPowerConfigurationInput struct {
	Driver   string
	Address  string
	PowerID  string
	Username string
	Password *string
}

// PowerConfigurationUseCase reads and writes a Server's provisioner-owned Power Configuration
// (decision 054).
//
// Both directions are live: the provisioner is the source of truth and swallow stores nothing, so a
// read always reflects the provisioner and a write is read back before it is reported. The write is
// gated on the provider-owned Server Lock like every mutation, validated by the driver family's
// power adapter before the provisioner is called, and never switches power or resumes a Workflow.
// The password crosses this use case only inward, on a write; it is never returned or logged.
type PowerConfigurationUseCase struct {
	servers   serverdomain.ServerRepository
	providers provisioningdomain.ProviderFactory
	adapters  *provisioningdomain.PowerAdapterRegistry
}

// NewPowerConfigurationUseCase wires the use case with the registry of supported driver families.
func NewPowerConfigurationUseCase(
	servers serverdomain.ServerRepository,
	providers provisioningdomain.ProviderFactory,
	adapters *provisioningdomain.PowerAdapterRegistry,
) *PowerConfigurationUseCase {
	return &PowerConfigurationUseCase{servers: servers, providers: providers, adapters: adapters}
}

// Get returns the Server's Power Configuration read live from its provisioner. A provisioner
// without the capability is refused as unsupported; ErrServerNotFound, ErrMachineNotFound, and
// provider errors pass through for the shared mapping.
func (uc *PowerConfigurationUseCase) Get(ctx context.Context, serverID string) (*PowerConfigurationItem, error) {
	server, err := uc.servers.FindByID(ctx, serverID)
	if err != nil {
		return nil, err
	}
	provider, err := uc.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return nil, err
	}
	reader, _, err := powerConfigurationPorts(provider)
	if err != nil {
		return nil, err
	}
	return uc.read(ctx, server, reader)
}

// Set validates input with the power adapter of its driver, writes it through to the provisioner,
// and returns the configuration read back. A driver no supported family knows, or a parameter the
// family refuses, is ErrInvalidPowerConfiguration before anything is sent; a locked Server is
// ErrServerLocked; a Server whose power a provisioner VM host manages is
// ErrPowerConfigurationManaged; a refusal by the provisioner is its ProviderError.
func (uc *PowerConfigurationUseCase) Set(ctx context.Context, serverID string, input SetPowerConfigurationInput) (*PowerConfigurationItem, error) {
	driver := provisioningdomain.PowerDriver(strings.ToLower(strings.TrimSpace(input.Driver)))
	adapter, ok := uc.adapters.ForDriver(driver)
	if !ok {
		return nil, fmt.Errorf("%w: driver %q is not one of %s", provisioningdomain.ErrInvalidPowerConfiguration,
			input.Driver, strings.Join(uc.driverNames(), ", "))
	}
	change, err := adapter.Normalize(provisioningdomain.PowerConfigurationChange{
		Driver: driver, Address: input.Address, PowerID: input.PowerID,
		Username: input.Username, Password: input.Password,
	})
	if err != nil {
		return nil, err
	}

	server, err := uc.servers.FindByID(ctx, serverID)
	if err != nil {
		return nil, err
	}
	provider, err := uc.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return nil, err
	}
	reader, writer, err := powerConfigurationPorts(provider)
	if err != nil {
		return nil, err
	}
	if err := requireServerUnlocked(ctx, uc.servers, server, provider); err != nil {
		return nil, err
	}
	if err := writer.SetPowerConfiguration(ctx, server.Source.ProviderMachineID, change); err != nil {
		return nil, err
	}
	return uc.read(ctx, server, reader)
}

// read shapes the live configuration into the API item, dropping the password.
func (uc *PowerConfigurationUseCase) read(
	ctx context.Context, server *serverdomain.Server, reader provisioningdomain.PowerConfigurationReader,
) (*PowerConfigurationItem, error) {
	config, err := reader.PowerConfiguration(ctx, server.Source.ProviderMachineID)
	if err != nil {
		return nil, err
	}
	item := &PowerConfigurationItem{
		ServerID:    server.ID,
		Driver:      string(config.Driver),
		Control:     string(provisioningdomain.PowerControlOf(config.Driver)),
		Address:     provisioningdomain.RedactPowerAddress(config.Address),
		PowerID:     config.PowerID,
		Username:    config.Username,
		PasswordSet: config.Password != "",
		Editable:    config.ManagedBy == "",
		Drivers:     uc.options(),
	}
	if adapter, ok := uc.adapters.ForDriver(config.Driver); ok {
		item.Family = string(adapter.Family())
	}
	if !item.Editable {
		item.ReadOnlyReason = fmt.Sprintf("This virtual machine takes its power from provisioner VM host %q; change it there.", config.ManagedBy)
	}
	return item, nil
}

func (uc *PowerConfigurationUseCase) options() []PowerDriverOptionItem {
	var items []PowerDriverOptionItem
	for _, option := range uc.adapters.Options() {
		items = append(items, PowerDriverOptionItem{Driver: string(option.Driver), Family: string(option.Family)})
	}
	return items
}

func (uc *PowerConfigurationUseCase) driverNames() []string {
	var names []string
	for _, option := range uc.adapters.Options() {
		names = append(names, string(option.Driver))
	}
	return names
}

// powerConfigurationPorts returns the provider's Power Configuration ports, or the unsupported
// refusal when the provider does not advertise and implement both.
func powerConfigurationPorts(provider provisioningdomain.OSProvisioningProvider) (
	provisioningdomain.PowerConfigurationReader, provisioningdomain.PowerConfigurationWriter, error,
) {
	reader, canRead := provider.(provisioningdomain.PowerConfigurationReader)
	writer, canWrite := provider.(provisioningdomain.PowerConfigurationWriter)
	if !provider.Capabilities().PowerConfiguration || !canRead || !canWrite {
		return nil, nil, unsupported("power configuration")
	}
	return reader, writer, nil
}
