package promstack

import (
	"context"
	"errors"
	"fmt"
	"math"
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

// Query evaluates expr at the current instant via GET /api/v1/query. Instant expressions are
// bounded by the 200-server request limit, so they fit comfortably in a URL.
func (q *Querier) Query(ctx context.Context, expr string) ([]monitoringdomain.Sample, error) {
	query := url.Values{}
	query.Set("query", expr)

	var out queryResponse
	if err := q.client.get(ctx, "/api/v1/query", query, &out); err != nil {
		return nil, translateError(err)
	}

	if err := checkEnvelope(out.Status, out.Error); err != nil {
		return nil, err
	}

	samples := make([]monitoringdomain.Sample, 0, len(out.Data.Result))
	for _, result := range out.Data.Result {
		timestamp, value, ok := parsePair(result.Value)
		if !ok {
			// A sample that cannot be parsed is skipped rather than failing the whole
			// query: one malformed series should not hide the rest.
			continue
		}
		samples = append(samples, monitoringdomain.Sample{Labels: result.Metric, Value: value, Timestamp: timestamp})
	}
	return samples, nil
}

// rangeResponse is the Prometheus range-query ("matrix") envelope: every result carries its
// values as [unix_seconds, "sample_value"] pairs in ascending time order.
type rangeResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Values [][]any           `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

// QueryRange evaluates expr over the window via a form-encoded POST /api/v1/query_range, so a
// scope with hundreds of server_ids does not overflow a URL. The window is forwarded as given:
// bounding it is the application's job. Unparsable or non-finite points are dropped, leaving
// a gap rather than a fabricated value.
func (q *Querier) QueryRange(
	ctx context.Context,
	expr string,
	window monitoringdomain.TimeRange,
) ([]monitoringdomain.Series, error) {
	form := url.Values{}
	form.Set("query", expr)
	form.Set("start", strconv.FormatInt(window.Start.Unix(), 10))
	form.Set("end", strconv.FormatInt(window.End.Unix(), 10))
	form.Set("step", strconv.FormatInt(int64(window.Step/time.Second), 10))

	var out rangeResponse
	if err := q.client.postForm(ctx, "/api/v1/query_range", form, &out); err != nil {
		return nil, translateError(err)
	}
	if err := checkEnvelope(out.Status, out.Error); err != nil {
		return nil, err
	}

	series := make([]monitoringdomain.Series, 0, len(out.Data.Result))
	for _, result := range out.Data.Result {
		points := make([]monitoringdomain.Point, 0, len(result.Values))
		for _, pair := range result.Values {
			timestamp, value, ok := parsePair(pair)
			if !ok {
				continue
			}
			points = append(points, monitoringdomain.Point{Timestamp: timestamp, Value: value})
		}
		series = append(series, monitoringdomain.Series{Labels: result.Metric, Points: points})
	}
	return series, nil
}

// checkEnvelope reports a logical failure. Prometheus can answer HTTP 200 with
// status "error", so the envelope has to be checked as well as the status code.
func checkEnvelope(status, detail string) error {
	if status == "success" {
		return nil
	}
	if detail == "" {
		detail = "the metrics store rejected the query."
	}
	return &monitoringdomain.QueryError{
		Kind:   monitoringdomain.QueryErrorRejected,
		Detail: detail,
	}
}

// parsePair decodes one [unix_seconds, "sample_value"] pair. The value is a string because
// Prometheus preserves exact float formatting. NaN and ±Inf — what the store returns for, say,
// a division by a zero-sized filesystem — are reported as not ok: they carry no usable value
// and encoding/json cannot represent them.
func parsePair(pair []any) (time.Time, float64, bool) {
	if len(pair) != 2 {
		return time.Time{}, 0, false
	}
	seconds, ok := pair[0].(float64)
	if !ok {
		return time.Time{}, 0, false
	}
	raw, ok := pair[1].(string)
	if !ok {
		return time.Time{}, 0, false
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return time.Time{}, 0, false
	}
	return time.Unix(int64(seconds), 0).UTC(), value, true
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
			Detail: "The monitoring backend rejected the credential swallow is configured with.",
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
