// Package redfish is swallow's Redfish client for Boot Media (decision 047): it probes a BMC's
// Redfish capability, mounts the installation's iPXE ISO on a virtual CD, and sets the boot
// override, implementing serverdomain.RedfishController.
//
// It speaks only the DMTF Redfish schema plus the vendor quirks observed on real BMCs (documented
// where they are handled). It owns no state: every call re-discovers the BMC's resources. It must
// not hold credentials beyond one call, log them, or put them in an error; use cases decide what
// to persist. Provider (MAAS) access and HTTP delivery do not belong here.
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

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
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
}

func (e *statusError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, e.Path, e.Status, e.Message)
	}
	return fmt.Sprintf("%s %s: HTTP %d", e.Method, e.Path, e.Status)
}

// unreachableError means the BMC could not be reached or stayed busy until ctx ended.
type unreachableError struct{ cause error }

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
			return nil, &unreachableError{cause: fmt.Errorf("%s %s: %w (last error: %v)", method, path, ctx.Err(), err)}
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
			return nil, false, &unreachableError{cause: fmt.Errorf("%s %s: %w", method, path, ctx.Err())}
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
		messageID, message := extractMessage(data)
		return nil, false, &statusError{Method: method, Path: path, Status: httpResp.StatusCode, MessageID: messageID, Message: message}
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
}

// extractMessage picks the most useful message from an error body: the first Critical, else the
// first Warning, else the envelope's own message. BMCs put the actionable text in ExtendedInfo.
func extractMessage(data []byte) (string, string) {
	var body redfishErrorBody
	if err := json.Unmarshal(data, &body); err != nil {
		return "", ""
	}
	pick := func(severity string) *redfishMessage {
		for i := range body.Error.ExtendedInfo {
			if strings.EqualFold(body.Error.ExtendedInfo[i].Severity, severity) {
				return &body.Error.ExtendedInfo[i]
			}
		}
		return nil
	}
	for _, severity := range []string{"Critical", "Warning"} {
		if m := pick(severity); m != nil {
			return m.MessageID, strings.TrimSpace(m.Message)
		}
	}
	if len(body.Error.ExtendedInfo) > 0 {
		m := body.Error.ExtendedInfo[0]
		return m.MessageID, strings.TrimSpace(m.Message)
	}
	return body.Error.Code, strings.TrimSpace(body.Error.Message)
}

// classify maps a transport or status failure onto the domain sentinels, keeping the BMC's own
// words as the detail for the operator.
func classify(err error) error {
	var unreachable *unreachableError
	var status *statusError
	switch {
	case errors.As(err, &unreachable):
		return &controllerError{sentinel: serverdomain.ErrBMCUnreachable, detail: unreachable.Error()}
	case errors.As(err, &status) && (status.Status == http.StatusUnauthorized || status.Status == http.StatusForbidden):
		return &controllerError{sentinel: serverdomain.ErrBMCUnreachable, detail: "The BMC rejected the provisioner's BMC account for Redfish."}
	case errors.As(err, &status):
		return &controllerError{sentinel: serverdomain.ErrBootMediaRejected, detail: status.Error()}
	default:
		return &controllerError{sentinel: serverdomain.ErrBMCUnreachable, detail: err.Error()}
	}
}

// controllerError carries a domain sentinel and the BMC's explanation out of the controller; the
// use case wraps it into a BootMediaError naming the Server.
type controllerError struct {
	sentinel error
	detail   string
}

func (e *controllerError) Error() string { return e.detail }
func (e *controllerError) Unwrap() error { return e.sentinel }

// Detail is the operator-facing explanation, without the sentinel's generic text.
func (e *controllerError) Detail() string { return e.detail }
