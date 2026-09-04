package delivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	overviewapp "github.com/maple52046/swallow/internal/overview/application"
	"github.com/maple52046/swallow/internal/shared/jwt"
	"github.com/maple52046/swallow/internal/shared/middleware"
)

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type routeSource struct{}

func (routeSource) ListSites(context.Context) ([]overviewapp.Site, error) {
	return []overviewapp.Site{{ID: "site-a"}}, nil
}

func (routeSource) ListIntegrations(context.Context, string) ([]overviewapp.Integration, error) {
	return []overviewapp.Integration{}, nil
}

func (routeSource) ListServers(context.Context, string) ([]overviewapp.Server, error) {
	return []overviewapp.Server{}, nil
}

func (routeSource) ListPlatforms(context.Context, string) ([]overviewapp.Platform, error) {
	return []overviewapp.Platform{}, nil
}

func (routeSource) ListOperations(context.Context, string) ([]overviewapp.Operation, error) {
	return []overviewapp.Operation{}, nil
}

func (routeSource) ListFiringAlerts(context.Context, string) ([]overviewapp.Alert, error) {
	return []overviewapp.Alert{}, nil
}

func TestOverviewRouteAuthorizationAndSiteValidation(t *testing.T) {
	jwtService := jwt.NewService("overview-test-secret", time.Hour)
	userToken, err := jwtService.Sign("user-1", "operator", "user")
	if err != nil {
		t.Fatalf("sign user token: %v", err)
	}
	adminToken, err := jwtService.Sign("admin-1", "admin", "admin")
	if err != nil {
		t.Fatalf("sign admin token: %v", err)
	}

	handler := NewHandler(overviewapp.NewService(routeSource{}, fixedClock{
		now: time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC),
	}))
	app := fiber.New()
	app.Get("/api/v1/overview", middleware.Auth(jwtService), middleware.AdminOnly(), handler.Get)

	tests := []struct {
		name       string
		target     string
		token      string
		wantStatus int
	}{
		{name: "missing token", target: "/api/v1/overview", wantStatus: http.StatusUnauthorized},
		{name: "non admin", target: "/api/v1/overview", token: userToken, wantStatus: http.StatusForbidden},
		{name: "admin", target: "/api/v1/overview", token: adminToken, wantStatus: http.StatusOK},
		{name: "unknown site", target: "/api/v1/overview?siteId=missing", token: adminToken, wantStatus: http.StatusNotFound},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.target, nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			response, err := app.Test(request)
			if err != nil {
				t.Fatalf("request overview: %v", err)
			}
			defer response.Body.Close()
			if response.StatusCode != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.StatusCode, test.wantStatus)
			}
		})
	}
}
