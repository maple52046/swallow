package infra

import (
	"context"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// EventingServerRepository decorates a ServerRepository so every successful write publishes
// a ServerEvent. Wrapping the sole persistence adapter at the composition root makes this
// the single choke point for change notification: reconcile, platform membership sync,
// deploy/release deployment updates, the inventory sweep, and deletes all publish uniformly,
// without each caller knowing about the stream.
//
// Reads are delegated unchanged. Writes delegate first and publish only on success, so a
// failed write never emits a phantom change. Publishing is best-effort and non-blocking
// (see ServerEventPublisher): a re-read needed to build the full projection is skipped
// silently on error rather than failing the write the caller already completed.
type EventingServerRepository struct {
	inner     serverdomain.ServerRepository
	publisher serverdomain.ServerEventPublisher
}

// NewEventingServerRepository wraps inner so its writes publish to publisher. Callers should
// use the returned repository everywhere the plain repository was injected.
func NewEventingServerRepository(inner serverdomain.ServerRepository, publisher serverdomain.ServerEventPublisher) *EventingServerRepository {
	return &EventingServerRepository{inner: inner, publisher: publisher}
}

func (r *EventingServerRepository) FindByID(ctx context.Context, id string) (*serverdomain.Server, error) {
	return r.inner.FindByID(ctx, id)
}

func (r *EventingServerRepository) FindBySource(ctx context.Context, source serverdomain.Source) (*serverdomain.Server, error) {
	return r.inner.FindBySource(ctx, source)
}

func (r *EventingServerRepository) FindByHardware(ctx context.Context, hardware serverdomain.Hardware) ([]*serverdomain.Server, error) {
	return r.inner.FindByHardware(ctx, hardware)
}

func (r *EventingServerRepository) List(ctx context.Context, filter serverdomain.ListFilter) (serverdomain.ListResult, error) {
	return r.inner.List(ctx, filter)
}

func (r *EventingServerRepository) CountByIntegration(ctx context.Context, integrationID string) (int, error) {
	return r.inner.CountByIntegration(ctx, integrationID)
}

// Upsert publishes the written projection directly, since the caller already holds it.
func (r *EventingServerRepository) Upsert(ctx context.Context, server *serverdomain.Server) error {
	if err := r.inner.Upsert(ctx, server); err != nil {
		return err
	}
	r.publishUpsert(server)
	return nil
}

// MarkAbsent publishes a removal for each Server that just left the default list, using the
// IDs the sweep reports.
func (r *EventingServerRepository) MarkAbsent(ctx context.Context, integrationID string, seenBefore time.Time) ([]string, error) {
	ids, err := r.inner.MarkAbsent(ctx, integrationID, seenBefore)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		r.publisher.PublishServerEvent(serverdomain.ServerEvent{Kind: serverdomain.ServerRemoved, ServerID: id})
	}
	return ids, nil
}

// SetMembership re-reads the projection so the published upsert carries the full, current
// row (membership plus the untouched provisioning/observed fields), which is what the list
// consumer renders.
func (r *EventingServerRepository) SetMembership(ctx context.Context, id string, membership *serverdomain.MembershipStatus) error {
	if err := r.inner.SetMembership(ctx, id, membership); err != nil {
		return err
	}
	r.publishByID(ctx, id)
	return nil
}

func (r *EventingServerRepository) SetGPUs(ctx context.Context, id string, gpus []serverdomain.GPU) error {
	if err := r.inner.SetGPUs(ctx, id, gpus); err != nil {
		return err
	}
	r.publishByID(ctx, id)
	return nil
}

func (r *EventingServerRepository) SetDeployment(ctx context.Context, id string, deployment *serverdomain.DeploymentStatus) error {
	if err := r.inner.SetDeployment(ctx, id, deployment); err != nil {
		return err
	}
	r.publishByID(ctx, id)
	return nil
}

// Delete reads the projection before removal so the removal event can be scoped to the
// Server's Site, then publishes the removal.
func (r *EventingServerRepository) Delete(ctx context.Context, id string) error {
	siteID := ""
	if existing, err := r.inner.FindByID(ctx, id); err == nil && existing != nil {
		siteID = existing.Source.SiteID
	}
	if err := r.inner.Delete(ctx, id); err != nil {
		return err
	}
	r.publisher.PublishServerEvent(serverdomain.ServerEvent{
		Kind: serverdomain.ServerRemoved, ServerID: id, SiteID: siteID,
	})
	return nil
}

// publishByID re-reads and publishes the full projection for an id-only write. A read
// failure is swallowed: the write already succeeded, and a missed live event self-heals on
// the next reconcile pass or a client reload.
func (r *EventingServerRepository) publishByID(ctx context.Context, id string) {
	server, err := r.inner.FindByID(ctx, id)
	if err != nil || server == nil {
		return
	}
	r.publishUpsert(server)
}

func (r *EventingServerRepository) publishUpsert(server *serverdomain.Server) {
	r.publisher.PublishServerEvent(serverdomain.ServerEvent{
		Kind:     serverdomain.ServerUpserted,
		ServerID: server.ID,
		SiteID:   server.Source.SiteID,
		Server:   server,
	})
}
