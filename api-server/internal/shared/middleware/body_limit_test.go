package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// bodyLimitTestLimit is the limit the tests bound bodies to; the server's own body limit is set to
// the same value so the tests run in the mode production runs in, where fasthttp streams any body
// over it instead of refusing it.
const bodyLimitTestLimit = 1 << 10

// newBodyLimitApp serves a JSON route behind BodyLimit and an exempt route that reads its stream
// itself, and records how many bytes each handler saw.
func newBodyLimitApp(t *testing.T, seen *int) *fiber.App {
	t.Helper()
	app := fiber.New(fiber.Config{StreamRequestBody: true, DisablePreParseMultipartForm: true, BodyLimit: bodyLimitTestLimit})
	app.Use(BodyLimit(bodyLimitTestLimit, func(c *fiber.Ctx) bool { return c.Path() == "/stream" }))
	app.Post("/json", func(c *fiber.Ctx) error {
		*seen = len(c.Body())
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Put("/stream", func(c *fiber.Ctx) error {
		n, err := io.Copy(io.Discard, c.Context().RequestBodyStream())
		if err != nil {
			return err
		}
		*seen = int(n)
		return c.SendStatus(fiber.StatusNoContent)
	})
	return app
}

// A body within the limit reaches the handler whole, declared or chunked; a larger one is refused
// with 413 before the handler runs, whether its Content-Length says so or a chunked body only
// shows it while being read. The exempt route still streams a body of any size.
func TestBodyLimit(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		size       int
		chunked    bool
		wantStatus int
		wantSeen   int
	}{
		{name: "declared body within the limit", method: "POST", path: "/json", size: bodyLimitTestLimit, wantStatus: http.StatusNoContent, wantSeen: bodyLimitTestLimit},
		{name: "declared body over the limit", method: "POST", path: "/json", size: 64 << 10, wantStatus: http.StatusRequestEntityTooLarge, wantSeen: -1},
		{name: "chunked body within the limit", method: "POST", path: "/json", size: 512, chunked: true, wantStatus: http.StatusNoContent, wantSeen: 512},
		{name: "chunked body over the limit", method: "POST", path: "/json", size: 64 << 10, chunked: true, wantStatus: http.StatusRequestEntityTooLarge, wantSeen: -1},
		{name: "exempt route streams a large body", method: "PUT", path: "/stream", size: 1 << 20, wantStatus: http.StatusNoContent, wantSeen: 1 << 20},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			seen := -1
			app := newBodyLimitApp(t, &seen)
			var body io.Reader = bytes.NewReader(bytes.Repeat([]byte("a"), tc.size))
			if tc.chunked {
				// A reader of unknown length makes the request chunked.
				body = io.NopCloser(strings.NewReader(strings.Repeat("a", tc.size)))
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			if tc.chunked {
				req.ContentLength = -1
				req.TransferEncoding = []string{"chunked"}
			}
			resp, err := app.Test(req, -1)
			if err != nil {
				t.Fatalf("Test() error = %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus || seen != tc.wantSeen {
				t.Errorf("status = %d, handler saw %d bytes; want %d and %d", resp.StatusCode, seen, tc.wantStatus, tc.wantSeen)
			}
			if tc.wantStatus == http.StatusRequestEntityTooLarge {
				var envelope struct {
					Error struct{ Code string } `json:"error"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil || envelope.Error.Code != "payload_too_large" {
					t.Errorf("413 body code = %q (decode error %v), want payload_too_large", envelope.Error.Code, err)
				}
			}
		})
	}
}
