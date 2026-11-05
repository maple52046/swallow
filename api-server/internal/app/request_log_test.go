package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

// A request the chain answers with an error is logged with the status the client receives, the
// error handler answers it exactly once, and the log line carries the response's request ID.
func TestRequestLogLogsTheStatusTheClientReceives(t *testing.T) {
	var output bytes.Buffer
	handled := 0
	app := fiber.New(fiber.Config{ErrorHandler: func(c *fiber.Ctx, err error) error {
		handled++
		return httpErrorHandler(c, err)
	}})
	app.Use(requestid.New())
	app.Use(requestLog(slog.New(slog.NewJSONHandler(&output, nil))))
	app.Get("/api/v1/things", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })
	app.Get("/api/v1/broken", func(*fiber.Ctx) error { return errors.New("mongo: connection refused") })
	// The body limit middleware raises this error before any route; the route stands in for it.
	app.Post("/api/v1/upload", func(*fiber.Ctx) error { return fiber.ErrRequestEntityTooLarge })

	for _, tc := range []struct {
		name, method, path string
		status             int
		errorHandled       bool
	}{
		{name: "an answered request", method: http.MethodGet, path: "/api/v1/things", status: 204},
		{name: "no route matches", method: http.MethodGet, path: "/api/v1/nothing", status: 404, errorHandled: true},
		{name: "the route takes another method", method: http.MethodPut, path: "/api/v1/things", status: 405, errorHandled: true},
		{name: "the body is over the limit", method: http.MethodPost, path: "/api/v1/upload", status: 413, errorHandled: true},
		{name: "a handler defect", method: http.MethodGet, path: "/api/v1/broken", status: 500, errorHandled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output.Reset()
			handled = 0
			resp, err := app.Test(httptest.NewRequest(tc.method, tc.path, strings.NewReader("")))
			if err != nil {
				t.Fatalf("Test(%s %s) error = %v", tc.method, tc.path, err)
			}
			if resp.StatusCode != tc.status {
				t.Errorf("%s %s status = %d, want %d", tc.method, tc.path, resp.StatusCode, tc.status)
			}
			if want := map[bool]int{true: 1}[tc.errorHandled]; handled != want {
				t.Errorf("%s %s ran the error handler %d times, want %d", tc.method, tc.path, handled, want)
			}
			var line struct {
				Msg       string `json:"msg"`
				RequestID string `json:"requestId"`
				Method    string `json:"method"`
				Path      string `json:"path"`
				Status    int    `json:"status"`
			}
			if err := json.Unmarshal(output.Bytes(), &line); err != nil {
				t.Fatalf("%s %s logged %q, want one JSON line: %v", tc.method, tc.path, output.String(), err)
			}
			if line.Msg != "http request" || line.Status != tc.status || line.Method != tc.method || line.Path != tc.path {
				t.Errorf("%s %s logged %+v, want status %d", tc.method, tc.path, line, tc.status)
			}
			if line.RequestID == "" || line.RequestID != resp.Header.Get(fiber.HeaderXRequestID) {
				t.Errorf("logged request ID %q, want the response's %q", line.RequestID, resp.Header.Get(fiber.HeaderXRequestID))
			}
		})
	}
}
