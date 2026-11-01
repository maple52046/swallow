// Package httpjson is the small JSON-over-HTTP client the platform modules share instead of a
// vendored client library: a per-client transport, base URL normalization, and request helpers whose
// failures tell whether the server was reached and what it answered. Each caller maps those
// failures to its own errors.
package httpjson

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// MaxErrorBodyBytes bounds how much of an error response is kept for the error message.
const MaxErrorBodyBytes = 512

// Transport builds the HTTP transport of one client. Keep-alives are disabled on purpose: platform
// clients are created per read and are not cached, so there is no connection reuse to protect,
// while a lingering keep-alive connection would accumulate on the server. slurmrestd in particular
// caps concurrent connections (around 124) and, once leaked idle connections fill that cap, refuses
// new ones. A per-client transport (not http.DefaultTransport) also avoids mutating shared global
// state for TLS.
//
// caPEM, when set, is the only CA the server's certificate is verified against. Without it,
// insecureSkipVerify turns verification off and otherwise the system roots verify it.
func Transport(insecureSkipVerify bool, caPEM string) (*http.Transport, error) {
	transport := &http.Transport{DisableKeepAlives: true}
	switch {
	case strings.TrimSpace(caPEM) != "":
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(caPEM)) {
			return nil, errors.New("the platform API CA certificate is not valid PEM")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	case insecureSkipVerify:
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return transport, nil
}

// NormalizeBaseURL trims rawURL and its trailing slashes and requires an http or https URL with a
// host.
func NormalizeBaseURL(rawURL string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if trimmed == "" {
		return "", errors.New("platform api url is empty")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid platform api url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("invalid platform api url: scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("invalid platform api url: missing host")
	}
	return trimmed, nil
}

// Do sends req and decodes a 2xx JSON body into out. A request that never got an answer is a
// *TransportError and a non-2xx answer a *StatusError.
func Do(client *http.Client, req *http.Request, out any) error {
	resp, err := send(client, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode platform api response: %w", err)
	}
	return nil
}

// DoRaw sends req and returns at most limit bytes of a 2xx body, failing like Do.
func DoRaw(client *http.Client, req *http.Request, limit int64) ([]byte, error) {
	resp, err := send(client, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func send(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, &TransportError{Err: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, MaxErrorBodyBytes))
		return nil, &StatusError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	return resp, nil
}

// StatusError is a non-2xx answer and the start of its body.
type StatusError struct {
	StatusCode int
	Body       string
}

// Error names the status and, when there is one, the body.
func (e *StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("platform api returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("platform api returned HTTP %d: %s", e.StatusCode, e.Body)
}

// TransportError is a request that got no answer: the server could not be reached, refused the
// connection, failed TLS verification, or timed out.
type TransportError struct {
	Err error
}

// Error is the underlying error's message.
func (e *TransportError) Error() string { return e.Err.Error() }

// Unwrap returns the underlying error.
func (e *TransportError) Unwrap() error { return e.Err }
