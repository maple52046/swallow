package memory

import (
	"context"
	"errors"
	"testing"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/core/store/storetest"
)

// memoryServerRepo implements the writes the eventing tests use; the embedded interface makes any
// other method panic, so a test that reaches one fails loudly.
type memoryServerRepo struct {
	serverdomain.ServerRepository
	servers map[string]*serverdomain.Server
}

func (r *memoryServerRepo) Upsert(_ context.Context, server *serverdomain.Server) error {
	copied := *server
	r.servers[server.ID] = &copied
	return nil
}

func (r *memoryServerRepo) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	server, ok := r.servers[id]
	if !ok {
		return nil, serverdomain.ErrServerNotFound
	}
	copied := *server
	return &copied, nil
}

func (r *memoryServerRepo) SetDeployment(_ context.Context, id string, deployment *serverdomain.DeploymentStatus) error {
	server, ok := r.servers[id]
	if !ok {
		return serverdomain.ErrServerNotFound
	}
	server.Deployment = deployment
	return nil
}

func TestEventingServerRepositoryPublishesOnlyAfterTheTransactionCommits(t *testing.T) {
	inner := &memoryServerRepo{servers: map[string]*serverdomain.Server{"s1": {ID: "s1"}}}
	publisher := &recordingPublisher{}
	repo := NewEventingServerRepository(inner, publisher)
	var tx storetest.Transactor

	err := tx.InTransaction(context.Background(), func(ctx context.Context) error {
		if err := repo.SetDeployment(ctx, "s1", &serverdomain.DeploymentStatus{OperationID: "w1"}); err != nil {
			return err
		}
		if len(publisher.events) != 0 {
			t.Errorf("published %d events before the commit, want 0", len(publisher.events))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("InTransaction() error = %v", err)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("published %d events after the commit, want 1", len(publisher.events))
	}
	got := publisher.events[0]
	if got.Kind != serverdomain.ServerUpserted || got.Server == nil || got.Server.Deployment == nil ||
		got.Server.Deployment.OperationID != "w1" {
		t.Errorf("published %+v, want the re-read Server with its deployment", got)
	}
}

func TestEventingServerRepositoryDropsEventsOfARolledBackTransaction(t *testing.T) {
	inner := &memoryServerRepo{servers: map[string]*serverdomain.Server{}}
	publisher := &recordingPublisher{}
	repo := NewEventingServerRepository(inner, publisher)
	var tx storetest.Transactor
	errAbort := errors.New("abort")

	err := tx.InTransaction(context.Background(), func(ctx context.Context) error {
		if err := repo.Upsert(ctx, &serverdomain.Server{ID: "s1"}); err != nil {
			return err
		}
		return errAbort
	})
	if !errors.Is(err, errAbort) {
		t.Fatalf("InTransaction() error = %v, want %v", err, errAbort)
	}
	if len(publisher.events) != 0 {
		t.Errorf("published %d events for a rolled-back transaction, want 0", len(publisher.events))
	}
}

func TestEventingServerRepositoryPublishesImmediatelyOutsideATransaction(t *testing.T) {
	inner := &memoryServerRepo{servers: map[string]*serverdomain.Server{}}
	publisher := &recordingPublisher{}
	repo := NewEventingServerRepository(inner, publisher)

	if err := repo.Upsert(context.Background(), &serverdomain.Server{ID: "s1"}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if len(publisher.events) != 1 {
		t.Errorf("published %d events, want 1", len(publisher.events))
	}
}
