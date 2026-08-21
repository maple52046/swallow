// Package promstack adapts a Prometheus-compatible query API and Alertmanager.
//
// "Prometheus-compatible" covers Prometheus itself, Thanos, Mimir, and VictoriaMetrics:
// all four answer the same query endpoint, which is why the choice of central store can
// be deferred without changing anything here.
package promstack

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxErrorBodyBytes = 512

// Client is an authenticated transport for a Prometheus-compatible API or Alertmanager.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewClient(rawURL, token string, timeout time.Duration, insecureSkipVerify bool) (*Client, error) {
	baseURL, err := normalizeBaseURL(rawURL)
	if err != nil {
		return nil, err
	}

	transport := http.DefaultTransport
	if insecureSkipVerify {
		transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}

	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}, nil
}

func normalizeBaseURL(rawURL string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if trimmed == "" {
		return "", errors.New("url is empty")
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("invalid url: scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("invalid url: missing host")
	}
	return trimmed, nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	return c.do(req, out)
}

func (c *Client) postJSON(ctx context.Context, path string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	// A central store may sit behind an authenticating proxy, or none at all.
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &transportError{err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &apiError{StatusCode: resp.StatusCode, Body: readErrorBody(resp.Body)}
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func readErrorBody(r io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(r, maxErrorBodyBytes))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

type apiError struct {
	StatusCode int
	Body       string
}

func (e *apiError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("monitoring backend returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("monitoring backend returned HTTP %d: %s", e.StatusCode, e.Body)
}

type transportError struct {
	err error
}

func (e *transportError) Error() string { return e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }
