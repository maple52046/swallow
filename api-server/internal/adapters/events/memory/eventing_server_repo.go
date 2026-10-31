package memory

import (
	"context"
	"errors"
	"fmt"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/core/store"
)

// EventingServerRepository decorates a ServerRepository so every successful write publishes
// a ServerEvent. Wrapping the sole persistence adapter at the composition root makes this
// the single choke point for change notification: reconcile, platform membership sync,
// deploy/release deployment updates, the inventory sweep, and deletes all publish uniformly,
// without each caller knowing about the stream.
//
// Reads are delegated unchanged. Writes delegate first and publish only on success, so a
// failed write never emits a phantom change. Inside a store transaction the event waits for the
// commit (store.AfterCommit), so a rolled-back or retried unit of work never announces state that
// was not committed; outside one it is published at once. An id-only write re-reads the
// projection with the caller's ctx, which inside a transaction sees the transaction's own writes,
// and publishes that snapshot after the commit. Publishing is best-effort and non-blocking
// (see ServerEventPublisher): a re-read failure is skipped silently rather than failing the
// write the caller already made.
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
	r.publishUpsert(ctx, server)
	return nil
}

// MarkAbsent publishes a removal for each Server that just left the default list, using the
// IDs the sweep reports.
func (r *EventingServerRepository) MarkAbsent(ctx context.Context, integrationID string, seenBefore time.Time) ([]string, error) {
	ids, err := r.inner.MarkAbsent(ctx, integrationID, seenBefore)
	if err != nil {
		return nil, err
	}
	store.AfterCommit(ctx, func() {
		for _, id := range ids {
			r.publisher.PublishServerEvent(serverdomain.ServerEvent{Kind: serverdomain.ServerRemoved, ServerID: id})
		}
	})
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

// SetDefaultUser publishes the re-read projection, so the Server detail and list show the new
// effective default user without a reload.
func (r *EventingServerRepository) SetDefaultUser(ctx context.Context, id string, user string) error {
	if err := r.inner.SetDefaultUser(ctx, id, user); err != nil {
		return err
	}
	r.publishByID(ctx, id)
	return nil
}

// UpdateSpec delegates to the inner repository's ServerConvergenceStore and publishes the re-read
// projection, so a tag edit shows as pending on every open list at once.
func (r *EventingServerRepository) UpdateSpec(ctx context.Context, id string, expectedGeneration int64, spec serverdomain.ServerSpec) (int64, error) {
	store, err := r.convergenceStore()
	if err != nil {
		return 0, err
	}
	generation, err := store.UpdateSpec(ctx, id, expectedGeneration, spec)
	if err != nil {
		return 0, err
	}
	r.publishByID(ctx, id)
	return generation, nil
}

// AdoptSpec delegates and publishes when the spec was adopted.
func (r *EventingServerRepository) AdoptSpec(ctx context.Context, id string, spec serverdomain.ServerSpec) (bool, error) {
	store, err := r.convergenceStore()
	if err != nil {
		return false, err
	}
	adopted, err := store.AdoptSpec(ctx, id, spec)
	if err == nil && adopted {
		r.publishByID(ctx, id)
	}
	return adopted, err
}

// SetConvergence delegates and publishes the re-read projection.
func (r *EventingServerRepository) SetConvergence(ctx context.Context, id string, status serverdomain.ServerConvergence) error {
	store, err := r.convergenceStore()
	if err != nil {
		return err
	}
	if err := store.SetConvergence(ctx, id, status); err != nil {
		return err
	}
	r.publishByID(ctx, id)
	return nil
}

// SetObservedTags delegates and publishes the re-read projection.
func (r *EventingServerRepository) SetObservedTags(ctx context.Context, id string, tags, automaticTags []string) error {
	store, err := r.convergenceStore()
	if err != nil {
		return err
	}
	if err := store.SetObservedTags(ctx, id, tags, automaticTags); err != nil {
		return err
	}
	r.publishByID(ctx, id)
	return nil
}

// SetObservedPlacement delegates and publishes the re-read projection.
func (r *EventingServerRepository) SetObservedPlacement(ctx context.Context, id string, zone, pool string) error {
	store, err := r.convergenceStore()
	if err != nil {
		return err
	}
	if err := store.SetObservedPlacement(ctx, id, zone, pool); err != nil {
		return err
	}
	r.publishByID(ctx, id)
	return nil
}

// CountSpecPlacement delegates; a read publishes nothing.
func (r *EventingServerRepository) CountSpecPlacement(ctx context.Context, zoneID, poolID string) (int, error) {
	store, err := r.convergenceStore()
	if err != nil {
		return 0, err
	}
	return store.CountSpecPlacement(ctx, zoneID, poolID)
}

// SetOrigin delegates and publishes the re-read projection.
func (r *EventingServerRepository) SetOrigin(ctx context.Context, id string, origin serverdomain.Origin) error {
	store, err := r.convergenceStore()
	if err != nil {
		return err
	}
	if err := store.SetOrigin(ctx, id, origin); err != nil {
		return err
	}
	r.publishByID(ctx, id)
	return nil
}

// convergenceStore returns the inner repository's ServerConvergenceStore, or an error when it does
// not persist the desired-state parts, rather than silently dropping a spec write.
func (r *EventingServerRepository) convergenceStore() (serverdomain.ServerConvergenceStore, error) {
	store, ok := r.inner.(serverdomain.ServerConvergenceStore)
	if !ok {
		return nil, fmt.Errorf("server repository %T does not store desired state", r.inner)
	}
	return store, nil
}

// ClaimForPlatform delegates to the inner repository's PlatformClaimStore and publishes every
// Server it was asked to claim, so lists show the claim without a reload. It fails with an error
// when the inner repository does not persist claims, rather than silently claiming nothing.
func (r *EventingServerRepository) ClaimForPlatform(ctx context.Context, platformID string, serverIDs []string, at time.Time) error {
	claims, ok := r.inner.(serverdomain.PlatformClaimStore)
	if !ok {
		return errPlatformClaimsUnsupported
	}
	if err := claims.ClaimForPlatform(ctx, platformID, serverIDs, at); err != nil {
		return err
	}
	for _, id := range serverIDs {
		r.publishByID(ctx, id)
	}
	return nil
}

// ReleasePlatformClaims reads the claimed Servers first, clears the claims, then publishes those
// Servers. A failed read only loses the live events; the release itself still runs.
func (r *EventingServerRepository) ReleasePlatformClaims(ctx context.Context, platformID string) error {
	claims, ok := r.inner.(serverdomain.PlatformClaimStore)
	if !ok {
		return errPlatformClaimsUnsupported
	}
	ids, _ := claims.ClaimedServerIDs(ctx, platformID)
	if err := claims.ReleasePlatformClaims(ctx, platformID); err != nil {
		return err
	}
	for _, id := range ids {
		r.publishByID(ctx, id)
	}
	return nil
}

// ClaimingPlatformIDs is a read and is delegated unchanged.
func (r *EventingServerRepository) ClaimingPlatformIDs(ctx context.Context) ([]string, error) {
	claims, ok := r.inner.(serverdomain.PlatformClaimStore)
	if !ok {
		return nil, errPlatformClaimsUnsupported
	}
	return claims.ClaimingPlatformIDs(ctx)
}

// ClaimedServerIDs is a read and is delegated unchanged.
func (r *EventingServerRepository) ClaimedServerIDs(ctx context.Context, platformID string) ([]string, error) {
	claims, ok := r.inner.(serverdomain.PlatformClaimStore)
	if !ok {
		return nil, errPlatformClaimsUnsupported
	}
	return claims.ClaimedServerIDs(ctx, platformID)
}

var errPlatformClaimsUnsupported = errors.New("the server repository does not persist platform claims")

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
	store.AfterCommit(ctx, func() {
		r.publisher.PublishServerEvent(serverdomain.ServerEvent{
			Kind: serverdomain.ServerRemoved, ServerID: id, SiteID: siteID,
		})
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
	r.publishUpsert(ctx, server)
}

// publishUpsert announces server once ctx's unit of work commits (immediately outside one).
func (r *EventingServerRepository) publishUpsert(ctx context.Context, server *serverdomain.Server) {
	store.AfterCommit(ctx, func() {
		r.publisher.PublishServerEvent(serverdomain.ServerEvent{
			Kind:     serverdomain.ServerUpserted,
			ServerID: server.ID,
			SiteID:   server.Source.SiteID,
			Server:   server,
		})
	})
}
