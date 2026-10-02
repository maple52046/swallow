// Package client is the swallow operator CLI's HTTP adapter for the api-server
// contract. It is the single place that knows how to reach the api-server: base
// path, bearer authentication, the shared JSON error envelope, and the
// Server-Sent Events framing. Command code depends on this package and never
// builds requests or parses the error envelope itself.
//
// Component boundary: the CLI is a conformist HTTP consumer of the api-server
// published contract (see the repository architecture spec). This package
// encodes only the contract's transport rules — paths, methods, headers, the
// error envelope from conventions.md — and must not import api-server internals
// or replicate its domain models. Request and response payloads pass through as
// opaque JSON so a contract change does not silently drift a hand-copied struct.
package client

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

// basePath is the contract base path from conventions.md. Callers pass a path
// relative to it (for example "servers/") and the client joins it once here so
// no command hard-codes the version prefix.
const basePath = "/api/v1/"

// AuthMode selects which credential a request presents. Most endpoints use
// AuthBearer (the API Key when configured, otherwise the Session access token);
// discovery endpoints use AuthMachine (a machine token, falling back to the
// bearer credential when none is configured); login and refresh use AuthNone
// because they issue the credential.
type AuthMode int

const (
	// AuthBearer presents the API Key or the Session access token as a Bearer
	// credential, refreshing the Session when needed.
	AuthBearer AuthMode = iota
	// AuthMachine presents the machine token when configured, otherwise the
	// interactive token, matching the machine-auth endpoints that accept either.
	AuthMachine
	// AuthNone sends no Authorization header.
	AuthNone
)

// Client performs authenticated HTTP calls against one api-server endpoint. It
// is meant for sequential CLI use: a Session refresh replaces its tokens in
// place, so it must not be shared by concurrent goroutines. A zero Client is not
// usable — construct it with New.
type Client struct {
	baseURL        *url.URL
	token          string
	tokenExpiresAt time.Time
	refreshToken   string
	apiKey         string
	machineToken   string
	onRefreshed    func(SessionTokens) error
	httpClient     *http.Client
}

// Options configures a Client. Endpoint is required and is the api-server base
// URL without the /api/v1 suffix; the client appends the contract base path.
// Timeout bounds each request; a non-positive value disables the client-side
// deadline so long-running reads such as an image upload are not truncated.
//
// Credentials: APIKey, when set, is the bearer credential and the Session fields
// are ignored. Otherwise Token is sent, and RefreshToken (if any) renews it:
// before a request when TokenExpiresAt is within a minute, and once after a 401.
// OnSessionRefreshed receives every renewal so the caller can persist it; an
// error from it fails the request, because a rotated refresh token that is not
// saved is lost.
type Options struct {
	Endpoint           string
	Token              string
	TokenExpiresAt     time.Time
	RefreshToken       string
	APIKey             string
	MachineToken       string
	OnSessionRefreshed func(SessionTokens) error
	InsecureTLS        bool
	Timeout            time.Duration
}

// New validates the options and constructs a Client. It returns an error when
// Endpoint is empty or unparseable so a command fails fast with a clear message
// instead of emitting requests to an unusable URL.
func New(opts Options) (*Client, error) {
	if strings.TrimSpace(opts.Endpoint) == "" {
		return nil, fmt.Errorf("no endpoint configured: set one with --endpoint, SWALLOW_ENDPOINT, or `swallow login`")
	}
	u, err := url.Parse(strings.TrimSpace(opts.Endpoint))
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint %q: %w", opts.Endpoint, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid endpoint %q: expected an absolute URL like https://swallow.example", opts.Endpoint)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if opts.InsecureTLS {
		// Opt-in only: this disables certificate verification for lab endpoints
		// with self-signed certificates and must never be the default.
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // operator opt-in via --insecure
	}

	c := &Client{
		baseURL:      u,
		apiKey:       opts.APIKey,
		machineToken: opts.MachineToken,
		httpClient:   &http.Client{Timeout: opts.Timeout, Transport: transport},
	}
	if opts.APIKey == "" {
		c.token = opts.Token
		c.tokenExpiresAt = opts.TokenExpiresAt
		c.refreshToken = opts.RefreshToken
		c.onRefreshed = opts.OnSessionRefreshed
	}
	return c, nil
}

// Request describes one call. Path is relative to the contract base path. Query
// is optional. Body, when non-nil, is JSON-encoded and sent with a JSON
// Content-Type unless RawBody is set. RawBody and ContentType let a caller send
// a non-JSON payload (for example a multipart upload assembled by the caller).
// Accept overrides the default JSON Accept header for text endpoints such as
// Task logs.
type Request struct {
	Method      string
	Path        string
	Query       url.Values
	Body        any
	RawBody     io.Reader
	ContentType string
	Accept      string
	Auth        AuthMode
}

// resolve builds the absolute URL for a request path plus query, joining the
// path onto the contract base path exactly once.
func (c *Client) resolve(path string, query url.Values) string {
	rel := &url.URL{Path: basePath + strings.TrimPrefix(path, "/")}
	abs := c.baseURL.ResolveReference(rel)
	if len(query) > 0 {
		abs.RawQuery = query.Encode()
	}
	return abs.String()
}

