package application

import (
	"context"
	"errors"
	"fmt"

	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
)

// MeOutput is the auth-me response body.
type MeOutput struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	// AuthMethod is set by the HTTP adapter from the request's credential (`session` or
	// `api_key`); the use case does not know how the caller authenticated.
	AuthMethod string `json:"authMethod"`
}

// MeUseCase resolves the authenticated caller's identity from the user record, so a consumer
// never decodes a token to learn who it is.
type MeUseCase struct {
	users authdomain.UserRepository
}

// NewMeUseCase wires the use case.
func NewMeUseCase(users authdomain.UserRepository) *MeUseCase {
	return &MeUseCase{users: users}
}

// Execute returns the user with userID, or authdomain.ErrUserNotFound when the credential is
// valid but its user was deleted (the contract's 404, on which a client ends its Session). Any
// other failure of the user store is returned wrapped (a 500), so an unavailable store never
// signs an operator out.
func (uc *MeUseCase) Execute(ctx context.Context, userID string) (*MeOutput, error) {
	user, err := uc.users.FindByID(ctx, userID)
	if errors.Is(err, authdomain.ErrUserNotFound) {
		return nil, authdomain.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}
	return &MeOutput{
		ID:       user.ID,
		Username: user.Username,
		Role:     string(user.Role),
	}, nil
}
