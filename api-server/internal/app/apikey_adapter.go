package app

import (
	"context"
	"errors"

	apikeydomain "github.com/maple52046/swallow/internal/apikey/domain"
	authdomain "github.com/maple52046/swallow/internal/auth/domain"
)

// apiKeyOwners adapts the auth slice's user store to the apikey slice's OwnerDirectory port, so
// API Key verification resolves the owner's current role without the apikey slice importing the
// auth slice. A deleted user maps to ErrOwnerNotFound, which makes the key stop authenticating.
type apiKeyOwners struct {
	users authdomain.UserRepository
}

// FindOwner implements apikeydomain.OwnerDirectory; infrastructure errors are returned as-is so
// key verification answers 500 rather than treating an outage as an unknown key.
func (o apiKeyOwners) FindOwner(ctx context.Context, userID string) (*apikeydomain.Owner, error) {
	user, err := o.users.FindByID(ctx, userID)
	if errors.Is(err, authdomain.ErrUserNotFound) {
		return nil, apikeydomain.ErrOwnerNotFound
	}
	if err != nil {
		return nil, err
	}
	return &apikeydomain.Owner{ID: user.ID, Username: user.Username, Role: string(user.Role)}, nil
}
