package bootstrap

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"

	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
	"github.com/maple52046/swallow/internal/core/store"
)

// EnsureAdminUser creates the installation's bootstrap admin when no User has its username, and does
// nothing otherwise, so every API start may call it. The admin gets a uidNumber in the same
// transaction (decision 082), so it is the first account of the installation's POSIX ID range on a
// fresh installation. A concurrent start that created the admin first is success.
//
// The username is the bootstrap admin's Linux login on the Servers the Directory reaches, but it is
// not checked against the username rule: an installation configures it, and refusing it here would
// keep the API from starting.
func EnsureAdminUser(ctx context.Context, tx store.Transactor, repo authdomain.UserRepository, uids authdomain.UIDAllocator, username, password string) error {
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
	now := time.Now().UTC()
	admin := &authdomain.User{
		ID:           uuid.NewString(),
		Username:     username,
		PasswordHash: hash,
		Role:         authdomain.RoleAdmin,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	err = tx.InTransaction(ctx, func(ctx context.Context) error {
		uid, err := uids.AllocateUID(ctx)
		if err != nil {
			return err
		}
		admin.UIDNumber = uid
		return repo.Create(ctx, admin)
	})
	if errors.Is(err, authdomain.ErrUsernameTaken) {
		return nil
	}
	if err != nil {
		return err
	}

	log.Printf("bootstrap: admin user '%s' created", username)
	return nil
}
