package httpjson

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A client is created per read and never cached, so its transport must close connections after
// each response. Keeping them alive leaked connections into slurmrestd's small concurrent-connection
// cap (~124) until it refused new ones and membership sync stalled with "Could not reach the Slurm
// API"; this guards that regression.
func TestTransportDisablesKeepAlivesAndChoosesVerification(t *testing.T) {
	secure, err := Transport(false, "")
	if err != nil || !secure.DisableKeepAlives || secure.TLSClientConfig != nil {
		t.Errorf("verifying transport: err %v, keep-alives off %v, TLS config %v", err, secure.DisableKeepAlives, secure.TLSClientConfig)
	}
	insecure, err := Transport(true, "")
	if err != nil || !insecure.DisableKeepAlives || insecure.TLSClientConfig == nil || !insecure.TLSClientConfig.InsecureSkipVerify {
		t.Error("an insecure transport must skip TLS verification and still disable keep-alives")
	}
	if _, err := Transport(false, "not a certificate"); err == nil {
		t.Error("an invalid CA must be refused")
	}
}

// With the Platform's CA a client verifies the API's certificate even when the Integration also says
// insecureSkipVerify, and a server whose certificate that CA did not sign is refused.
func TestTransportTrustsOnlyTheGivenCA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	caPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))

	transport, err := Transport(true, caPEM)
	if err != nil {
		t.Fatalf("Transport: %v", err)
	}
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("a CA must turn verification on")
	}
	var out struct{ OK bool }
	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	if err := Do(&http.Client{Transport: transport}, req, &out); err != nil || !out.OK {
		t.Fatalf("Do with the server's CA: %v, %+v", err, out)
	}

	untrusted, err := Transport(false, selfSignedCA(t))
	if err != nil {
		t.Fatalf("Transport: %v", err)
	}
	req, _ = http.NewRequest(http.MethodGet, server.URL, nil)
	err = Do(&http.Client{Transport: untrusted}, req, &out)
	var transportErr *TransportError
	var unknown x509.UnknownAuthorityError
	if !errors.As(err, &transportErr) || !errors.As(err, &unknown) {
		t.Errorf("a CA that did not sign the server: err = %v, want a TransportError for an unknown authority", err)
	}
}

// selfSignedCA is the PEM of a new CA that signed nothing.
func selfSignedCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "unrelated CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// A non-2xx answer keeps its status and the start of its body, and names the address that
// answered without the userinfo and query the request carried.
func TestDoReportsStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(strings.Repeat("x", MaxErrorBodyBytes+10)))
	}))
	defer server.Close()
	target := strings.Replace(server.URL, "http://", "http://reader:hunter2@", 1) + "/api/v1/nodes?token=abc#frag"
	req, _ := http.NewRequest(http.MethodGet, target, nil)
	_, err := DoRaw(server.Client(), req, 1<<20)
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusForbidden || len(statusErr.Body) != MaxErrorBodyBytes {
		t.Fatalf("err = %v, want a 403 StatusError with a bounded body", err)
	}
	if want := server.URL + "/api/v1/nodes"; statusErr.URL != want {
		t.Errorf("StatusError.URL = %q, want %q", statusErr.URL, want)
	}
}

// Failure tells how a request without an answer failed, from the client's own error chain.
func TestTransportErrorFailure(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	refusedURL := closed.URL
	closed.Close()

	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer slow.Close()

	untrusted := httptest.NewTLSServer(http.NotFoundHandler())
	defer untrusted.Close()
	plain := httptest.NewServer(http.NotFoundHandler())
	defer plain.Close()

	live := []struct {
		name   string
		client *http.Client
		url    string
		want   TransportFailure
	}{
		{"nothing listens", &http.Client{Timeout: time.Second}, refusedURL, TransportRefused},
		{"no answer in time", &http.Client{Timeout: 50 * time.Millisecond}, slow.URL, TransportTimeout},
		{"untrusted certificate", &http.Client{Timeout: time.Second}, untrusted.URL, TransportTLS},
		{"https to a plain server", &http.Client{Timeout: time.Second}, strings.Replace(plain.URL, "http://", "https://", 1), TransportTLS},
	}
	for _, tc := range live {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, tc.url, nil)
			err := Do(tc.client, req, &struct{}{})
			var transportErr *TransportError
			if !errors.As(err, &transportErr) {
				t.Fatalf("Do(%s) error = %v, want a TransportError", tc.url, err)
			}
			if got := transportErr.Failure(); got != tc.want {
				t.Errorf("Failure() = %q for %v, want %q", got, err, tc.want)
			}
		})
	}

	// Causes a test cannot provoke reliably without the network are classified from their shape.
	wrapped := func(cause error) *TransportError {
		return &TransportError{Err: &url.Error{Op: "Get", URL: "https://api.example:6443/x", Err: cause}}
	}
	shaped := []struct {
		name string
		err  *TransportError
		want TransportFailure
	}{
		{"unknown host", wrapped(&net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "api.example", IsNotFound: true}}), TransportDNS},
		{"resolver timeout", wrapped(&net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "i/o timeout", Name: "api.example", IsTimeout: true}}), TransportDNS},
		{"certificate for another host", wrapped(&tls.CertificateVerificationError{Err: x509.HostnameError{Certificate: &x509.Certificate{}, Host: "api.example"}}), TransportTLS},
		{"context deadline", wrapped(context.DeadlineExceeded), TransportTimeout},
		{"connection reset", wrapped(&net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}), TransportOther},
	}
	for _, tc := range shaped {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Failure(); got != tc.want {
				t.Errorf("Failure() = %q for %v, want %q", got, tc.err, tc.want)
			}
		})
	}
}

// URL names where an unanswered request went, never the credentials or query it carried.
func TestTransportErrorURLDropsUserinfoAndQuery(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	base := closed.URL
	closed.Close()
	req, _ := http.NewRequest(http.MethodGet, strings.Replace(base, "http://", "http://reader:hunter2@", 1)+"/api/v1/nodes?token=abc", nil)
	err := Do(&http.Client{Timeout: time.Second}, req, &struct{}{})
	var transportErr *TransportError
	if !errors.As(err, &transportErr) {
		t.Fatalf("Do error = %v, want a TransportError", err)
	}
	if got, want := transportErr.URL(), base+"/api/v1/nodes"; got != want {
		t.Errorf("URL() = %q, want %q", got, want)
	}
	if got := (&TransportError{Err: errors.New("no url")}).URL(); got != "" {
		t.Errorf("URL() without a *url.Error = %q, want empty", got)
	}
}
