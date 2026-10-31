package store

import "context"

// ChangeOperation is what happened to a record a ChangeFeed reports.
type ChangeOperation string

const (
	ChangeInsert  ChangeOperation = "insert"
	ChangeUpdate  ChangeOperation = "update"
	ChangeReplace ChangeOperation = "replace"
	ChangeDelete  ChangeOperation = "delete"
	// ChangeReset means changes may have been missed (for example the store no longer holds the
	// subscriber's resume position). It names no record; a subscriber must re-read everything it
	// watches, as a controller does on resync.
	ChangeReset ChangeOperation = "reset"
)

// Change names one committed change: the collection and the key (record id) it touched. It
// carries no record content, so a subscriber always reads current state from its repository; a
// controller acts on that state, never on the event (desired-state-model.md §7.2).
type Change struct {
	Collection string
	Key        string
	Operation  ChangeOperation
}

// ChangeFeed delivers committed changes of the named collections to one subscriber, in commit
// order, so that writes made by any process are visible to every other (desired-state-model.md
// §10.1).
//
// Watch blocks until ctx ends, returning ctx.Err(), or until the feed fails or handle returns an
// error, returning that error; the caller decides when to watch again. Delivery is at least once:
// the subscriber's position is saved durably after changes are handled and Watch resumes from it,
// so a change may be delivered again after a restart and handle must be idempotent. When the
// saved position can no longer be resumed, Watch delivers a ChangeReset and continues from the
// present. Each subscriber name must have one watcher at a time.
type ChangeFeed interface {
	Watch(ctx context.Context, subscriber string, collections []string, handle func(context.Context, Change) error) error
}
