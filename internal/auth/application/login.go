package application

import (
	"context"

	authdomain "github.com/AFDEAPAC/swallow/internal/auth/domain"
	"github.com/AFDEAPAC/swallow/internal/shared/jwt"
)

type LoginInput struct {
	Username string
	Password string
}

type LoginOutput struct {
	AccessToken string
}

type LoginUseCase struct {
	users  authdomain.UserRepository
	jwtSvc *jwt.Service
}

func NewLoginUseCase(users authdomain.UserRepository, jwtSvc *jwt.Service) *LoginUseCase {
	return &LoginUseCase{users: users, jwtSvc: jwtSvc}
}

func (uc *LoginUseCase) Execute(ctx context.Context, input LoginInput) (*LoginOutput, error) {
	user, err := uc.users.FindByUsername(ctx, input.Username)
	if err != nil {
		return nil, authdomain.ErrInvalidCredentials
	}

	if !user.PasswordHash.Matches(input.Password) {
		return nil, authdomain.ErrInvalidCredentials
	}

	token, err := uc.jwtSvc.Sign(user.ID, user.Username, string(user.Role))
	if err != nil {
		return nil, err
	}

	return &LoginOutput{AccessToken: token}, nil
}
