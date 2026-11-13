package application

import (
	"context"
	"errors"
	"testing"

	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
	"github.com/maple52046/swallow/internal/core/store/storetest"
	"github.com/maple52046/swallow/internal/shared/identity"
)

type accessFixture struct {
	users       *memUsers
	memberships *memMemberships
	policies    *memPolicies
	audit       *memAudit
	cleanup     *recordingCleanup
	authorizer  *Authorizer
	userService *UserService
	members     *MembershipService
}

// newAccessFixture: admin "root-admin" (uid 10000), user "alice" (10001), user "bob" (10002); Pool
// pool-a holds srv-a1 and srv-a2, pool-b holds srv-b1, srv-none is in no Pool; Platform plat-a is in
// pool-a; Workflow wf-a targets srv-a1, wf-mixed srv-a1 and srv-b1.
func newAccessFixture(t *testing.T) *accessFixture {
	t.Helper()
	f := &accessFixture{
		users: newMemUsers(
			&authdomain.User{ID: "u-admin", Username: "root-admin", Role: authdomain.RoleAdmin, UIDNumber: 10000},
			&authdomain.User{ID: "u-alice", Username: "alice", Role: authdomain.RoleUser, UIDNumber: 10001},
			&authdomain.User{ID: "u-bob", Username: "bob", Role: authdomain.RoleUser, UIDNumber: 10002},
		),
		memberships: &memMemberships{}, policies: &memPolicies{}, audit: &memAudit{}, cleanup: &recordingCleanup{},
	}
	var err error
	f.authorizer, err = NewAuthorizer(AuthorizerDeps{
		Users: f.users, Memberships: f.memberships,
		Servers:   fixedServerPools{"srv-a1": "pool-a", "srv-a2": "pool-a", "srv-b1": "pool-b", "srv-none": ""},
		Platforms: fixedPlatformPools{"plat-a": "pool-a", "plat-mixed": ""},
		Workflows: fixedWorkflowTargets{"wf-a": {"srv-a1"}, "wf-mixed": {"srv-a1", "srv-b1"}, "wf-none": nil},
	})
	if err != nil {
		t.Fatalf("NewAuthorizer: %v", err)
	}
	f.userService, err = NewUserService(UserServiceDeps{
		ServiceAccounts: []string{"slurm"}, Transactor: &storetest.Transactor{}, Users: f.users,
		UIDs: &counterUIDs{next: 10003}, Memberships: f.memberships, Cleanup: f.cleanup, Audit: f.audit,
	})
	if err != nil {
		t.Fatalf("NewUserService: %v", err)
	}
	f.members, err = NewMembershipService(MembershipServiceDeps{
		Transactor: &storetest.Transactor{}, Users: f.users, Memberships: f.memberships, Policies: f.policies,
		Settings: &memSettings{}, Audit: f.audit,
		Pools: fixedPools{"pool-a": {ID: "pool-a", Name: "Research"}, "pool-b": {ID: "pool-b", Name: "Prod"}},
	})
	if err != nil {
		t.Fatalf("NewMembershipService: %v", err)
	}
	return f
}

func (f *accessFixture) scope(t *testing.T, userID string) accessdomain.Scope {
	t.Helper()
	scope, err := f.authorizer.ScopeFor(context.Background(), identity.Principal{UserID: userID})
	if err != nil {
		t.Fatalf("ScopeFor(%s): %v", userID, err)
	}
	return scope
}

func TestAuthorizer_ScopeFollowsCurrentRoleAndMemberships(t *testing.T) {
	f := newAccessFixture(t)
	ctx := context.Background()
	if scope := f.scope(t, "u-admin"); !scope.Admin {
		t.Error("an admin's scope is not admin")
	}
	f.memberships.rows = append(f.memberships.rows, &accessdomain.Membership{PoolID: "pool-a", UserID: "u-alice", Role: accessdomain.RoleMember})
	alice := f.scope(t, "u-alice")
	if alice.Admin || !alice.ReadsPool("pool-a") || alice.OperatesPool("pool-a") || alice.ReadsPool("pool-b") || alice.ReadsPool("") {
		t.Errorf("member scope = %+v, want read-only pool-a", alice)
	}
	if _, err := f.authorizer.ScopeFor(ctx, identity.Principal{UserID: "gone"}); !errors.Is(err, authdomain.ErrUserNotFound) {
		t.Errorf("ScopeFor(deleted user) error = %v, want ErrUserNotFound", err)
	}
}

