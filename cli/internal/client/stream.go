package client

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// StreamEvent is one decoded Server-Sent Events frame. Type is the SSE `event:`
// field when present (empty for the default event) and Data is the concatenated
// `data:` payload. The servers stream sends its change type inside the JSON Data
// payload rather than the SSE event field, so callers parse Data as JSON.
type StreamEvent struct {
	Type string
	Data string
}

// Stream opens a text/event-stream endpoint and invokes onEvent for each frame
// until the context is canceled, the server closes the connection, or onEvent
// returns an error. It is used by the servers watch command.
//
// Contract notes (servers-stream.md): the endpoint accepts the access token in
// the `access_token` query parameter for header-less clients; this client can
// set headers, so it sends the standard Bearer header and does not add the query
// parameter. Heartbeat comment lines (":" prefix) are ignored. Delivery is
// best-effort: a dropped connection returns and the caller re-reads the list to
// resync, so Stream does not itself retry.
func (c *Client) Stream(ctx context.Context, path string, query url.Values, onEvent func(StreamEvent) error) error {
	resp, err := c.Do(ctx, Request{
		Method: "GET",
		Path:   path,
		Query:  query,
		Accept: "text/event-stream",
		Auth:   AuthBearer,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	var eventType string
	var data strings.Builder

	// dispatch flushes the accumulated frame at a blank-line boundary. It resets
	// the frame buffers so the next frame starts clean, and suppresses empty
	// frames produced by consecutive blank lines or heartbeat-only gaps.
	dispatch := func() error {
		if data.Len() == 0 && eventType == "" {
			return nil
		}
		evt := StreamEvent{Type: eventType, Data: strings.TrimSuffix(data.String(), "\n")}
		eventType = ""
		data.Reset()
		return onEvent(evt)
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			switch {
			case trimmed == "":
				if derr := dispatch(); derr != nil {
					return derr
				}
			case strings.HasPrefix(trimmed, ":"):
				// SSE comment / heartbeat; carries no data.
			case strings.HasPrefix(trimmed, "event:"):
				eventType = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
			case strings.HasPrefix(trimmed, "data:"):
				data.WriteString(strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
				data.WriteByte('\n')
			default:
				// Unknown field; ignore to stay forward compatible with new SSE fields.
			}
		}
		if err != nil {
			if err == io.EOF {
				// Flush any trailing frame that arrived without a closing blank line.
				if derr := dispatch(); derr != nil {
					return derr
				}
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("read stream: %w", err)
		}
	}
}
