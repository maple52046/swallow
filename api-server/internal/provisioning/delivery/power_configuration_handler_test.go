package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
)

// powerHandlerServers holds one Server; any other repository call panics.
type powerHandlerServers struct {
	serverdomain.ServerRepository
}

func (powerHandlerServers) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	if id != "srv-1" {
		return nil, serverdomain.ErrServerNotFound
	}
	return &serverdomain.Server{ID: id, Source: serverdomain.Source{IntegrationID: "maas-a", ProviderMachineID: "m-1"}}, nil
}

func (powerHandlerServers) Upsert(context.Context, *serverdomain.Server) error { return nil }

// powerHandlerProvider records the change it was given and reports a fixed configuration, or a
// VM host when managedBy is set.
type powerHandlerProvider struct {
	provisioningdomain.OSProvisioningProvider
	managedBy string
	change    *provisioningdomain.PowerConfigurationChange
}

func (p *powerHandlerProvider) Capabilities() provisioningdomain.ProviderCapabilities {
	return provisioningdomain.ProviderCapabilities{PowerConfiguration: true}
}

func (p *powerHandlerProvider) GetMachine(context.Context, string) (*provisioningdomain.Machine, error) {
	return &provisioningdomain.Machine{ID: "m-1", Status: provisioningdomain.MachineStatusNew}, nil
}

func (p *powerHandlerProvider) PowerConfiguration(context.Context, string) (*provisioningdomain.PowerConfiguration, error) {
	return &provisioningdomain.PowerConfiguration{
		Driver: "virsh", Address: "qemu+ssh://maas@h/system", PowerID: "vm", Password: "never-returned",
	}, nil
}

func (p *powerHandlerProvider) SetPowerConfiguration(_ context.Context, _ string, change provisioningdomain.PowerConfigurationChange) error {
	if p.managedBy != "" {
		return provisioningdomain.ErrPowerConfigurationManaged
	}
	p.change = &change
	return nil
}

type powerHandlerProviders struct {
	provider provisioningdomain.OSProvisioningProvider
}

func (f powerHandlerProviders) For(context.Context, string) (provisioningdomain.OSProvisioningProvider, error) {
	return f.provider, nil
}

func newPowerHandlerApp(provider *powerHandlerProvider) *fiber.App {
	handler := &ProvisioningHandler{}
	handler.AttachPowerConfiguration(application.NewPowerConfigurationUseCase(
		powerHandlerServers{}, powerHandlerProviders{provider: provider}, provisioningdomain.DefaultPowerAdapters()))
	app := fiber.New()
	app.Get("/servers/:id/power-configuration", handler.GetPowerConfiguration)
	app.Put("/servers/:id/power-configuration", handler.SetPowerConfiguration)
	return app
}

func putPower(t *testing.T, app *fiber.App, body string) (*http.Response, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/servers/srv-1/power-configuration", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp, decoded
}

// The read is uncached and never carries the password.
func TestGetPowerConfiguration(t *testing.T) {
	app := newPowerHandlerApp(&powerHandlerProvider{})
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/servers/srv-1/power-configuration", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StatusCode != http.StatusOK || body["driver"] != "virsh" || body["passwordSet"] != true {
		t.Errorf("status %d body %v, want 200 with the virsh configuration", resp.StatusCode, body)
	}
	if resp.Header.Get(fiber.HeaderCacheControl) != "no-store" {
		t.Error("response is cacheable")
	}
	if _, leaked := body["password"]; leaked {
		t.Error("response carries a password field")
	}
}

// An absent password keeps the stored one (nil), "" clears it; validation and VM-host refusals map
// to 400 and 409 (server-detail-actions.md).
func TestSetPowerConfiguration(t *testing.T) {
	provider := &powerHandlerProvider{}
	app := newPowerHandlerApp(provider)

	resp, body := putPower(t, app, `{"driver":"virsh","address":"qemu+ssh://maas@h/system","powerId":"vm"}`)
	if resp.StatusCode != http.StatusOK || body["driver"] != "virsh" {
		t.Fatalf("status %d body %v, want 200 with the read-back configuration", resp.StatusCode, body)
	}
	if provider.change == nil || provider.change.Password != nil {
		t.Errorf("change = %+v, want an omitted password passed as nil", provider.change)
	}

	putPower(t, app, `{"driver":"virsh","address":"qemu+ssh://maas@h/system","powerId":"vm","password":""}`)
	if provider.change.Password == nil || *provider.change.Password != "" {
		t.Errorf("change.Password = %v, want a pointer to \"\" that clears it", provider.change.Password)
	}

	for name, payload := range map[string]string{
		"missing driver":   `{"address":"10.0.0.5"}`,
		"unknown driver":   `{"driver":"lxd","address":"https://h"}`,
		"virsh with query": `{"driver":"virsh","address":"qemu+ssh://h/system?no_verify=1","powerId":"vm"}`,
		"not JSON":         `[`,
	} {
		resp, body := putPower(t, app, payload)
		errBody, _ := body["error"].(map[string]any)
		if resp.StatusCode != http.StatusBadRequest || errBody["code"] != string(apierror.CodeValidation) {
			t.Errorf("%s: status %d body %v, want 400 validation_error", name, resp.StatusCode, body)
		}
	}

	resp, body = putPower(t, newPowerHandlerApp(&powerHandlerProvider{managedBy: "kvm-3"}),
		`{"driver":"virsh","address":"qemu+ssh://h/system","powerId":"vm"}`)
	if errBody, _ := body["error"].(map[string]any); resp.StatusCode != http.StatusConflict || errBody["code"] != string(apierror.CodeConflict) {
		t.Errorf("VM-host member: status %d body %v, want 409 conflict", resp.StatusCode, body)
	}
}
