package client

import "fmt"

// APIError is the typed form of the api-server shared error envelope
// (conventions.md): a machine-readable Code, a human-readable Message, and an
// opaque RequestID that correlates with the server's X-Request-ID log entry.
// Commands surface Code and RequestID so an operator can branch on the failure
// class and quote the correlation ID when reporting a problem.
//
// Status is the HTTP status code carried alongside the envelope. It is retained
// separately because a caller may need it when the body could not be decoded
// into the standard envelope (see newAPIError).
type APIError struct {
	Status    int
	Code      string
	Message   string
	RequestID string
}

// Error renders a stable, greppable single-line description. It always includes
// the status and code; the request ID is appended only when present so a
// transport-level failure without an envelope stays readable.
func (e *APIError) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("%s (HTTP %d, request %s): %s", e.Code, e.Status, e.RequestID, e.Message)
	}
	return fmt.Sprintf("%s (HTTP %d): %s", e.Code, e.Status, e.Message)
}

// errorEnvelope mirrors the on-the-wire shape documented in conventions.md so a
// non-2xx response body decodes into APIError without the command layer parsing
// JSON itself.
type errorEnvelope struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"requestId"`
	} `json:"error"`
}
