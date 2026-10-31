package domain

import "context"

type UserRepository interface {
	FindByUsername(ctx context.Context, username string) (*User, error)
	FindByID(ctx context.Context, id string) (*User, error)
	Create(ctx context.Context, user *User) error
	ExistsByUsername(ctx context.Context, username string) (bool, error)
}
