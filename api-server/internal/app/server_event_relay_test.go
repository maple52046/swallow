package app

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/adapters/events/memory"
	storemongo "github.com/maple52046/swallow/internal/adapters/store/mongodb"
	serverstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/server"
	serverapp "github.com/maple52046/swallow/internal/core/server/application"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// A Server write made by another process (here a second repository on the same database, as the
// worker or the executor holds) reaches the API's stream subscribers through the change feed: an
// upsert for a written Server, a removal once it is absent. A timestamp-only rewrite reaches nobody.
func TestServerEventRelayStreamsWritesOfOtherProcesses(t *testing.T) {
	uri := os.Getenv("SWALLOW_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("SWALLOW_TEST_MONGO_URI is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := storemongo.Connect(ctx, uri)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	db := client.Database(fmt.Sprintf("swallow_relay_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	apiServers, err := serverstore.NewMongoServerRepo(db)
	if err != nil {
		t.Fatalf("NewMongoServerRepo() error = %v", err)
	}
	workerServers, err := serverstore.NewMongoServerRepo(db)
	if err != nil {
		t.Fatalf("NewMongoServerRepo() error = %v", err)
	}

	broker := memory.NewServerEventBroker()
	subscription := broker.Subscribe(ctx, "")
	relayCtx, stopRelay := context.WithCancel(ctx)
	defer stopRelay()
	go serverapp.NewServerEventRelay(storemongo.NewChangeFeed(db), apiServers, broker).Run(relayCtx)

	// The relay resets subscribers when its watch opens, which ends this subscription; subscribe
	// again once that happened, so the writes below are certainly after the watch opened.
	select {
	case _, open := <-subscription.Events():
		if open {
			t.Fatal("first event arrived before the watch opened")
		}
	case <-ctx.Done():
		t.Fatal("the relay never opened its watch")
	}
	subscription = broker.Subscribe(ctx, "")
	defer subscription.Close()

	now := time.Now().UTC()
	server := &serverdomain.Server{
		ID:       "srv-1",
		Source:   serverdomain.Source{SiteID: "site-1", IntegrationID: "maas-1", ProviderMachineID: "m1"},
		Observed: serverdomain.Observed{Hostname: "gpu-01"},
		Provisioning: &serverdomain.ProvisioningStatus{
			State: "ready", ObservedAt: now,
		},
		CreatedAt: now, UpdatedAt: now, LastSeenAt: now,
	}
	if err := workerServers.Upsert(ctx, server); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	event := next(t, ctx, subscription)
	if event.Kind != serverdomain.ServerUpserted || event.ServerID != "srv-1" || event.Server == nil || event.Server.Observed.Hostname != "gpu-01" {
		t.Fatalf("event = %+v, want the upsert of srv-1", event)
	}

	// Only the observation timestamps change: nothing reaches the subscriber.
	later := now.Add(time.Minute)
	server.LastSeenAt, server.UpdatedAt, server.Provisioning.ObservedAt = later, later, later
	if err := workerServers.Upsert(ctx, server); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if _, err := workerServers.MarkAbsent(ctx, "maas-1", later.Add(time.Minute)); err != nil {
		t.Fatalf("MarkAbsent() error = %v", err)
	}
	event = next(t, ctx, subscription)
	if event.Kind != serverdomain.ServerRemoved || event.ServerID != "srv-1" {
		t.Fatalf("event = %+v, want the removal of srv-1 (the timestamp-only write must not be relayed)", event)
	}
}

func next(t *testing.T, ctx context.Context, subscription serverdomain.ServerEventSubscription) serverdomain.ServerEvent {
	t.Helper()
	select {
	case event, open := <-subscription.Events():
		if !open {
			t.Fatal("subscription closed")
		}
		return event
	case <-ctx.Done():
		t.Fatal("no event before the deadline")
	}
	return serverdomain.ServerEvent{}
}
