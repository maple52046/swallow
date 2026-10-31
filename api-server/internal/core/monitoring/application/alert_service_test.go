package application

import (
	"context"
	"errors"
	"reflect"
	"testing"

	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
)

// stubAlertSource reports a fixed set of alerts and records the silences requested.
type stubAlertSource struct {
	alerts   []*monitoringdomain.Alert
	silences []monitoringdomain.SilenceRequest
}

func (s *stubAlertSource) ListAlerts(context.Context) ([]*monitoringdomain.Alert, error) {
	return s.alerts, nil
}

func (s *stubAlertSource) Silence(_ context.Context, request monitoringdomain.SilenceRequest) (string, error) {
	s.silences = append(s.silences, request)
	return "silence-1", nil
}

// stubFactory serves one alert source for every Site.
type stubFactory struct {
	monitoringdomain.MonitoringFactory
	source *stubAlertSource
}

func (f stubFactory) Alerts(context.Context, string) (monitoringdomain.AlertSource, error) {
	return f.source, nil
}

// sitesNamed knows exactly the listed Site ids.
type sitesNamed map[string]bool

func (s sitesNamed) SiteExists(_ context.Context, siteID string) (bool, error) { return s[siteID], nil }

func TestAcknowledgeSilencesTheNamedAlert(t *testing.T) {
	labels := map[string]string{"alertname": "GPUHot", "server_id": "srv-1", "severity": "critical"}
	tests := []struct {
		name     string
		input    AcknowledgeInput
		wantErr  error
		matchers map[string]string
	}{
		{name: "full label set without matchers", input: AcknowledgeInput{Fingerprint: "fp-1"}, matchers: labels},
		{name: "a subset of the alert's labels", input: AcknowledgeInput{Fingerprint: "fp-1", Matchers: map[string]string{"alertname": "GPUHot"}},
			matchers: map[string]string{"alertname": "GPUHot"}},
		{name: "a matcher the alert does not carry", input: AcknowledgeInput{Fingerprint: "fp-1", Matchers: map[string]string{"server_id": "srv-2"}},
			wantErr: ErrInvalidMatchers},
		{name: "an unknown fingerprint", input: AcknowledgeInput{Fingerprint: "fp-x"}, wantErr: ErrAlertNotFound},
		{name: "an unknown Site", input: AcknowledgeInput{SiteID: "site-x", Fingerprint: "fp-1"}, wantErr: ErrUnknownSite},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := &stubAlertSource{alerts: []*monitoringdomain.Alert{{Fingerprint: "fp-1", Labels: labels}}}
			service := NewAlertService(stubFactory{source: source}, sitesNamed{"site-a": true})

			_, err := service.Acknowledge(context.Background(), tc.input)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Acknowledge() error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if len(source.silences) != 0 {
					t.Errorf("a refused acknowledgement created silences %v", source.silences)
				}
				return
			}
			if len(source.silences) != 1 || !reflect.DeepEqual(source.silences[0].Matchers, tc.matchers) {
				t.Errorf("silences = %+v, want one matching %v", source.silences, tc.matchers)
			}
		})
	}
}
