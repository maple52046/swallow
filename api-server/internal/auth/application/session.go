package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	authdomain "github.com/maple52046/swallow/internal/auth/domain"
	"github.com/maple52046/swallow/internal/shared/jwt"
)

// DefaultRotationGrace is how long a refresh token that was just replaced still yields an access
// token (decision 042). It covers concurrent refreshes from several tabs or CLI processes of one
// Session; it is not configurable because the contract documents it.
const DefaultRotationGrace = 30 * time.Second

// SessionConfig holds the Session lifetimes from config.
type SessionConfig struct {
	// RefreshTokenTTL is the idle limit: a Session not refreshed for this long ends.
	RefreshTokenTTL time.Duration
	// MaxAge is the absolute limit from login, regardless of activity.
	MaxAge time.Duration
	// RotationGrace defaults to DefaultRotationGrace when zero; tests shorten it.
	RotationGrace time.Duration
}

// LoginInput is a password sign-in. Client selects the refresh-token delivery the caller asked
// for; it is recorded on the Session.
type LoginInput struct {
	Username string
	Password string
	Client   authdomain.SessionClient
}

// Tokens is what a login or refresh hands to the caller.
type Tokens struct {
	SessionID            string
	AccessToken          string
	AccessTokenExpiresAt time.Time
	// RefreshToken is the new refresh token. It is empty on the rotation-grace path, where the
	// caller must keep the newest refresh token it already has.
	RefreshToken string
	// RefreshTokenExpiresAt is the Session's current idle expiry (never past its maximum age);
	// the browser cookie lives exactly this long.
	RefreshTokenExpiresAt time.Time
}

// LogoutInput identifies the Session to end: a refresh token wins over a SessionID taken from
// an access token. Both may be empty, in which case logout does nothing.
type LogoutInput struct {
	RefreshToken string
	SessionID    string
}

// SessionService owns the Session lifecycle: password login, refresh-token rotation with reuse
// detection, and logout (decision 042, contracts auth-login, auth-refresh, auth-logout).
//
// It never stores or logs a refresh token, only its SHA-256 hash; refresh tokens carry 256 bits
// of randomness, so a fast hash is enough. Access tokens are issued by the shared jwt.Service and
// carry the Session ID. The service is safe for concurrent use; the race between two refreshes of
// one token is settled by SessionRepository.Rotate's compare-and-swap.
type SessionService struct {
	users    authdomain.UserRepository
	sessions authdomain.SessionRepository
	tokens   *jwt.Service
	cfg      SessionConfig
	now      func() time.Time
}

// NewSessionService wires the service. cfg.RefreshTokenTTL and cfg.MaxAge must be positive
// (config validation guarantees it).
func NewSessionService(users authdomain.UserRepository, sessions authdomain.SessionRepository, tokens *jwt.Service, cfg SessionConfig) *SessionService {
	if cfg.RotationGrace <= 0 {
		cfg.RotationGrace = DefaultRotationGrace
	}
	return &SessionService{users: users, sessions: sessions, tokens: tokens, cfg: cfg, now: time.Now}
}

// Login checks the password and creates a Session. Unknown users and wrong passwords both
// return authdomain.ErrInvalidCredentials, which the contract requires to be indistinguishable.
func (s *SessionService) Login(ctx context.Context, input LoginInput) (*Tokens, error) {
	user, err := s.users.FindByUsername(ctx, input.Username)
	if err != nil {
		return nil, authdomain.ErrInvalidCredentials
	}
	if !user.PasswordHash.Matches(input.Password) {
		return nil, authdomain.ErrInvalidCredentials
	}

	refreshToken, hash, err := newRefreshToken()
	if err != nil {
		return nil, err
	}
	now := s.now()
	absolute := now.Add(s.cfg.MaxAge)
	session := &authdomain.Session{
		ID:                uuid.NewString(),
		UserID:            user.ID,
		Client:            input.Client,
		TokenHash:         hash,
		CreatedAt:         now,
		LastUsedAt:        now,
		ExpiresAt:         earliest(now.Add(s.cfg.RefreshTokenTTL), absolute),
		AbsoluteExpiresAt: absolute,
	}
	if err := s.sessions.Create(ctx, session); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return s.issue(user, session, refreshToken)
}

