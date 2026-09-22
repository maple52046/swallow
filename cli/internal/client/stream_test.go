package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestStreamParsesFramesAndSkipsComments verifies the SSE reader accumulates
// data lines into a frame at the blank-line boundary and ignores heartbeat
// comment lines, matching the servers stream contract.
func TestStreamParsesFramesAndSkipsComments(t *testing.T) {
	body := ": connected\n\n" +
		"data: {\"type\":\"upsert\",\"id\":\"s1\"}\n\n" +
		": ping\n" +
		"data: {\"type\":\"removed\",\"id\":\"s2\"}\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	var frames []string
	err := c.Stream(context.Background(), "servers/stream", nil, func(evt StreamEvent) error {
		frames = append(frames, evt.Data)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2: %v", len(frames), frames)
	}
	if frames[0] != `{"type":"upsert","id":"s1"}` {
		t.Errorf("frame 0 = %q", frames[0])
	}
	if frames[1] != `{"type":"removed","id":"s2"}` {
		t.Errorf("frame 1 = %q", frames[1])
	}
}
