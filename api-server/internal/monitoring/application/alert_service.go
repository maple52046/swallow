package application

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	monitoringdomain "github.com/maple52046/swallow/internal/monitoring/domain"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// defaultSilenceDuration is how long an acknowledgement lasts when the caller does not
// say. Bounded on purpose: an indefinite silence is how an alert gets forgotten.
const defaultSilenceDuration = 4 * time.Hour

// AlertItem is an alert as Alertmanager reports it, with swallow's correlation attached.
type AlertItem struct {
	Fingerprint string            `json:"fingerprint"`
	Name        string            `json:"name"`
	Severity    string            `json:"severity"`
	State       string            `json:"state"`
	Summary     string            `json:"summary"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels"`
	StartsAt    *string           `json:"startsAt"`

	ServerID  *string `json:"serverId"`
	SiteID    *string `json:"siteId"`
	ClusterID *string `json:"clusterId"`
}

type AlertService struct {
	factory monitoringdomain.MonitoringFactory
}

func NewAlertService(factory monitoringdomain.MonitoringFactory) *AlertService {
	return &AlertService{factory: factory}
}

type ListAlertsInput struct {
	SiteID   string
	ServerID string
	Severity string
	// State filters on firing or suppressed. Empty returns both, so that an
	// acknowledged alert stays visible as acknowledged rather than looking fixed.
	State string
}

func (s *AlertService) List(ctx context.Context, input ListAlertsInput) ([]AlertItem, error) {
	source, err := s.factory.Alerts(ctx, input.SiteID)
	if err != nil {
		return nil, err
	}

	alerts, err := source.ListAlerts(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]AlertItem, 0, len(alerts))
	for _, alert := range alerts {
		if input.ServerID != "" && alert.ServerID != input.ServerID {
			continue
		}
		if input.Severity != "" && !strings.EqualFold(alert.Severity, input.Severity) {
			continue
		}
		if input.State != "" && string(alert.State) != input.State {
			continue
		}
		items = append(items, toAlertItem(alert))
	}

	// Most severe first, then newest, so that the top of the list is what to look at.
	sort.SliceStable(items, func(i, j int) bool {
		left, right := severityRank(items[i].Severity), severityRank(items[j].Severity)
		if left != right {
			return left < right
		}
		return derefTime(items[i].StartsAt) > derefTime(items[j].StartsAt)
	})

	return items, nil
}

// AcknowledgeInput silences an alert. Acknowledging is creating a silence in
// Alertmanager: the state lives where the alerting pipeline can see it, so swallow and
// Alertmanager cannot disagree about whether something was dealt with.
type AcknowledgeInput struct {
	SiteID      string
	Fingerprint string
	// Matchers identify what to silence. Alertmanager silences match labels, not
	// fingerprints, so the caller supplies the labels to match.
	Matchers map[string]string
	Duration time.Duration
	Actor    string
	Comment  string
}

func (s *AlertService) Acknowledge(ctx context.Context, input AcknowledgeInput) (string, error) {
	if len(input.Matchers) == 0 {
		return "", fmt.Errorf("matchers are required: Alertmanager silences match labels, not fingerprints")
	}

	source, err := s.factory.Alerts(ctx, input.SiteID)
	if err != nil {
		return "", err
	}

	duration := input.Duration
	if duration <= 0 {
		duration = defaultSilenceDuration
	}

	comment := input.Comment
	if comment == "" {
		comment = "Acknowledged in swallow"
	}

	return source.Silence(ctx, monitoringdomain.SilenceRequest{
		Matchers:  input.Matchers,
		Duration:  duration,
		CreatedBy: input.Actor,
		Comment:   comment,
	})
}

func toAlertItem(alert *monitoringdomain.Alert) AlertItem {
	labels := alert.Labels
	if labels == nil {
		labels = map[string]string{}
	}

	return AlertItem{
		Fingerprint: alert.Fingerprint,
		Name:        alert.Name,
		Severity:    alert.Severity,
		State:       string(alert.State),
		Summary:     alert.Summary,
		Description: alert.Description,
		Labels:      labels,
		StartsAt:    wire.TimePtr(alert.StartsAt),
		ServerID:    wire.String(alert.ServerID),
		SiteID:      wire.String(alert.SiteID),
		ClusterID:   wire.String(alert.ClusterID),
	}
}

func severityRank(severity string) int {
	switch strings.ToLower(severity) {
	case "critical":
		return 0
	case "warning":
		return 1
	case "info":
		return 2
	default:
		return 3
	}
}

func derefTime(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
