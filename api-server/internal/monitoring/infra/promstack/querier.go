package promstack

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	monitoringdomain "github.com/maple52046/swallow/internal/monitoring/domain"
)

// Querier implements monitoringdomain.MetricsQuerier against a Prometheus-compatible
// query API.
type Querier struct {
	client *Client
}

func NewQuerier(client *Client) *Querier {
	return &Querier{client: client}
}

func (q *Querier) Name() string { return "prometheus" }

// queryResponse is the Prometheus instant-query envelope.
//
// Value is [unix_seconds, "sample_value"] — the value is a string because Prometheus
// preserves exact float formatting, so it has to be parsed rather than decoded.
type queryResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func (q *Querier) Query(ctx context.Context, expr string) ([]monitoringdomain.Sample, error) {
	query := url.Values{}
	query.Set("query", expr)

	var out queryResponse
	if err := q.client.get(ctx, "/api/v1/query", query, &out); err != nil {
		return nil, translateError(err)
	}

	// Prometheus can answer HTTP 200 with a logical error, so the envelope has to be
	// checked as well as the status code.
	if out.Status != "success" {
		detail := out.Error
		if detail == "" {
			detail = "the metrics store rejected the query."
		}
		return nil, &monitoringdomain.QueryError{
			Kind:   monitoringdomain.QueryErrorRejected,
			Detail: detail,
		}
	}

	samples := make([]monitoringdomain.Sample, 0, len(out.Data.Result))
	for _, result := range out.Data.Result {
		sample, ok := toSample(result.Metric, result.Value)
		if !ok {
			// A sample that cannot be parsed is skipped rather than failing the whole
			// query: one malformed series should not hide the rest.
			continue
		}
		samples = append(samples, sample)
	}
	return samples, nil
}

func toSample(labels map[string]string, value []any) (monitoringdomain.Sample, bool) {
	if len(value) != 2 {
		return monitoringdomain.Sample{}, false
	}

	seconds, ok := value[0].(float64)
	if !ok {
		return monitoringdomain.Sample{}, false
	}
	raw, ok := value[1].(string)
	if !ok {
		return monitoringdomain.Sample{}, false
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return monitoringdomain.Sample{}, false
	}

	return monitoringdomain.Sample{
		Labels:    labels,
		Value:     parsed,
		Timestamp: time.Unix(int64(seconds), 0).UTC(),
	}, true
}

func translateError(err error) error {
	var transportErr *transportError
	if errors.As(err, &transportErr) {
		return &monitoringdomain.QueryError{
			Kind:   monitoringdomain.QueryErrorUnavailable,
			Detail: "Could not reach the monitoring backend.",
			Err:    err,
		}
	}

	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		return err
	}

	switch {
	case apiErr.StatusCode == http.StatusUnauthorized, apiErr.StatusCode == http.StatusForbidden:
		return &monitoringdomain.QueryError{
			Kind:   monitoringdomain.QueryErrorAuth,
			Detail: "The monitoring backend rejected the credential gdcm is configured with.",
			Err:    err,
		}
	case apiErr.StatusCode >= http.StatusInternalServerError:
		return &monitoringdomain.QueryError{
			Kind:   monitoringdomain.QueryErrorUnavailable,
			Detail: fmt.Sprintf("The monitoring backend reported an internal error (HTTP %d).", apiErr.StatusCode),
			Err:    err,
		}
	default:
		return &monitoringdomain.QueryError{
			Kind:   monitoringdomain.QueryErrorRejected,
			Detail: apiErr.Error(),
			Err:    err,
		}
	}
}
