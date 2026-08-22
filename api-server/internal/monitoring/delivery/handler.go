package delivery

import (
	"errors"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/monitoring/application"
	monitoringdomain "github.com/maple52046/swallow/internal/monitoring/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/middleware"
)

// maxMetricServers bounds a metrics request so that one call cannot ask the store for
// the whole fleet at once.
const maxMetricServers = 200

type MonitoringHandler struct {
	alerts  *application.AlertService
	metrics *application.ServerMetricsService
}

func NewMonitoringHandler(
	alerts *application.AlertService,
	metrics *application.ServerMetricsService,
) *MonitoringHandler {
	return &MonitoringHandler{alerts: alerts, metrics: metrics}
}

func (h *MonitoringHandler) ListAlerts(c *fiber.Ctx) error {
	items, err := h.alerts.List(c.Context(), application.ListAlertsInput{
		SiteID:   c.Query("siteId"),
		ServerID: c.Query("serverId"),
		Severity: c.Query("severity"),
		State:    c.Query("state"),
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(items)
}

type acknowledgeRequest struct {
	// Matchers are the labels to silence on. Alertmanager silences match labels, so
	// the caller says which ones — usually alertname plus server_id.
	Matchers map[string]string `json:"matchers"`
	Duration string            `json:"duration"`
	Comment  string            `json:"comment"`
}

// Acknowledge creates an Alertmanager silence. There is no swallow-side acknowledged flag:
// the state belongs to the system that evaluates the rules.
func (h *MonitoringHandler) Acknowledge(c *fiber.Ctx) error {
	fingerprint := c.Params("fingerprint")
	if fingerprint == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "fingerprint is required."))
	}

	var req acknowledgeRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if len(req.Matchers) == 0 {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation,
			"matchers is required: Alertmanager silences match labels, not fingerprints."))
	}

	var duration time.Duration
	if req.Duration != "" {
		parsed, err := time.ParseDuration(req.Duration)
		if err != nil || parsed <= 0 {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation,
				"duration must be a positive Go duration, e.g. \"4h\"."))
		}
		duration = parsed
	}

	actor := ""
	if claims := middleware.GetClaims(c); claims != nil {
		actor = claims.Username
	}

	silenceID, err := h.alerts.Acknowledge(c.Context(), application.AcknowledgeInput{
		SiteID:      c.Query("siteId"),
		Fingerprint: fingerprint,
		Matchers:    req.Matchers,
		Duration:    duration,
		Actor:       actor,
		Comment:     req.Comment,
	})
	if err != nil {
		return respondError(c, err)
	}
	return c.JSON(fiber.Map{"silenceId": silenceID})
}

// ServerMetrics evaluates the named metric queries for one or more servers.
func (h *MonitoringHandler) ServerMetrics(c *fiber.Ctx) error {
	serverIDs := splitList(c.Query("serverIds"))
	if len(serverIDs) == 0 {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "serverIds is required."))
	}
	if len(serverIDs) > maxMetricServers {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation,
			"too many servers in one request; page through them instead."))
	}

	items, err := h.metrics.Query(c.Context(), serverIDs, splitList(c.Query("metrics")))
	if err != nil {
		return respondError(c, err)
	}

	return c.JSON(fiber.Map{
		"items":   items,
		"grafana": h.metrics.GrafanaLink(c.Context()),
	})
}

// MetricNames lists what a client may ask for, so that the fixed query set is
// discoverable rather than something to read from source.
func (h *MonitoringHandler) MetricNames(c *fiber.Ctx) error {
	return c.JSON(application.MetricNames())
}

func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func respondError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, monitoringdomain.ErrNoMetricsIntegration):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable,
			"No metrics backend is registered."))

	case errors.Is(err, monitoringdomain.ErrNoAlertSource):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable,
			"The metrics integration has no Alertmanager address configured."))

	case errors.Is(err, monitoringdomain.ErrInvalidServerID),
		errors.Is(err, application.ErrUnknownMetric):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))
	}

	var queryErr *monitoringdomain.QueryError
	if errors.As(err, &queryErr) {
		if queryErr.Kind == monitoringdomain.QueryErrorRejected {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation, queryErr.Detail))
		}
		log.Printf("monitoring backend %s: %v", queryErr.Kind, queryErr.Err)
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, queryErr.Detail))
	}

	log.Printf("monitoring: unhandled error: %v", err)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
}
