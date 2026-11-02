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

// LiveChangeFeed delivers committed changes of the named collections from the moment a watch
// opens, to a subscriber that keeps no position: a fan-out inside one process (the API's Server
// event stream), where every process watches for itself and a restarted process need not replay
// what it missed. Each watch therefore first delivers a ChangeReset, meaning changes before it
// opened are not delivered and the subscriber re-reads what it serves.
//
// ignoredFields are field paths (for example "lastSeenAt" or "provisioning.observedAt") whose
// change alone does not matter to the subscriber: an update that sets or removes only those
// fields, or fields below them, is not delivered. WatchLive blocks until ctx ends, returning
// ctx.Err(), or until the feed fails or handle returns an error, returning that error; the caller
// decides when to watch again. Any number of live watchers may run at once.
type LiveChangeFeed interface {
	WatchLive(ctx context.Context, collections, ignoredFields []string, handle func(context.Context, Change) error) error
}
