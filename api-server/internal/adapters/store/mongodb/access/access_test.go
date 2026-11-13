package access

import (
	"context"
	"errors"
	"testing"
	"time"

	authstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/auth"
	"github.com/maple52046/swallow/internal/adapters/store/mongodb/mongotest"
	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
)

// A User may belong to a Pool once; memberships list by Pool and by User and are deleted with
// either. Skipped without SWALLOW_TEST_MONGO_URI.
func TestMemberships_UniquePerPoolAndUser(t *testing.T) {
	_, db := mongotest.Database(t, "access_memberships")
	ctx := context.Background()
	repo, err := NewMongoMemberships(ctx, db)
	if err != nil {
		t.Fatalf("NewMongoMemberships: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	alice := &accessdomain.Membership{ID: "m1", PoolID: "p1", SiteID: "s", UserID: "alice", Role: accessdomain.RoleOwner,
		Sudo: accessdomain.SudoInherit, SudoPasswordless: accessdomain.SudoOn, CreatedAt: now, UpdatedAt: now}
	if err := repo.Create(ctx, alice); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Create(ctx, &accessdomain.Membership{ID: "m2", PoolID: "p1", UserID: "alice", Role: accessdomain.RoleMember}); !errors.Is(err, accessdomain.ErrMembershipExists) {
		t.Fatalf("second membership error = %v, want ErrMembershipExists", err)
	}
	if err := repo.Create(ctx, &accessdomain.Membership{ID: "m3", PoolID: "p2", UserID: "alice", Role: accessdomain.RoleMember}); err != nil {
		t.Fatalf("Create in another Pool: %v", err)
	}
	got, err := repo.Find(ctx, "p1", "alice")
	if err != nil || *got != *alice {
		t.Fatalf("Find = %+v, %v; want %+v", got, err, alice)
	}
	if mine, _ := repo.ListByUser(ctx, "alice"); len(mine) != 2 {
		t.Errorf("ListByUser = %d, want 2", len(mine))
	}
	got.Role = accessdomain.RoleMember
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := repo.DeleteByPool(ctx, "p1"); err != nil {
		t.Fatalf("DeleteByPool: %v", err)
	}
	if _, err := repo.Find(ctx, "p1", "alice"); !errors.Is(err, accessdomain.ErrMembershipNotFound) {
		t.Errorf("Find after DeleteByPool error = %v, want ErrMembershipNotFound", err)
	}
	if err := repo.DeleteByUser(ctx, "alice"); err != nil {
		t.Fatalf("DeleteByUser: %v", err)
	}
	if all, _ := repo.ListAll(ctx); len(all) != 0 {
		t.Errorf("ListAll after DeleteByUser = %d, want 0", len(all))
	}
}

// uids and gids count separately from the start recorded at the first allocation, a later start is
// ignored, and a group key keeps its first allocation.
func TestPOSIXIDs_AllocateOnceAndKeepTheRecordedStart(t *testing.T) {
	_, db := mongotest.Database(t, "access_posix")
	ctx := context.Background()
	ids := NewMongoPOSIXIDs(db, 10000)
	if _, ok, err := ids.RecordedStart(ctx); err != nil || ok {
		t.Fatalf("RecordedStart before any allocation = %v, %v; want none", ok, err)
	}
	for want := 10000; want < 10003; want++ {
		if uid, err := ids.AllocateUID(ctx); err != nil || uid != want {
			t.Fatalf("AllocateUID = %d, %v; want %d", uid, err, want)
		}
	}
	later := NewMongoPOSIXIDs(db, 50000)
	if uid, err := later.AllocateUID(ctx); err != nil || uid != 10003 {
		t.Errorf("AllocateUID with a changed start = %d, %v; want 10003", uid, err)
	}
	if start, ok, _ := later.RecordedStart(ctx); !ok || start != 10000 {
		t.Errorf("RecordedStart = %d, %v; want 10000", start, ok)
	}
	group, err := ids.Allocate(ctx, "users", "swallow-users")
	if err != nil || group.GIDNumber != 10000 {
		t.Fatalf("Allocate(users) = %+v, %v; want gid 10000", group, err)
	}
	again, err := ids.Allocate(ctx, "users", "renamed")
	if err != nil || again != group {
		t.Errorf("Allocate(users) again = %+v, %v; want the first allocation", again, err)
	}
	if _, err := ids.Allocate(ctx, "pool:p1", "pool-research"); err != nil {
		t.Fatalf("Allocate(pool): %v", err)
	}
	groups, err := ids.Groups(ctx)
	if err != nil || len(groups) != 2 || groups["pool:p1"].GIDNumber != 10001 {
		t.Errorf("Groups = %+v, %v; want users and pool:p1 (gid 10001)", groups, err)
	}
}

// The users store keeps usernames and allocated uids unique.
func TestUsers_UniqueUsernameAndUID(t *testing.T) {
	_, db := mongotest.Database(t, "access_users")
	ctx := context.Background()
	if err := authstore.EnsureUserIndexes(ctx, db); err != nil {
		t.Fatalf("EnsureUserIndexes: %v", err)
	}
	users := authstore.NewMongoUserRepo(db)
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := users.Create(ctx, &authdomain.User{ID: "u1", Username: "alice", Role: authdomain.RoleUser, UIDNumber: 10001, CreatedAt: now}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := users.Create(ctx, &authdomain.User{ID: "u2", Username: "alice", Role: authdomain.RoleUser, UIDNumber: 10002}); !errors.Is(err, authdomain.ErrUsernameTaken) {
		t.Errorf("duplicate username error = %v, want ErrUsernameTaken", err)
	}
	if err := users.Create(ctx, &authdomain.User{ID: "u3", Username: "bob", Role: authdomain.RoleUser, UIDNumber: 10001}); err == nil {
		t.Error("a second User with uid 10001 was stored")
	}
	if err := users.Create(ctx, &authdomain.User{ID: "u4", Username: "legacy", Role: authdomain.RoleAdmin}); err != nil {
		t.Fatalf("Create without a uid: %v", err)
	}
	if err := users.Create(ctx, &authdomain.User{ID: "u5", Username: "legacy2", Role: authdomain.RoleAdmin}); err != nil {
		t.Errorf("a second User without a uid was refused: %v", err)
	}
	if count, err := users.CountByRole(ctx, authdomain.RoleAdmin); err != nil || count != 2 {
		t.Errorf("CountByRole(admin) = %d, %v; want 2", count, err)
	}
}
