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

// regionURL is the MAAS region URL a host reaches, i.e. the API root without its "/api/2.0"
// suffix (for example "http://10.0.0.5:5240/MAAS"); MAAS serves maas-run-scripts beneath it.
func (c *Client) regionURL() string {
	return strings.TrimSuffix(c.apiRoot, "/api/2.0")
}

// get issues an authenticated GET and decodes a JSON body into out.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	req, err := c.newRequest(ctx, http.MethodGet, path, query, nil, "")
	if err != nil {
		return err
	}
	return c.do(req, out)
}

// delete issues an authenticated DELETE with no provider-specific override parameters.
// MAAS deletion safeguards therefore remain authoritative; callers cannot accidentally
// force-delete hosted virtual machines through this transport.
func (c *Client) delete(ctx context.Context, path string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, path, nil, nil, "")
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

// postOperation invokes a MAAS named operation, e.g. op=deploy.
//
// The MAAS 2.0 API does not accept JSON request bodies: every parameter must be a
// separate multipart/form-data part, and sending JSON yields an opaque HTTP 500.
// Parameterless operations follow the official MAAS CLI shape and send no body
// or Content-Type header.
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

// postOperationValues invokes a MAAS named operation whose parameters may repeat under one name,
// such as tag update_nodes with several add / remove system ids. Like postOperation it puts op= in
// the query and the parameters in a multipart body, but it takes url.Values so a repeated field is
// sent as several parts rather than one — the single-value multipartBody cannot express that shape.
func (c *Client) postOperationValues(ctx context.Context, path, operation string, values url.Values, out any) error {
	query := url.Values{}
	query.Set("op", operation)

	body, contentType, err := multipartValuesBody(values)
	if err != nil {
		return err
	}

	req, err := c.newRequest(ctx, http.MethodPost, path, query, body, contentType)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

// postMultipart issues an authenticated POST whose parameters are multipart/form-data fields,
// used for MAAS collection creates that are not named operations (no op= query), such as
// reserving a boot resource before its content is uploaded. Empty values are dropped by
// multipartBody so an unset optional parameter is absent rather than sent as "".
func (c *Client) postMultipart(ctx context.Context, path string, fields map[string]string, out any) error {
	body, contentType, err := multipartBody(fields)
	if err != nil {
		return err
	}

	req, err := c.newRequest(ctx, http.MethodPost, path, nil, body, contentType)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

// putMultipart issues an authenticated PUT whose parameters are multipart/form-data fields,
// used for MAAS resource updates such as renaming a zone or setting a machine's zone/pool. Like
// the MAAS CLI's own "update" verb, the write is a PUT to the resource path with form fields;
// MAAS rejects a JSON body the same way it does for named operations. Empty values are dropped by
// multipartBody so an unset optional parameter is absent rather than sent as "".
func (c *Client) putMultipart(ctx context.Context, path string, fields map[string]string, out any) error {
	body, contentType, err := multipartBody(fields)
	if err != nil {
		return err
	}

	req, err := c.newRequest(ctx, http.MethodPut, path, nil, body, contentType)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

// putMultipartExact is putMultipart for updates where an empty value is meaningful: every field is
// sent, including empty ones. MAAS fills each power parameter the request omits from the machine's
// stored parameters, so clearing one (a password when the driver changes) needs an explicit empty
// part, which putMultipart would drop.
func (c *Client) putMultipartExact(ctx context.Context, path string, fields map[string]string, out any) error {
	body, contentType, err := multipartExactBody(fields)
	if err != nil {
		return err
	}

	req, err := c.newRequest(ctx, http.MethodPut, path, nil, body, contentType)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

// putUpload streams one raw chunk to a MAAS boot-resource upload target.
//
// rawURL is the upload_uri MAAS returns for an incomplete boot-resource file: a host-absolute
// path resolved against the configured API root so the request keeps the same scheme, host, and
// OAuth PLAINTEXT authentication (which signs neither the URL nor the body). Each call sends one
// chunk with an explicit content length; MAAS accumulates chunks until it has received the
// declared size, then verifies the sha256, so a corrupt or short upload surfaces as a provider
// error here or on the following read. The body is not retried: the caller owns a
// forward-only stream.
func (c *Client) putUpload(ctx context.Context, rawURL string, body io.Reader, contentLength int64) error {
	endpoint, err := c.resolveURL(rawURL)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, body)
	if err != nil {
		return fmt.Errorf("build maas upload request: %w", err)
	}
	req.ContentLength = contentLength

	nonce, err := newNonce()
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.key.authorizationHeader(nonce, time.Now().Unix()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/octet-stream")
	return c.do(req, nil)
}

// resolveURL resolves a MAAS-returned reference (typically a host-absolute upload path) against
// the configured API root, so a path such as "/MAAS/api/2.0/boot-resources/1/upload/2/" becomes a
// full URL on the same host without assuming the API root's own path prefix.
func (c *Client) resolveURL(rawURL string) (string, error) {
	base, err := url.Parse(c.apiRoot)
	if err != nil {
		return "", fmt.Errorf("parse maas api root: %w", err)
	}
	ref, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse maas upload uri: %w", err)
	}
	return base.ResolveReference(ref).String(), nil
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
// that an unset optional parameter is absent rather than sent as "". It returns
// a nil body when no values remain because MAAS rejects empty multipart bodies
// for parameterless operations such as release.
func multipartBody(fields map[string]string) (io.Reader, string, error) {
	names := make([]string, 0, len(fields))
	for name, value := range fields {
		if value != "" {
			names = append(names, name)
		}
	}
	// Sorted so the encoded body is deterministic, which is what lets tests
	// assert on it.
	sort.Strings(names)
	if len(names) == 0 {
		return nil, "", nil
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

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

// multipartExactBody encodes every field as multipart/form-data, empty values included, sorted so
// the body is deterministic for tests. Callers pass at least one field; MAAS rejects an empty
// multipart body.
func multipartExactBody(fields map[string]string) (io.Reader, string, error) {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
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

// multipartValuesBody encodes url.Values as multipart/form-data, emitting one part per value so a
// repeated field is sent as several parts under the same name. This is the shape MAAS's batch
// operations require — tag update_nodes carries the machine system ids as repeated add / remove
// fields — which the one-value-per-key multipartBody cannot produce. Empty values are skipped and
// keys are sorted so the encoded body is deterministic, which is what lets tests assert on it. A
// nil body is returned when no value remains, matching multipartBody.
func multipartValuesBody(values url.Values) (io.Reader, string, error) {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	wrote := false
	for _, name := range names {
		for _, value := range values[name] {
			if value == "" {
				continue
			}
			if err := writer.WriteField(name, value); err != nil {
				return nil, "", fmt.Errorf("encode maas request field %q: %w", name, err)
			}
			wrote = true
		}
	}
	if !wrote {
		return nil, "", nil
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
