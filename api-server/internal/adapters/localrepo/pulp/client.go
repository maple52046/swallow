package pulp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	"github.com/maple52046/swallow/internal/shared/redact"
)

// apiPath is where pulpcore serves its REST API beneath the endpoint. The hrefs Pulp returns are
// host-absolute paths that start with it, so they are joined to the endpoint's origin as they are.
const apiPath = "/pulp/api/v3"

// adminUser is the Pulp user swallow authenticates as. The Integration's credential is that user's
// password; the installation creates the user, so the name is not configurable.
const adminUser = "admin"

// maxErrorBodyBytes caps how much of a Pulp error answer is kept. Validation errors are short JSON
// objects; anything longer is an HTML error page whose start is enough.
const maxErrorBodyBytes = 1024

// correlationHeader is the request header Pulp records as a task's logging_cid. Pulp keeps the
// value only when it parses as a UUID, which is why correlationID builds one.
const correlationHeader = "Correlation-ID"

// client is an authenticated JSON transport for Pulp's REST API. It knows how to address hrefs,
// authenticate, and turn failures into provider errors, and nothing about what the objects mean.
// It is safe for concurrent use: it holds no mutable state.
type client struct {
	// base is the endpoint without a trailing slash or the API path, so base+href is a full URL.
	base     string
	password string
	http     *http.Client
}

// newClient validates endpoint, the Integration's endpoint, and returns a client for it. endpoint
// may be the Pulp origin ("http://pulp-web:8080") or the API root a user copies from Pulp's docs
// ("http://pulp-web:8080/pulp/api/v3/"); a path prefix of a proxy in front of Pulp is kept.
func newClient(endpoint, password string, httpClient *http.Client) (*client, error) {
	base, err := normalizeOrigin(endpoint, "endpoint")
	if err != nil {
		return nil, err
	}
	return &client{base: strings.TrimSuffix(base, apiPath), password: password, http: httpClient}, nil
}

// normalizeOrigin trims raw and its trailing slashes and requires an http or https URL with a
// host, without userinfo (a credential in a URL would be stored and shown as a setting).
func normalizeOrigin(raw, what string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return "", fmt.Errorf("the Pulp %s is empty", what)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("the Pulp %s is not a URL: %w", what, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("the Pulp %s must be an http or https URL, got scheme %q", what, parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("the Pulp %s has no host", what)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("the Pulp %s must not carry a user or password", what)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("the Pulp %s must not carry a query or fragment", what)
	}
	return trimmed, nil
}

// api returns the href of a collection beneath the API root, for example "/remotes/deb/apt/".
func api(collection string) string {
	return apiPath + collection
}

// get reads href (a collection with query, or an object) and decodes the JSON answer into out.
func (c *client) get(ctx context.Context, href string, query url.Values, out any) error {
	_, err := c.do(ctx, http.MethodGet, href, query, nil, "", out)
	return err
}

// send writes body to href with method and decodes the answer into out (nil to discard it). cid,
// when set, is sent as the correlation id Pulp records on the task the request dispatches. It
// returns the HTTP status, which tells a synchronous answer (200, 201) from a dispatched task (202).
func (c *client) send(ctx context.Context, method, href string, body any, cid string, out any) (int, error) {
	return c.do(ctx, method, href, nil, body, cid, out)
}

// do performs one request. A call Pulp answered outside 2xx, or did not answer, fails with a
// *localrepodomain.ProviderError whose Err is the *apiError or the HTTP client's error, so callers
// can still tell an expected miss such as a 404 (isStatus). A canceled or expired ctx is returned as
// the ctx's error, and a request or answer that cannot be encoded or decoded as a plain error.
func (c *client) do(ctx context.Context, method, href string, query url.Values, body any, cid string, out any) (int, error) {
	target := c.base + href
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("encode pulp request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return 0, fmt.Errorf("build pulp request: %w", err)
	}
	req.SetBasicAuth(adminUser, c.password)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cid != "" {
		req.Header.Set(correlationHeader, cid)
	}

	operation := method + " " + strings.TrimPrefix(href, apiPath)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return 0, ctxErr
		}
		return 0, translateError(err, c.base, operation)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return resp.StatusCode, translateError(&apiError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(raw))}, c.base, operation)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyBytes))
		return resp.StatusCode, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return resp.StatusCode, fmt.Errorf("decode pulp answer to %s: %w", operation, err)
	}
	return resp.StatusCode, nil
}

