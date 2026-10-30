package apicontract

import (
	"encoding/json"
	"mime"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/gofiber/fiber/v2"
)

var (
	strictOnce sync.Once
	strict     *openapi3.T
	strictErr  error
)

// strictDocument returns a separately loaded copy of the contract in which every object schema
// that declares no additionalProperties is closed to undocumented properties. The copy is private
// to ResponseValidator so Load keeps returning the document exactly as authored.
func strictDocument() (*openapi3.T, error) {
	strictOnce.Do(func() {
		strict, strictErr = loadDocument(Dir(), areaFilter(os.Getenv(AreasEnv)))
		if strictErr == nil {
			closeObjects(strict)
		}
	})
	return strict, strictErr
}

// closeObjects sets additionalProperties: false on every object schema that leaves it
// unspecified. An author opts an object out (for example a passthrough map of extra variables)
// by declaring additionalProperties explicitly.
func closeObjects(document *openapi3.T) {
	closed := false
	walkSchemas(document, func(_ string, schema *openapi3.Schema) {
		isObject := len(schema.Properties) > 0 || (schema.Type != nil && schema.Type.Is(openapi3.TypeObject))
		if isObject && schema.AdditionalProperties.Has == nil && schema.AdditionalProperties.Schema == nil {
			schema.AdditionalProperties.Has = &closed
		}
	})
}

// ResponseValidator returns Fiber middleware that checks every response produced behind it
// against the operation the contract documents for the matched route, and reports each
// violation through t.Errorf. Install it first on a test app (app.Use) so it wraps every route.
//
// A response passes when its status is documented for the operation (an undocumented status of
// 400 or above may fall back to `default`), its Content-Type is documented for that status, and a
// JSON body matches the schema with undocumented object properties rejected. Bodies that are
// streamed (Server-Sent Events) or not JSON are checked for status and Content-Type only.
//
// Routes the contract does not document are not checked here; the route-table test in
// internal/app enforces that every registered route is documented. With AreasEnv set, only the
// listed areas are loaded, so routes of other areas count as undocumented. A handler that returns an
// error to Fiber's error handler is skipped too, because that response is rendered after this
// middleware returns.
//
// t.Errorf is safe from the goroutine fiber.App.Test serves requests on; the middleware never
// calls t.Fatal there. Loading the contract happens before any request, on the caller's
// goroutine, and fails the test immediately when the contract is invalid.
func ResponseValidator(t testing.TB) fiber.Handler {
	t.Helper()
	document, err := strictDocument()
	if err != nil {
		t.Fatalf("load API contract: %v", err)
	}
	return newResponseValidator(document, t.Errorf)
}

// newResponseValidator is ResponseValidator with its document and reporting injected, so the
// checks can be tested against a small document.
func newResponseValidator(document *openapi3.T, report func(format string, args ...any)) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if err := c.Next(); err != nil {
			return err
		}
		checkResponse(report, document, c)
		return nil
	}
}

// RouteTemplate converts a Fiber route path such as `/api/v1/servers/:id/` into the contract's
// path template form `/api/v1/servers/{id}`: parameters become braces and a trailing slash is
// dropped, because Fiber's non-strict routing serves both spellings of a group root.
func RouteTemplate(fiberPath string) string {
	segments := strings.Split(fiberPath, "/")
	for index, segment := range segments {
		if strings.HasPrefix(segment, ":") {
			segments[index] = "{" + strings.TrimSuffix(strings.TrimPrefix(segment, ":"), "?") + "}"
		}
	}
	template := strings.Join(segments, "/")
	if len(template) > 1 {
		template = strings.TrimSuffix(template, "/")
	}
	return template
}

// checkResponse validates one completed exchange; see ResponseValidator for the rules. After
// c.Next returns, c.Route is the route whose handler produced the response, not this middleware's.
func checkResponse(report func(format string, args ...any), document *openapi3.T, c *fiber.Ctx) {
	method := c.Method()
	if method == fiber.MethodHead || method == fiber.MethodOptions {
		return
	}
	template := RouteTemplate(c.Route().Path)
	item := document.Paths.Find(template)
	if item == nil {
		return
	}
	operation := item.GetOperation(method)
	if operation == nil {
		return
	}
	exchange := method + " " + template
	response := c.Response()
	status := response.StatusCode()
	ref := operation.Responses.Status(status)
	if ref == nil && status >= fiber.StatusBadRequest {
		ref = operation.Responses.Default()
	}
	if ref == nil || ref.Value == nil {
		report("%s: status %d is not documented in the API contract", exchange, status)
		return
	}

	contentType := string(response.Header.ContentType())
	// 204 and 304 never carry a body on the wire; Fiber's SendStatus still fills the buffered body
	// with the status text, which the client never receives.
	if status == fiber.StatusNoContent || status == fiber.StatusNotModified {
		return
	}
	if len(ref.Value.Content) == 0 {
		if !response.IsBodyStream() && len(response.Body()) > 0 {
			report("%s -> %d: the contract documents no body, got %q", exchange, status, contentType)
		}
		return
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		report("%s -> %d: unparsable Content-Type %q: %v", exchange, status, contentType, err)
		return
	}
	media := ref.Value.Content.Get(mediaType)
	if media == nil {
		report("%s -> %d: Content-Type %q is not documented", exchange, status, mediaType)
		return
	}
	// A streamed body is never buffered here: reading it would block on an open event stream.
	if response.IsBodyStream() || !isJSON(mediaType) || media.Schema == nil || media.Schema.Value == nil {
		return
	}
	var body any
	if err := json.Unmarshal(response.Body(), &body); err != nil {
		report("%s -> %d: body is not valid JSON: %v", exchange, status, err)
		return
	}
	if err := media.Schema.Value.VisitJSON(body, openapi3.MultiErrors(), openapi3.EnableFormatValidation()); err != nil {
		report("%s -> %d: response does not match the API contract:\n%v", exchange, status, err)
	}
}

func isJSON(mediaType string) bool {
	return mediaType == fiber.MIMEApplicationJSON || strings.HasSuffix(mediaType, "+json")
}
