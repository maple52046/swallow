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
	AuthMethod  string               `json:"authMethod"`
	Memberships []MembershipOfCaller `json:"memberships"`
}

// MembershipOfCaller is one of the caller's Pool Memberships as auth-me shows it (decision 082).
type MembershipOfCaller struct {
	PoolID   string `json:"poolId"`
	PoolName string `json:"poolName"`
	SiteID   string `json:"siteId"`
	Role     string `json:"role"`
}

// MembershipReader reads a User's Pool Memberships with each Pool's name, so auth-me can say what
// the caller may do without the auth context owning memberships (the access context does). An error
// is an infrastructure failure.
type MembershipReader interface {
	MembershipsOf(ctx context.Context, userID string) ([]MembershipOfCaller, error)
}

// MeUseCase resolves the authenticated caller's identity from the user record, so a consumer
// never decodes a token to learn who it is.
type MeUseCase struct {
	users       authdomain.UserRepository
	memberships MembershipReader
}

// NewMeUseCase wires the use case; memberships is required, because a missing one would report a
// caller without the memberships it has.
func NewMeUseCase(users authdomain.UserRepository, memberships MembershipReader) *MeUseCase {
	return &MeUseCase{users: users, memberships: memberships}
}

// Execute returns the user with userID and its memberships, or authdomain.ErrUserNotFound when the
// credential is valid but its user was deleted (the contract's 404, on which a client ends its
// Session). Any other failure of the user store is returned wrapped (a 500), so an unavailable store
// never signs a user out.
func (uc *MeUseCase) Execute(ctx context.Context, userID string) (*MeOutput, error) {
	user, err := uc.users.FindByID(ctx, userID)
	if errors.Is(err, authdomain.ErrUserNotFound) {
		return nil, authdomain.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}
	memberships, err := uc.memberships.MembershipsOf(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("read memberships: %w", err)
	}
	if memberships == nil {
		memberships = []MembershipOfCaller{}
	}
	return &MeOutput{
		ID:          user.ID,
		Username:    user.Username,
		Role:        string(user.Role),
		Memberships: memberships,
	}, nil
}
