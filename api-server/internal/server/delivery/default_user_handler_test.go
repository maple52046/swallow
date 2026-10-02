package delivery

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
)

// Each use-case error maps onto the status and code server-detail-actions.md publishes.
func TestRespondDefaultUserError(t *testing.T) {
	wrap := func(sentinel error) error {
		return &serverdomain.DefaultUserError{Err: sentinel, Server: "tainan-ci", User: "amd"}
	}
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   apierror.Code
	}{
		{"invalid user", serverdomain.ErrInvalidDefaultUser, http.StatusBadRequest, apierror.CodeValidation},
		{"password rejected", wrap(serverdomain.ErrHostPasswordRejected), http.StatusBadRequest, apierror.CodeValidation},
		{"unknown server", serverdomain.ErrServerNotFound, http.StatusNotFound, apierror.CodeNotFound},
		{"not deployed", wrap(serverdomain.ErrDefaultUserNotDeployed), http.StatusConflict, apierror.CodeConflict},
		{"key rejected", wrap(serverdomain.ErrDeploymentKeyRejected), http.StatusConflict, apierror.CodeConflict},
		{"no deployment key", wrap(serverdomain.ErrDeploymentKeyMissing), http.StatusConflict, apierror.CodeConflict},
		{"locked", &serverdomain.ServerLockedError{Name: "tainan-ci"}, http.StatusConflict, apierror.CodeConflict},
		{"unreachable", wrap(serverdomain.ErrHostUnreachable), http.StatusServiceUnavailable, apierror.CodeProviderUnavailable},
		{"lock unknown", &serverdomain.ServerLockUnavailableError{Name: "tainan-ci"}, http.StatusServiceUnavailable, apierror.CodeProviderUnavailable},
		{"unexpected", fmt.Errorf("boom"), http.StatusInternalServerError, apierror.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Use(requestid.New())
			app.Put("/:id", func(c *fiber.Ctx) error { return respondDefaultUserError(c, tc.err) })
			resp, err := app.Test(httptest.NewRequest(http.MethodPut, "/srv-1", nil))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			var body struct {
				Error struct {
					Code apierror.Code `json:"code"`
				} `json:"error"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.StatusCode != tc.wantStatus || body.Error.Code != tc.wantCode {
				t.Errorf("respondDefaultUserError(%v) = %d %s, want %d %s", tc.err, resp.StatusCode, body.Error.Code, tc.wantStatus, tc.wantCode)
			}
		})
	}
}
