package domain

import (
	"context"
	"errors"
	"time"
)

// SessionClient records how a Session's refresh token is delivered, which follows from the
// client kind that logged in.
type SessionClient string

const (
	// SessionClientBrowser receives its refresh token in the HttpOnly `swallow_refresh` cookie.
	SessionClientBrowser SessionClient = "browser"
	// SessionClientCLI receives its refresh token in the response body (the CLI and scripts).
	SessionClientCLI SessionClient = "cli"
)

// Session is one password sign-in of a User (glossary: Session, decision 042).
//
// swallow never stores a refresh token, only the SHA-256 hash of the current one and of the one
// it replaced (for the rotation grace). A Session is usable while it is not revoked, not past
// ExpiresAt (the idle limit, which every refresh moves forward), and not past AbsoluteExpiresAt
// (the maximum age, fixed at login). ExpiresAt never exceeds AbsoluteExpiresAt.
type Session struct {
	ID        string
	UserID    string
	Client    SessionClient
	TokenHash string
	// PreviousTokenHash is the hash replaced by the latest rotation; empty until the first
	// refresh. RotatedAt is when that rotation happened.
	PreviousTokenHash string
	RotatedAt         time.Time
	CreatedAt         time.Time
	LastUsedAt        time.Time
	ExpiresAt         time.Time
	AbsoluteExpiresAt time.Time
	// RevokedAt is set by logout or by refresh-token reuse; a revoked Session never revives.
	RevokedAt *time.Time
}

// Active reports whether the Session may still issue access tokens at now.
func (s *Session) Active(now time.Time) bool {
	return s.RevokedAt == nil && now.Before(s.ExpiresAt) && now.Before(s.AbsoluteExpiresAt)
}

// SessionRepository persists Sessions. Implementations must be safe for concurrent use and
// durable in production; in-memory implementations are for tests only.
type SessionRepository interface {
	// Create stores a new Session. TokenHash must be unique across all Sessions.
	Create(ctx context.Context, session *Session) error
	// FindByTokenHash returns the Session whose current refresh token has this hash, or
	// ErrSessionNotFound.
	FindByTokenHash(ctx context.Context, hash string) (*Session, error)
	// FindByPreviousTokenHash returns the Session whose replaced refresh token has this hash, or
	// ErrSessionNotFound.
	FindByPreviousTokenHash(ctx context.Context, hash string) (*Session, error)
	// FindByID returns the Session with this ID, or ErrSessionNotFound.
	FindByID(ctx context.Context, id string) (*Session, error)
	// Rotate replaces the current token hash only if it still equals fromHash and the Session is
	// not revoked, recording fromHash as the previous hash. It returns ErrSessionNotFound when that
	// condition no longer holds (another refresh won the race, or the Session was revoked), so two
	// concurrent refreshes can never both rotate.
	Rotate(ctx context.Context, id, fromHash, toHash string, rotatedAt, expiresAt time.Time) error
	// Touch records a use without rotation (the grace path).
	Touch(ctx context.Context, id string, usedAt time.Time) error
	// Revoke marks the Session revoked at revokedAt. Revoking an already revoked or missing
	// Session is not an error.
	Revoke(ctx context.Context, id string, revokedAt time.Time) error
}

var (
	// ErrSessionNotFound is a domain miss: no Session matches.
	ErrSessionNotFound = errors.New("session not found")
	// ErrInvalidRefreshToken means a refresh token cannot be used: unknown, expired, revoked,
	// reused after its grace, or its User is gone. Callers answer 401 without saying which.
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
)
