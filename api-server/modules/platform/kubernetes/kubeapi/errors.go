package kubeapi

import (
	"errors"
	"fmt"
	"net/http"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	"github.com/maple52046/swallow/internal/shared/httpjson"
)

// translateError maps a failed Kubernetes API call to the *platformdomain.ReaderError the core and
// delivery classify; other errors pass through.
func translateError(err error) error {
	var transportErr *httpjson.TransportError
	if errors.As(err, &transportErr) {
		return &platformdomain.ReaderError{
			Kind: platformdomain.ReaderErrorUnavailable, Detail: "Could not reach the Kubernetes API.", Err: err,
		}
	}
	var statusErr *httpjson.StatusError
	if !errors.As(err, &statusErr) {
		return err
	}
	switch {
	case statusErr.StatusCode == http.StatusUnauthorized, statusErr.StatusCode == http.StatusForbidden:
		return &platformdomain.ReaderError{
			Kind: platformdomain.ReaderErrorAuth, Detail: "The Kubernetes API rejected the credential swallow is configured with.", Err: err,
		}
	case statusErr.StatusCode == http.StatusNotFound:
		return &platformdomain.ReaderError{Kind: platformdomain.ReaderErrorNotFound, Detail: statusErr.Error(), Err: err}
	case statusErr.StatusCode == http.StatusConflict:
		return &platformdomain.ReaderError{Kind: platformdomain.ReaderErrorConflict, Detail: statusErr.Error(), Err: err}
	case statusErr.StatusCode >= http.StatusInternalServerError:
		return &platformdomain.ReaderError{
			Kind:   platformdomain.ReaderErrorUnavailable,
			Detail: fmt.Sprintf("The Kubernetes API reported an internal error (HTTP %d).", statusErr.StatusCode),
			Err:    err,
		}
	default:
		return &platformdomain.ReaderError{Kind: platformdomain.ReaderErrorRejected, Detail: statusErr.Error(), Err: err}
	}
}
