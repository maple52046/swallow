package explorerhttp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/shared/apicontract"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/modules/software/docker-ce/explorer"
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
		{"host unavailable", fmt.Errorf("%w: x", explorer.ErrDockerHostUnavailable), http.StatusConflict, apierror.CodeConflict},
		{"docker not installed", fmt.Errorf("%w: x", explorer.ErrDockerNotInstalled), http.StatusConflict, apierror.CodeConflict},
		{"api disabled", fmt.Errorf("%w: x", explorer.ErrDockerAPIDisabled), http.StatusConflict, apierror.CodeConflict},
		{"predefined network", fmt.Errorf("%w: bridge", explorer.ErrPredefinedDockerNetwork), http.StatusConflict, apierror.CodeConflict},
		{"invalid request", fmt.Errorf("%w: image is required", explorer.ErrInvalidDockerRequest), http.StatusBadRequest, apierror.CodeValidation},
		{"server locked", &serverdomain.ServerLockedError{Name: "lab"}, http.StatusConflict, apierror.CodeConflict},
		{"lock unknown", &serverdomain.ServerLockUnavailableError{Name: "lab"}, http.StatusServiceUnavailable, apierror.CodeProviderUnavailable},
		{"engine not found", &explorer.DockerEngineError{Kind: explorer.DockerEngineNotFound, Detail: "No such image"}, http.StatusNotFound, apierror.CodeNotFound},
		{"engine conflict", &explorer.DockerEngineError{Kind: explorer.DockerEngineConflict, Detail: "in use"}, http.StatusConflict, apierror.CodeConflict},
		{"engine rejected", &explorer.DockerEngineError{Kind: explorer.DockerEngineRejected, Detail: "bad"}, http.StatusBadRequest, apierror.CodeValidation},
		{"engine unavailable", &explorer.DockerEngineError{Kind: explorer.DockerEngineUnavailable, Detail: "refused"}, http.StatusServiceUnavailable, apierror.CodeProviderUnavailable},
		{"unknown", fmt.Errorf("boom"), http.StatusInternalServerError, apierror.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Use(apicontract.ResponseValidator(t))
			app.Use(requestid.New())
			app.Get("/api/v1/servers/:id/docker", func(c *fiber.Ctx) error { return respondDockerError(c, tc.err) })
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/servers/srv-1/docker", nil))
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

// Clients URL-encode Engine ids (image ids contain ':'); the handler must hand the decoded id on.
func TestPathParamDecodesEncodedIDs(t *testing.T) {
	app := fiber.New()
	app.Use(apicontract.ResponseValidator(t))
	var got string
	app.Delete("/api/v1/servers/:id/docker/images/:imageId", func(c *fiber.Ctx) error {
		got = pathParam(c, "imageId")
		return success(c)
	})
	if _, err := app.Test(httptest.NewRequest(http.MethodDelete, "/api/v1/servers/srv-1/docker/images/sha256%3Aabc", nil)); err != nil {
		t.Fatalf("request: %v", err)
	}
	if got != "sha256:abc" {
		t.Errorf("pathParam = %q, want sha256:abc", got)
	}
}
