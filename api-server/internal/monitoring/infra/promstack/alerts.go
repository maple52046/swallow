package promstack

import (
	"context"
	"net/url"
	"time"

	monitoringdomain "github.com/AFDEAPAC/swallow/internal/monitoring/domain"
)

// AlertSource implements monitoringdomain.AlertSource against Alertmanager.
//
// Alerts are read rather than received: Alertmanager owns alert state, so querying it
// means gdcm has no copy that can disagree. Acknowledging is creating a silence, which
// is the same fact expressed in the system that owns it.
type AlertSource struct {
	client *Client
}

func NewAlertSource(client *Client) *AlertSource {
	return &AlertSource{client: client}
}

type alertJSON struct {
	Fingerprint string            `json:"fingerprint"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    string            `json:"startsAt"`
	Status      struct {
		State string `json:"state"`
	} `json:"status"`
}

func (a *AlertSource) ListAlerts(ctx context.Context) ([]*monitoringdomain.Alert, error) {
	query := url.Values{}
	// Silenced alerts are included so that an acknowledged alert stays visible as
	// acknowledged rather than disappearing, which would look like it was fixed.
	query.Set("silenced", "true")
	query.Set("inhibited", "true")
	query.Set("active", "true")

	var out []alertJSON
	if err := a.client.get(ctx, "/api/v2/alerts", query, &out); err != nil {
		return nil, translateError(err)
	}

	alerts := make([]*monitoringdomain.Alert, 0, len(out))
	for _, raw := range out {
		alerts = append(alerts, toAlert(raw))
	}
	return alerts, nil
}

func toAlert(raw alertJSON) *monitoringdomain.Alert {
	alert := &monitoringdomain.Alert{
		Fingerprint: raw.Fingerprint,
		Name:        raw.Labels["alertname"],
		Severity:    raw.Labels["severity"],
		State:       mapAlertState(raw.Status.State),
		Summary:     raw.Annotations["summary"],
		Description: raw.Annotations["description"],
		Labels:      raw.Labels,
		ServerID:    raw.Labels[monitoringdomain.LabelServerID],
		SiteID:      raw.Labels[monitoringdomain.LabelSite],
		ClusterID:   raw.Labels[monitoringdomain.LabelCluster],
	}

	if parsed, err := time.Parse(time.RFC3339, raw.StartsAt); err == nil {
		alert.StartsAt = parsed.UTC()
	}
	return alert
}

func mapAlertState(state string) monitoringdomain.AlertState {
	switch state {
	case "active":
		return monitoringdomain.AlertStateFiring
	case "suppressed":
		return monitoringdomain.AlertStateSuppressed
	default:
		return monitoringdomain.AlertStateUnknown
	}
}

type silenceMatcherJSON struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
	IsEqual bool   `json:"isEqual"`
}

type silenceRequestJSON struct {
	Matchers  []silenceMatcherJSON `json:"matchers"`
	StartsAt  string               `json:"startsAt"`
	EndsAt    string               `json:"endsAt"`
	CreatedBy string               `json:"createdBy"`
	Comment   string               `json:"comment"`
}

type silenceResponseJSON struct {
	SilenceID string `json:"silenceID"`
}

func (a *AlertSource) Silence(ctx context.Context, req monitoringdomain.SilenceRequest) (string, error) {
	now := time.Now().UTC()

	payload := silenceRequestJSON{
		StartsAt:  now.Format(time.RFC3339),
		EndsAt:    now.Add(req.Duration).Format(time.RFC3339),
		CreatedBy: req.CreatedBy,
		Comment:   req.Comment,
	}
	for name, value := range req.Matchers {
		payload.Matchers = append(payload.Matchers, silenceMatcherJSON{
			Name:    name,
			Value:   value,
			IsRegex: false,
			IsEqual: true,
		})
	}

	var out silenceResponseJSON
	if err := a.client.postJSON(ctx, "/api/v2/silences", payload, &out); err != nil {
		return "", translateError(err)
	}
	return out.SilenceID, nil
}