func TestAuthorizer_ResourceChecks(t *testing.T) {
	f := newAccessFixture(t)
	ctx := context.Background()
	f.memberships.rows = append(f.memberships.rows,
		&accessdomain.Membership{PoolID: "pool-a", UserID: "u-alice", Role: accessdomain.RoleOwner},
		&accessdomain.Membership{PoolID: "pool-b", UserID: "u-alice", Role: accessdomain.RoleMember})
	alice := f.scope(t, "u-alice")
	tests := []struct {
		name  string
		check func() error
		want  error
	}{
		{"owner operates its Pool's Servers", func() error {
			return f.authorizer.RequireServers(ctx, alice, accessdomain.Operate, []string{"srv-a1", "srv-a2"})
		}, nil},
		{"member reads but cannot operate", func() error {
			return f.authorizer.RequireServers(ctx, alice, accessdomain.Operate, []string{"srv-b1"})
		}, accessdomain.ErrForbidden},
		{"a Server in no Pool is invisible", func() error {
			return f.authorizer.RequireServers(ctx, alice, accessdomain.Read, []string{"srv-none"})
		}, accessdomain.ErrNotVisible},
		{"an unknown Server is invisible", func() error {
			return f.authorizer.RequireServers(ctx, alice, accessdomain.Read, []string{"ghost"})
		}, accessdomain.ErrNotVisible},
		{"a Platform of an owned Pool is operable", func() error {
			return f.authorizer.RequirePlatform(ctx, alice, accessdomain.Operate, "plat-a")
		}, nil},
		{"a Platform across Pools belongs to admins", func() error {
			return f.authorizer.RequirePlatform(ctx, alice, accessdomain.Read, "plat-mixed")
		}, accessdomain.ErrNotVisible},
		{"a Workflow whose every target is owned is operable", func() error {
			return f.authorizer.RequireWorkflow(ctx, alice, accessdomain.Operate, "wf-a")
		}, nil},
		{"a Workflow with a target only read cannot be operated", func() error {
			return f.authorizer.RequireWorkflow(ctx, alice, accessdomain.Operate, "wf-mixed")
		}, accessdomain.ErrForbidden},
		{"a Workflow without targets belongs to admins", func() error {
			return f.authorizer.RequireWorkflow(ctx, alice, accessdomain.Read, "wf-none")
		}, accessdomain.ErrNotVisible},
		{"Platform members in one owned Pool", func() error {
			return f.authorizer.RequirePlatformMembers(ctx, alice, []string{"srv-a1", "srv-a2"})
		}, nil},
		{"Platform members across Pools", func() error {
			return f.authorizer.RequirePlatformMembers(ctx, alice, []string{"srv-a1", "srv-b1"})
		}, accessdomain.ErrMixedPools},
		{"Platform members in a Pool only read", func() error {
			return f.authorizer.RequirePlatformMembers(ctx, alice, []string{"srv-b1"})
		}, accessdomain.ErrForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.check(); !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
		})
	}
	servers, err := f.authorizer.ReadableServers(ctx, alice)
	if err != nil || len(servers) != 3 {
		t.Errorf("ReadableServers = %v, %v; want the three Servers of pool-a and pool-b", servers, err)
	}
}

