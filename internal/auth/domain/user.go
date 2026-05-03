package domain

import (
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type Role string

const (
	RoleAdmin Role = "admin"
	RoleOwner Role = "owner"
	RoleUser  Role = "user"
)

func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleOwner, RoleUser:
		return true
	}
	return false
}

type PasswordHash string

func HashPassword(raw string) (PasswordHash, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return PasswordHash(b), nil
}

func (h PasswordHash) Matches(raw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(h), []byte(raw)) == nil
}

type User struct {
	ID           string
	Username     string
	PasswordHash PasswordHash
	Role         Role
	CreatedAt    time.Time
}

var ErrUserNotFound = errors.New("user not found")
var ErrInvalidCredentials = errors.New("invalid credentials")
