// Package domain defines swallow's API Key model (glossary API Key, decision 042): a named,
// long-lived secret a User creates so non-interactive clients call the API as that User.
//
// swallow keeps only a one-way hash of each secret and a short display prefix; the secret exists
// in plaintext only in the create response. This package holds the entity, its invariants, the
// persistence and owner-lookup ports, and domain errors. It must not import MongoDB, Fiber, or
// the auth slice: owners are reached through the OwnerDirectory port.
package domain

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// SecretPrefix starts every API Key secret. The auth middleware routes bearer values on it,
	// and it lets secret scanners recognise leaked keys.
	SecretPrefix = "swk_"
	// MaxNameLength bounds a key name, in characters.
	MaxNameLength = 64
	// MaxKeysPerUser bounds how many keys one User may hold, so a runaway script cannot fill the
	// collection.
	MaxKeysPerUser = 50
	// LastUsedResolution is the most often LastUsedAt is written, so verifying a key does not
	// cost a database write on every request.
	LastUsedResolution = time.Minute
)

// APIKey is one API Key. SecretHash is the hex SHA-256 of the full secret and is unique across
// the installation; Prefix is the first characters of the secret, safe to display and useless
// for authentication. ExpiresAt nil means the key never expires.
type APIKey struct {
	ID         string
	UserID     string
	Name       string
	Prefix     string
	SecretHash string
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
}

// Expired reports whether the key no longer authenticates at now.
func (k *APIKey) Expired(now time.Time) bool {
	return k.ExpiresAt != nil && !now.Before(*k.ExpiresAt)
}

// NormalizeName trims name and checks the contract's rules: 1–64 characters, no control
// characters. It returns ErrInvalidName otherwise.
func NormalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxNameLength {
		return "", ErrInvalidName
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", ErrInvalidName
		}
	}
	return name, nil
}

// Repository persists API Keys. Implementations must be safe for concurrent use and durable in
// production; in-memory implementations are for tests only.
type Repository interface {
	// List returns userID's keys oldest first.
	List(ctx context.Context, userID string) ([]*APIKey, error)
	// Count returns how many keys userID holds.
	Count(ctx context.Context, userID string) (int, error)
	// Create stores key. A name already used by the same User (compared case-insensitively)
	// returns ErrDuplicateName; uniqueness is enforced by the store, not by a prior read.
	Create(ctx context.Context, key *APIKey) error
	// FindBySecretHash returns the key whose secret hashes to hash, or ErrKeyNotFound.
	FindBySecretHash(ctx context.Context, hash string) (*APIKey, error)
	// Delete removes userID's key id, or returns ErrKeyNotFound when no such key belongs to
	// userID (another user's key is a miss, not a permission error).
	Delete(ctx context.Context, userID, id string) error
	// TouchLastUsed sets LastUsedAt to at unless it was already set within LastUsedResolution
	// before at; skipping is not an error.
	TouchLastUsed(ctx context.Context, id string, at time.Time) error
}

// Owner is the identity an API Key acts as.
type Owner struct {
	ID       string
	Username string
	Role     string
}

// OwnerDirectory resolves a key's owning User at verification time, so a key always carries its
// owner's current role and stops working when the owner is deleted. It returns ErrOwnerNotFound
// for a deleted User.
type OwnerDirectory interface {
	FindOwner(ctx context.Context, userID string) (*Owner, error)
}

var (
	// ErrKeyNotFound is a domain miss: no such key for this caller.
	ErrKeyNotFound = errors.New("api key not found")
	// ErrDuplicateName means the caller already has a key with this name.
	ErrDuplicateName = errors.New("api key name already in use")
	// ErrInvalidName means the name breaks NormalizeName's rules.
	ErrInvalidName = errors.New("api key name must be 1-64 characters without control characters")
	// ErrInvalidExpiry means the requested expiry is not in the future.
	ErrInvalidExpiry = errors.New("api key expiry must be in the future")
	// ErrKeyLimit means the caller already holds MaxKeysPerUser keys.
	ErrKeyLimit = errors.New("api key limit reached")
	// ErrOwnerNotFound means the key's owning User no longer exists.
	ErrOwnerNotFound = errors.New("api key owner not found")
)
