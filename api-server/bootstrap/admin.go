package bootstrap

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	authdomain "github.com/AFDEAPAC/swallow/internal/auth/domain"
)

func EnsureAdminUser(ctx context.Context, repo authdomain.UserRepository, username, password string) error {
	exists, err := repo.ExistsByUsername(ctx, username)
	if err != nil {
		return err
	}
	if exists {
		log.Printf("bootstrap: admin user '%s' already exists, skipping", username)
		return nil
	}

	hash, err := authdomain.HashPassword(password)
	if err != nil {
		return err
	}

	admin := &authdomain.User{
		ID:           uuid.NewString(),
		Username:     username,
		PasswordHash: hash,
		Role:         authdomain.RoleAdmin,
		CreatedAt:    time.Now().UTC(),
	}

	if err := repo.Create(ctx, admin); err != nil {
		return err
	}

	log.Printf("bootstrap: admin user '%s' created", username)
	return nil
}
