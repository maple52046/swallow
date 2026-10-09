package application

import (
	"context"
	"errors"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// powerServers is an in-memory ServerRepository with the two calls the use case makes; any other
// call panics through the embedded nil interface.
type powerServers struct {
	serverdomain.ServerRepository
	server *serverdomain.Server
}

func (r *powerServers) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	if r.server == nil || r.server.ID != id {
		return nil, serverdomain.ErrServerNotFound
	}
	copied := *r.server
	return &copied, nil
}

func (r *powerServers) Upsert(context.Context, *serverdomain.Server) error { return nil }

// powerProvider is a provisioner holding one machine's Power Configuration and lock state. A write
// replaces the configuration the way MAAS does, keeping the password when the change omits it.
type powerProvider struct {
	provisioningdomain.OSProvisioningProvider
	capable bool
	locked  bool
	config  provisioningdomain.PowerConfiguration
	writes  []provisioningdomain.PowerConfigurationChange
	setErr  error
}

func (p *powerProvider) Capabilities() provisioningdomain.ProviderCapabilities {
	return provisioningdomain.ProviderCapabilities{PowerConfiguration: p.capable}
}

func (p *powerProvider) GetMachine(context.Context, string) (*provisioningdomain.Machine, error) {
	return &provisioningdomain.Machine{ID: "machine-1", Status: provisioningdomain.MachineStatusNew, Locked: p.locked}, nil
}

func (p *powerProvider) PowerConfiguration(context.Context, string) (*provisioningdomain.PowerConfiguration, error) {
	config := p.config
	return &config, nil
}

func (p *powerProvider) SetPowerConfiguration(_ context.Context, _ string, change provisioningdomain.PowerConfigurationChange) error {
	if p.setErr != nil {
		return p.setErr
	}
	p.writes = append(p.writes, change)
	password := p.config.Password
	if change.Password != nil {
		password = *change.Password
	}
	p.config = provisioningdomain.PowerConfiguration{
		Driver: change.Driver, Address: change.Address, PowerID: change.PowerID, Username: change.Username, Password: password,
	}
	return nil
}

func newPowerUseCase(provider *powerProvider) *PowerConfigurationUseCase {
	servers := &powerServers{server: &serverdomain.Server{
		ID: "srv-1", Observed: serverdomain.Observed{Hostname: "simple-pig"},
		Source: serverdomain.Source{IntegrationID: "maas-a", ProviderMachineID: "machine-1"},
	}}
	return NewPowerConfigurationUseCase(servers, fixedProviders{provider: provider}, provisioningdomain.DefaultPowerAdapters())
}

// The read reports the driver's family and control, redacts the address, and never carries the
// password — only whether one is set.
func TestPowerConfigurationGet(t *testing.T) {
	provider := &powerProvider{capable: true, config: provisioningdomain.PowerConfiguration{
		Driver: "virsh", Address: "qemu+ssh://maas:pw@tainan-ci/system", PowerID: "simple-pig", Password: "secret",
	}}
	item, err := newPowerUseCase(provider).Get(context.Background(), "srv-1")
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if item.Driver != "virsh" || item.Family != "virsh" || item.Control != "automatic" || item.PowerID != "simple-pig" {
		t.Errorf("Get = %+v", item)
	}
	if item.Address != "qemu+ssh://maas@tainan-ci/system" || !item.PasswordSet || !item.Editable {
		t.Errorf("Get address %q, passwordSet %v, editable %v; want a redacted address, a set password, editable",
			item.Address, item.PasswordSet, item.Editable)
	}
	if len(item.Drivers) != 3 || item.Drivers[2] != (PowerDriverOptionItem{Driver: "virsh", Family: "virsh"}) {
		t.Errorf("Drivers = %+v", item.Drivers)
	}

	provider.config = provisioningdomain.PowerConfiguration{}
	item, err = newPowerUseCase(provider).Get(context.Background(), "srv-1")
	if err != nil || item.Driver != "" || item.Family != "" || item.Control != "none" {
		t.Errorf("Get without a driver = %+v, %v; want no driver, no family, control none", item, err)
	}

	provider.config = provisioningdomain.PowerConfiguration{Driver: "virsh", ManagedBy: "kvm-3"}
	item, err = newPowerUseCase(provider).Get(context.Background(), "srv-1")
	if err != nil || item.Editable || item.ReadOnlyReason == "" {
		t.Errorf("Get of a VM-host member = %+v, %v; want read-only with a reason", item, err)
	}
}

