// Package redfish is swallow's Redfish client for Boot Media (decision 047): it probes a BMC's
// Redfish capability, mounts the installation's iPXE ISO on a virtual CD, and sets the boot
// override, implementing serverdomain.RedfishController.
//
// It is split into the common part and the BMC Profiles (docs/development/bmc-profiles.md). The
// common part (transport.go, controller.go, resources.go, boot.go, profile.go) speaks only the
// DMTF Redfish schema; a vendor's names, OEM fields, and firmware quirks live only in its
// profile_<vendor>.go with where they were observed, and a BMC no profile recognizes gets the
// DMTF-only default profile. It owns no state: every call re-discovers the BMC's resources and
// recognizes its profile again. It must not hold credentials beyond one call, log them, or put
// them in an error; use cases decide what to persist. Provider (MAAS) access and HTTP delivery do
// not belong here.
package redfish

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

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// requestTimeout bounds one HTTP exchange. BMC web servers are slow (several seconds per
// request is normal) but a single request must not consume a caller's whole deadline.
const requestTimeout = 30 * time.Second

// maxBackoff caps the wait between retries of a busy BMC. A BMC commonly answers 503 for minutes
// after the host posts (its Redfish service reloads BIOS data), so callers set the overall
// patience through ctx and this only spaces the attempts.
const maxBackoff = 15 * time.Second

// session is one authenticated conversation with one BMC for one controller call. It sends HTTP
// basic authentication on every request (no Redfish session is created, so nothing has to be
// logged out) and holds the credential only for the life of the call.
type session struct {
	client   *http.Client
	base     string
	username string
	password string
	// pause spaces the few targeted retries a read makes outside the busy-BMC backoff.
	pause time.Duration
}

// newHTTPClient builds the client for BMC traffic. Certificate verification is disabled on
// purpose: BMCs ship self-signed certificates for their own IPs, and swallow has no trust anchor
// for them. The exposure is limited to the BMC network the provisioner already uses, and the
// credential is the provisioner's BMC account, not a swallow secret.
func newHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	transport.MaxIdleConnsPerHost = 2
	return &http.Client{Transport: transport, Timeout: requestTimeout}
}

// serviceBase derives the scheme and host of the Redfish service from the provisioner's BMC
// address: a bare host (IPMI's power_address) becomes https://host, and a URL keeps its scheme
// and host while dropping userinfo, path, and query (the Redfish root is always /redfish/v1).
func serviceBase(address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", errors.New("empty BMC address")
	}
	if !strings.Contains(address, "://") {
		address = "https://" + address
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("BMC address %q is not a host or URL", redactedAddress(address))
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && scheme != "http" {
		scheme = "https"
	}
	return scheme + "://" + parsed.Host, nil
}

// redactedAddress strips any userinfo so an address can appear in an error.
func redactedAddress(address string) string {
	if parsed, err := url.Parse(address); err == nil && parsed.User != nil {
		parsed.User = nil
		return parsed.String()
	}
	return address
}

// statusError is a non-success HTTP answer from the BMC, with the Redfish message it carried and
// the request it answered (method and path only; the path never carries a credential).
type statusError struct {
	Method    string
	Path      string
	Status    int
	MessageID string
	Message   string
	// MessageArgs are the DMTF message's arguments, for example the refused property's name in
	// PropertyUnknown; non-string arguments are left out.
	MessageArgs []string
}

func (e *statusError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, e.Path, e.Status, e.Message)
	}
	return fmt.Sprintf("%s %s: HTTP %d", e.Method, e.Path, e.Status)
}

// operation is the request the error answered, "METHOD /path".
func (e *statusError) operation() string { return e.Method + " " + e.Path }

// permanent reports whether repeating the request unchanged cannot succeed: a client error other
// than a timeout, a conflict, or a rate limit. A server error may pass.
func (e *statusError) permanent() bool {
	switch e.Status {
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusPreconditionFailed, http.StatusTooManyRequests:
		return false
	}
	return e.Status >= 400 && e.Status < 500
}

