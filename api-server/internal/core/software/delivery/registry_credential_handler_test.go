package delivery

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	"github.com/maple52046/swallow/internal/shared/apicontract"
)

// The credential response shape has no password field at all, so no code path can echo one, and the
// contract's error codes are pinned.
func TestRegistryCredentialResponsesNeverCarryPasswords(t *testing.T) {
	encoded, err := json.Marshal(toRegistryCredentialResponse(&softwaredomain.RegistryCredential{
		ID: "cred-1", Registry: "harbor.lab.local", Username: "robot",
	}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(strings.ToLower(string(encoded)), "password") {
		t.Errorf("credential response %s mentions a password", encoded)
	}

	tests := []struct {
		err        error
		wantStatus int
	}{
		{fmt.Errorf("%w: registry", softwaredomain.ErrInvalidRegistryCredential), http.StatusBadRequest},
		{softwaredomain.ErrRegistryCredentialNotFound, http.StatusNotFound},
		{softwaredomain.ErrRegistryCredentialExists, http.StatusConflict},
	}
	for _, tc := range tests {
		app := fiber.New()
		app.Use(apicontract.ResponseValidator(t))
		app.Put("/api/v1/software/docker-ce/registry-credentials/:credentialId", func(c *fiber.Ctx) error {
			return respondRegistryCredentialError(c, tc.err)
		})
		resp, err := app.Test(httptest.NewRequest(http.MethodPut, "/api/v1/software/docker-ce/registry-credentials/cred-1", nil))
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != tc.wantStatus {
			t.Errorf("respondRegistryCredentialError(%v) status = %d, want %d", tc.err, resp.StatusCode, tc.wantStatus)
		}
	}
}
