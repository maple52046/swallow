package delivery

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/core/monitoring/application"
	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
)

// PlatformHeader carries the Platform id to the label proxy, which forces every query to the
// series whose platform_id label equals it (decision 003). The gateway always sets it from the
// authenticated route and never forwards a client's own value.
const PlatformHeader = "X-Swallow-Platform-Id"

// platformLabel is the series label the label proxy enforces.
const platformLabel = "platform_id"

// gatewayTimeout bounds one forwarded read; a Grafana panel that needs longer is asking for too much.
const gatewayTimeout = 60 * time.Second

// GatewayRoute is one Prometheus read path the gateway forwards. Path may hold one `:name`
// parameter, which is substituted from the request.
type GatewayRoute struct {
	Method string
	Path   string
}

// GatewayRoutes are the read paths the label proxy confines to a label (its query, series, label,
// and federation APIs); nothing else of the metrics store is reachable through the gateway.
var GatewayRoutes = []GatewayRoute{
	{http.MethodGet, "/api/v1/query"}, {http.MethodPost, "/api/v1/query"},
	{http.MethodGet, "/api/v1/query_range"}, {http.MethodPost, "/api/v1/query_range"},
	{http.MethodGet, "/api/v1/query_exemplars"}, {http.MethodPost, "/api/v1/query_exemplars"},
	{http.MethodGet, "/api/v1/series"},
	{http.MethodGet, "/api/v1/labels"}, {http.MethodPost, "/api/v1/labels"},
	{http.MethodGet, "/api/v1/label/:name/values"},
	{http.MethodGet, "/federate"},
}

// GatewayHandler serves a Platform's Grafana (platform-metrics-gateway.md): it authenticates the
// Platform's monitoring token and forwards the read to the label proxy of the Platform's Site with
// the Platform's id, so the response holds only that Platform's members' series.
type GatewayHandler struct {
	access   *application.PlatformAccess
	upstream monitoringdomain.PlatformMetricsUpstream
	client   *http.Client
}

// NewGatewayHandler wires the gateway to the token service and the upstream resolver.
func NewGatewayHandler(access *application.PlatformAccess, upstream monitoringdomain.PlatformMetricsUpstream) *GatewayHandler {
	return &GatewayHandler{access: access, upstream: upstream, client: &http.Client{Timeout: gatewayTimeout}}
}

// Forward returns the handler for one GatewayRoute.
func (h *GatewayHandler) Forward(route GatewayRoute) fiber.Handler {
	return func(c *fiber.Ctx) error {
		platformID := c.Params("id")
		token, _ := strings.CutPrefix(c.Get(fiber.HeaderAuthorization), "Bearer ")
		if err := h.access.Verify(c.Context(), platformID, strings.TrimSpace(token)); err != nil {
			if !errors.Is(err, application.ErrPlatformAccessDenied) {
				slog.Error("verify a platform monitoring token", "platformId", platformID, "error", err)
			}
			return apierror.Respond(c, apierror.New(apierror.CodeUnauthorized,
				"A valid Platform monitoring token is required."))
		}

		base, err := h.upstream.LabelProxyURL(c.Context(), platformID)
		switch {
		case errors.Is(err, monitoringdomain.ErrScopeNotFound):
			return apierror.Respond(c, apierror.New(apierror.CodeUnauthorized,
				"A valid Platform monitoring token is required."))
		case errors.Is(err, monitoringdomain.ErrNoMetricsIntegration):
			return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable,
				"The Platform's Site has no metrics store to read."))
		case err != nil:
			slog.Error("resolve a platform's label proxy", "platformId", platformID, "error", err)
			return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
		}

		target := strings.TrimRight(base, "/") + strings.Replace(route.Path, ":name", url.PathEscape(c.Params("name")), 1)
		query, err := url.ParseQuery(string(c.Request().URI().QueryString()))
		if err != nil {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, "The query string could not be parsed."))
		}
		// The label proxy takes the label value from PlatformHeader only; a client's own value is
		// dropped so no proxy configuration can ever read it.
		query.Del(platformLabel)
		if encoded := query.Encode(); encoded != "" {
			target += "?" + encoded
		}
		request, err := http.NewRequestWithContext(c.Context(), route.Method, target, bytes.NewReader(c.Body()))
		if err != nil {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, "The request could not be forwarded."))
		}
		if contentType := c.Get(fiber.HeaderContentType); contentType != "" {
			request.Header.Set(fiber.HeaderContentType, contentType)
		}
		request.Header.Set(PlatformHeader, platformID)

		response, err := h.client.Do(request)
		if err != nil {
			slog.Warn("forward a platform metrics read", "platformId", platformID, "error", err)
			return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable,
				"The Site's metrics store could not be reached."))
		}
		c.Status(response.StatusCode)
		if contentType := response.Header.Get(fiber.HeaderContentType); contentType != "" {
			c.Set(fiber.HeaderContentType, contentType)
		}
		c.Context().SetBodyStream(response.Body, -1)
		return nil
	}
}
