package slurmapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// A failed slurmrestd read keeps its cause and names the versioned call and the endpoint it went
// to, so a wrong slurmApiVersion or a stopped slurmrestd is visible from the error alone; the
// endpoint never shows the credential or query a configured URL carried.
func TestReaderErrorsNameTheCallAndEndpoint(t *testing.T) {
	answering := func(status int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "slurmrestd says no", status)
		}))
	}
	closed := answering(http.StatusOK)
	refusedURL := closed.URL
	closed.Close()
	unauthorized, broken, missing := answering(http.StatusUnauthorized), answering(http.StatusBadGateway), answering(http.StatusNotFound)
	defer unauthorized.Close()
	defer broken.Close()
	defer missing.Close()

	cases := []struct {
		name      string
		endpoint  string
		kind      platformdomain.ReaderErrorKind
		transport platformdomain.TransportFailure
	}{
		{"slurmrestd stopped", refusedURL, platformdomain.ReaderErrorUnavailable, platformdomain.TransportRefused},
		{"token rejected", unauthorized.URL, platformdomain.ReaderErrorAuth, ""},
		{"internal error", broken.URL, platformdomain.ReaderErrorUnavailable, ""},
		{"version not served", missing.URL, platformdomain.ReaderErrorNotFound, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			configured := strings.Replace(tc.endpoint, "http://", "http://slurm:hunter2@", 1)
			reader, err := NewReader(platformdomain.Connection{Endpoint: configured, Token: "jwt-value", Timeout: time.Second})
			if err != nil {
				t.Fatalf("NewReader: %v", err)
			}
			_, err = reader.ListMembers(context.Background())
			var readerErr *platformdomain.ReaderError
			if !errors.As(err, &readerErr) {
				t.Fatalf("ListMembers error = %v, want a ReaderError", err)
			}
			if readerErr.Kind != tc.kind || readerErr.Transport != tc.transport {
				t.Errorf("kind, transport = %q, %q, want %q, %q", readerErr.Kind, readerErr.Transport, tc.kind, tc.transport)
			}
			if readerErr.Endpoint != tc.endpoint || readerErr.Operation != "GET /slurm/"+DefaultSlurmAPIVersion+"/nodes" {
				t.Errorf("endpoint, operation = %q, %q, want %q and the versioned nodes call", readerErr.Endpoint, readerErr.Operation, tc.endpoint)
			}
			if readerErr.Err == nil {
				t.Error("the ReaderError dropped its cause")
			}
			sentence := err.Error()
			if !strings.Contains(sentence, readerErr.Operation+" at "+tc.endpoint) {
				t.Errorf("Error() = %q, want it to name the call and endpoint", sentence)
			}
			for _, secret := range []string{"hunter2", "jwt-value"} {
				if strings.Contains(sentence, secret) || strings.Contains(readerErr.Detail, secret) {
					t.Errorf("Error() = %q or Detail %q shows %q", sentence, readerErr.Detail, secret)
				}
			}
		})
	}
}