// refusal explains a 403: which request the BMC refused and its own words (for example a feature
// its license does not cover), so the user is not sent to fix an account that works.
func (e *statusError) refusal() string {
	reason := fmt.Sprintf("The BMC refused %s with HTTP 403", e.operation())
	if e.Message != "" {
		reason += ": " + strings.TrimSuffix(e.Message, ".")
	}
	if e.MessageID != "" {
		reason += " (" + e.MessageID + ")"
	}
	return reason + "."
}

// unreachableError means the BMC could not be reached or stayed busy until ctx ended. operation is
// the request that was being tried, or "" when the wait was not for a request.
type unreachableError struct {
	cause     error
	operation string
}

func (e *unreachableError) Error() string { return e.cause.Error() }
func (e *unreachableError) Unwrap() error { return e.cause }

// response is one successful exchange.
type response struct {
	Status   int
	Header   http.Header
	Body     []byte
	Location string
}

// get reads path (a Redfish @odata.id or absolute path) into out. A nil out discards the body.
func (s *session) get(ctx context.Context, path string, out any) error {
	resp, err := s.do(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Body, out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

// do sends one request, retrying transport failures and busy answers (502, 503, 504) with capped
// exponential backoff until ctx is done. Only the request path appears in errors; the base URL
// carries no credential either, because authentication travels in the header.
func (s *session) do(ctx context.Context, method, path string, body any, header http.Header) (*response, error) {
	var payload []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = encoded
	}
	backoff := time.Second
	for {
		resp, retry, err := s.once(ctx, method, path, payload, header)
		if !retry {
			return resp, err
		}
		select {
		case <-ctx.Done():
			return nil, &unreachableError{cause: fmt.Errorf("%s %s: %w (last error: %v)", method, path, ctx.Err(), err), operation: method + " " + path}
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// once performs a single exchange and reports whether it is worth retrying.
func (s *session) once(ctx context.Context, method, path string, payload []byte, header http.Header) (*response, bool, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.base+path, reader)
	if err != nil {
		return nil, false, err
	}
	req.SetBasicAuth(s.username, s.password)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, values := range header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	httpResp, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, &unreachableError{cause: fmt.Errorf("%s %s: %w", method, path, ctx.Err()), operation: method + " " + path}
		}
		// The URL in a transport error has no userinfo; still report only the path.
		return nil, true, fmt.Errorf("%s %s: %s", method, path, transportReason(err))
	}
	defer httpResp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(httpResp.Body, 4<<20))
	if readErr != nil {
		return nil, true, fmt.Errorf("%s %s: read body: %v", method, path, readErr)
	}
	switch {
	case httpResp.StatusCode == http.StatusBadGateway,
		httpResp.StatusCode == http.StatusServiceUnavailable,
		httpResp.StatusCode == http.StatusGatewayTimeout:
		return nil, true, fmt.Errorf("%s %s: BMC busy (HTTP %d)", method, path, httpResp.StatusCode)
	case httpResp.StatusCode >= 200 && httpResp.StatusCode < 300:
		return &response{
			Status: httpResp.StatusCode, Header: httpResp.Header, Body: data,
			Location: httpResp.Header.Get("Location"),
		}, false, nil
	default:
		m := extractMessage(data)
		return nil, false, &statusError{Method: method, Path: path, Status: httpResp.StatusCode, MessageID: m.MessageID, Message: m.Message, MessageArgs: m.args()}
	}
}

// transportReason reduces a net/http error to its cause without the URL it embeds.
func transportReason(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err.Error()
	}
	return err.Error()
}

// redfishErrorBody is the DMTF error envelope.
type redfishErrorBody struct {
	Error struct {
		Code         string           `json:"code"`
		Message      string           `json:"message"`
		ExtendedInfo []redfishMessage `json:"@Message.ExtendedInfo"`
	} `json:"error"`
}

// redfishMessage is one DMTF Message (also used in Task resources).
type redfishMessage struct {
	MessageID string `json:"MessageId"`
	Message   string `json:"Message"`
	Severity  string `json:"Severity"`
	// MessageArgs is untyped because firmware is not consistent about the arguments' JSON types,
	// and one odd argument must not lose the whole message.
	MessageArgs []any `json:"MessageArgs"`
}

// args returns the message's string arguments.
func (m redfishMessage) args() []string {
	var args []string
	for _, arg := range m.MessageArgs {
		if s, ok := arg.(string); ok {
			args = append(args, s)
		}
	}
	return args
}