// apiError is a Pulp answer outside 2xx and the start of its body. The body is kept because Pulp
// puts the reason there ({"url": ["Enter a valid URL."]}); request headers never are.
type apiError struct {
	StatusCode int
	Body       string
}

// Error names the status and, when there is one, the body.
func (e *apiError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("Pulp returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("Pulp returned HTTP %d: %s", e.StatusCode, e.Body)
}

// isStatus reports whether err is Pulp's answer with one of statuses.
func isStatus(err error, statuses ...int) bool {
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, status := range statuses {
		if apiErr.StatusCode == status {
			return true
		}
	}
	return false
}

// translateError maps a failed call to the provider error the core classifies. endpoint is the
// address the call went to and operation its method and path ("POST /remotes/deb/apt/"); both are
// the administrator's evidence (Endpoint, Operation), while Detail, which the API and item messages
// show verbatim, names neither and holds no credential.
func translateError(err error, endpoint, operation string) error {
	providerErr := &localrepodomain.ProviderError{Endpoint: redact.URL(endpoint), Operation: operation, Err: err}
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		providerErr.Kind, providerErr.Detail = localrepodomain.ProviderErrorUnavailable, "Could not reach Pulp."
		return providerErr
	}
	switch {
	case apiErr.StatusCode == http.StatusUnauthorized, apiErr.StatusCode == http.StatusForbidden:
		providerErr.Kind = localrepodomain.ProviderErrorAuth
		providerErr.Detail = "Pulp rejected the admin password the repository Integration is configured with."
	case apiErr.StatusCode >= http.StatusInternalServerError, apiErr.StatusCode == http.StatusTooManyRequests:
		providerErr.Kind = localrepodomain.ProviderErrorUnavailable
		providerErr.Detail = fmt.Sprintf("Pulp reported an internal error (HTTP %d).", apiErr.StatusCode)
	default:
		providerErr.Kind, providerErr.Detail = localrepodomain.ProviderErrorRejected, rejectionDetail(apiErr)
	}
	return providerErr
}

// rejectionDetail renders Pulp's own reason for a refused request on one line. Pulp answers a
// validation failure with a JSON object mapping a field (or "detail", or "non_field_errors") to a
// message or a list of messages; anything else is quoted as it came.
func rejectionDetail(e *apiError) string {
	if e.Body == "" {
		return fmt.Sprintf("Pulp refused the request (HTTP %d).", e.StatusCode)
	}
	var byField map[string]any
	if err := json.Unmarshal([]byte(e.Body), &byField); err == nil && len(byField) > 0 {
		return "Pulp refused the request: " + redact.Text(flattenReasons(byField))
	}
	return "Pulp refused the request: " + redact.Text(e.Body)
}

// flattenReasons joins a Pulp error object into "field: reason; field: reason", sorted by field so
// the sentence is stable.
func flattenReasons(byField map[string]any) string {
	fields := make([]string, 0, len(byField))
	for field := range byField {
		fields = append(fields, field)
	}
	slices.Sort(fields)
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		reason := reasonText(byField[field])
		if field == "detail" || field == "non_field_errors" {
			parts = append(parts, reason)
			continue
		}
		parts = append(parts, field+": "+reason)
	}
	return strings.Join(parts, "; ")
}

// reasonText renders one value of a Pulp error object: a message, a list of messages, or a nested
// object of a field with sub-fields.
func reasonText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		texts := make([]string, 0, len(typed))
		for _, item := range typed {
			texts = append(texts, reasonText(item))
		}
		return strings.Join(texts, " ")
	case map[string]any:
		return flattenReasons(typed)
	default:
		encoded, _ := json.Marshal(typed)
		return string(encoded)
	}
}

// rejected is the provider error of a request the adapter refuses before calling Pulp (a malformed
// source, a format this adapter does not serve) or of a Pulp state it cannot proceed from.
func rejected(operation, detail string) error {
	return &localrepodomain.ProviderError{Kind: localrepodomain.ProviderErrorRejected, Detail: detail, Operation: operation}
}
