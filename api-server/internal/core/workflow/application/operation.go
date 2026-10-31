// Package application owns the workflow context's use cases: accepting durable intent,
// dispatching it to the local ansible-runner, and exposing execution state, progress, and
// logs. It depends on the workflow domain and shared abstractions, never on delivery or
// adapters.
package application

import (
	"context"
	"errors"
	"time"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	"github.com/maple52046/swallow/internal/shared/pagination"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// ErrInvalidOperation is the validation failure the delivery layer maps to a 400. Callers
// wrap it with a specific reason.
var ErrInvalidOperation = errors.New("invalid operation")

// ErrTargetsSpanSites means an operation's targets belong to more than one Site. It is a fault
// of the request, a 400 in every delivery, and is wrapped together with ErrInvalidOperation.
var ErrTargetsSpanSites = errors.New("targets must belong to one site")

// PolicyChecker refuses operations that contradict a platform policy.
//
// It is a port so the operation context does not depend on the platform context: the check
// (for example, that a GPU driver install does not fight a platform's GPU operator) is
// implemented in the platform context and injected here.
type PolicyChecker interface {
	CheckOperation(ctx context.Context, kind workflowdomain.WorkflowKind, platformID string, targetServerIDs []string) error
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
