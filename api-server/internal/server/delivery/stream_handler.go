package delivery

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/server/application"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
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
func (h *ServerStreamHandler) Stream(c *fiber.Ctx) error {
	siteID := c.Query("siteId")

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
