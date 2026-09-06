// Package application owns the embedded operation use cases: accepting durable intent,
// dispatching it to the local ansible-runner, and exposing execution state, progress, and
// logs. It depends on the operation domain and shared abstractions, never on delivery or
// infrastructure.
package application

import (
	"context"
	"errors"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/shared/pagination"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// ErrInvalidOperation is the validation failure the delivery layer maps to a 400. Callers
// wrap it with a specific reason.
var ErrInvalidOperation = errors.New("invalid operation")

// PolicyChecker refuses operations that contradict a platform policy.
//
// It is a port so the operation context does not depend on the platform context: the check
// (for example, that a GPU driver install does not fight a platform's GPU operator) is
// implemented in the platform context and injected here.
type PolicyChecker interface {
	CheckOperation(ctx context.Context, kind operationdomain.WorkflowKind, platformID string, targetServerIDs []string) error
}

// ListOperationsInput narrows an operation listing. Empty fields mean no constraint.
type ListOperationsInput struct {
	SiteID     string
	PlatformID string
	ServerID   string
	Kind       string
	Status     string
	Active     bool
	Page       pagination.Page
}

// optionalTime formats a nullable timestamp for a response, returning nil when unset so the
// field serialises as JSON null rather than a zero time.
func optionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return wire.TimePtr(*t)
}
