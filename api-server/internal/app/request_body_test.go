package app

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// Only the OS image upload content is left unbounded: the same path with another method, other
// upload routes, and every other route are bounded.
func TestIsImageUploadContent(t *testing.T) {
	tests := []struct {
		method, path string
		want         bool
	}{
		{method: fiber.MethodPut, path: "/api/v1/provisioning/image-uploads/8b1f3c2e/content", want: true},
		{method: fiber.MethodPost, path: "/api/v1/provisioning/image-uploads/8b1f3c2e/content"},
		{method: fiber.MethodPost, path: "/api/v1/provisioning/image-uploads"},
		{method: fiber.MethodPut, path: "/api/v1/provisioning/image-uploads/a/b/content"},
		{method: fiber.MethodPut, path: "/api/v1/provisioning/templates/t1"},
		{method: fiber.MethodPost, path: "/api/v1/auth/login"},
	}
	for _, tc := range tests {
		var got bool
		app := fiber.New()
		app.Use(func(c *fiber.Ctx) error {
			got = isImageUploadContent(c)
			return c.SendStatus(fiber.StatusNoContent)
		})
		if _, err := app.Test(httptest.NewRequest(tc.method, tc.path, nil)); err != nil {
			t.Fatalf("Test(%s %s) error = %v", tc.method, tc.path, err)
		}
		if got != tc.want {
			t.Errorf("isImageUploadContent(%s %s) = %t, want %t", tc.method, tc.path, got, tc.want)
		}
	}
}
