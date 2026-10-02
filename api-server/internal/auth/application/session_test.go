package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	authdomain "github.com/maple52046/swallow/internal/auth/domain"
	"github.com/maple52046/swallow/internal/shared/jwt"
)

type memUsers struct{ users map[string]*authdomain.User }

func (m *memUsers) FindByUsername(_ context.Context, username string) (*authdomain.User, error) {
	for _, u := range m.users {
		if u.Username == username {
			return u, nil
		}
	}
	return nil, authdomain.ErrUserNotFound
}

func (m *memUsers) FindByID(_ context.Context, id string) (*authdomain.User, error) {
	if u, ok := m.users[id]; ok {
		return u, nil
	}
	return nil, authdomain.ErrUserNotFound
}

func (m *memUsers) Create(context.Context, *authdomain.User) error { return nil }

func (m *memUsers) ExistsByUsername(context.Context, string) (bool, error) { return false, nil }

// memSessions keeps the production compare-and-swap semantics of Rotate.
type memSessions struct {
	mu   sync.Mutex
	byID map[string]*authdomain.Session
}

func (m *memSessions) Create(_ context.Context, s *authdomain.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := *s
	m.byID[s.ID] = &c
	return nil
}

func (m *memSessions) match(f func(*authdomain.Session) bool) (*authdomain.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.byID {
		if f(s) {
			c := *s
			return &c, nil
		}
	}
	return nil, authdomain.ErrSessionNotFound
}

func (m *memSessions) FindByTokenHash(_ context.Context, h string) (*authdomain.Session, error) {
	return m.match(func(s *authdomain.Session) bool { return s.TokenHash == h })
}

func (m *memSessions) FindByPreviousTokenHash(_ context.Context, h string) (*authdomain.Session, error) {
	return m.match(func(s *authdomain.Session) bool { return s.PreviousTokenHash != "" && s.PreviousTokenHash == h })
}

func (m *memSessions) FindByID(_ context.Context, id string) (*authdomain.Session, error) {
	return m.match(func(s *authdomain.Session) bool { return s.ID == id })
}

func (m *memSessions) Rotate(_ context.Context, id, from, to string, at, exp time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.byID[id]
	if !ok || s.TokenHash != from || s.RevokedAt != nil {
		return authdomain.ErrSessionNotFound
	}
	s.PreviousTokenHash, s.TokenHash, s.RotatedAt, s.LastUsedAt, s.ExpiresAt = from, to, at, at, exp
	return nil
}

func (m *memSessions) Touch(context.Context, string, time.Time) error { return nil }

func (m *memSessions) Revoke(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.byID[id]; ok && s.RevokedAt == nil {
		s.RevokedAt = &at
	}
	return nil
}

type sessionFixture struct {
	svc      *SessionService
	users    *memUsers
	sessions *memSessions
	clock    time.Time
}

func newSessionFixture(t *testing.T, cfg SessionConfig) *sessionFixture {
	t.Helper()
	hash, err := authdomain.HashPassword("pw")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	f := &sessionFixture{
		users:    &memUsers{users: map[string]*authdomain.User{"u1": {ID: "u1", Username: "alice", PasswordHash: hash, Role: authdomain.RoleAdmin}}},
		sessions: &memSessions{byID: map[string]*authdomain.Session{}},
		clock:    time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
	}
	f.svc = NewSessionService(f.users, f.sessions, jwt.NewService("secret", 15*time.Minute), cfg)
	f.svc.now = func() time.Time { return f.clock }
	return f
}

func (f *sessionFixture) login(t *testing.T) *Tokens {
	t.Helper()
	tokens, err := f.svc.Login(context.Background(), LoginInput{Username: "alice", Password: "pw", Client: authdomain.SessionClientCLI})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	return tokens
}

