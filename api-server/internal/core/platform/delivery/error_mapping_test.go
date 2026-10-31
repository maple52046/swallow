package delivery

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// DF-05: the platform error mapper follows conventions.md. A cluster's own not-found and conflict
// pass through as 404 and 409, a target in the wrong state is a conflict, and an unsupported
// capability or targets of several Sites are request errors.
func TestRespondErrorFollowsTheConventions(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"cluster object missing", &platformdomain.ReaderError{Kind: platformdomain.ReaderErrorNotFound, Detail: "pods \"x\" not found"}, http.StatusNotFound},
		{"cluster object exists", &platformdomain.ReaderError{Kind: platformdomain.ReaderErrorConflict, Detail: "namespaces \"a\" already exists"}, http.StatusConflict},
		{"cluster rejected the request", &platformdomain.ReaderError{Kind: platformdomain.ReaderErrorRejected, Detail: "invalid"}, http.StatusBadRequest},
		{"cluster unreachable", &platformdomain.ReaderError{Kind: platformdomain.ReaderErrorUnavailable}, http.StatusServiceUnavailable},
		{"target in the wrong state", fmt.Errorf("%w: srv-a requires \"deployed\"", workflowdomain.ErrTargetStateInvalid), http.StatusConflict},
		{"network configuration unsupported in a batch", fmt.Errorf("%w: %w", provisioningdomain.ErrDeploymentBatchConflict, provisioningdomain.ErrNetworkConfigurationUnsupported), http.StatusBadRequest},
		{"targets of several Sites", fmt.Errorf("%w: %w", workflowapp.ErrInvalidOperation, workflowapp.ErrTargetsSpanSites), http.StatusBadRequest},
		{"no playbook mapping stays unavailable", fmt.Errorf("%w: no playbook mapping", workflowapp.ErrInvalidOperation), http.StatusServiceUnavailable},
		{"an unknown failure", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/", func(c *fiber.Ctx) error { return respondError(c, tc.err) })
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}
