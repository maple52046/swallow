package application

import (
	"context"
	"sync"
	"testing"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/core/store"
	"github.com/maple52046/swallow/internal/core/store/storetest"
)

// relayServers answers FindByID from a fixed set; every other method is unused.
type relayServers struct {
	serverdomain.ServerRepository
	servers map[string]*serverdomain.Server
}

func (r relayServers) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	if server, ok := r.servers[id]; ok {
		return server, nil
	}
	return nil, serverdomain.ErrServerNotFound
}

// recordingSink records what the relay published and how many resets it asked for.
type recordingSink struct {
	mu     sync.Mutex
	events []serverdomain.ServerEvent
	resets int
}

func (s *recordingSink) PublishServerEvent(event serverdomain.ServerEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *recordingSink) ResetSubscribers() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resets++
}

func (s *recordingSink) snapshot() ([]serverdomain.ServerEvent, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]serverdomain.ServerEvent(nil), s.events...), s.resets
}

// A change of any process's Server write becomes an event read from the current record: an
// upsert for a present Server, a removal for an absent, missing, or deleted one. Opening the watch
// resets the subscribers, and a change of another collection is not relayed.
func TestServerEventRelayPublishesWhatTheStoreHolds(t *testing.T) {
	feed := &storetest.ChangeFeed{}
	servers := relayServers{servers: map[string]*serverdomain.Server{
		"present": {ID: "present", Source: serverdomain.Source{SiteID: "site-1"}},
		"absent":  {ID: "absent", Absent: true, Source: serverdomain.Source{SiteID: "site-2"}},
	}}
	sink := &recordingSink{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		NewServerEventRelay(feed, servers, sink).Run(ctx)
	}()
	waitFor(t, func() bool { return feed.Watchers() == 1 })

	for _, change := range []store.Change{
		{Collection: "servers", Key: "present", Operation: store.ChangeUpdate},
		{Collection: "servers", Key: "absent", Operation: store.ChangeUpdate},
		{Collection: "servers", Key: "missing", Operation: store.ChangeInsert},
		{Collection: "servers", Key: "deleted", Operation: store.ChangeDelete},
		{Collection: "platforms", Key: "p1", Operation: store.ChangeUpdate},
	} {
		feed.Publish(change)
	}
	waitFor(t, func() bool { events, _ := sink.snapshot(); return len(events) == 4 })
	cancel()
	<-done

	events, resets := sink.snapshot()
	if resets != 1 {
		t.Errorf("resets = %d, want 1 when the watch opened", resets)
	}
	want := []serverdomain.ServerEvent{
		{Kind: serverdomain.ServerUpserted, ServerID: "present", SiteID: "site-1"},
		{Kind: serverdomain.ServerRemoved, ServerID: "absent", SiteID: "site-2"},
		{Kind: serverdomain.ServerRemoved, ServerID: "missing"},
		{Kind: serverdomain.ServerRemoved, ServerID: "deleted"},
	}
	for i, got := range events {
		if got.Kind != want[i].Kind || got.ServerID != want[i].ServerID || got.SiteID != want[i].SiteID {
			t.Errorf("event %d = %+v, want %+v", i, got, want[i])
		}
	}
	if events[0].Server == nil || events[0].Server.ID != "present" {
		t.Errorf("upsert carries %+v, want the current record", events[0].Server)
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
