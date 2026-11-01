package delivery

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/core/module/domain"
	"github.com/maple52046/swallow/internal/shared/apicontract"
)

func mustModule(t *testing.T, manifest string) domain.Module {
	t.Helper()
	module, err := domain.Load([]byte(manifest), nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return module
}

// The list follows the contract, a module without a spec reads null, and a schema is served as the
// manifest wrote it, so its settings keep their order for a generic form.
func TestListServesTheManifestSchemaAsWritten(t *testing.T) {
	registry, err := domain.NewRegistry(
		mustModule(t, `{"contractVersion":1,"kind":"software","id":"share","version":"0.1.0",
			"spec":{"type":"object","additionalProperties":false,"properties":{
				"zeta":{"type":"string","title":"Zeta"},"alpha":{"type":"integer","title":"Alpha","minimum":1}}}}`),
		mustModule(t, `{"contractVersion":1,"kind":"platform","id":"runtime","version":"1.2.3"}`),
	)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	app := fiber.New()
	app.Use(apicontract.ResponseValidator(t))
	app.Get("/api/v1/modules", NewModuleHandler(registry).List)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/modules", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, `"kind":"platform","id":"runtime","version":"1.2.3","spec":null`) {
		t.Errorf("body %s does not list the platform module with a null spec", body)
	}
	zeta, alpha := strings.Index(body, `"zeta"`), strings.Index(body, `"alpha"`)
	if zeta < 0 || alpha < zeta {
		t.Errorf("body %s does not keep the manifest's property order", body)
	}
}