func TestPowerConfigurationSet(t *testing.T) {
	t.Run("writes the normalized change and returns it read back", func(t *testing.T) {
		provider := &powerProvider{capable: true}
		item, err := newPowerUseCase(provider).Set(context.Background(), "srv-1", SetPowerConfigurationInput{
			Driver: " Virsh ", Address: " qemu+ssh://maas@tainan-ci/system ", PowerID: "simple-pig",
		})
		if err != nil {
			t.Fatalf("Set error = %v", err)
		}
		if len(provider.writes) != 1 || provider.writes[0].Driver != "virsh" || provider.writes[0].Address != "qemu+ssh://maas@tainan-ci/system" {
			t.Errorf("writes = %+v, want one normalized virsh change", provider.writes)
		}
		if item.Driver != "virsh" || item.Family != "virsh" || item.PasswordSet {
			t.Errorf("Set = %+v", item)
		}
	})
	t.Run("an invalid change never reaches the provisioner", func(t *testing.T) {
		provider := &powerProvider{capable: true}
		for name, input := range map[string]SetPowerConfigurationInput{
			"unknown driver": {Driver: "lxd", Address: "https://h"},
			"bad virsh URI":  {Driver: "virsh", Address: "qemu+ssh://h/system?keyfile=/k", PowerID: "vm"},
		} {
			if _, err := newPowerUseCase(provider).Set(context.Background(), "srv-1", input); !errors.Is(err, provisioningdomain.ErrInvalidPowerConfiguration) {
				t.Errorf("Set(%s) error = %v, want ErrInvalidPowerConfiguration", name, err)
			}
		}
		if len(provider.writes) != 0 {
			t.Errorf("writes = %+v, want none", provider.writes)
		}
	})
	t.Run("a locked Server is refused before the write", func(t *testing.T) {
		provider := &powerProvider{capable: true, locked: true}
		_, err := newPowerUseCase(provider).Set(context.Background(), "srv-1", SetPowerConfigurationInput{
			Driver: "ipmi", Address: "10.0.0.5",
		})
		if !errors.Is(err, serverdomain.ErrServerLocked) || len(provider.writes) != 0 {
			t.Errorf("Set error = %v after %d writes, want ErrServerLocked and no write", err, len(provider.writes))
		}
	})
	t.Run("a provisioner without the capability is unsupported", func(t *testing.T) {
		provider := &powerProvider{}
		_, err := newPowerUseCase(provider).Set(context.Background(), "srv-1", SetPowerConfigurationInput{
			Driver: "ipmi", Address: "10.0.0.5",
		})
		var providerErr *provisioningdomain.ProviderError
		if !errors.As(err, &providerErr) || providerErr.Kind != provisioningdomain.ProviderErrorRejected {
			t.Errorf("Set error = %v, want an unsupported refusal", err)
		}
	})
	t.Run("a VM-host member's refusal passes through", func(t *testing.T) {
		provider := &powerProvider{capable: true, setErr: provisioningdomain.ErrPowerConfigurationManaged}
		_, err := newPowerUseCase(provider).Set(context.Background(), "srv-1", SetPowerConfigurationInput{
			Driver: "virsh", Address: "qemu+ssh://h/system", PowerID: "vm",
		})
		if !errors.Is(err, provisioningdomain.ErrPowerConfigurationManaged) {
			t.Errorf("Set error = %v, want ErrPowerConfigurationManaged", err)
		}
	})
}
