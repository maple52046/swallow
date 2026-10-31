package delivery

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// DF-05: a target that is not deployed is a state conflict (409), targets of several Sites are a
// request error (400), and a lock state that cannot be confirmed is unavailable (503), as in the
// other contexts' handlers.
func TestRespondErrorFollowsTheConventions(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"target not deployed", fmt.Errorf("%w: srv-a is not deployed", workflowdomain.ErrTargetStateInvalid), http.StatusConflict},
		{"targets of several Sites", fmt.Errorf("%w: %w", workflowapp.ErrInvalidOperation, workflowapp.ErrTargetsSpanSites), http.StatusBadRequest},
		{"no playbook mapping stays unavailable", fmt.Errorf("%w: no playbook mapping", workflowapp.ErrInvalidOperation), http.StatusServiceUnavailable},
		{"lock state unknown", &serverdomain.ServerLockUnavailableError{}, http.StatusServiceUnavailable},
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
