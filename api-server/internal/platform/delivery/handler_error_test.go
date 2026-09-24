package delivery

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
)

// A platform deploy runs the provisioning preflight, so a deployment batch conflict (for example a
// custom OS image not verified for the requested deploy target) can reach the platform delivery
// error mapper. It must surface as an actionable 409, not leak as a generic 500 (which is what the
// dashboard showed as "Internal error").
func TestRespondErrorMapsDeploymentBatchConflictToConflict(t *testing.T) {
	app := fiber.New()
	app.Use(requestid.New())
	app.Post("/deploy", func(c *fiber.Ctx) error {
		// Mirror the exact wrapping the deploy preflight produces.
		return respondError(c, fmt.Errorf(
			"%w: this custom image is not verified for disk deployment; verify it on a ready Server first",
			provisioningdomain.ErrDeploymentBatchConflict,
		))
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/deploy", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	var body apierror.ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != apierror.CodeConflict {
		t.Errorf("code = %q, want %q", body.Error.Code, apierror.CodeConflict)
	}
	if want := "not verified for disk deployment"; !strings.Contains(body.Error.Message, want) {
		t.Errorf("message = %q, want it to contain %q", body.Error.Message, want)
	}
}
