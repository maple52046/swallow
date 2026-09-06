package infra

import (
	"context"
	"fmt"
	"testing"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

func brokerTestServer(id, siteID, provisioningState string) *serverdomain.Server {
	return &serverdomain.Server{
		ID:           id,
		Source:       serverdomain.Source{SiteID: siteID, IntegrationID: "int", ProviderMachineID: "m-" + id},
		Provisioning: &serverdomain.ProvisioningStatus{State: provisioningState},
	}
}

func upsertEvent(server *serverdomain.Server) serverdomain.ServerEvent {
	return serverdomain.ServerEvent{
		Kind: serverdomain.ServerUpserted, ServerID: server.ID,
		SiteID: server.Source.SiteID, Server: server,
	}
}

// The broker must deliver a changed projection but suppress an identical re-publish, so the
// 30s reconcile pump does not flood clients with unchanged rows.
func TestServerEventBrokerFanoutAndDedup(t *testing.T) {
	broker := NewServerEventBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := broker.Subscribe(ctx, "")
	defer sub.Close()

	broker.PublishServerEvent(upsertEvent(brokerTestServer("a", "s1", "ready")))
	select {
	case event := <-sub.Events():
		if event.Kind != serverdomain.ServerUpserted || event.ServerID != "a" {
			t.Fatalf("first event = %#v, want upsert of a", event)
		}
	default:
		t.Fatal("expected the initial upsert to be delivered")
	}

	// Same UI-relevant projection: deduplicated.
	broker.PublishServerEvent(upsertEvent(brokerTestServer("a", "s1", "ready")))
	select {
	case event := <-sub.Events():
		t.Fatalf("expected an unchanged re-publish to be deduped, got %#v", event)
	default:
	}

	// Changed provisioning state: delivered.
	broker.PublishServerEvent(upsertEvent(brokerTestServer("a", "s1", "deploying")))
	select {
	case event := <-sub.Events():
		if event.Server == nil || event.Server.Provisioning.State != "deploying" {
			t.Fatalf("changed event = %#v, want provisioning state deploying", event)
		}
	default:
		t.Fatal("expected the changed projection to be delivered")
	}
}

// A Site-scoped subscriber must ignore another Site's upserts but still receive unscoped
// removals, which carry no Site.
func TestServerEventBrokerSiteScope(t *testing.T) {
	broker := NewServerEventBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := broker.Subscribe(ctx, "s1")
	defer sub.Close()

	broker.PublishServerEvent(upsertEvent(brokerTestServer("b", "s2", "ready")))
	select {
	case event := <-sub.Events():
		t.Fatalf("scoped subscriber received another Site's event: %#v", event)
	default:
	}

	broker.PublishServerEvent(serverdomain.ServerEvent{Kind: serverdomain.ServerRemoved, ServerID: "c"})
	select {
	case event := <-sub.Events():
		if event.Kind != serverdomain.ServerRemoved || event.ServerID != "c" {
			t.Fatalf("event = %#v, want removed of c", event)
		}
	default:
		t.Fatal("expected an unscoped removal to be delivered to a scoped subscriber")
	}
}

// A subscriber that never drains must be dropped (its channel closed) rather than blocking
// the publisher, so one stuck client cannot stall reconcile.
func TestServerEventBrokerDropsSlowSubscriber(t *testing.T) {
	broker := NewServerEventBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := broker.Subscribe(ctx, "")

	for i := 0; i < serverEventBufferSize+5; i++ {
		// Distinct removals never dedup, so the buffer fills and overflows.
		broker.PublishServerEvent(serverdomain.ServerEvent{
			Kind: serverdomain.ServerRemoved, ServerID: fmt.Sprintf("s-%d", i),
		})
	}

	closed := false
	for i := 0; i < serverEventBufferSize+10; i++ {
		if _, ok := <-sub.Events(); !ok {
			closed = true
			break
		}
	}
	if !closed {
		t.Fatal("expected the overflowing subscriber to be dropped and its channel closed")
	}
}

// Close is idempotent, and publishing after Close neither panics nor delivers.
func TestServerEventBrokerCloseIsIdempotent(t *testing.T) {
	broker := NewServerEventBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := broker.Subscribe(ctx, "")
	sub.Close()
	sub.Close()

	broker.PublishServerEvent(upsertEvent(brokerTestServer("a", "s1", "ready")))
	if _, ok := <-sub.Events(); ok {
		t.Fatal("expected a closed subscription channel after Close")
	}
}
