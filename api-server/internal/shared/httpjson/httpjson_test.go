package httpjson

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
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
// insecureSkipVerify, and a server whose certificate that CA did not sign is refused (DF-01).
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

// A non-2xx answer keeps its status and the start of its body.
func TestDoReportsStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(strings.Repeat("x", MaxErrorBodyBytes+10)))
	}))
	defer server.Close()
	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	_, err := DoRaw(server.Client(), req, 1<<20)
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusForbidden || len(statusErr.Body) != MaxErrorBodyBytes {
		t.Errorf("err = %v, want a 403 StatusError with a bounded body", err)
	}
}
