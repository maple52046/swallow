package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
	"github.com/maple52046/swallow/internal/core/store"
)

// MembershipService manages Pool Memberships, Pools' Sudo Policy defaults, the installation's
// access settings, and reads the audit log (identity-and-access.md §6). Every write runs in one
// store transaction together with its audit entry. Authorization is checked here against the
// caller's Scope, because who may change a Pool's members depends on the Pool: an admin or an owner
// of that Pool; reading them needs any role in the Pool. The access settings and the audit log are
// admin-only, which delivery enforces.
type MembershipService struct {
	tx          store.Transactor
	users       authdomain.UserRepository
	memberships accessdomain.MembershipRepository
	policies    accessdomain.PolicyRepository
	settings    accessdomain.SettingsRepository
	audit       accessdomain.AuditRepository
	pools       accessdomain.PoolLookup
	now         func() time.Time
	newID       func() string
}

// MembershipServiceDeps are the MembershipService's dependencies; every field is required.
type MembershipServiceDeps struct {
	Transactor  store.Transactor
	Users       authdomain.UserRepository
	Memberships accessdomain.MembershipRepository
	Policies    accessdomain.PolicyRepository
	Settings    accessdomain.SettingsRepository
	Audit       accessdomain.AuditRepository
	Pools       accessdomain.PoolLookup
}

// NewMembershipService wires the service, refusing a missing dependency.
func NewMembershipService(deps MembershipServiceDeps) (*MembershipService, error) {
	if deps.Transactor == nil || deps.Users == nil || deps.Memberships == nil || deps.Policies == nil ||
		deps.Settings == nil || deps.Audit == nil || deps.Pools == nil {
		return nil, errors.New("access membership service: every dependency is required")
	}
	return &MembershipService{
		tx: deps.Transactor, users: deps.Users, memberships: deps.Memberships, policies: deps.Policies,
		settings: deps.Settings, audit: deps.Audit, pools: deps.Pools,
		now: func() time.Time { return time.Now().UTC() }, newID: uuid.NewString,
	}, nil
}

// MemberView is one membership as the pool-access contract shows it: the User, the Pool Role, the
// stored overrides, and the resolved Sudo Policy.
type MemberView struct {
	Membership accessdomain.Membership
	Username   string
	Effective  accessdomain.EffectiveSudo
}

// AddMemberInput adds an existing User, named by username, to a Pool.
type AddMemberInput struct {
	Username         string
	Role             accessdomain.PoolRole
	Sudo             accessdomain.SudoSetting
	SudoPasswordless accessdomain.SudoSetting
}

// UpdateMemberInput changes a membership; a nil field is left as it is.
type UpdateMemberInput struct {
	Role             *accessdomain.PoolRole
	Sudo             *accessdomain.SudoSetting
	SudoPasswordless *accessdomain.SudoSetting
}

// Members lists a Pool's memberships with their resolved Sudo Policy. The caller needs any role in
// the Pool; an unknown Pool or one the caller cannot read is ErrNotVisible.
func (s *MembershipService) Members(ctx context.Context, scope accessdomain.Scope, poolID string) ([]MemberView, error) {
	if _, err := s.visiblePool(ctx, scope, accessdomain.Read, poolID); err != nil {
		return nil, err
	}
	memberships, err := s.memberships.ListByPool(ctx, poolID)
	if err != nil {
		return nil, err
	}
	policy, err := s.policies.Find(ctx, poolID)
	if err != nil {
		return nil, err
	}
	users, err := s.users.List(ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(users))
	for _, user := range users {
		names[user.ID] = user.Username
	}
	views := make([]MemberView, 0, len(memberships))
	for _, membership := range memberships {
		views = append(views, MemberView{Membership: *membership, Username: names[membership.UserID], Effective: accessdomain.ResolveSudo(*membership, policy)})
	}
	return views, nil
}

// AddMember adds an existing User to a Pool. The caller must be an admin or an owner of the Pool. An
// unknown username is authdomain.ErrUserNotFound and an existing membership ErrMembershipExists;
// empty overrides default to inherit.
func (s *MembershipService) AddMember(ctx context.Context, scope accessdomain.Scope, poolID string, input AddMemberInput) (*MemberView, error) {
	pool, err := s.visiblePool(ctx, scope, accessdomain.Operate, poolID)
	if err != nil {
		return nil, err
	}
	if input.Sudo == "" {
		input.Sudo = accessdomain.SudoInherit
	}
	if input.SudoPasswordless == "" {
		input.SudoPasswordless = accessdomain.SudoInherit
	}
	if !input.Role.Valid() {
		return nil, accessdomain.ErrInvalidPoolRole
	}
	if !input.Sudo.Valid() || !input.SudoPasswordless.Valid() {
		return nil, accessdomain.ErrInvalidSudoSetting
	}
	var view *MemberView
	err = s.tx.InTransaction(ctx, func(ctx context.Context) error {
		user, err := s.users.FindByUsername(ctx, strings.TrimSpace(input.Username))
		if err != nil {
			return err
		}
		now := s.now()
		membership := &accessdomain.Membership{
			ID: s.newID(), PoolID: pool.ID, UserID: user.ID, Role: input.Role,
			Sudo: input.Sudo, SudoPasswordless: input.SudoPasswordless, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.memberships.Create(ctx, membership); err != nil {
			return err
		}
		policy, err := s.policies.Find(ctx, pool.ID)
		if err != nil {
			return err
		}
		view = &MemberView{Membership: *membership, Username: user.Username, Effective: accessdomain.ResolveSudo(*membership, policy)}
		return s.record(ctx, scope, accessdomain.AuditMemberAdded, user.Username, pool.ID,
			fmt.Sprintf("%s of pool %s; sudo %s, passwordless %s", membership.Role, pool.Name, membership.Sudo, membership.SudoPasswordless))
	})
	if err != nil {
		return nil, err
	}
	return view, nil
}

// UpdateMember changes a membership's Pool Role or overrides. The caller must be an admin or an
// owner of the Pool; an unknown membership is ErrMembershipNotFound.
func (s *MembershipService) UpdateMember(ctx context.Context, scope accessdomain.Scope, poolID, userID string, input UpdateMemberInput) (*MemberView, error) {
	pool, err := s.visiblePool(ctx, scope, accessdomain.Operate, poolID)
	if err != nil {
		return nil, err
	}
	if input.Role != nil && !input.Role.Valid() {
		return nil, accessdomain.ErrInvalidPoolRole
	}
	if (input.Sudo != nil && !input.Sudo.Valid()) || (input.SudoPasswordless != nil && !input.SudoPasswordless.Valid()) {
		return nil, accessdomain.ErrInvalidSudoSetting
	}
	var view *MemberView
	err = s.tx.InTransaction(ctx, func(ctx context.Context) error {
		membership, err := s.memberships.Find(ctx, pool.ID, userID)
		if err != nil {
			return err
		}
		user, err := s.users.FindByID(ctx, userID)
		if err != nil {
			return err
		}
		var changes []string
		if input.Role != nil && *input.Role != membership.Role {
			changes = append(changes, fmt.Sprintf("role %s -> %s", membership.Role, *input.Role))
			membership.Role = *input.Role
		}
		if input.Sudo != nil && *input.Sudo != membership.Sudo {
			changes = append(changes, fmt.Sprintf("sudo %s -> %s", membership.Sudo, *input.Sudo))
			membership.Sudo = *input.Sudo
		}
		if input.SudoPasswordless != nil && *input.SudoPasswordless != membership.SudoPasswordless {
			changes = append(changes, fmt.Sprintf("passwordless %s -> %s", membership.SudoPasswordless, *input.SudoPasswordless))
			membership.SudoPasswordless = *input.SudoPasswordless
		}
		policy, err := s.policies.Find(ctx, pool.ID)
		if err != nil {
			return err
		}
		if len(changes) > 0 {
			membership.UpdatedAt = s.now()
			if err := s.memberships.Update(ctx, membership); err != nil {
				return err
			}
			if err := s.record(ctx, scope, accessdomain.AuditMemberUpdated, user.Username, pool.ID,
				fmt.Sprintf("pool %s: %s", pool.Name, strings.Join(changes, "; "))); err != nil {
				return err
			}
		}
		view = &MemberView{Membership: *membership, Username: user.Username, Effective: accessdomain.ResolveSudo(*membership, policy)}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return view, nil
}

// RemoveMember removes a User from a Pool. The caller must be an admin or an owner of the Pool (an
// owner may remove itself); an unknown membership is ErrMembershipNotFound.
func (s *MembershipService) RemoveMember(ctx context.Context, scope accessdomain.Scope, poolID, userID string) error {
	pool, err := s.visiblePool(ctx, scope, accessdomain.Operate, poolID)
	if err != nil {
		return err
	}
	return s.tx.InTransaction(ctx, func(ctx context.Context) error {
		membership, err := s.memberships.Find(ctx, pool.ID, userID)
		if err != nil {
			return err
		}
		username := userID
		if user, err := s.users.FindByID(ctx, userID); err == nil {
			username = user.Username
		} else if !errors.Is(err, authdomain.ErrUserNotFound) {
			return err
		}
		if err := s.memberships.Delete(ctx, pool.ID, userID); err != nil {
			return err
		}
		return s.record(ctx, scope, accessdomain.AuditMemberRemoved, username, pool.ID,
			fmt.Sprintf("%s of pool %s", membership.Role, pool.Name))
	})
}

// Policy returns a Pool's Sudo Policy defaults; the caller needs any role in the Pool.
func (s *MembershipService) Policy(ctx context.Context, scope accessdomain.Scope, poolID string) (accessdomain.PoolPolicy, error) {
	if _, err := s.visiblePool(ctx, scope, accessdomain.Read, poolID); err != nil {
		return accessdomain.PoolPolicy{}, err
	}
	return s.policies.Find(ctx, poolID)
}

// SetPolicy replaces a Pool's Sudo Policy defaults; the caller must be an admin or an owner of it.
func (s *MembershipService) SetPolicy(ctx context.Context, scope accessdomain.Scope, poolID string, memberSudo, passwordless bool) (accessdomain.PoolPolicy, error) {
	pool, err := s.visiblePool(ctx, scope, accessdomain.Operate, poolID)
	if err != nil {
		return accessdomain.PoolPolicy{}, err
	}
	policy := accessdomain.PoolPolicy{PoolID: pool.ID, MemberSudo: memberSudo, SudoPasswordless: passwordless, UpdatedAt: s.now(), UpdatedBy: scope.Username}
	err = s.tx.InTransaction(ctx, func(ctx context.Context) error {
		if err := s.policies.Save(ctx, policy); err != nil {
			return err
		}
		return s.record(ctx, scope, accessdomain.AuditPoolPolicyUpdated, pool.Name, pool.ID,
			fmt.Sprintf("memberSudo %t, sudoPasswordless %t", memberSudo, passwordless))
	})
	if err != nil {
		return accessdomain.PoolPolicy{}, err
	}
	return policy, nil
}

// Settings returns the installation's access settings.
func (s *MembershipService) Settings(ctx context.Context) (accessdomain.Settings, error) {
	return s.settings.Find(ctx)
}

// SetSettings replaces the installation's access settings; delivery admits only admins.
func (s *MembershipService) SetSettings(ctx context.Context, scope accessdomain.Scope, adminSudoPasswordless bool) (accessdomain.Settings, error) {
	settings := accessdomain.Settings{AdminSudoPasswordless: adminSudoPasswordless, UpdatedAt: s.now(), UpdatedBy: scope.Username}
	err := s.tx.InTransaction(ctx, func(ctx context.Context) error {
		if err := s.settings.Save(ctx, settings); err != nil {
			return err
		}
		return s.record(ctx, scope, accessdomain.AuditSettingsUpdated, "installation", "",
			fmt.Sprintf("adminSudoPasswordless %t", adminSudoPasswordless))
	})
	if err != nil {
		return accessdomain.Settings{}, err
	}
	return settings, nil
}

// Audit returns the most recent audit entries, newest first: limit of them, 100 when limit is not
// positive, and at most 500.
func (s *MembershipService) Audit(ctx context.Context, limit int) ([]accessdomain.AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	return s.audit.List(ctx, min(limit, 500))
}

// Memberships returns the caller's own memberships with each Pool's name, for auth-me.
func (s *MembershipService) Memberships(ctx context.Context, userID string) ([]MembershipOfUser, error) {
	memberships, err := s.memberships.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]MembershipOfUser, 0, len(memberships))
	for _, membership := range memberships {
		pool, found, err := s.pools.Pool(ctx, membership.PoolID)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		out = append(out, MembershipOfUser{PoolID: pool.ID, PoolName: pool.Name, Role: membership.Role})
	}
	return out, nil
}

// MembershipOfUser is one of the caller's memberships as auth-me shows it.
type MembershipOfUser struct {
	PoolID   string
	PoolName string
	Role     accessdomain.PoolRole
}

// DeletePool removes a deleted Pool's memberships and Sudo Policy; the infrastructure context calls
// it inside the transaction that deletes the Pool.
func (s *MembershipService) DeletePool(ctx context.Context, poolID string) error {
	if err := s.memberships.DeleteByPool(ctx, poolID); err != nil {
		return err
	}
	return s.policies.Delete(ctx, poolID)
}

// visiblePool loads a Pool the caller may access: an unknown Pool or one the caller cannot read is
// ErrNotVisible, one it can read but not operate (when access is Operate) ErrForbidden.
func (s *MembershipService) visiblePool(ctx context.Context, scope accessdomain.Scope, access accessdomain.Access, poolID string) (accessdomain.PoolRef, error) {
	if !scope.ReadsPool(poolID) {
		return accessdomain.PoolRef{}, accessdomain.ErrNotVisible
	}
	pool, found, err := s.pools.Pool(ctx, poolID)
	if err != nil {
		return accessdomain.PoolRef{}, err
	}
	if !found {
		return accessdomain.PoolRef{}, accessdomain.ErrNotVisible
	}
	if !scope.Allows(access, poolID) {
		return accessdomain.PoolRef{}, accessdomain.ErrForbidden
	}
	return pool, nil
}

// record writes an audit entry inside the caller's transaction.
func (s *MembershipService) record(ctx context.Context, scope accessdomain.Scope, action accessdomain.AuditAction, target, poolID, detail string) error {
	return s.audit.Record(ctx, accessdomain.AuditEntry{
		ID: s.newID(), At: s.now(), ActorID: scope.UserID, Actor: scope.Username,
		Action: action, Target: target, PoolID: poolID, Detail: detail,
	})
}
