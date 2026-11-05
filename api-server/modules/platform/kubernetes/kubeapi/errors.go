package kubeapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	"github.com/maple52046/swallow/internal/shared/httpjson"
	"github.com/maple52046/swallow/internal/shared/redact"
)

// translateError maps a failed Kubernetes API call to the *platformdomain.ReaderError the core and
// delivery classify; other errors pass through. endpoint is the API base URL the call went to and
// operation the call's short name: they fill the error's Endpoint (redacted, so no userinfo or
// query value survives) and Operation, and with Transport and the original err (kept as Err) they
// are the evidence ReaderError.Error and Task diagnostics show. Detail stays the user's sentence,
// which delivery returns verbatim, so it never names the endpoint.
func translateError(err error, endpoint, operation string) error {
	readerErr := &platformdomain.ReaderError{Endpoint: redact.URL(endpoint), Operation: operation, Err: err}
	var transportErr *httpjson.TransportError
	if errors.As(err, &transportErr) {
		readerErr.Kind, readerErr.Detail = platformdomain.ReaderErrorUnavailable, "Could not reach the Kubernetes API."
		readerErr.Transport = transportFailure(transportErr.Failure())
		return readerErr
	}
	var statusErr *httpjson.StatusError
	if !errors.As(err, &statusErr) {
		return err
	}
	switch {
	case statusErr.StatusCode == http.StatusUnauthorized, statusErr.StatusCode == http.StatusForbidden:
		readerErr.Kind, readerErr.Detail = platformdomain.ReaderErrorAuth, "The Kubernetes API rejected the credential swallow is configured with."
	case statusErr.StatusCode == http.StatusNotFound:
		readerErr.Kind, readerErr.Detail = platformdomain.ReaderErrorNotFound, statusErr.Error()
	case statusErr.StatusCode == http.StatusConflict:
		readerErr.Kind, readerErr.Detail = platformdomain.ReaderErrorConflict, statusErr.Error()
	case statusErr.StatusCode >= http.StatusInternalServerError:
		readerErr.Kind = platformdomain.ReaderErrorUnavailable
		readerErr.Detail = fmt.Sprintf("The Kubernetes API reported an internal error (HTTP %d).", statusErr.StatusCode)
	default:
		readerErr.Kind, readerErr.Detail = platformdomain.ReaderErrorRejected, statusErr.Error()
	}
	return readerErr
}

// transportFailure maps the HTTP client's classification of an unanswered request to the
// platform domain's, which a wait uses to tell a failure that clears by itself from one that
// needs a change.
func transportFailure(failure httpjson.TransportFailure) platformdomain.TransportFailure {
	switch failure {
	case httpjson.TransportTimeout:
		return platformdomain.TransportTimeout
	case httpjson.TransportRefused:
		return platformdomain.TransportRefused
	case httpjson.TransportDNS:
		return platformdomain.TransportDNS
	case httpjson.TransportTLS:
		return platformdomain.TransportTLS
	default:
		return platformdomain.TransportOther
	}
}

// requestOperation names an explorer call by its method and API path, without the query, which
// may carry selectors that say nothing about which call failed.
func requestOperation(method, path string) string {
	path, _, _ = strings.Cut(path, "?")
	return method + " " + path
}
