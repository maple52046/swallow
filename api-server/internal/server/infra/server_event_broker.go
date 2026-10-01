package infra

import (
	"context"
	"encoding/json"
	"sync"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// serverEventBufferSize bounds each subscriber's pending-event queue. A live dashboard
// applies events far faster than reconcile produces them, so this only has to absorb a
// short burst (a full reconcile pass of a changed fleet). A subscriber that still overflows
// is dropped rather than allowed to stall every writer; it reconnects and reloads to
// resync, which is cheaper than backpressure on the persistence hot path.
const serverEventBufferSize = 256

// ServerEventBroker is an in-memory fan-out of Server projection changes. It is both the
// write side (ServerEventPublisher, called by the persistence boundary) and the read side
// (ServerEventSubscriber, used by the SSE delivery handler).
//
// Ownership/lifecycle: one broker instance lives for the process, created at the
// composition root and shared by the eventing repository and the stream handler. All state
// is guarded by mu; publishing never blocks because delivery to a full subscriber is a
// non-blocking send that instead drops the subscriber.
//
// Deduplication: reconcile re-Upserts every Server every pass, so the broker keeps the last
// delivered fingerprint per Server and suppresses an upsert whose UI-relevant fields are
// unchanged. This turns the periodic reconcile into a change pump without flooding clients.
type ServerEventBroker struct {
	mu           sync.Mutex
	nextID       uint64
	subs         map[uint64]*brokerSubscription
	fingerprints map[string]string
}

// NewServerEventBroker returns an empty broker ready to publish and accept subscriptions.
func NewServerEventBroker() *ServerEventBroker {
	return &ServerEventBroker{
		subs:         make(map[uint64]*brokerSubscription),
		fingerprints: make(map[string]string),
	}
}

// PublishServerEvent fans one change out to matching subscribers. It never blocks: an
// unchanged upsert is dropped by the fingerprint check, and a subscriber whose buffer is
// full is closed instead of waited on. Safe for concurrent use.
func (b *ServerEventBroker) PublishServerEvent(event serverdomain.ServerEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch event.Kind {
	case serverdomain.ServerUpserted:
		if event.Server != nil {
			fingerprint := serverFingerprintJSON(event.Server)
			if b.fingerprints[event.ServerID] == fingerprint {
				return
			}
			b.fingerprints[event.ServerID] = fingerprint
		}
	case serverdomain.ServerRemoved:
		delete(b.fingerprints, event.ServerID)
	}

	for id, sub := range b.subs {
		if sub.siteID != "" && event.SiteID != "" && sub.siteID != event.SiteID {
			continue
		}
		select {
		case sub.ch <- event:
		default:
			// The subscriber is not keeping up. Drop it so one stuck client cannot stall
			// reconcile or a durable Operation; the client reconnects and reloads to resync.
			sub.closed = true
			close(sub.ch)
			delete(b.subs, id)
		}
	}
}

// Subscribe registers a live subscription scoped to siteID ("" for every Site). The
// subscription is closed when ctx is done or when the caller calls Close, whichever comes
// first; a dropped-for-lag subscription is also closed. Safe for concurrent use.
func (b *ServerEventBroker) Subscribe(ctx context.Context, siteID string) serverdomain.ServerEventSubscription {
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	sub := &brokerSubscription{
		broker: b,
		id:     id,
		siteID: siteID,
		ch:     make(chan serverdomain.ServerEvent, serverEventBufferSize),
	}
	b.subs[id] = sub
	b.mu.Unlock()

	// Bind the subscription to the request lifetime so a disconnecting client is cleaned up
	// even if it never calls Close explicitly.
	go func() {
		<-ctx.Done()
		sub.Close()
	}()
	return sub
}

// remove unregisters and closes a subscription exactly once. It is idempotent so both an
// explicit Close and the ctx-cancel goroutine can call it safely.
func (b *ServerEventBroker) remove(id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sub, ok := b.subs[id]
	if !ok || sub.closed {
		return
	}
	sub.closed = true
	close(sub.ch)
	delete(b.subs, id)
}

// brokerSubscription is one consumer's buffered view of the broker's event stream.
type brokerSubscription struct {
	broker *ServerEventBroker
	id     uint64
	siteID string
	ch     chan serverdomain.ServerEvent
	// closed is guarded by the broker mutex; it prevents a double close from a Close call
	// racing the ctx-cancel goroutine or a lag drop.
	closed bool
}

// Events exposes the receive side; the channel is closed when the subscription ends.
func (s *brokerSubscription) Events() <-chan serverdomain.ServerEvent { return s.ch }

// Close ends the subscription; idempotent.
func (s *brokerSubscription) Close() { s.broker.remove(s.id) }

// serverFingerprintJSON produces a stable string over the Server's UI-relevant fields,
// excluding the churny observation timestamps (ObservedAt/UpdatedAt/LastSeenAt) that
// advance every reconcile pass without a meaningful change. Two projections with the same
// fingerprint render identically in the list, so the broker can safely suppress the second.
func serverFingerprintJSON(s *serverdomain.Server) string {
	type provisioning struct {
		State, ProviderState, ErrorDescription, PowerState, OSSystem, DistroSeries string
		// DeployedImageName is part of the fingerprint because an OS Image rename changes only
		// this mirrored field: without it a rename's eager re-mirror (SetOSImageOverlayUseCase)
		// would collide with the prior fingerprint and be suppressed, so the fleet list would not
		// update live until an unrelated field changed.
		DeployedImageName string
		// DeployedImageDefaultUser is included for the same reason: an overlay default-user edit
		// changes only this mirrored field.
		DeployedImageDefaultUser                      string
		Ephemeral, Locked                             bool
		HWEKernel, CommissioningStatus, TestingStatus string
	}
	type membership struct{ PlatformID, NodeName, Role, State string }
	type deployment struct {
		State, Code, Stage, StatusReason, OperationID, StepID string
		Attempt                                               int
	}
	fp := struct {
		Absent       bool
		Observed     serverdomain.Observed
		Provisioning *provisioning
		Membership   *membership
		Deployment   *deployment
		Health       string
	}{
		Absent:   s.Absent,
		Observed: s.Observed,
	}
	if s.Provisioning != nil {
		fp.Provisioning = &provisioning{
			State: s.Provisioning.State, ProviderState: s.Provisioning.ProviderState,
			ErrorDescription: s.Provisioning.ErrorDescription,
			PowerState:       s.Provisioning.PowerState, OSSystem: s.Provisioning.OSSystem,
			DistroSeries:             s.Provisioning.DistroSeries,
			DeployedImageName:        s.Provisioning.DeployedImageName,
			DeployedImageDefaultUser: s.Provisioning.DeployedImageDefaultUser,
			Ephemeral:                s.Provisioning.Ephemeral,
			Locked:                   s.Provisioning.Locked, HWEKernel: s.Provisioning.HWEKernel,
			CommissioningStatus: s.Provisioning.CommissioningStatus, TestingStatus: s.Provisioning.TestingStatus,
		}
	}
	if s.Membership != nil {
		fp.Membership = &membership{
			PlatformID: s.Membership.PlatformID, NodeName: s.Membership.NodeName,
			Role: s.Membership.Role, State: s.Membership.State,
		}
	}
	if s.Deployment != nil {
		fp.Deployment = &deployment{
			State: string(s.Deployment.State), Code: s.Deployment.Code, Stage: s.Deployment.Stage,
			StatusReason: s.Deployment.StatusReason, OperationID: s.Deployment.OperationID,
			StepID: s.Deployment.StepID, Attempt: s.Deployment.Attempt,
		}
	}
	if s.Health != nil {
		fp.Health = s.Health.State
	}
	encoded, err := json.Marshal(fp)
	if err != nil {
		// A projection that will not marshal is treated as always-changed rather than
		// silently deduped, so a bug here degrades to extra events, not missed ones.
		return ""
	}
	return string(encoded)
}
