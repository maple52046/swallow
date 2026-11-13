package delivery

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"

	accessdelivery "github.com/maple52046/swallow/internal/core/access/delivery"
	"github.com/maple52046/swallow/internal/core/server/application"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
)

// serverStreamHeartbeat keeps intermediaries from closing an idle stream and gives the
// writer a periodic chance to notice a disconnected client between change events.
const serverStreamHeartbeat = 25 * time.Second

// ServerStreamHandler serves the Server change stream as Server-Sent Events, so the
// dashboard patches individual list rows live instead of re-reading the whole list. It is a
// read-only complement to the Server list contract and never emits credentials.
type ServerStreamHandler struct {
	subscriber serverdomain.ServerEventSubscriber
}

// NewServerStreamHandler wires the handler to the process's Server event broker.
func NewServerStreamHandler(subscriber serverdomain.ServerEventSubscriber) *ServerStreamHandler {
	return &ServerStreamHandler{subscriber: subscriber}
}

// serverStreamMessage is one SSE frame's JSON body. An `upsert` carries the full,
// list-shaped projection so the client renders the row without a follow-up fetch; a
// `removed` carries only the id so the client drops the row.
type serverStreamMessage struct {
	Type   string                  `json:"type"`
	ID     string                  `json:"id"`
	Server *application.ServerItem `json:"server,omitempty"`
}

// Stream subscribes the caller to Server changes and writes each as an SSE `data:` frame.
//
// Scope: an optional `siteId` query narrows the stream to one Site. Lifecycle: the
// subscription is bound to the request and closed when the client disconnects (detected by a
// failed flush) or when the broker drops a lagging subscriber (its channel closes); the
// client then reconnects and reloads once to resync any events missed while away.
//
// Access: a caller that is not an admin receives only the Servers placed in the Pools it belonged to
// when the stream opened (servers-stream.md, Authorization). A visible Server that moves out of those
// Pools is sent as removed; a Server that was never visible is never mentioned.
func (h *ServerStreamHandler) Stream(c *fiber.Ctx) error {
	siteID := c.Query("siteId")
	filter, err := newStreamFilter(c)
	if err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	// Ask reverse proxies (e.g. nginx) not to buffer, so events reach the browser promptly.
	c.Set("X-Accel-Buffering", "no")

	// A cancelable context ties the subscription to this request; canceling it on writer
	// return closes the subscription even when the client vanishes without a clean close.
	ctx, cancel := context.WithCancel(context.Background())
	subscription := h.subscriber.Subscribe(ctx, siteID)

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer cancel()
		defer subscription.Close()

		// Open the stream immediately so the client's onopen fires and a proxy commits the
		// response headers before the first real event.
		if !writeAndFlush(w, ": connected\n\n") {
			return
		}
		heartbeat := time.NewTicker(serverStreamHeartbeat)
		defer heartbeat.Stop()

		for {
			select {
			case event, ok := <-subscription.Events():
				if !ok {
					return
				}
				event, send := filter.admit(event)
				if !send {
					continue
				}
				frame, err := encodeServerEventFrame(event)
				if err != nil {
					continue
				}
				if !writeAndFlush(w, frame) {
					return
				}
			case <-heartbeat.C:
				if !writeAndFlush(w, ": ping\n\n") {
					return
				}
			}
		}
	})
	return nil
}

// streamFilter decides which events a stream's caller may receive. An admin's filter admits every
// event; any other caller's admits upserts of Servers in its readable Pools, and turns the upsert of
// a Server it could see into a removal once the Server leaves those Pools. It is used by one stream's
// writer goroutine only.
type streamFilter struct {
	all     bool
	pools   map[string]bool
	visible map[string]bool
}

// newStreamFilter builds the filter from the request's Visibility, preloading the Servers the caller
// can see now so that one moving out of its Pools is removed from the client's list.
func newStreamFilter(c *fiber.Ctx) (*streamFilter, error) {
	visibility, ok := accessdelivery.VisibilityFrom(c)
	if !ok {
		return nil, errors.New("server stream has no access scope")
	}
	if visibility.Admin() {
		return &streamFilter{all: true}, nil
	}
	filter := &streamFilter{pools: map[string]bool{}, visible: map[string]bool{}}
	for _, pool := range visibility.ReadablePools() {
		filter.pools[pool] = true
	}
	servers, err := visibility.ReadableServers(c.UserContext())
	if err != nil {
		return nil, err
	}
	for _, id := range servers {
		filter.visible[id] = true
	}
	return filter, nil
}

// admit returns the event to send, if any.
func (f *streamFilter) admit(event serverdomain.ServerEvent) (serverdomain.ServerEvent, bool) {
	if f.all {
		return event, true
	}
	switch event.Kind {
	case serverdomain.ServerUpserted:
		if event.Server != nil && f.pools[event.Server.Spec.PoolID] {
			f.visible[event.ServerID] = true
			return event, true
		}
		if f.visible[event.ServerID] {
			delete(f.visible, event.ServerID)
			return serverdomain.ServerEvent{Kind: serverdomain.ServerRemoved, ServerID: event.ServerID}, true
		}
	case serverdomain.ServerRemoved:
		if f.visible[event.ServerID] {
			delete(f.visible, event.ServerID)
			return event, true
		}
	}
	return serverdomain.ServerEvent{}, false
}

// writeAndFlush reports whether the write reached the client. A write or flush error on a
// streamed response is the only reliable signal that the client has disconnected, so the
// caller stops the loop on false.
func writeAndFlush(w *bufio.Writer, frame string) bool {
	if _, err := w.WriteString(frame); err != nil {
		return false
	}
	return w.Flush() == nil
}

// encodeServerEventFrame renders one event as an SSE `data:` frame terminated by a blank
// line. Upserts reuse the list DTO so the stream and the list stay in one shape.
func encodeServerEventFrame(event serverdomain.ServerEvent) (string, error) {
	message := serverStreamMessage{ID: event.ServerID}
	switch event.Kind {
	case serverdomain.ServerUpserted:
		message.Type = "upsert"
		if event.Server != nil {
			item := application.ToServerItem(event.Server)
			message.Server = &item
		}
	case serverdomain.ServerRemoved:
		message.Type = "removed"
	default:
		return "", fmt.Errorf("unknown server event kind %q", event.Kind)
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return "", err
	}
	return "data: " + string(encoded) + "\n\n", nil
}
