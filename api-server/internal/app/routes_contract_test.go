package app

import (
	"net/http"
	"regexp"
	"sort"
	"testing"

	"github.com/gofiber/fiber/v2"

	provisioningdelivery "github.com/maple52046/swallow/internal/provisioning/delivery"
	"github.com/maple52046/swallow/internal/shared/apicontract"
)

// pathParameter matches an OpenAPI path template parameter; names are compared loosely because
// Fiber and the contract may name the same parameter differently.
var pathParameter = regexp.MustCompile(`\{[^}]+\}`)

// Every route the API process registers must be documented in the OpenAPI contract, and every
// documented operation must be served, so the contract cannot silently fall behind the router
// (or describe routes that do not exist).
func TestRegisteredRoutesMatchTheContract(t *testing.T) {
	app := fiber.New()
	noop := func(c *fiber.Ctx) error { return nil }
	// registerRoutes only stores handler method values, so nil handlers are enough to build the
	// route table. The optional routes are enabled so the table is the full surface.
	registerRoutes(app, routeDeps{
		bootMediaISO: noop,
		cliBinary:    noop,
		bootISOs:     &provisioningdelivery.BootISOHandler{},
	})

	registered := map[string]bool{}
	for _, route := range app.GetRoutes(true) {
		// Fiber's Get also registers HEAD for the same handler; the contract documents GET.
		if route.Method == http.MethodHead {
			continue
		}
		registered[endpointKey(route.Method, apicontract.RouteTemplate(route.Path))] = true
	}

	document, err := apicontract.Load()
	if err != nil {
		t.Fatalf("load API contract: %v", err)
	}
	documented := map[string]bool{}
	for path, item := range document.Paths.Map() {
		for method := range item.Operations() {
			documented[endpointKey(method, path)] = true
		}
	}

	if missing := difference(registered, documented); len(missing) > 0 {
		t.Errorf("registered routes missing from the API contract (%d):\n%v", len(missing), missing)
	}
	if extra := difference(documented, registered); len(extra) > 0 {
		t.Errorf("API contract operations that no route serves (%d):\n%v", len(extra), extra)
	}
}

// endpointKey identifies an operation by method and path shape, ignoring parameter names.
func endpointKey(method, template string) string {
	return method + " " + pathParameter.ReplaceAllString(template, "{}")
}

// difference returns the sorted keys of a that are not in b.
func difference(a, b map[string]bool) []string {
	var keys []string
	for key := range a {
		if !b[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
