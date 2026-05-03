package application

import (
	"context"

	authdomain "github.com/AFDEAPAC/swallow/internal/auth/domain"
)

type MeOutput struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type MeUseCase struct {
	users authdomain.UserRepository
}

func NewMeUseCase(users authdomain.UserRepository) *MeUseCase {
	return &MeUseCase{users: users}
}

func (uc *MeUseCase) Execute(ctx context.Context, userID string) (*MeOutput, error) {
	user, err := uc.users.FindByID(ctx, userID)
	if err != nil {
		return nil, authdomain.ErrUserNotFound
	}
	return &MeOutput{
		ID:       user.ID,
		Username: user.Username,
		Role:     string(user.Role),
	}, nil
}