func TestUserService_CreateUpdateDelete(t *testing.T) {
	f := newAccessFixture(t)
	ctx := context.Background()
	admin := f.scope(t, "u-admin")

	if _, err := f.userService.Create(ctx, admin, CreateUserInput{Username: "Carol", Password: "longenough", Role: authdomain.RoleUser}); !errors.Is(err, authdomain.ErrInvalidUsername) {
		t.Errorf("uppercase username error = %v, want ErrInvalidUsername", err)
	}
	for _, name := range []string{"root", "ubuntu", "slurm", "swallow-users", "systemd-x"} {
		if _, err := f.userService.Create(ctx, admin, CreateUserInput{Username: name, Password: "longenough", Role: authdomain.RoleUser}); !errors.Is(err, authdomain.ErrReservedUsername) {
			t.Errorf("Create(%q) error = %v, want ErrReservedUsername", name, err)
		}
	}
	if _, err := f.userService.Create(ctx, admin, CreateUserInput{Username: "carol", Password: "short", Role: authdomain.RoleUser}); !errors.Is(err, authdomain.ErrInvalidPassword) {
		t.Errorf("short password error = %v, want ErrInvalidPassword", err)
	}
	carol, err := f.userService.Create(ctx, admin, CreateUserInput{Username: "carol", Password: "longenough", Role: authdomain.RoleUser})
	if err != nil {
		t.Fatalf("Create(carol): %v", err)
	}
	if carol.UIDNumber != 10003 || !carol.PasswordHash.Matches("longenough") {
		t.Errorf("carol = uid %d, want 10003 and her password hashed", carol.UIDNumber)
	}
	if _, err := f.userService.Create(ctx, admin, CreateUserInput{Username: "carol", Password: "longenough", Role: authdomain.RoleUser}); !errors.Is(err, authdomain.ErrUsernameTaken) {
		t.Errorf("duplicate username error = %v, want ErrUsernameTaken", err)
	}

	userRole := authdomain.RoleUser
	if _, err := f.userService.Update(ctx, admin, "u-admin", UpdateUserInput{Role: &userRole}); !errors.Is(err, authdomain.ErrLastAdmin) {
		t.Errorf("demoting the last admin error = %v, want ErrLastAdmin", err)
	}
	if err := f.userService.Delete(ctx, admin, "u-admin"); !errors.Is(err, authdomain.ErrDeleteSelf) {
		t.Errorf("deleting oneself error = %v, want ErrDeleteSelf", err)
	}

	f.memberships.rows = append(f.memberships.rows, &accessdomain.Membership{PoolID: "pool-a", UserID: carol.ID, Role: accessdomain.RoleMember})
	if err := f.userService.Delete(ctx, admin, carol.ID); err != nil {
		t.Fatalf("Delete(carol): %v", err)
	}
	if rows, _ := f.memberships.ListByUser(ctx, carol.ID); len(rows) != 0 {
		t.Error("a deleted User kept its memberships")
	}
	if len(f.cleanup.removed) != 1 || f.cleanup.removed[0] != carol.ID {
		t.Errorf("cleanup removed %v, want carol's data", f.cleanup.removed)
	}
	if next, err := f.userService.Create(ctx, admin, CreateUserInput{Username: "dave", Password: "longenough", Role: authdomain.RoleUser}); err != nil || next.UIDNumber != 10004 {
		t.Errorf("next uid = %v (%v), want 10004: a deleted User's uid is never reused", next, err)
	}
	if len(f.audit.entries) != 3 {
		t.Errorf("audit has %d entries, want create, delete, create", len(f.audit.entries))
	}
}

