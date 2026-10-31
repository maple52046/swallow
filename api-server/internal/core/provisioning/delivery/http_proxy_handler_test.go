package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/core/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	"github.com/maple52046/swallow/internal/shared/apicontract"
	"github.com/maple52046/swallow/internal/shared/apierror"
)

// proxyHandlerProvider holds one Provisioner HTTP Proxy and replaces it on write.
type proxyHandlerProvider struct {
	provisioningdomain.OSProvisioningProvider
	config provisioningdomain.HTTPProxyConfiguration
	writes int
}

func (p *proxyHandlerProvider) Capabilities() provisioningdomain.ProviderCapabilities {
	return provisioningdomain.ProviderCapabilities{HTTPProxy: true}
}

func (p *proxyHandlerProvider) HTTPProxy(context.Context) (provisioningdomain.HTTPProxyConfiguration, error) {
	return p.config, nil
}

func (p *proxyHandlerProvider) SetHTTPProxy(_ context.Context, config provisioningdomain.HTTPProxyConfiguration) error {
	p.config = config
	p.writes++
	return nil
}

// proxyHandlerProviders knows only Integration maas-a, like the factory backed by the registry.
type proxyHandlerProviders struct {
	provider provisioningdomain.OSProvisioningProvider
}

func (f proxyHandlerProviders) For(_ context.Context, integrationID string) (provisioningdomain.OSProvisioningProvider, error) {
	if integrationID != "maas-a" {
		return nil, sitedomain.ErrIntegrationNotFound
	}
	return f.provider, nil
}

// newProxyHandlerApp mounts the handlers at their production paths so the contract validator
// matches each response to its documented operation.
func newProxyHandlerApp(t *testing.T, provider *proxyHandlerProvider) *fiber.App {
	t.Helper()
	handler := &ProvisioningHandler{}
	handler.AttachHTTPProxy(application.NewHTTPProxyUseCase(proxyHandlerProviders{provider: provider}))
	app := fiber.New()
	app.Use(apicontract.ResponseValidator(t))
	app.Get("/api/v1/provisioning/integrations/:id/http-proxy", handler.GetHTTPProxy)
	app.Put("/api/v1/provisioning/integrations/:id/http-proxy", handler.SetHTTPProxy)
	return app
}

func doProxyRequest(t *testing.T, app *fiber.App, method, integrationID, body string) (*http.Response, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, "/api/v1/provisioning/integrations/"+integrationID+"/http-proxy", strings.NewReader(body))
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

// The read is uncached and shaped by provisioner-http-proxy.md; an unknown Integration is 404.
func TestGetHTTPProxy(t *testing.T) {
	app := newProxyHandlerApp(t, &proxyHandlerProvider{config: provisioningdomain.HTTPProxyConfiguration{
		Mode: provisioningdomain.HTTPProxyModePeer, URL: "http://squid:3128",
	}})
	resp, body := doProxyRequest(t, app, http.MethodGet, "maas-a", "")
	if resp.StatusCode != http.StatusOK || body["integrationId"] != "maas-a" || body["mode"] != "peer" || body["httpProxy"] != "http://squid:3128" {
		t.Errorf("GET status %d body %v, want 200 with the peer proxy", resp.StatusCode, body)
	}
	if resp.Header.Get(fiber.HeaderCacheControl) != "no-store" {
		t.Error("GET response is cacheable")
	}

	resp, body = doProxyRequest(t, app, http.MethodGet, "missing", "")
	if errBody, _ := body["error"].(map[string]any); resp.StatusCode != http.StatusNotFound || errBody["code"] != string(apierror.CodeNotFound) {
		t.Errorf("GET unknown Integration: status %d body %v, want 404 not_found", resp.StatusCode, body)
	}
}

// A valid write answers 200 with the read-back; malformed bodies and URLs are 400 without a write.
func TestSetHTTPProxy(t *testing.T) {
	provider := &proxyHandlerProvider{}
	app := newProxyHandlerApp(t, provider)

	resp, body := doProxyRequest(t, app, http.MethodPut, "maas-a", `{"mode":"external","httpProxy":"http://10.170.168.20:18080"}`)
	if resp.StatusCode != http.StatusOK || body["mode"] != "external" || body["httpProxy"] != "http://10.170.168.20:18080" {
		t.Fatalf("PUT status %d body %v, want 200 with the external proxy read back", resp.StatusCode, body)
	}
	if resp.Header.Get(fiber.HeaderCacheControl) != "no-store" {
		t.Error("PUT response is cacheable")
	}

	for name, payload := range map[string]string{
		"missing mode":           `{"httpProxy":"http://proxy:3128"}`,
		"unknown mode":           `{"mode":"squid"}`,
		"external without URL":   `{"mode":"external"}`,
		"URL with builtin":       `{"mode":"builtin","httpProxy":"http://proxy:3128"}`,
		"credentials in the URL": `{"mode":"external","httpProxy":"http://u:p@proxy:3128"}`,
		"not JSON":               `[`,
	} {
		resp, body := doProxyRequest(t, app, http.MethodPut, "maas-a", payload)
		errBody, _ := body["error"].(map[string]any)
		if resp.StatusCode != http.StatusBadRequest || errBody["code"] != string(apierror.CodeValidation) {
			t.Errorf("%s: status %d body %v, want 400 validation_error", name, resp.StatusCode, body)
		}
	}
	if provider.writes != 1 {
		t.Errorf("writes = %d, want only the valid PUT to reach the provisioner", provider.writes)
	}
}
