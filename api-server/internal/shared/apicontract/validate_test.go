package apicontract

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/gofiber/fiber/v2"
)

const validatorTestDocument = `
openapi: 3.0.3
info: {title: t, version: v1}
paths:
  /api/v1/things/{id}:
    get:
      tags: [things]
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                required: [id]
                properties:
                  id: {type: string}
                  labels:
                    type: object
                    additionalProperties: {type: string}
        default:
          description: error
          content:
            application/json:
              schema:
                type: object
                properties:
                  error: {type: object, properties: {code: {type: string}}}
    delete:
      tags: [things]
      responses:
        '204': {description: gone}
`

// reportRecorder collects violations; the validator may report from Fiber's serving goroutine.
type reportRecorder struct {
	mu       sync.Mutex
	messages []string
}

func (r *reportRecorder) report(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}

func TestResponseValidator(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		handler     fiber.Handler
		wantMessage string
	}{
		{
			name:    "documented response",
			method:  http.MethodGet,
			handler: func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"id": "a", "labels": fiber.Map{"k": "v"}}) },
		},
		{
			name:        "undocumented property",
			method:      http.MethodGet,
			handler:     func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"id": "a", "extra": true}) },
			wantMessage: "does not match the API contract",
		},
		{
			name:        "missing required property",
			method:      http.MethodGet,
			handler:     func(c *fiber.Ctx) error { return c.JSON(fiber.Map{}) },
			wantMessage: "does not match the API contract",
		},
		{
			name:        "undocumented success status",
			method:      http.MethodGet,
			handler:     func(c *fiber.Ctx) error { return c.Status(http.StatusAccepted).JSON(fiber.Map{"id": "a"}) },
			wantMessage: "status 202 is not documented",
		},
		{
			name:   "error status falls back to default",
			method: http.MethodGet,
			handler: func(c *fiber.Ctx) error {
				return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": fiber.Map{"code": "not_found"}})
			},
		},
		{
			name:        "undocumented content type",
			method:      http.MethodGet,
			handler:     func(c *fiber.Ctx) error { return c.SendString("plain") },
			wantMessage: `Content-Type "text/plain" is not documented`,
		},
		{
			name:    "empty documented no-content response",
			method:  http.MethodDelete,
			handler: func(c *fiber.Ctx) error { return c.SendStatus(http.StatusNoContent) },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, err := openapi3.NewLoader().LoadFromData([]byte(validatorTestDocument))
			if err != nil {
				t.Fatalf("load test document: %v", err)
			}
			closeObjects(document)
			recorder := &reportRecorder{}
			app := fiber.New()
			app.Use(newResponseValidator(document, recorder.report))
			// Group middleware mirrors production routing: the validator must see the handler's
			// route, not a group or middleware route, once the chain returns.
			group := app.Group("/api/v1/things", func(c *fiber.Ctx) error { return c.Next() })
			group.Add(test.method, "/:id", test.handler)

			response, err := app.Test(httptest.NewRequest(test.method, "/api/v1/things/a", nil))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			response.Body.Close()

			got := strings.Join(recorder.messages, "\n")
			if test.wantMessage == "" && got != "" {
				t.Errorf("validator reported %q, want no violation", got)
			}
			if test.wantMessage != "" && !strings.Contains(got, test.wantMessage) {
				t.Errorf("validator reported %q, want a message containing %q", got, test.wantMessage)
			}
		})
	}
}

// Undocumented routes are left to the route-table test in internal/app.
func TestResponseValidatorSkipsUndocumentedRoutes(t *testing.T) {
	document, err := openapi3.NewLoader().LoadFromData([]byte(validatorTestDocument))
	if err != nil {
		t.Fatalf("load test document: %v", err)
	}
	closeObjects(document)
	recorder := &reportRecorder{}
	app := fiber.New()
	app.Use(newResponseValidator(document, recorder.report))
	app.Get("/api/v1/other", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"bad": true}) })

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/other", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	response.Body.Close()
	if len(recorder.messages) > 0 {
		t.Errorf("validator reported %v, want nothing", recorder.messages)
	}
}

func TestAreaFilterLoadsOnlyNamedAreas(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "common.yaml"), "openapi: 3.0.3\ninfo: {title: t, version: v1}\npaths: {}\n")
	writeFile(t, filepath.Join(dir, "areas", "a.yaml"), "paths:\n  /a:\n    get: {responses: {'200': {description: ok}}}\n")
	// b.yaml is invalid on purpose: a filtered load must not read it.
	writeFile(t, filepath.Join(dir, "areas", "b.yaml"), "info: {title: other, version: v2}\n")
	document, err := loadDocument(dir, areaFilter(" a "))
	if err != nil {
		t.Fatalf("loadDocument(only a) error = %v", err)
	}
	if document.Paths.Value("/a") == nil {
		t.Errorf("loadDocument(only a) lacks /a")
	}
}
