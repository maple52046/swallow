package application

import (
	"context"
	"errors"
	"log/slog"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/core/store"
)

// serverCollection is the store collection of Server records the relay watches.
const serverCollection = "servers"

// serverEventIgnoredFields are the observation timestamps every reconcile and membership pass
// rewrites. A change of them alone shows nothing new to a Server list, so the change feed does not
// deliver it, and the relay does not re-read every Server on every pass.
var serverEventIgnoredFields = []string{"lastSeenAt", "updatedAt", "provisioning.observedAt", "membership.observedAt"}

// Reconnect backoff of the relay's change feed: it doubles from the first value up to the second.
const (
	relayRetryInitial = time.Second
	relayRetryMax     = 30 * time.Second
)

// ServerEventSink is where the relay delivers Server changes: the API process's in-memory broker.
type ServerEventSink interface {
	serverdomain.ServerEventPublisher
	// ResetSubscribers ends every live subscription, so each consumer reconnects and reloads. The
	// relay calls it when changes may have been missed.
	ResetSubscribers()
}

// ServerEventRelay turns the store's change feed of Server records into Server events, so the
// API's Server stream shows every process's writes (the worker's reconcile and controllers, the
// executor's Tasks), not only its own. It reads the current record for each change, because a
// change names a record but carries no content; it publishes a removal for a deleted, missing, or
// absent Server and an upsert otherwise.
//
// The relay is a fan-out inside one process and takes no lease: every API process runs its own,
// watching from the present. Each time its watch opens it resets the sink's subscribers, because
// changes made while it was not watching were not delivered; their consumers reload.
type ServerEventRelay struct {
	feed    store.LiveChangeFeed
	servers serverdomain.ServerRepository
	sink    ServerEventSink
}

// NewServerEventRelay wires the relay. Every dependency is required.
func NewServerEventRelay(feed store.LiveChangeFeed, servers serverdomain.ServerRepository, sink ServerEventSink) *ServerEventRelay {
	return &ServerEventRelay{feed: feed, servers: servers, sink: sink}
}

// Run relays changes until ctx ends. A failed watch or a failed read is logged and the watch
// reopens after a capped exponential backoff, so a store outage delays the stream but never ends
// it. Run returns when ctx is done.
func (r *ServerEventRelay) Run(ctx context.Context) {
	delay := relayRetryInitial
	for {
		err := r.feed.WatchLive(ctx, []string{serverCollection}, serverEventIgnoredFields, r.relay)
		if ctx.Err() != nil {
			return
		}
		slog.Warn("server event relay: change feed stopped; reopening", "error", err, "retryIn", delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(2*delay, relayRetryMax)
	}
}

// relay publishes the event one change implies. An error ends the watch, which Run reopens with a
// reset, so a Server whose read failed is reloaded by every consumer rather than left stale.
func (r *ServerEventRelay) relay(ctx context.Context, change store.Change) error {
	switch change.Operation {
	case store.ChangeReset:
		r.sink.ResetSubscribers()
		return nil
	case store.ChangeDelete:
		r.sink.PublishServerEvent(serverdomain.ServerEvent{Kind: serverdomain.ServerRemoved, ServerID: change.Key})
		return nil
	}
	server, err := r.servers.FindByID(ctx, change.Key)
	if errors.Is(err, serverdomain.ErrServerNotFound) {
		r.sink.PublishServerEvent(serverdomain.ServerEvent{Kind: serverdomain.ServerRemoved, ServerID: change.Key})
		return nil
	}
	if err != nil {
		return err
	}
	if server.Absent {
		r.sink.PublishServerEvent(serverdomain.ServerEvent{
			Kind: serverdomain.ServerRemoved, ServerID: server.ID, SiteID: server.Source.SiteID,
		})
		return nil
	}
	r.sink.PublishServerEvent(serverdomain.ServerEvent{
		Kind: serverdomain.ServerUpserted, ServerID: server.ID, SiteID: server.Source.SiteID, Server: server,
	})
	return nil
}
