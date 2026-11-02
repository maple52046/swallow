package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

// Fiber's own errors keep their meaning in the contract's envelope, with the request ID, and any
// other error is a 500 whose detail stays in the log.
func TestHTTPErrorHandlerMapsFiberErrorsToTheEnvelope(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: httpErrorHandler})
	app.Use(requestid.New())
	app.Post("/api/v1/things", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })
	app.Get("/api/v1/broken", func(*fiber.Ctx) error { return errors.New("mongo: connection refused at 10.0.0.5") })
	// The body limit is enforced while the request is read, before any test route; this stands in
	// for the error it raises.
	app.Post("/api/v1/upload", func(*fiber.Ctx) error { return fiber.ErrRequestEntityTooLarge })

	for _, tc := range []struct {
		name, method, path, body string
		status                   int
		code                     string
	}{
		{name: "unknown path", method: http.MethodGet, path: "/api/v1/operations", status: 404, code: "not_found"},
		{name: "wrong method", method: http.MethodGet, path: "/api/v1/things", status: 405, code: "method_not_allowed"},
		{name: "body over the limit", method: http.MethodPost, path: "/api/v1/upload", status: 413, code: "payload_too_large"},
		{name: "defect", method: http.MethodGet, path: "/api/v1/broken", status: 500, code: "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := app.Test(httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if err != nil {
				t.Fatalf("Test() error = %v", err)
			}
			var body struct {
				Error struct {
					Code, Message, RequestID string
				} `json:"error"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.StatusCode != tc.status || body.Error.Code != tc.code || body.Error.RequestID == "" {
				t.Errorf("response = %d %+v, want %d %s with a request ID", resp.StatusCode, body.Error, tc.status, tc.code)
			}
			if strings.Contains(body.Error.Message, "10.0.0.5") {
				t.Errorf("message %q leaks the error detail", body.Error.Message)
			}
		})
	}
}
