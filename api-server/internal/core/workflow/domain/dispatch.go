package domain

import (
	"context"
	"errors"
	"time"
)

// DriverVersion names the driver a Workflow was last dispatched to. A Workflow whose recorded
// driver differs (one accepted before this driver existed) is re-dispatched, which terminates its
// old execution and resumes it from the store (decision 072).
const DriverVersion = "v2"

// Dispatch records how the Workflow is handed to the executor. Generation increases on every
// dispatch (the first run and every resume); a run and its activities carry the generation they
// were started with, and the core rejects a write from an older one (ErrDispatchFenced).
type Dispatch struct {
	Generation int64  `json:"generation" bson:"generation"`
	Driver     string `json:"driver,omitempty" bson:"driver,omitempty"`
	// DeliveredCommand is the command sequence the dispatcher last delivered to a run; a larger
	// Commands.Sequence means a command still has to be delivered.
	DeliveredCommand int64      `json:"deliveredCommand" bson:"deliveredCommand"`
	DispatchedAt     *time.Time `json:"dispatchedAt,omitempty" bson:"dispatchedAt,omitempty"`
}

// Commands are the operator requests stored for a Workflow. Operators and launchers only write
// commands; the Workflow's run applies them, and the dispatcher makes sure a run exists to do so.
type Commands struct {
	// Sequence increases with every command, so the dispatcher can tell whether one is new.
	Sequence int64          `json:"sequence" bson:"sequence"`
	Cancel   *CancelCommand `json:"cancel,omitempty" bson:"cancel,omitempty"`
	Retries  []RetryCommand `json:"retries,omitempty" bson:"retries,omitempty"`
}

// CancelCommand asks the Workflow to cancel. Reason becomes the canceled Workflow's statusReason.
type CancelCommand struct {
	Reason      string    `json:"reason" bson:"reason"`
	RequestedAt time.Time `json:"requestedAt" bson:"requestedAt"`
}

// RetryCommand asks the Workflow to retry one failed, retryable Task. The run applies it with the
// granularity of the Task's Job and then removes it.
type RetryCommand struct {
	TaskID      string    `json:"taskId" bson:"taskId"`
	RequestedAt time.Time `json:"requestedAt" bson:"requestedAt"`
}

// CanceledByOperatorReason is the statusReason of a Workflow an operator canceled.
const CanceledByOperatorReason = "Canceled by operator."

// ResourceOwnership records that a Workflow owns one Server or Platform. It has no expiry: the
// Workflow releases it in the transaction that makes it terminal (decision 072).
type ResourceOwnership struct {
	ResourceKey string    `json:"resourceKey" bson:"_id"`
	Owner       string    `json:"owner" bson:"owner"`
	AcquiredAt  time.Time `json:"acquiredAt" bson:"acquiredAt"`
}

// OwnershipRepository stores resource ownership. Acquire and Release run inside the transaction
// of the Workflow transition that needs them. Implementations must be safe for concurrent use; a
// concurrent acquisition of the same resource by two Workflows must fail for one of them (the
// transition is retried and then waits).
type OwnershipRepository interface {
	// Acquire takes every resource for owner, or none: it reports false without writing when any
	// resource is owned by another Workflow. Resources owner already holds count as acquired.
	Acquire(ctx context.Context, owner string, resourceKeys []string, at time.Time) (bool, error)
	// Release frees every resource owner holds; freeing none is not an error.
	Release(ctx context.Context, owner string) error
	// FindByOwner returns the resources owner holds, sorted by key.
	FindByOwner(ctx context.Context, owner string) ([]ResourceOwnership, error)
	// IsHeld reports whether any Workflow owns resourceKey.
	IsHeld(ctx context.Context, resourceKey string) (bool, error)
}

var (
	// ErrDispatchFenced reports a write from a run whose dispatch generation is no longer the
	// Workflow's: a later dispatch superseded it, so it must stop without side effects.
	ErrDispatchFenced = errors.New("the workflow run was superseded by a later dispatch")
	// ErrTaskNotRunning reports an outcome or progress write for a Task that is not running at
	// that attempt, for example a late write after the Task was canceled or already recorded.
	// Writers treat it as nothing to do.
	ErrTaskNotRunning = errors.New("the task is not running at this attempt")
)
