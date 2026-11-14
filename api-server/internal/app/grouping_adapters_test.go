package app

import (
	"context"
	"errors"
	"testing"
	"time"

	storemongo "github.com/maple52046/swallow/internal/adapters/store/mongodb"
	infrastructurestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/infrastructure"
	"github.com/maple52046/swallow/internal/adapters/store/mongodb/mongotest"
	serverstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/server"
	infrastructureapp "github.com/maple52046/swallow/internal/core/infrastructure/application"
	infradomain "github.com/maple52046/swallow/internal/core/infrastructure/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// fixedPlatformServers maps a Platform id onto its Servers, standing in for the Platform and claim
// stores the PoolResolver reads.
type fixedPlatformServers map[string][]string

func (p fixedPlatformServers) ServersOf(_ context.Context, platformID string) ([]string, bool, error) {
	ids, ok := p[platformID]
	return ids, ok, nil
}

// A Platform's move to another Pool writes its Servers' placement through the Server store in one
// transaction: while one Server is not adopted yet, the Server already written is rolled back, and
// once it is adopted every Server moves, each keeping its Zone.
func TestMovePlatformToPoolIsAtomicOnTheStore(t *testing.T) {
	client, db := mongotest.Database(t, "platform_pool")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	servers, err := serverstore.NewMongoServerRepo(db)
	if err != nil {
		t.Fatalf("NewMongoServerRepo() error = %v", err)
	}
	pools, err := infrastructurestore.NewMongoPoolRepo(db)
	if err != nil {
		t.Fatalf("NewMongoPoolRepo() error = %v", err)
	}
	now := time.Now().UTC()
	if err := pools.Create(ctx, &infradomain.Pool{ID: "p-1", Name: "research", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create Pool: %v", err)
	}
	for _, id := range []string{"srv-a", "srv-b"} {
		server := &serverdomain.Server{
			ID:        id,
			Source:    serverdomain.Source{SiteID: "site-1", IntegrationID: "maas-1", ProviderMachineID: "m-" + id},
			CreatedAt: now, UpdatedAt: now, LastSeenAt: now,
		}
		if err := servers.Upsert(ctx, server); err != nil {
			t.Fatalf("Upsert(%s) error = %v", id, err)
		}
	}
	if _, err := servers.AdoptSpec(ctx, "srv-a", serverdomain.ServerSpec{ZoneID: "z-1"}); err != nil {
		t.Fatalf("AdoptSpec(srv-a) error = %v", err)
	}
	service := infrastructureapp.NewGroupingService(nil, pools, nil,
		serverPlacements{servers: servers, specs: servers}, placementReferences{specs: servers}, nil,
		fixedPlatformServers{"plat-1": {"srv-a", "srv-b"}}, nil, storemongo.NewTransactor(client))
	move := infrastructureapp.MovePlatformInput{PlatformID: "plat-1", PoolID: "p-1"}

	if _, err := service.MovePlatformToPool(ctx, move); !errors.Is(err, infradomain.ErrServerNotAdopted) {
		t.Fatalf("moving with srv-b not adopted: err = %v, want ErrServerNotAdopted", err)
	}
	if got := readServer(ctx, t, servers, "srv-a"); got.Spec.PoolID != "" || got.Generation != 1 {
		t.Errorf("srv-a after the refused move: Pool %q at generation %d, want no Pool at generation 1", got.Spec.PoolID, got.Generation)
	}

	if _, err := servers.AdoptSpec(ctx, "srv-b", serverdomain.ServerSpec{}); err != nil {
		t.Fatalf("AdoptSpec(srv-b) error = %v", err)
	}
	if _, err := service.MovePlatformToPool(ctx, move); err != nil {
		t.Fatalf("MovePlatformToPool() error = %v", err)
	}
	a, b := readServer(ctx, t, servers, "srv-a"), readServer(ctx, t, servers, "srv-b")
	if a.Spec.PoolID != "p-1" || a.Spec.ZoneID != "z-1" || a.Generation != 2 || b.Spec.PoolID != "p-1" {
		t.Errorf("after the move: srv-a %+v at %d, srv-b %+v, want both in p-1 and srv-a still in z-1 at 2", a.Spec, a.Generation, b.Spec)
	}
}

func readServer(ctx context.Context, t *testing.T, servers *serverstore.MongoServerRepo, id string) *serverdomain.Server {
	t.Helper()
	server, err := servers.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID(%s) error = %v", id, err)
	}
	return server
}
