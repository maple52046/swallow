package kubeapi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	"github.com/maple52046/swallow/internal/shared/httpjson"
)

// unanswered is the TransportError the HTTP client returns for a request to the cluster API that
// failed with cause.
func unanswered(cause error) error {
	return &httpjson.TransportError{Err: &url.Error{Op: "Get", URL: "https://10.0.0.5:6443/api/v1/nodes", Err: cause}}
}

// Each failure keeps its cause and gains the kind, the transport failure, and the call and
// endpoint it happened on, while Detail stays the user's sentence without the address.
func TestTranslateErrorClassifiesTheFailure(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	cases := []struct {
		name      string
		err       error
		kind      platformdomain.ReaderErrorKind
		transport platformdomain.TransportFailure
	}{
		{"refused", unanswered(refused), platformdomain.ReaderErrorUnavailable, platformdomain.TransportRefused},
		{"timeout", unanswered(context.DeadlineExceeded), platformdomain.ReaderErrorUnavailable, platformdomain.TransportTimeout},
		{"dns", unanswered(&net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "api.lab", IsNotFound: true}}),
			platformdomain.ReaderErrorUnavailable, platformdomain.TransportDNS},
		{"tls", unanswered(&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}),
			platformdomain.ReaderErrorUnavailable, platformdomain.TransportTLS},
		{"401", &httpjson.StatusError{StatusCode: http.StatusUnauthorized, Body: "Unauthorized"}, platformdomain.ReaderErrorAuth, ""},
		{"403", &httpjson.StatusError{StatusCode: http.StatusForbidden, Body: "forbidden"}, platformdomain.ReaderErrorAuth, ""},
		{"404", &httpjson.StatusError{StatusCode: http.StatusNotFound}, platformdomain.ReaderErrorNotFound, ""},
		{"409", &httpjson.StatusError{StatusCode: http.StatusConflict}, platformdomain.ReaderErrorConflict, ""},
		{"503", &httpjson.StatusError{StatusCode: http.StatusServiceUnavailable, Body: "etcdserver: request timed out"},
			platformdomain.ReaderErrorUnavailable, ""},
		{"422", &httpjson.StatusError{StatusCode: http.StatusUnprocessableEntity}, platformdomain.ReaderErrorRejected, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := translateError(tc.err, "https://10.0.0.5:6443", "list nodes")
			var readerErr *platformdomain.ReaderError
			if !errors.As(err, &readerErr) {
				t.Fatalf("translateError(%v) = %v, want a ReaderError", tc.err, err)
			}
			if readerErr.Kind != tc.kind || readerErr.Transport != tc.transport {
				t.Errorf("kind, transport = %q, %q, want %q, %q", readerErr.Kind, readerErr.Transport, tc.kind, tc.transport)
			}
			if readerErr.Endpoint != "https://10.0.0.5:6443" || readerErr.Operation != "list nodes" {
				t.Errorf("endpoint, operation = %q, %q, want the call's", readerErr.Endpoint, readerErr.Operation)
			}
			if !errors.Is(err, tc.err) {
				t.Errorf("translateError(%v) dropped the cause", tc.err)
			}
			if strings.Contains(readerErr.Detail, "10.0.0.5") {
				t.Errorf("Detail %q names the endpoint; delivery returns it verbatim", readerErr.Detail)
			}
		})
	}
	if plain := errors.New("decode kubernetes response"); translateError(plain, "https://10.0.0.5:6443", "list nodes") != plain {
		t.Error("an error that is neither a transport nor a status failure must pass through")
	}
}

// The sentence membership sync records names the call, the endpoint, how it failed, and the
// cause, so an administrator can act on it without the worker's log.
func TestReaderErrorSentenceNamesTheEndpointAndCause(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	endpoint := closed.URL
	closed.Close()

	reader, err := NewReader(platformdomain.Connection{Endpoint: endpoint, Token: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	_, err = reader.ListMembers(context.Background())
	var readerErr *platformdomain.ReaderError
	if !errors.As(err, &readerErr) || readerErr.Transport != platformdomain.TransportRefused {
		t.Fatalf("ListMembers error = %v, want a refused ReaderError", err)
	}
	sentence := err.Error()
	t.Logf("Error() = %s", sentence)
	for _, want := range []string{
		"Could not reach the Kubernetes API: the connection was refused",
		"list nodes at " + endpoint,
		"connection refused).",
	} {
		if !strings.Contains(sentence, want) {
			t.Errorf("Error() = %q, want it to contain %q", sentence, want)
		}
	}
	if strings.Contains(sentence, "/api/v1/nodes") {
		t.Errorf("Error() = %q repeats the request URL; the operation already names the call", sentence)
	}
}

// A credential the API answered with a refusal for is reported as such, and neither userinfo nor
// a token in the configured endpoint reaches the error's text.
func TestReaderErrorNeverShowsCredentials(t *testing.T) {
	var gotToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("Authorization")
		http.Error(w, `{"kind":"Status","message":"Unauthorized","code":401}`, http.StatusUnauthorized)
	}))
	defer server.Close()
	endpoint := strings.Replace(server.URL, "http://", "http://admin:hunter2@", 1) + "?token=s3cr3t"

	reader, err := NewReader(platformdomain.Connection{Endpoint: endpoint, Token: "bearer-value", Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	_, err = reader.ListMembers(context.Background())
	var readerErr *platformdomain.ReaderError
	if !errors.As(err, &readerErr) || readerErr.Kind != platformdomain.ReaderErrorAuth {
		t.Fatalf("ListMembers error = %v (sent %q), want an auth ReaderError", err, gotToken)
	}
	t.Logf("Error() = %s", err)
	for _, text := range []string{readerErr.Endpoint, err.Error()} {
		for _, secret := range []string{"hunter2", "s3cr3t", "bearer-value"} {
			if strings.Contains(text, secret) {
				t.Errorf("%q shows the secret %q", text, secret)
			}
		}
	}
	if !strings.Contains(err.Error(), "HTTP 401") {
		t.Errorf("Error() = %q, want the API's answer as the cause", err)
	}
}