// newHTTPRequest assembles the *http.Request including body encoding and the
// Authorization header selected by Auth. It centralizes header and credential
// handling so no command sets Authorization directly.
func (c *Client) newHTTPRequest(ctx context.Context, r Request) (*http.Request, error) {
	var bodyReader io.Reader
	contentType := r.ContentType
	switch {
	case r.RawBody != nil:
		bodyReader = r.RawBody
	case r.Body != nil:
		encoded, err := json.Marshal(r.Body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		bodyReader = bytes.NewReader(encoded)
		if contentType == "" {
			contentType = "application/json"
		}
	}

	req, err := http.NewRequestWithContext(ctx, r.Method, c.resolve(r.Path, r.Query), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	accept := r.Accept
	if accept == "" {
		accept = "application/json"
	}
	req.Header.Set("Accept", accept)

	c.applyAuth(req, r.Auth)
	return req, nil
}

// applyAuth sets the Authorization header per the request's AuthMode. For
// AuthMachine it prefers the machine token and falls back to the bearer
// credential, matching endpoints that accept either.
func (c *Client) applyAuth(req *http.Request, mode AuthMode) {
	switch mode {
	case AuthNone:
		return
	case AuthMachine:
		if c.machineToken != "" {
			req.Header.Set("Authorization", "Bearer "+c.machineToken)
			return
		}
	}
	if credential := c.bearer(); credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
}

// bearer is the credential AuthBearer presents: the API Key when configured,
// otherwise the Session access token.
func (c *Client) bearer() string {
	if c.apiKey != "" {
		return c.apiKey
	}
	return c.token
}

// Do performs the request and returns the raw response for the caller to
// consume. On a non-2xx status it closes the body and returns an *APIError, so a
// successful return always carries a live body the caller must close. Prefer the
// JSON, Text, Discard, and Decode helpers over calling Do directly.
//
// For a Session credential Do refreshes the access token first when it expires
// within a minute, and on a 401 refreshes once and retries the request once —
// unless the body is a RawBody stream, which cannot be replayed. A 401 that
// cannot be recovered carries a hint telling the operator what to do.
func (c *Client) Do(ctx context.Context, r Request) (*http.Response, error) {
	if c.sessionRenewable(r.Auth) && c.expiresSoon() {
		if err := c.refresh(ctx); err != nil {
			return nil, err
		}
	}
	resp, err := c.send(ctx, r)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized || r.Auth == AuthNone {
		return resp, err
	}
	if c.sessionRenewable(r.Auth) && r.RawBody == nil {
		if rerr := c.refresh(ctx); rerr != nil {
			return nil, rerr
		}
		return c.send(ctx, r)
	}
	apiErr.Hint = c.unauthorizedHint()
	return nil, apiErr
}

// send performs exactly one HTTP exchange.
func (c *Client) send(ctx context.Context, r Request) (*http.Response, error) {
	req, err := c.newHTTPRequest(ctx, r)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s %s: %w", r.Method, r.Path, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	defer resp.Body.Close()
	return nil, newAPIError(resp)
}

// JSON performs the request and decodes a JSON success body into out. out may be
// nil to ignore the body (for endpoints whose success payload is uninteresting).
// A 204 or empty body with a non-nil out leaves out at its zero value.
func (c *Client) JSON(ctx context.Context, r Request, out any) error {
	resp, err := c.Do(ctx, r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeBody(resp, out)
}

// decodeBody decodes a JSON success body into out without closing it; nil out,
// a 204, or an empty body leave out untouched.
func decodeBody(resp *http.Response, out any) error {
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response body: %w", err)
	}
	return nil
}

// Text performs the request and returns the response body verbatim. It is used
// by the text/plain contract endpoints (Task logs and stderr) where the payload
// is not JSON and must be printed as-is.
func (c *Client) Text(ctx context.Context, r Request) (string, error) {
	if r.Accept == "" {
		r.Accept = "text/plain"
	}
	resp, err := c.Do(ctx, r)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}
	return string(data), nil
}

// Discard performs the request and drops the success body. It is used for
// actions whose success is conveyed by status alone (for example a 204 delete).
func (c *Client) Discard(ctx context.Context, r Request) error {
	return c.JSON(ctx, r, nil)
}

// newAPIError converts a non-2xx response into an *APIError. It decodes the
// shared envelope when present; when the body is not the expected envelope (an
// unexpected proxy error page, say) it preserves the status and raw body so the
// operator still sees something actionable rather than an empty error.
func newAPIError(resp *http.Response) *APIError {
	apiErr := &APIError{Status: resp.StatusCode, Code: "http_error"}
	data, _ := io.ReadAll(resp.Body)
	var env errorEnvelope
	if len(bytes.TrimSpace(data)) > 0 && json.Unmarshal(data, &env) == nil && env.Error.Code != "" {
		apiErr.Code = env.Error.Code
		apiErr.Message = env.Error.Message
		apiErr.RequestID = env.Error.RequestID
		return apiErr
	}
	msg := strings.TrimSpace(string(data))
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	apiErr.Message = msg
	return apiErr
}
