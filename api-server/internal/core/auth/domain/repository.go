package domain

import "context"

// UserRepository persists Users. Implementations must be safe for concurrent use and durable in
// production; in-memory implementations are for tests only. Every method joins the caller's store
// transaction through ctx.
//
// Implementations must keep usernames unique durably (concurrent creators cannot both win) and map
// a violation to ErrUsernameTaken, and must keep a non-zero UIDNumber unique the same way.
type UserRepository interface {
	// FindByUsername returns the User with this username, or ErrUserNotFound.
	FindByUsername(ctx context.Context, username string) (*User, error)
	// FindByID returns the User with this id, or ErrUserNotFound.
	FindByID(ctx context.Context, id string) (*User, error)
	// Create stores a new User; a taken username is ErrUsernameTaken.
	Create(ctx context.Context, user *User) error
	// ExistsByUsername reports whether a User has this username.
	ExistsByUsername(ctx context.Context, username string) (bool, error)
	// List returns every User ordered by username.
	List(ctx context.Context) ([]*User, error)
	// Update replaces the User's role, password hash, uidNumber, and updatedAt (never its id,
	// username, or createdAt); an unknown id is ErrUserNotFound.
	Update(ctx context.Context, user *User) error
	// Delete removes the User; an unknown id is ErrUserNotFound.
	Delete(ctx context.Context, id string) error
	// CountByRole counts the Users with role.
	CountByRole(ctx context.Context, role Role) (int, error)
}

// UIDAllocator hands out POSIX uidNumbers (decision 082). Allocate returns the next unused number of
// the installation's range, never one it returned before, even if the User it went to is gone.
// The range's start is recorded with the first allocation and later start values are ignored, so
// numbers already on the Servers never move. It joins the caller's transaction through ctx, so a
// rolled-back User creation also rolls back its number.
type UIDAllocator interface {
	AllocateUID(ctx context.Context) (int, error)
}
