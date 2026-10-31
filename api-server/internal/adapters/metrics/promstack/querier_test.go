package promstack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
)

// newTestQuerier serves handler as the metrics store and returns a querier pointed at it.
func newTestQuerier(t *testing.T, handler http.HandlerFunc) *Querier {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, "store-token", 5*time.Second, false)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return NewQuerier(client)
}

// Range queries go out as a form-encoded POST, so a long server_id alternation never meets a
// URL-length limit, and carry the window in Unix seconds.
func TestQueryRange_PostsFormAndParsesMatrix(t *testing.T) {
	var gotMethod, gotQuery, gotStart, gotEnd, gotStep, gotAuth string
	querier := newTestQuerier(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotAuth = r.Method, r.Header.Get("Authorization")
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		gotQuery, gotStart, gotEnd, gotStep = r.PostForm.Get("query"), r.PostForm.Get("start"), r.PostForm.Get("end"), r.PostForm.Get("step")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[` +
			`{"metric":{"server_id":"srv-1"},"values":[[1760230800,"12.5"],[1760230830,"NaN"],[1760230860,"+Inf"],[1760230890,"13"]]}]}}`))
	})

	start := time.Unix(1760230800, 0)
	series, err := querier.QueryRange(context.Background(), `up{server_id=~"srv-1"}`, monitoringdomain.TimeRange{
		Start: start, End: start.Add(time.Hour), Step: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("QueryRange: %v", err)
	}

	if gotMethod != http.MethodPost || gotAuth != "Bearer store-token" {
		t.Errorf("request = %s with auth %q, want POST with the bearer token", gotMethod, gotAuth)
	}
	if gotQuery != `up{server_id=~"srv-1"}` || gotStart != "1760230800" || gotEnd != "1760234400" || gotStep != "30" {
		t.Errorf("form = query %q start %s end %s step %s", gotQuery, gotStart, gotEnd, gotStep)
	}
	if len(series) != 1 || series[0].ServerID() != "srv-1" {
		t.Fatalf("series = %+v, want one series for srv-1", series)
	}
	// NaN and +Inf are gaps, not values.
	points := series[0].Points
	if len(points) != 2 || points[0].Value != 12.5 || points[1].Value != 13 {
		t.Errorf("points = %+v, want the two finite values", points)
	}
}

// A 200 with status "error" is a rejected expression, not an outage.
func TestQueryRange_LogicalErrorIsRejected(t *testing.T) {
	querier := newTestQuerier(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"error","error":"parse error"}`))
	})

	_, err := querier.QueryRange(context.Background(), "bad(", monitoringdomain.TimeRange{
		Start: time.Unix(0, 0), End: time.Unix(3600, 0), Step: time.Minute,
	})
	var queryErr *monitoringdomain.QueryError
	if !errors.As(err, &queryErr) || queryErr.Kind != monitoringdomain.QueryErrorRejected {
		t.Errorf("QueryRange error = %v, want a rejected QueryError", err)
	}
}

// The instant read drops non-finite samples too, so a zero-sized filesystem cannot break the
// JSON response with a NaN.
func TestQuery_DropsNonFiniteSamples(t *testing.T) {
	querier := newTestQuerier(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[` +
			`{"metric":{"server_id":"srv-1"},"value":[1760230800,"NaN"]},` +
			`{"metric":{"server_id":"srv-2"},"value":[1760230800,"41.5"]}]}}`))
	})

	samples, err := querier.Query(context.Background(), "x")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(samples) != 1 || samples[0].ServerID() != "srv-2" {
		t.Errorf("samples = %+v, want only the finite srv-2 sample", samples)
	}
}
