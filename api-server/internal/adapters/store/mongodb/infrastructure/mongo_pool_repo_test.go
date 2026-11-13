package infrastructure

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/adapters/store/mongodb/mongotest"
	infradomain "github.com/maple52046/swallow/internal/core/infrastructure/domain"
)

// Pool names are unique across the installation: a second Pool with a name, or a rename onto one,
// is ErrPoolNameTaken, and the list is every Pool by name. Skipped without SWALLOW_TEST_MONGO_URI.
func TestPoolRepo_NamesAreUniqueInTheInstallation(t *testing.T) {
	_, db := mongotest.Database(t, "pool_repo")
	ctx := context.Background()
	repo, err := NewMongoPoolRepo(db)
	if err != nil {
		t.Fatalf("NewMongoPoolRepo: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, pool := range []*infradomain.Pool{
		{ID: "p1", Name: "research", Description: "GPU team", CreatedAt: now, UpdatedAt: now},
		{ID: "p2", Name: "default", CreatedAt: now, UpdatedAt: now},
	} {
		if err := repo.Create(ctx, pool); err != nil {
			t.Fatalf("Create(%s): %v", pool.Name, err)
		}
	}
	if err := repo.Create(ctx, &infradomain.Pool{ID: "p3", Name: "research"}); !errors.Is(err, infradomain.ErrPoolNameTaken) {
		t.Fatalf("duplicate Create: err = %v, want ErrPoolNameTaken", err)
	}
	if err := repo.Update(ctx, &infradomain.Pool{ID: "p2", Name: "research", UpdatedAt: now}); !errors.Is(err, infradomain.ErrPoolNameTaken) {
		t.Fatalf("rename onto a taken name: err = %v, want ErrPoolNameTaken", err)
	}
	if err := repo.Update(ctx, &infradomain.Pool{ID: "missing", Name: "x"}); !errors.Is(err, infradomain.ErrPoolNotFound) {
		t.Fatalf("Update of an unknown Pool: err = %v, want ErrPoolNotFound", err)
	}

	pools, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(pools) != 2 || pools[0].Name != "default" || pools[1].Name != "research" || pools[1].Description != "GPU team" {
		t.Errorf("List = %+v, want default then research", pools)
	}
	if err := repo.Delete(ctx, "p1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.FindByID(ctx, "p1"); !errors.Is(err, infradomain.ErrPoolNotFound) {
		t.Errorf("FindByID after Delete: err = %v, want ErrPoolNotFound", err)
	}
}
