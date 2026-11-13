package domain

import (
	"context"
	"errors"
	"time"
)

// PoolRole is what a Pool Membership lets its User do with the Pool's resources (glossary Pool
// Role). The set is closed.
type PoolRole string

const (
	// RoleOwner operates the Pool's Servers and Platforms, manages its members and Sudo Policy, and
	// always has sudo on its Servers.
	RoleOwner PoolRole = "owner"
	// RoleMember reads the Pool's resources and logs in where Host Access allows.
	RoleMember PoolRole = "member"
)

// Valid reports whether r is a Pool Role.
func (r PoolRole) Valid() bool { return r == RoleOwner || r == RoleMember }

// SudoSetting is a member's override of one Pool default: inherit the Pool's value, or force it on
// or off (glossary Sudo Policy).
type SudoSetting string

const (
	SudoInherit SudoSetting = "inherit"
	SudoOn      SudoSetting = "on"
	SudoOff     SudoSetting = "off"
)

// Valid reports whether s is a SudoSetting.
func (s SudoSetting) Valid() bool { return s == SudoInherit || s == SudoOn || s == SudoOff }

// Membership is one User's Pool Membership (glossary Pool Membership): desired state written only
// through the API by an admin or an owner of the Pool. Invariants: (PoolID, UserID) is unique; Role
// is valid; Sudo and SudoPasswordless are valid settings (the Sudo override of an owner is kept but
// ignored, because owners always have sudo).
type Membership struct {
	ID               string
	PoolID           string
	UserID           string
	Role             PoolRole
	Sudo             SudoSetting
	SudoPasswordless SudoSetting
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// PoolPolicy is a Pool's Sudo Policy defaults (glossary Sudo Policy). A Pool without a stored
// policy has the zero value: members have no sudo, and sudo asks for a password.
type PoolPolicy struct {
	PoolID           string
	MemberSudo       bool
	SudoPasswordless bool
	UpdatedAt        time.Time
	UpdatedBy        string
}

// Settings are the installation's access settings. AdminSudoPasswordless decides whether admins'
// sudo on every Server asks for their password. The zero value (a password is asked) is the default.
type Settings struct {
	AdminSudoPasswordless bool
	UpdatedAt             time.Time
	UpdatedBy             string
}

// SudoSource says which layer of the Sudo Policy decided an effective value; the values are wire
// values of the pool-access contract.
type SudoSource string

const (
	SourceRole     SudoSource = "role"
	SourceSettings SudoSource = "settings"
	SourcePool     SudoSource = "pool"
	SourceMember   SudoSource = "member"
)

// EffectiveSudo is the resolved Sudo Policy of one membership: whether the User has sudo on the
// Pool's Servers and whether sudo is passwordless, each with the layer that decided it. A
// Platform's Host Access limits may still take a member's sudo away on its nodes; that is applied
// per Server when the Directory content is computed, not here.
type EffectiveSudo struct {
	Sudo               bool
	SudoSource         SudoSource
	Passwordless       bool
	PasswordlessSource SudoSource
}

// ResolveSudo resolves a membership's Sudo Policy against its Pool's defaults: an owner always has
// sudo; a member has it when its override says on, or inherits the Pool's MemberSudo; passwordless
// follows the override, or the Pool's SudoPasswordless when the override inherits.
func ResolveSudo(membership Membership, policy PoolPolicy) EffectiveSudo {
	var out EffectiveSudo
	switch {
	case membership.Role == RoleOwner:
		out.Sudo, out.SudoSource = true, SourceRole
	case membership.Sudo == SudoOn || membership.Sudo == SudoOff:
		out.Sudo, out.SudoSource = membership.Sudo == SudoOn, SourceMember
	default:
		out.Sudo, out.SudoSource = policy.MemberSudo, SourcePool
	}
	switch membership.SudoPasswordless {
	case SudoOn, SudoOff:
		out.Passwordless, out.PasswordlessSource = membership.SudoPasswordless == SudoOn, SourceMember
	default:
		out.Passwordless, out.PasswordlessSource = policy.SudoPasswordless, SourcePool
	}
	return out
}

// MembershipRepository persists Pool Memberships. Implementations must keep (PoolID, UserID)
// unique durably and map a violation to ErrMembershipExists; every method joins the caller's store
// transaction through ctx.
type MembershipRepository interface {
	// Create stores a new membership; an existing (pool, user) pair is ErrMembershipExists.
	Create(ctx context.Context, membership *Membership) error
	// Find returns the membership of userID in poolID, or ErrMembershipNotFound.
	Find(ctx context.Context, poolID, userID string) (*Membership, error)
	// ListByPool returns the Pool's memberships.
	ListByPool(ctx context.Context, poolID string) ([]*Membership, error)
	// ListByUser returns the User's memberships.
	ListByUser(ctx context.Context, userID string) ([]*Membership, error)
	// ListAll returns every membership; the Directory projection reads them all at once.
	ListAll(ctx context.Context) ([]*Membership, error)
	// Update replaces the membership's role, overrides, and updatedAt; an unknown one is
	// ErrMembershipNotFound.
	Update(ctx context.Context, membership *Membership) error
	// Delete removes the membership of userID in poolID; an unknown one is ErrMembershipNotFound.
	Delete(ctx context.Context, poolID, userID string) error
	// DeleteByUser and DeleteByPool remove every membership of a deleted User or Pool; none is a
	// success.
	DeleteByUser(ctx context.Context, userID string) error
	DeleteByPool(ctx context.Context, poolID string) error
}

// PolicyRepository persists Pools' Sudo Policy defaults. Find of a Pool without a stored policy
// returns the zero PoolPolicy for it (not an error); every method joins the caller's transaction.
type PolicyRepository interface {
	Find(ctx context.Context, poolID string) (PoolPolicy, error)
	ListAll(ctx context.Context) ([]PoolPolicy, error)
	Save(ctx context.Context, policy PoolPolicy) error
	Delete(ctx context.Context, poolID string) error
}

// SettingsRepository persists the installation's access settings; Find without stored settings
// returns the zero Settings.
type SettingsRepository interface {
	Find(ctx context.Context) (Settings, error)
	Save(ctx context.Context, settings Settings) error
}

var (
	// ErrMembershipNotFound is a domain miss.
	ErrMembershipNotFound = errors.New("pool membership not found")
	// ErrMembershipExists means the User already belongs to the Pool.
	ErrMembershipExists = errors.New("the user is already a member of the pool")
	// ErrInvalidPoolRole means a value is not a Pool Role.
	ErrInvalidPoolRole = errors.New("invalid pool role")
	// ErrInvalidSudoSetting means a value is not inherit, on, or off.
	ErrInvalidSudoSetting = errors.New("invalid sudo setting")
	// ErrForbidden means the caller may not do what it asked; delivery answers 403.
	ErrForbidden = errors.New("forbidden")
	// ErrNotVisible means the caller may not read the resource; delivery answers 404 so a caller
	// cannot probe other Pools' resources.
	ErrNotVisible = errors.New("not visible")
	// ErrMixedPools means a Platform's members are not all placed in one Pool, which only an admin
	// may build; delivery answers 400.
	ErrMixedPools = errors.New("every member of a platform must be placed in the same pool")
)
