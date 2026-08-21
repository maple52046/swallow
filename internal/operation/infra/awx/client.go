// Package awx adapts Ansible AWX to the automation controller port.
package awx

import (
	"bytes"
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

// maxErrorBodyBytes caps how much of an AWX error body is retained. AWX validation
// errors are short; anything longer is likely an HTML error page.
const maxErrorBodyBytes = 512

// Client is an authenticated transport for the AWX v2 API.
type Client struct {
	apiRoot    string
	token      string
	httpClient *http.Client
}

func NewClient(rawURL, token string, timeout time.Duration, insecureSkipVerify bool) (*Client, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("AWX token is empty")
	}

	apiRoot, err := normalizeAPIRoot(rawURL)
	if err != nil {
		return nil, err
	}

	transport := http.DefaultTransport
	if insecureSkipVerify {
		transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}

	return &Client{
		apiRoot: apiRoot,
		token:   token,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}, nil
}

// normalizeAPIRoot accepts the AWX address an operator copies from a browser, with or
// without a trailing slash and with or without the /api/v2 suffix.
func normalizeAPIRoot(rawURL string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if trimmed == "" {
		return "", errors.New("AWX url is empty")
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid AWX url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("invalid AWX url: scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("invalid AWX url: missing host")
	}

	path := strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(path, "/api/v2") {
		path += "/api/v2"
	}
	u.Path = path

	return u.String(), nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	endpoint := c.apiRoot + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build awx request: %w", err)
	}
	return c.do(req, out)
}

func (c *Client) postJSON(ctx context.Context, path string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode awx request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiRoot+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build awx request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

// getText fetches a plain-text body, which is how job output is served.
func (c *Client) getText(ctx context.Context, path string, query url.Values) (string, error) {
	endpoint := c.apiRoot + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("build awx request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "text/plain")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", &transportError{err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", &apiError{StatusCode: resp.StatusCode, Body: readErrorBody(resp.Body)}
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read awx job output: %w", err)
	}
	return string(raw), nil
}

func (c *Client) do(req *http.Request, out any) error {
	req.Header.Set("Authorization", "Bearer "+c.token)
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
		return fmt.Errorf("decode awx response: %w", err)
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

// apiError is a non-2xx response from AWX. The body is retained because AWX puts
// actionable validation messages there; request headers never are.
type apiError struct {
	StatusCode int
	Body       string
}

func (e *apiError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("AWX returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("AWX returned HTTP %d: %s", e.StatusCode, e.Body)
}

// transportError is a failure to reach AWX at all: DNS, TCP, TLS, or timeout.
type transportError struct {
	err error
}

func (e *transportError) Error() string { return e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }
