package delivery

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/core/monitoring/application"
	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
)

type memoryTokens map[string][]byte

func (m memoryTokens) SaveToken(_ context.Context, platformID string, hash []byte, _ time.Time) error {
	m[platformID] = hash
	return nil
}

func (m memoryTokens) TokenHash(_ context.Context, platformID string) ([]byte, error) {
	if hash, ok := m[platformID]; ok {
		return hash, nil
	}
	return nil, monitoringdomain.ErrPlatformTokenNotFound
}

type staticUpstream map[string]string

func (u staticUpstream) LabelProxyURL(_ context.Context, platformID string) (string, error) {
	if proxy, ok := u[platformID]; ok {
		if proxy == "" {
			return "", monitoringdomain.ErrNoMetricsIntegration
		}
		return proxy, nil
	}
	return "", monitoringdomain.ErrScopeNotFound
}

// forwarded records what the fake label proxy received.
type forwarded struct {
	method, path, platform, authorization string
	query                                 url.Values
	body                                  string
}

func gatewayFixture(t *testing.T) (*fiber.App, *application.PlatformAccess, *[]forwarded, string) {
	t.Helper()
	var calls []forwarded
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, forwarded{
			method: r.Method, path: r.URL.Path, platform: r.Header.Get(PlatformHeader),
			authorization: r.Header.Get("Authorization"), query: r.URL.Query(), body: string(body),
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
	}))
	t.Cleanup(proxy.Close)

	access := application.NewPlatformAccess(memoryTokens{})
	handler := NewGatewayHandler(access, staticUpstream{"p-a": proxy.URL, "p-b": proxy.URL, "p-dark": ""})
	app := fiber.New()
	group := app.Group("/api/v1/platform-metrics/:id/prometheus")
	for _, route := range GatewayRoutes {
		group.Add(route.Method, route.Path, handler.Forward(route))
	}
	return app, access, &calls, proxy.URL
}

func gatewayRequest(t *testing.T, app *fiber.App, method, target, token, body string) *http.Response {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	request.Header.Set(PlatformHeader, "p-b")
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	return response
}

// A read with the Platform's current token reaches the label proxy with the Platform's id from the
// route, never the client's own header, query parameter, or credential; the proxy's answer is
// returned unchanged.
func TestGatewayForwardsTheAuthenticatedPlatformOnly(t *testing.T) {
	app, access, calls, _ := gatewayFixture(t)
	token, err := access.Issue(context.Background(), "p-a")
	if err != nil {
		t.Fatal(err)
	}

	response := gatewayRequest(t, app, http.MethodGet,
		"/api/v1/platform-metrics/p-a/prometheus/api/v1/query?query=up&platform_id=p-b", token, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	if body, _ := io.ReadAll(response.Body); !strings.Contains(string(body), `"resultType":"vector"`) {
		t.Errorf("body = %s, want the proxy's answer", body)
	}
	got := (*calls)[0]
	if got.path != "/api/v1/query" || got.platform != "p-a" || got.authorization != "" ||
		got.query.Get("query") != "up" || got.query.Has("platform_id") {
		t.Errorf("forwarded = %+v, want /api/v1/query for p-a with only the query", got)
	}

	gatewayRequest(t, app, http.MethodPost, "/api/v1/platform-metrics/p-a/prometheus/api/v1/query_range", token,
		"query=rate(node_cpu_seconds_total[5m])&start=1&end=2&step=30")
	gatewayRequest(t, app, http.MethodGet, "/api/v1/platform-metrics/p-a/prometheus/api/v1/label/job/values", token, "")
	if len(*calls) != 3 || (*calls)[1].method != http.MethodPost || !strings.Contains((*calls)[1].body, "node_cpu_seconds_total") ||
		(*calls)[2].path != "/api/v1/label/job/values" {
		t.Errorf("calls = %+v, want the form body and the label values path forwarded", *calls)
	}
}

// Without the Platform's current token nothing reaches the proxy: no token, another Platform's
// token, a replaced token, and an unknown Platform all answer 401 alike.
func TestGatewayRefusesAnyOtherCredential(t *testing.T) {
	app, access, calls, _ := gatewayFixture(t)
	old, _ := access.Issue(context.Background(), "p-a")
	current, _ := access.Issue(context.Background(), "p-a")
	other, _ := access.Issue(context.Background(), "p-b")

	for name, request := range map[string][2]string{
		"no token":               {"/api/v1/platform-metrics/p-a/prometheus/api/v1/query?query=up", ""},
		"another Platform":       {"/api/v1/platform-metrics/p-a/prometheus/api/v1/query?query=up", other},
		"replaced token":         {"/api/v1/platform-metrics/p-a/prometheus/api/v1/query?query=up", old},
		"unknown Platform":       {"/api/v1/platform-metrics/p-x/prometheus/api/v1/query?query=up", current},
		"not a monitoring token": {"/api/v1/platform-metrics/p-a/prometheus/api/v1/query?query=up", "swk_session"},
	} {
		if response := gatewayRequest(t, app, http.MethodGet, request[0], request[1], ""); response.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", name, response.StatusCode)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("the proxy was reached %d times, want never", len(*calls))
	}
}

// Only the label proxy's confined read paths exist; a Site without a label proxy is unavailable.
func TestGatewayExposesOnlyConfinedReads(t *testing.T) {
	app, access, calls, _ := gatewayFixture(t)
	token, _ := access.Issue(context.Background(), "p-a")
	for _, path := range []string{"/api/v1/status/config", "/api/v1/admin/tsdb/delete_series", "/-/reload"} {
		if response := gatewayRequest(t, app, http.MethodPost, "/api/v1/platform-metrics/p-a/prometheus"+path, token, ""); response.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, response.StatusCode)
		}
	}
	dark, _ := access.Issue(context.Background(), "p-dark")
	if response := gatewayRequest(t, app, http.MethodGet, "/api/v1/platform-metrics/p-dark/prometheus/api/v1/query?query=up", dark, ""); response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("a Site without a label proxy: status = %d, want 503", response.StatusCode)
	}
	if len(*calls) != 0 {
		t.Errorf("the proxy was reached %d times, want never", len(*calls))
	}
}