func TestRefresh_IdleLimitEndsSession(t *testing.T) {
	f := newSessionFixture(t, SessionConfig{RefreshTokenTTL: time.Hour, MaxAge: 24 * time.Hour})
	tokens := f.login(t)

	f.clock = f.clock.Add(59 * time.Minute)
	refreshed, err := f.svc.Refresh(context.Background(), tokens.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh inside the idle limit: %v", err)
	}
	// The refresh restarted the idle limit, so another 59 minutes later it still works.
	f.clock = f.clock.Add(59 * time.Minute)
	if _, err := f.svc.Refresh(context.Background(), refreshed.RefreshToken); err != nil {
		t.Fatalf("Refresh after the idle limit restarted: %v", err)
	}

	idle := f.login(t)
	f.clock = f.clock.Add(61 * time.Minute)
	if _, err := f.svc.Refresh(context.Background(), idle.RefreshToken); !errors.Is(err, authdomain.ErrInvalidRefreshToken) {
		t.Fatalf("Refresh after the idle limit error = %v, want ErrInvalidRefreshToken", err)
	}
}

func TestRefresh_MaxAgeCapsSessionDespiteActivity(t *testing.T) {
	f := newSessionFixture(t, SessionConfig{RefreshTokenTTL: time.Hour, MaxAge: 2 * time.Hour})
	tokens := f.login(t)

	token := tokens.RefreshToken
	for i := 0; i < 3; i++ {
		f.clock = f.clock.Add(50 * time.Minute)
		next, err := f.svc.Refresh(context.Background(), token)
		if i < 2 {
			if err != nil {
				t.Fatalf("Refresh %d before max age: %v", i, err)
			}
			if !next.RefreshTokenExpiresAt.After(f.clock) || next.RefreshTokenExpiresAt.After(f.clock.Add(time.Hour)) {
				t.Errorf("RefreshTokenExpiresAt = %v, want within the idle limit", next.RefreshTokenExpiresAt)
			}
			token = next.RefreshToken
			continue
		}
		if !errors.Is(err, authdomain.ErrInvalidRefreshToken) {
			t.Fatalf("Refresh past max age error = %v, want ErrInvalidRefreshToken", err)
		}
	}
}

func TestRefresh_DeletedUserEndsSession(t *testing.T) {
	f := newSessionFixture(t, SessionConfig{RefreshTokenTTL: time.Hour, MaxAge: 24 * time.Hour})
	tokens := f.login(t)
	delete(f.users.users, "u1")

	if _, err := f.svc.Refresh(context.Background(), tokens.RefreshToken); !errors.Is(err, authdomain.ErrInvalidRefreshToken) {
		t.Fatalf("Refresh for a deleted user error = %v, want ErrInvalidRefreshToken", err)
	}
	session, err := f.sessions.FindByID(context.Background(), tokens.SessionID)
	if err != nil || session.RevokedAt == nil {
		t.Errorf("session after deleted-user refresh = %+v, %v; want revoked", session, err)
	}
}

func TestLogin_StoresOnlyTokenHash(t *testing.T) {
	f := newSessionFixture(t, SessionConfig{RefreshTokenTTL: time.Hour, MaxAge: 24 * time.Hour})
	tokens := f.login(t)

	session, err := f.sessions.FindByID(context.Background(), tokens.SessionID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if session.TokenHash == tokens.RefreshToken || session.TokenHash != hashToken(tokens.RefreshToken) {
		t.Errorf("stored TokenHash = %q, want the SHA-256 of the refresh token, never the token", session.TokenHash)
	}
	if session.Client != authdomain.SessionClientCLI {
		t.Errorf("Client = %q, want cli", session.Client)
	}
}

func TestLogin_WrongPasswordAndUnknownUserAreIndistinguishable(t *testing.T) {
	f := newSessionFixture(t, SessionConfig{RefreshTokenTTL: time.Hour, MaxAge: 24 * time.Hour})
	for _, in := range []LoginInput{{Username: "alice", Password: "nope"}, {Username: "bob", Password: "pw"}} {
		if _, err := f.svc.Login(context.Background(), in); !errors.Is(err, authdomain.ErrInvalidCredentials) {
			t.Errorf("Login(%s) error = %v, want ErrInvalidCredentials", in.Username, err)
		}
	}
}
