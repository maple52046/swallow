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

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// The contract (servers-docker.md) fixes the status and code for every explorer failure; consumers
// branch on the code (the dashboard treats 409 as "not available"), so the mapping is pinned here.
func TestRespondDockerErrorFollowsContract(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   apierror.Code
	}{
		{"server not found", serverdomain.ErrServerNotFound, http.StatusNotFound, apierror.CodeNotFound},
		{"host unavailable", fmt.Errorf("%w: x", softwaredomain.ErrDockerHostUnavailable), http.StatusConflict, apierror.CodeConflict},
		{"docker not installed", fmt.Errorf("%w: x", softwaredomain.ErrDockerNotInstalled), http.StatusConflict, apierror.CodeConflict},
		{"api disabled", fmt.Errorf("%w: x", softwaredomain.ErrDockerAPIDisabled), http.StatusConflict, apierror.CodeConflict},
		{"predefined network", fmt.Errorf("%w: bridge", softwaredomain.ErrPredefinedDockerNetwork), http.StatusConflict, apierror.CodeConflict},
		{"invalid request", fmt.Errorf("%w: image is required", softwaredomain.ErrInvalidDockerRequest), http.StatusBadRequest, apierror.CodeValidation},
		{"server locked", &serverdomain.ServerLockedError{Name: "lab"}, http.StatusConflict, apierror.CodeConflict},
		{"lock unknown", &serverdomain.ServerLockUnavailableError{Name: "lab"}, http.StatusServiceUnavailable, apierror.CodeProviderUnavailable},
		{"engine not found", &softwaredomain.DockerEngineError{Kind: softwaredomain.DockerEngineNotFound, Detail: "No such image"}, http.StatusNotFound, apierror.CodeNotFound},
		{"engine conflict", &softwaredomain.DockerEngineError{Kind: softwaredomain.DockerEngineConflict, Detail: "in use"}, http.StatusConflict, apierror.CodeConflict},
		{"engine rejected", &softwaredomain.DockerEngineError{Kind: softwaredomain.DockerEngineRejected, Detail: "bad"}, http.StatusBadRequest, apierror.CodeValidation},
		{"engine unavailable", &softwaredomain.DockerEngineError{Kind: softwaredomain.DockerEngineUnavailable, Detail: "refused"}, http.StatusServiceUnavailable, apierror.CodeProviderUnavailable},
		{"unknown", fmt.Errorf("boom"), http.StatusInternalServerError, apierror.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Use(requestid.New())
			app.Get("/", func(c *fiber.Ctx) error { return respondDockerError(c, tc.err) })
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			var body apierror.ErrorResponse
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Error.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", body.Error.Code, tc.wantCode)
			}
		})
	}
}

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
		app.Get("/", func(c *fiber.Ctx) error { return respondRegistryCredentialError(c, tc.err) })
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != tc.wantStatus {
			t.Errorf("respondRegistryCredentialError(%v) status = %d, want %d", tc.err, resp.StatusCode, tc.wantStatus)
		}
	}
}

// Clients URL-encode Engine ids (image ids contain ':'); the handler must hand the decoded id on.
func TestPathParamDecodesEncodedIDs(t *testing.T) {
	app := fiber.New()
	var got string
	app.Delete("/images/:imageId", func(c *fiber.Ctx) error {
		got = pathParam(c, "imageId")
		return nil
	})
	if _, err := app.Test(httptest.NewRequest(http.MethodDelete, "/images/sha256%3Aabc", nil)); err != nil {
		t.Fatalf("request: %v", err)
	}
	if got != "sha256:abc" {
		t.Errorf("pathParam = %q, want sha256:abc", got)
	}
}
