package convergence

import (
	"context"
	"time"
)

// LeaseStore keeps the singleton leases that let only one instance of each Controller act at a
// time (desired-state-model.md §7.6). A lease is a record in the store with a holder and an expiry,
// separate from the resource leases Workflows hold.
//
// TryAcquire takes the named lease for holder when it is free, expired, or already held by holder,
// extending it to now+ttl, and reports whether holder now holds it; calling it again before the TTL
// ends is how a holder renews. A false result with a nil error means another holder has it.
// Release gives the lease up if holder still holds it; releasing a lease held by someone else is a
// no-op. Implementations are safe for concurrent use; expiry compares against the store host's
// clock as seen by the caller, so processes must share a clock (one installation host).
type LeaseStore interface {
	TryAcquire(ctx context.Context, name, holder string, ttl time.Duration) (bool, error)
	Release(ctx context.Context, name, holder string) error
}

// EventType names what a convergence event records.
type EventType string

const (
	// EventAdopted: the first observation became the resource's spec (desired-state-model.md §6.5).
	EventAdopted EventType = "adopted"
	// EventConverged: a spec change was realized.
	EventConverged EventType = "converged"
	// EventDriftDetected: an observed field departed from the spec.
	EventDriftDetected EventType = "drift_detected"
	// EventDriftCorrected: a controller restored a drifted field to the spec at resync.
	EventDriftCorrected EventType = "drift_corrected"
	// EventDriftResolved: a drifted field matches the spec again without a correction.
	EventDriftResolved EventType = "drift_resolved"
	// EventConvergenceFailed: the provider rejected a change and the controller stopped trying.
	EventConvergenceFailed EventType = "convergence_failed"
)

// Event is one entry in a resource's convergence timeline: what the controller observed or did,
// for which spec generation, and when. Message is for people and must not carry a secret.
type Event struct {
	ID           string
	ResourceKind string
	ResourceID   string
	Type         EventType
	Fields       []string
	Message      string
	Generation   int64
	At           time.Time
}

// EventRecorder stores convergence events and lists a resource's most recent ones, newest first.
// Recording is best-effort for the controller: a failed Record is logged and does not undo the
// change it describes.
type EventRecorder interface {
	Record(ctx context.Context, event Event) error
	List(ctx context.Context, resourceKind, resourceID string, limit int) ([]Event, error)
}

// RequestCollection is the store collection convergence requests are written to. A controller
// that honors requests watches it and treats a change whose key is its Name as a request to
// converge every key now, rather than at its next resync.
const RequestCollection = "convergence_requests"

// Requester asks a controller, which runs in another process, to converge every key now
// (desired-state-model.md §7.6: the API asks through the store; the worker converges). A request
// made while another is pending is coalesced with it.
type Requester interface {
	Request(ctx context.Context, controller string) error
}
