package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
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

	ServerID   *string `json:"serverId"`
	SiteID     *string `json:"siteId"`
	PlatformID *string `json:"platformId"`
	ClusterID  *string `json:"clusterId"`
}

// SiteLookup reports whether a Site exists, so an alert read or acknowledgement scoped to a Site
// that does not exist is not_found rather than "no metrics backend" (DF-05).
type SiteLookup interface {
	SiteExists(ctx context.Context, siteID string) (bool, error)
}

// ErrUnknownSite means the request names a Site that does not exist.
var ErrUnknownSite = errors.New("site not found")

// ErrAlertNotFound means the fingerprint names no alert the Site's Alertmanager reports.
var ErrAlertNotFound = errors.New("alert not found")

// ErrInvalidMatchers means an acknowledgement's matchers would not cover the alert being
// acknowledged.
var ErrInvalidMatchers = errors.New("invalid silence matchers")

// AlertService reads and acknowledges the alerts of a Site's Alertmanager.
type AlertService struct {
	factory monitoringdomain.MonitoringFactory
	sites   SiteLookup
}

// NewAlertService wires the alert use cases; both dependencies are required.
func NewAlertService(factory monitoringdomain.MonitoringFactory, sites SiteLookup) *AlertService {
	return &AlertService{factory: factory, sites: sites}
}

// requireSite refuses a Site id that names no Site. An empty id is not checked: it reads the
// alerts of any metrics Integration.
func (s *AlertService) requireSite(ctx context.Context, siteID string) error {
	if siteID == "" {
		return nil
	}
	exists, err := s.sites.SiteExists(ctx, siteID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrUnknownSite
	}
	return nil
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
	if err := s.requireSite(ctx, input.SiteID); err != nil {
		return nil, err
	}
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
	SiteID string
	// Fingerprint names the alert being acknowledged; it must be one the Site's Alertmanager
	// reports.
	Fingerprint string
	// Matchers narrow the silence to labels of the alert, for example alertname and server_id
	// to also cover the alert's later instances. Empty silences the alert's full label set.
	// Alertmanager silences match labels, not fingerprints.
	Matchers map[string]string
	Duration time.Duration
	Actor    string
	Comment  string
}

// Acknowledge silences the alert the fingerprint names (DF-14). The silence matches the
// requested matchers, each of which must be a label of that alert so the silence always covers
// it, or, without matchers, the alert's full label set. ErrAlertNotFound is a fingerprint the
// Alertmanager does not report; ErrInvalidMatchers is a matcher the alert does not carry.
func (s *AlertService) Acknowledge(ctx context.Context, input AcknowledgeInput) (string, error) {
	if err := s.requireSite(ctx, input.SiteID); err != nil {
		return "", err
	}

	source, err := s.factory.Alerts(ctx, input.SiteID)
	if err != nil {
		return "", err
	}
	alerts, err := source.ListAlerts(ctx)
	if err != nil {
		return "", err
	}
	var alert *monitoringdomain.Alert
	for _, candidate := range alerts {
		if candidate.Fingerprint == input.Fingerprint {
			alert = candidate
			break
		}
	}
	if alert == nil {
		return "", ErrAlertNotFound
	}
	matchers, err := silenceMatchers(alert, input.Matchers)
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
		Matchers:  matchers,
		Duration:  duration,
		CreatedBy: input.Actor,
		Comment:   comment,
	})
}

// silenceMatchers returns the labels a silence of alert matches: requested, when given, after
// checking each is a label of the alert; otherwise the alert's full label set.
func silenceMatchers(alert *monitoringdomain.Alert, requested map[string]string) (map[string]string, error) {
	if len(requested) == 0 {
		if len(alert.Labels) == 0 {
			return nil, fmt.Errorf("%w: the alert has no labels to silence on", ErrInvalidMatchers)
		}
		matchers := make(map[string]string, len(alert.Labels))
		for name, value := range alert.Labels {
			matchers[name] = value
		}
		return matchers, nil
	}
	for name, value := range requested {
		if actual, ok := alert.Labels[name]; !ok || actual != value {
			return nil, fmt.Errorf("%w: %s=%q is not a label of the alert, so the silence would not cover it",
				ErrInvalidMatchers, name, value)
		}
	}
	return requested, nil
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
		PlatformID:  wire.String(alert.PlatformID),
		ClusterID:   wire.String(alert.PlatformID),
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
