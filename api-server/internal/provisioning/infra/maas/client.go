package maas

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// maxErrorBodyBytes caps how much of a MAAS error body is retained. MAAS
// validation errors are short; anything longer is likely an HTML error page.
const maxErrorBodyBytes = 512

// Client is a thin authenticated transport for the MAAS 2.0 API. It knows how to
// sign and encode requests but nothing about the provisioning domain.
type Client struct {
	apiRoot    string
	key        APIKey
	httpClient *http.Client
}

func NewClient(rawURL, rawKey string, timeout time.Duration, insecureSkipVerify bool) (*Client, error) {
	key, err := ParseAPIKey(rawKey)
	if err != nil {
		return nil, err
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
		key:     key,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}, nil
}

// normalizeAPIRoot turns the URL an operator copies out of MAAS into the API root
// to build request paths from.
//
// It accepts the address with or without a trailing slash and with or without the
// "/api/2.0" suffix. A URL with no path at all gets the default "/MAAS" prefix,
// because MAAS is served from there and a bare host can only be an omission.
func normalizeAPIRoot(rawURL string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if trimmed == "" {
		return "", errors.New("MAAS url is empty")
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid MAAS url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("invalid MAAS url: scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("invalid MAAS url: missing host")
	}

	path := strings.TrimRight(u.Path, "/")
	switch {
	case strings.HasSuffix(path, "/api/2.0"):
		// Already an API root.
	case path == "":
		path = "/MAAS/api/2.0"
	default:
		path += "/api/2.0"
	}

	u.Path = path
	return u.String(), nil
}

// get issues an authenticated GET and decodes a JSON body into out.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	req, err := c.newRequest(ctx, http.MethodGet, path, query, nil, "")
	if err != nil {
		return err
	}
	return c.do(req, out)
}

// postOperation invokes a MAAS named operation, e.g. op=deploy.
//
// The MAAS 2.0 API does not accept JSON request bodies: every parameter must be a
// separate multipart/form-data part, and sending JSON yields an opaque HTTP 500.
func (c *Client) postOperation(ctx context.Context, path, operation string, fields map[string]string, out any) error {
	query := url.Values{}
	query.Set("op", operation)

	body, contentType, err := multipartBody(fields)
	if err != nil {
		return err
	}

	req, err := c.newRequest(ctx, http.MethodPost, path, query, body, contentType)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

// getOperation invokes a read-only MAAS named operation, e.g. op=query_power_state.
//
// MAAS exposes these as GET with an op query parameter; using POST for a read would be
// refused. No body is sent because a read takes no parameters swallow supplies.
func (c *Client) getOperation(ctx context.Context, path, operation string, out any) error {
	query := url.Values{}
	query.Set("op", operation)
	return c.get(ctx, path, query, out)
}

func (c *Client) newRequest(
	ctx context.Context,
	method, path string,
	query url.Values,
	body io.Reader,
	contentType string,
) (*http.Request, error) {
	endpoint := c.apiRoot + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("build maas request: %w", err)
	}

	nonce, err := newNonce()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.key.authorizationHeader(nonce, time.Now().Unix()))
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	return req, nil
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Wrapped without the URL: it carries no credentials, but the caller
		// turns this into a client-visible message and the address is noise there.
		return &transportError{err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &apiError{
			StatusCode: resp.StatusCode,
			Body:       readErrorBody(resp.Body),
		}
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode maas response: %w", err)
	}
	return nil
}

// multipartBody encodes fields as multipart/form-data, skipping empty values so
// that an unset optional parameter is absent rather than sent as "".
func multipartBody(fields map[string]string) (io.Reader, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	names := make([]string, 0, len(fields))
	for name, value := range fields {
		if value != "" {
			names = append(names, name)
		}
	}
	// Sorted so the encoded body is deterministic, which is what lets tests
	// assert on it.
	sort.Strings(names)

	for _, name := range names {
		if err := writer.WriteField(name, fields[name]); err != nil {
			return nil, "", fmt.Errorf("encode maas request field %q: %w", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("encode maas request: %w", err)
	}

	return &buf, writer.FormDataContentType(), nil
}

func readErrorBody(r io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(r, maxErrorBodyBytes))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// apiError is a non-2xx response from MAAS. The body is retained because MAAS
// puts actionable validation messages there ("this node cannot be deployed
// because..."); request headers are never included.
type apiError struct {
	StatusCode int
	Body       string
}

func (e *apiError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("MAAS returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("MAAS returned HTTP %d: %s", e.StatusCode, e.Body)
}

// transportError is a failure to reach MAAS at all: DNS, TCP, TLS, or timeout.
type transportError struct {
	err error
}

func (e *transportError) Error() string { return e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }
