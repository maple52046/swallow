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

// UserService is the admin's management of Users and every User's own password change
// (identity-and-access.md §3). Each write runs in one store transaction together with its audit
// entry, and a deletion also removes the User's memberships and, through UserCleanup, its Sessions,
// API Keys, and Access Keys. Authorization is the caller's: delivery admits only admins to every
// method except ChangePassword, which acts on the caller alone.
type UserService struct {
	reserved    map[string]bool
	tx          store.Transactor
	users       authdomain.UserRepository
	uids        authdomain.UIDAllocator
	memberships accessdomain.MembershipRepository
	cleanup     accessdomain.UserCleanup
	audit       accessdomain.AuditRepository
	now         func() time.Time
	newID       func() string
}

// UserServiceDeps are the UserService's dependencies; every field is required.
type UserServiceDeps struct {
	// ServiceAccounts are the local accounts swallow's modules create on Servers; no User may take
	// one of their names. It may be empty, never nil.
	ServiceAccounts []string
	Transactor      store.Transactor
	Users           authdomain.UserRepository
	UIDs            authdomain.UIDAllocator
	Memberships     accessdomain.MembershipRepository
	Cleanup         accessdomain.UserCleanup
	Audit           accessdomain.AuditRepository
}

// NewUserService wires the service, refusing a missing dependency.
func NewUserService(deps UserServiceDeps) (*UserService, error) {
	if deps.ServiceAccounts == nil || deps.Transactor == nil || deps.Users == nil || deps.UIDs == nil ||
		deps.Memberships == nil || deps.Cleanup == nil || deps.Audit == nil {
		return nil, errors.New("access user service: every dependency is required")
	}
	reserved := make(map[string]bool, len(deps.ServiceAccounts))
	for _, name := range deps.ServiceAccounts {
		reserved[name] = true
	}
	return &UserService{
		reserved: reserved,
		tx:       deps.Transactor, users: deps.Users, uids: deps.UIDs, memberships: deps.Memberships,
		cleanup: deps.Cleanup, audit: deps.Audit,
		now: func() time.Time { return time.Now().UTC() }, newID: uuid.NewString,
	}, nil
}

// CreateUserInput is a new User as an admin asks for it.
type CreateUserInput struct {
	Username string
	Password string
	Role     authdomain.Role
}

// UpdateUserInput changes a User; a nil field is left as it is.
type UpdateUserInput struct {
	Role     *authdomain.Role
	Password *string
}

// List returns every User, ordered by username.
func (s *UserService) List(ctx context.Context) ([]*authdomain.User, error) {
	return s.users.List(ctx)
}

// Get returns one User or authdomain.ErrUserNotFound.
func (s *UserService) Get(ctx context.Context, id string) (*authdomain.User, error) {
	return s.users.FindByID(ctx, id)
}

// Create stores a new User with a freshly allocated uidNumber. It returns ErrInvalidUsername,
// ErrReservedUsername, ErrInvalidPassword, ErrInvalidRole, or ErrUsernameTaken for a request that
// cannot be accepted; nothing is written then.
func (s *UserService) Create(ctx context.Context, actor accessdomain.Scope, input CreateUserInput) (*authdomain.User, error) {
	if err := authdomain.ValidateUsername(input.Username, s.reserved); err != nil {
		return nil, err
	}
	if err := authdomain.ValidatePassword(input.Password); err != nil {
		return nil, err
	}
	if !input.Role.Valid() {
		return nil, authdomain.ErrInvalidRole
	}
	hash, err := authdomain.HashPassword(input.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	now := s.now()
	user := &authdomain.User{ID: s.newID(), Username: input.Username, PasswordHash: hash, Role: input.Role, CreatedAt: now, UpdatedAt: now}
	err = s.tx.InTransaction(ctx, func(ctx context.Context) error {
		exists, err := s.users.ExistsByUsername(ctx, user.Username)
		if err != nil {
			return err
		}
		if exists {
			return authdomain.ErrUsernameTaken
		}
		uid, err := s.uids.AllocateUID(ctx)
		if err != nil {
			return err
		}
		user.UIDNumber = uid
		if err := s.users.Create(ctx, user); err != nil {
			return err
		}
		return s.record(ctx, actor, accessdomain.AuditUserCreated, user.Username, "", fmt.Sprintf("role %s, uid %d", user.Role, uid))
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}

// Update changes a User's role or resets its password. Demoting the last admin is ErrLastAdmin; an
// invalid role or password is ErrInvalidRole or ErrInvalidPassword; an unknown id is
// ErrUserNotFound.
func (s *UserService) Update(ctx context.Context, actor accessdomain.Scope, id string, input UpdateUserInput) (*authdomain.User, error) {
	if input.Role != nil && !input.Role.Valid() {
		return nil, authdomain.ErrInvalidRole
	}
	var hash authdomain.PasswordHash
	if input.Password != nil {
		if err := authdomain.ValidatePassword(*input.Password); err != nil {
			return nil, err
		}
		var err error
		if hash, err = authdomain.HashPassword(*input.Password); err != nil {
			return nil, fmt.Errorf("hash password: %w", err)
		}
	}
	var updated *authdomain.User
	err := s.tx.InTransaction(ctx, func(ctx context.Context) error {
		user, err := s.users.FindByID(ctx, id)
		if err != nil {
			return err
		}
		var changes []string
		if input.Role != nil && *input.Role != user.Role {
			if user.Role == authdomain.RoleAdmin {
				if err := s.requireAnotherAdmin(ctx); err != nil {
					return err
				}
			}
			changes = append(changes, fmt.Sprintf("role %s -> %s", user.Role, *input.Role))
			user.Role = *input.Role
		}
		if input.Password != nil {
			user.PasswordHash = hash
			changes = append(changes, "password reset")
		}
		if len(changes) == 0 {
			updated = user
			return nil
		}
		user.UpdatedAt = s.now()
		if err := s.users.Update(ctx, user); err != nil {
			return err
		}
		updated = user
		return s.record(ctx, actor, accessdomain.AuditUserUpdated, user.Username, "", strings.Join(changes, "; "))
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// Delete removes a User with its memberships, Sessions, API Keys, and Access Keys, in one
// transaction. Deleting oneself is ErrDeleteSelf and deleting the last admin ErrLastAdmin; the
// User's uidNumber stays allocated.
func (s *UserService) Delete(ctx context.Context, actor accessdomain.Scope, id string) error {
	if id == actor.UserID {
		return authdomain.ErrDeleteSelf
	}
	return s.tx.InTransaction(ctx, func(ctx context.Context) error {
		user, err := s.users.FindByID(ctx, id)
		if err != nil {
			return err
		}
		if user.Role == authdomain.RoleAdmin {
			if err := s.requireAnotherAdmin(ctx); err != nil {
				return err
			}
		}
		if err := s.memberships.DeleteByUser(ctx, id); err != nil {
			return err
		}
		if err := s.cleanup.RemoveUserData(ctx, id); err != nil {
			return err
		}
		if err := s.users.Delete(ctx, id); err != nil {
			return err
		}
		return s.record(ctx, actor, accessdomain.AuditUserDeleted, user.Username, "", fmt.Sprintf("uid %d stays allocated", user.UIDNumber))
	})
}

// ChangePassword replaces the caller's own password after checking its current one: the wrong
// current password is ErrWrongPassword and an invalid new one ErrInvalidPassword. It records no
// audit entry (nothing changes for anyone else), and existing Sessions stay valid.
func (s *UserService) ChangePassword(ctx context.Context, userID, current, next string) error {
	if err := authdomain.ValidatePassword(next); err != nil {
		return err
	}
	hash, err := authdomain.HashPassword(next)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	return s.tx.InTransaction(ctx, func(ctx context.Context) error {
		user, err := s.users.FindByID(ctx, userID)
		if err != nil {
			return err
		}
		if !user.PasswordHash.Matches(current) {
			return authdomain.ErrWrongPassword
		}
		user.PasswordHash = hash
		user.UpdatedAt = s.now()
		return s.users.Update(ctx, user)
	})
}

// EnsurePOSIXIdentities allocates a uidNumber for every User that has none (Users created before
// POSIX identities existed, the bootstrap admin of an upgraded installation). The API runs it at
// start; it is idempotent, and each User is updated in its own transaction.
func (s *UserService) EnsurePOSIXIdentities(ctx context.Context) error {
	users, err := s.users.List(ctx)
	if err != nil {
		return err
	}
	for _, listed := range users {
		if listed.UIDNumber > 0 {
			continue
		}
		err := s.tx.InTransaction(ctx, func(ctx context.Context) error {
			user, err := s.users.FindByID(ctx, listed.ID)
			if err != nil || user.UIDNumber > 0 {
				return err
			}
			if user.UIDNumber, err = s.uids.AllocateUID(ctx); err != nil {
				return err
			}
			user.UpdatedAt = s.now()
			return s.users.Update(ctx, user)
		})
		if err != nil && !errors.Is(err, authdomain.ErrUserNotFound) {
			return fmt.Errorf("allocate uid for %s: %w", listed.Username, err)
		}
	}
	return nil
}

// requireAnotherAdmin refuses a change that removes an admin when it is the only one.
func (s *UserService) requireAnotherAdmin(ctx context.Context) error {
	admins, err := s.users.CountByRole(ctx, authdomain.RoleAdmin)
	if err != nil {
		return err
	}
	if admins <= 1 {
		return authdomain.ErrLastAdmin
	}
	return nil
}

// record writes an audit entry inside the caller's transaction.
func (s *UserService) record(ctx context.Context, actor accessdomain.Scope, action accessdomain.AuditAction, target, poolID, detail string) error {
	return s.audit.Record(ctx, accessdomain.AuditEntry{
		ID: s.newID(), At: s.now(), ActorID: actor.UserID, Actor: actor.Username,
		Action: action, Target: target, PoolID: poolID, Detail: detail,
	})
}