// extractMessage picks the most useful message from an error body: the first Critical, else the
// first Warning, else the first, else the envelope's own code and message. BMCs put the actionable
// text in ExtendedInfo. A body that is not a DMTF error gives an empty message.
func extractMessage(data []byte) redfishMessage {
	var body redfishErrorBody
	if err := json.Unmarshal(data, &body); err != nil {
		return redfishMessage{}
	}
	pick := func(severity string) *redfishMessage {
		for i := range body.Error.ExtendedInfo {
			if strings.EqualFold(body.Error.ExtendedInfo[i].Severity, severity) {
				return &body.Error.ExtendedInfo[i]
			}
		}
		return nil
	}
	m := pick("Critical")
	if m == nil {
		m = pick("Warning")
	}
	if m == nil && len(body.Error.ExtendedInfo) > 0 {
		m = &body.Error.ExtendedInfo[0]
	}
	if m == nil {
		return redfishMessage{MessageID: body.Error.Code, Message: strings.TrimSpace(body.Error.Message)}
	}
	picked := *m
	picked.Message = strings.TrimSpace(picked.Message)
	return picked
}

// rejectedAccount is the explanation of a 401: the BMC refused the credential itself.
const rejectedAccount = "The BMC rejected the provisioner's BMC account for Redfish."

// classify maps a transport or status failure onto the domain sentinels, keeping the BMC's own
// words as the detail for the user and the failed request as the operation:
//
//   - not reached, or busy until ctx ended: ErrBMCUnreachable, transient;
//   - 401: ErrBMCUnreachable, permanent — the account itself was rejected;
//   - 403: ErrRedfishUnsupported, permanent — the account works but the BMC refuses this request
//     (a privilege or a license it lacks), so the refused request and the BMC's message are kept;
//   - any other answer: ErrBootMediaRejected, with the status's own permanence.
func classify(err error) error {
	var unreachable *unreachableError
	var status *statusError
	switch {
	case errors.As(err, &unreachable):
		return &controllerError{sentinel: serverdomain.ErrBMCUnreachable, detail: unreachable.Error(), operation: unreachable.operation}
	case errors.As(err, &status) && status.Status == http.StatusUnauthorized:
		return &controllerError{sentinel: serverdomain.ErrBMCUnreachable, detail: rejectedAccount, operation: status.operation(), permanent: true}
	case errors.As(err, &status) && status.Status == http.StatusForbidden:
		return &controllerError{sentinel: serverdomain.ErrRedfishUnsupported, detail: status.refusal(), operation: status.operation(), permanent: true}
	case errors.As(err, &status):
		return &controllerError{sentinel: serverdomain.ErrBootMediaRejected, detail: status.Error(), operation: status.operation(), permanent: status.permanent()}
	default:
		return &controllerError{sentinel: serverdomain.ErrBMCUnreachable, detail: err.Error()}
	}
}

// controllerError carries a domain sentinel and the BMC's explanation out of the controller; the
// use case wraps it into a BootMediaError naming the Server. None of its fields holds a credential:
// the detail is the BMC's message or swallow's sentence, and the operation is a request's method
// and path or a step's name, and the endpoint is the service root without userinfo.
type controllerError struct {
	sentinel  error
	detail    string
	operation string
	permanent bool
	// endpoint is set by the Controller's port methods (atEndpoint), not where the error is made.
	endpoint string
}

func (e *controllerError) Error() string { return e.detail }
func (e *controllerError) Unwrap() error { return e.sentinel }

// Detail is the user-facing explanation, without the sentinel's generic text.
func (e *controllerError) Detail() string { return e.detail }

// Operation is the request ("GET /redfish/v1/Systems/1/Bios") or step ("mount the ISO") that
// failed, or "" when the failure was not one request's.
func (e *controllerError) Operation() string { return e.operation }

// Permanent reports whether retrying unchanged cannot succeed (a rejected account, a refused
// request); a BMC that could not be reached or stayed busy is not permanent.
func (e *controllerError) Permanent() bool { return e.permanent }

// Endpoint is the BMC's Redfish service root, or "" when its address is not a host.
func (e *controllerError) Endpoint() string { return e.endpoint }
