package delivery

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"

	"github.com/maple52046/swallow/internal/shared/apicontract"
)

// A Platform deploy takes the OS deployment contract's deploy-mode vocabulary (DF-02): deployTarget
// wins, ephemeral is its deprecated alias. Before, deployTarget was not read, so a request naming
// only `ram` was silently deployed to disk.
func TestMachinePreparationResolvesTheDeployTarget(t *testing.T) {
	tests := []struct {
		name string
		body string
		want *bool
		ok   bool
	}{
		{name: "ram deploy target", body: `{"settings":{"deployTarget":"ram"}}`, want: boolPtr(true), ok: true},
		{name: "disk deploy target", body: `{"settings":{"deployTarget":"disk"}}`, want: boolPtr(false), ok: true},
		{name: "deploy target wins over the alias", body: `{"settings":{"deployTarget":"disk","ephemeral":true}}`, want: boolPtr(false), ok: true},
		{name: "deprecated alias alone", body: `{"settings":{"ephemeral":true}}`, want: boolPtr(true), ok: true},
		{name: "no deploy mode", body: `{"settings":{"imageId":"noble"}}`, ok: true},
		{name: "unknown deploy target", body: `{"settings":{"deployTarget":"tape"}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var request machinePreparationRequest
			if err := json.Unmarshal([]byte(tc.body), &request); err != nil {
				t.Fatal(err)
			}
			preparation, ok := machinePreparationFromRequest(request)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			got := preparation.Ephemeral
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Errorf("Ephemeral = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDeployRejectsAnUnknownDeployTarget(t *testing.T) {
	app := fiber.New()
	app.Use(apicontract.ResponseValidator(t))
	app.Use(requestid.New())
	app.Post("/api/v1/platforms/deploy", (&PlatformHandler{}).Deploy)

	body := `{"siteId":"site-1","machinePreparation":{"mode":"provision_os","settings":{"imageId":"noble","deployTarget":"tape"}}}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/platforms/deploy", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(request)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func boolPtr(value bool) *bool { return &value }
