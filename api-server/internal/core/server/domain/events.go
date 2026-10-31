package domain

import "context"

// ServerEventKind distinguishes a projection change from a removal so a live consumer —
// the dashboard Server list fed by an SSE stream — can patch a single row or drop it,
// instead of re-reading the whole list.
type ServerEventKind string

const (
	// ServerUpserted means the Server projection was created or changed. The event carries
	// the full, secret-free projection (same shape as the list contract) so the consumer
	// can render the row without a follow-up read.
	ServerUpserted ServerEventKind = "upsert"
	// ServerRemoved means the Server left the default list — deleted, or marked absent from
	// provider inventory. Only ServerID (and best-effort SiteID) is set.
	ServerRemoved ServerEventKind = "removed"
)

// ServerEvent is one change to the Server projection. It is published by the persistence
// boundary after a Server is written, so downstream consumers update incrementally rather
// than polling the whole list. It carries no BMC, SSH, integration, or automation
// credential, matching the Server list contract.
type ServerEvent struct {
	Kind     ServerEventKind
	ServerID string
	// SiteID scopes delivery to a Site-scoped subscriber. It is set for upserts (from the
	// Server's source) and best-effort for removals; an empty value is delivered to every
	// subscriber, who ignores a row it is not showing.
	SiteID string
	// Server is the full projection for an upsert and nil for a removal.
	Server *Server
}

// ServerEventPublisher is the write side of the change stream, called by the persistence
// boundary after a successful Server write.
//
// Contract: PublishServerEvent MUST NOT block the caller. A reconcile pass or a durable
// Operation persists Servers on the hot path, so an implementation owns its own buffering
// and drops or disconnects a slow subscriber rather than applying backpressure to the
// writer. It is safe for concurrent use.
type ServerEventPublisher interface {
	PublishServerEvent(event ServerEvent)
}

// ServerEventSubscription is one consumer's live view of Server changes. Events delivers
// events until the consumer calls Close or the subscription is dropped for lagging behind;
// a closed subscription's channel is closed so the reader's range loop ends. Close is
// idempotent.
type ServerEventSubscription interface {
	Events() <-chan ServerEvent
	Close()
}

// ServerEventSubscriber is the read side used by a delivery adapter (the SSE handler) to
// obtain a live subscription. siteID scopes the stream to one Site; "" streams every Site.
// The returned subscription is closed automatically when ctx is done, and may also be
// closed explicitly by the caller.
type ServerEventSubscriber interface {
	Subscribe(ctx context.Context, siteID string) ServerEventSubscription
}