func TestUserService_ChangePasswordAndPOSIXBackfill(t *testing.T) {
	f := newAccessFixture(t)
	ctx := context.Background()
	hash, _ := authdomain.HashPassword("old-password")
	f.users.users["u-legacy"] = &authdomain.User{ID: "u-legacy", Username: "legacy", Role: authdomain.RoleAdmin, PasswordHash: hash}

	if err := f.userService.ChangePassword(ctx, "u-legacy", "wrong", "new-password"); !errors.Is(err, authdomain.ErrWrongPassword) {
		t.Errorf("wrong current password error = %v, want ErrWrongPassword", err)
	}
	if err := f.userService.ChangePassword(ctx, "u-legacy", "old-password", "new-password"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if err := f.userService.EnsurePOSIXIdentities(ctx); err != nil {
		t.Fatalf("EnsurePOSIXIdentities: %v", err)
	}
	legacy, _ := f.users.FindByID(ctx, "u-legacy")
	if legacy.UIDNumber != 10003 || !legacy.PasswordHash.Matches("new-password") {
		t.Errorf("legacy = uid %d, want 10003 with the new password", legacy.UIDNumber)
	}
}

func TestMembershipService_OwnersManageTheirPool(t *testing.T) {
	f := newAccessFixture(t)
	ctx := context.Background()
	f.memberships.rows = append(f.memberships.rows,
		&accessdomain.Membership{PoolID: "pool-a", UserID: "u-alice", Role: accessdomain.RoleOwner, Sudo: accessdomain.SudoInherit, SudoPasswordless: accessdomain.SudoInherit})
	alice := f.scope(t, "u-alice")

	view, err := f.members.AddMember(ctx, alice, "pool-a", AddMemberInput{Username: "bob", Role: accessdomain.RoleMember})
	if err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if view.Effective.Sudo || view.Effective.SudoSource != accessdomain.SourcePool {
		t.Errorf("new member's sudo = %+v, want none, from the Pool", view.Effective)
	}
	if _, err := f.members.AddMember(ctx, alice, "pool-a", AddMemberInput{Username: "bob", Role: accessdomain.RoleMember}); !errors.Is(err, accessdomain.ErrMembershipExists) {
		t.Errorf("second membership error = %v, want ErrMembershipExists", err)
	}
	if _, err := f.members.AddMember(ctx, alice, "pool-b", AddMemberInput{Username: "bob", Role: accessdomain.RoleMember}); !errors.Is(err, accessdomain.ErrNotVisible) {
		t.Errorf("adding to a Pool alice is not in: error = %v, want ErrNotVisible", err)
	}
	bob := f.scope(t, "u-bob")
	if _, err := f.members.AddMember(ctx, bob, "pool-a", AddMemberInput{Username: "root-admin", Role: accessdomain.RoleOwner}); !errors.Is(err, accessdomain.ErrForbidden) {
		t.Errorf("a member adding to its Pool: error = %v, want ErrForbidden", err)
	}

	if _, err := f.members.SetPolicy(ctx, alice, "pool-a", true, true); err != nil {
		t.Fatalf("SetPolicy: %v", err)
	}
	off := accessdomain.SudoOff
	view, err = f.members.UpdateMember(ctx, alice, "pool-a", "u-bob", UpdateMemberInput{SudoPasswordless: &off})
	if err != nil {
		t.Fatalf("UpdateMember: %v", err)
	}
	if !view.Effective.Sudo || view.Effective.SudoSource != accessdomain.SourcePool ||
		view.Effective.Passwordless || view.Effective.PasswordlessSource != accessdomain.SourceMember {
		t.Errorf("bob's sudo = %+v, want sudo from the Pool and a password from his override", view.Effective)
	}
	views, err := f.members.Members(ctx, bob, "pool-a")
	if err != nil || len(views) != 2 {
		t.Fatalf("Members as a member = %d, %v; want both memberships", len(views), err)
	}
	for _, v := range views {
		if v.Membership.Role == accessdomain.RoleOwner && (!v.Effective.Sudo || v.Effective.SudoSource != accessdomain.SourceRole) {
			t.Errorf("owner's sudo = %+v, want always, from the role", v.Effective)
		}
	}
	if err := f.members.RemoveMember(ctx, alice, "pool-a", "u-bob"); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	if err := f.members.RemoveMember(ctx, alice, "pool-a", "u-bob"); !errors.Is(err, accessdomain.ErrMembershipNotFound) {
		t.Errorf("removing again error = %v, want ErrMembershipNotFound", err)
	}
	mine, err := f.members.Memberships(ctx, "u-alice")
	if err != nil || len(mine) != 1 || mine[0].PoolName != "Research" || mine[0].Role != accessdomain.RoleOwner {
		t.Errorf("alice's memberships = %+v, %v; want owner of Research", mine, err)
	}
	if len(f.audit.entries) != 4 {
		t.Errorf("audit has %d entries, want add, policy, update, remove", len(f.audit.entries))
	}
}