// Refresh exchanges a refresh token for a new access token and, normally, a new refresh token.
//
//   - The current token of an active Session rotates: the Session's idle expiry restarts and the
//     old token becomes the "previous" one.
//   - The previous token presented within the rotation grace yields an access token only
//     (Tokens.RefreshToken is empty); this is the losing side of a concurrent refresh.
//   - The previous token presented after the grace is treated as copied: the Session is revoked.
//
// Every failure — unknown, expired, revoked, reused, or a User that no longer exists — returns
// authdomain.ErrInvalidRefreshToken; repository failures are returned wrapped.
func (s *SessionService) Refresh(ctx context.Context, refreshToken string) (*Tokens, error) {
	if refreshToken == "" {
		return nil, authdomain.ErrInvalidRefreshToken
	}
	hash := hashToken(refreshToken)
	now := s.now()

	session, err := s.sessions.FindByTokenHash(ctx, hash)
	switch {
	case errors.Is(err, authdomain.ErrSessionNotFound):
		return s.refreshPrevious(ctx, hash, now)
	case err != nil:
		return nil, fmt.Errorf("find session: %w", err)
	}
	if !session.Active(now) {
		return nil, authdomain.ErrInvalidRefreshToken
	}
	user, err := s.sessionUser(ctx, session, now)
	if err != nil {
		return nil, err
	}

	next, nextHash, err := newRefreshToken()
	if err != nil {
		return nil, err
	}
	expiresAt := earliest(now.Add(s.cfg.RefreshTokenTTL), session.AbsoluteExpiresAt)
	err = s.sessions.Rotate(ctx, session.ID, hash, nextHash, now, expiresAt)
	if errors.Is(err, authdomain.ErrSessionNotFound) {
		// Another refresh rotated this token between our read and write (or it was revoked);
		// treat the token as the previous one so the grace rules decide.
		return s.refreshPrevious(ctx, hash, now)
	}
	if err != nil {
		return nil, fmt.Errorf("rotate session: %w", err)
	}
	session.ExpiresAt = expiresAt
	return s.issue(user, session, next)
}

// refreshPrevious handles a token that is no longer a Session's current refresh token.
func (s *SessionService) refreshPrevious(ctx context.Context, hash string, now time.Time) (*Tokens, error) {
	session, err := s.sessions.FindByPreviousTokenHash(ctx, hash)
	switch {
	case errors.Is(err, authdomain.ErrSessionNotFound):
		return nil, authdomain.ErrInvalidRefreshToken
	case err != nil:
		return nil, fmt.Errorf("find session: %w", err)
	}
	if !session.Active(now) {
		return nil, authdomain.ErrInvalidRefreshToken
	}
	if now.Sub(session.RotatedAt) > s.cfg.RotationGrace {
		if err := s.sessions.Revoke(ctx, session.ID, now); err != nil {
			return nil, fmt.Errorf("revoke reused session: %w", err)
		}
		// Only identifiers are logged; the token itself is never written anywhere.
		slog.Warn("refresh token reused after rotation; session revoked",
			"sessionId", session.ID, "userId", session.UserID)
		return nil, authdomain.ErrInvalidRefreshToken
	}
	user, err := s.sessionUser(ctx, session, now)
	if err != nil {
		return nil, err
	}
	if err := s.sessions.Touch(ctx, session.ID, now); err != nil {
		return nil, fmt.Errorf("touch session: %w", err)
	}
	return s.issue(user, session, "")
}

// Logout revokes the Session named by input. Unknown or already-ended Sessions are not errors,
// so logout is idempotent and reveals nothing about the credential.
func (s *SessionService) Logout(ctx context.Context, input LogoutInput) error {
	sessionID := input.SessionID
	if input.RefreshToken != "" {
		hash := hashToken(input.RefreshToken)
		session, err := s.sessions.FindByTokenHash(ctx, hash)
		if errors.Is(err, authdomain.ErrSessionNotFound) {
			session, err = s.sessions.FindByPreviousTokenHash(ctx, hash)
		}
		switch {
		case errors.Is(err, authdomain.ErrSessionNotFound):
			return nil
		case err != nil:
			return fmt.Errorf("find session: %w", err)
		}
		sessionID = session.ID
	}
	if sessionID == "" {
		return nil
	}
	if err := s.sessions.Revoke(ctx, sessionID, s.now()); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// sessionUser loads the Session's User so the access token carries its current role. A User that
// no longer exists ends the Session.
func (s *SessionService) sessionUser(ctx context.Context, session *authdomain.Session, now time.Time) (*authdomain.User, error) {
	user, err := s.users.FindByID(ctx, session.UserID)
	if errors.Is(err, authdomain.ErrUserNotFound) {
		if revokeErr := s.sessions.Revoke(ctx, session.ID, now); revokeErr != nil {
			return nil, fmt.Errorf("revoke orphaned session: %w", revokeErr)
		}
		return nil, authdomain.ErrInvalidRefreshToken
	}
	if err != nil {
		return nil, fmt.Errorf("find session user: %w", err)
	}
	return user, nil
}

func (s *SessionService) issue(user *authdomain.User, session *authdomain.Session, refreshToken string) (*Tokens, error) {
	access, expiresAt, err := s.tokens.Issue(jwt.AccessTokenInput{
		UserID: user.ID, Username: user.Username, Role: string(user.Role), SessionID: session.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("issue access token: %w", err)
	}
	return &Tokens{
		SessionID:             session.ID,
		AccessToken:           access,
		AccessTokenExpiresAt:  expiresAt,
		RefreshToken:          refreshToken,
		RefreshTokenExpiresAt: session.ExpiresAt,
	}, nil
}

// newRefreshToken returns a random refresh token and its storage hash.
func newRefreshToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func earliest(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
