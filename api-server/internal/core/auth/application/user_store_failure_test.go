package application

import (
	"context"
	"errors"
	"testing"
	"time"

	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
	"github.com/maple52046/swallow/internal/shared/jwt"
)

// failingUsers is a user store that cannot be read.
type failingUsers struct{ memUsers }

var errStoreDown = errors.New("connection refused")

func (*failingUsers) FindByUsername(context.Context, string) (*authdomain.User, error) {
	return nil, errStoreDown
}

func (*failingUsers) FindByID(context.Context, string) (*authdomain.User, error) {
	return nil, errStoreDown
}

// DF-04: an unavailable user store is an internal failure, not a deleted user (on which the
// dashboard ends the Session) or wrong credentials. Only a real not-found is a miss.
func TestUserStoreFailureIsNotAMissingUser(t *testing.T) {
	users := &failingUsers{}
	sessions := &memSessions{byID: map[string]*authdomain.Session{}}
	service := NewSessionService(users, sessions, jwt.NewService("secret", 15*time.Minute),
		SessionConfig{RefreshTokenTTL: time.Hour, MaxAge: 24 * time.Hour})

	_, err := service.Login(context.Background(), LoginInput{Username: "alice", Password: "pw"})
	if !errors.Is(err, errStoreDown) || errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("Login() error = %v, want the store failure, not invalid credentials", err)
	}
	_, err = NewMeUseCase(users).Execute(context.Background(), "u1")
	if !errors.Is(err, errStoreDown) || errors.Is(err, authdomain.ErrUserNotFound) {
		t.Errorf("Me() error = %v, want the store failure, not a missing user", err)
	}
	if _, err := NewMeUseCase(&memUsers{users: map[string]*authdomain.User{}}).Execute(context.Background(), "gone"); !errors.Is(err, authdomain.ErrUserNotFound) {
		t.Errorf("Me() of a deleted user error = %v, want ErrUserNotFound", err)
	}
}
