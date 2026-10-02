// Package application implements the API Key use cases (api-keys contract, decision 042):
// listing, creating, and deleting a caller's keys, and verifying a presented secret for the auth
// middleware. It depends only on the domain ports; the HTTP adapter supplies the caller.
package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	apikeydomain "github.com/maple52046/swallow/internal/apikey/domain"
	"github.com/maple52046/swallow/internal/shared/identity"
)

// prefixLength is how many characters of the secret are kept for display: `swk_` plus eight
// random characters, enough to tell keys apart and far too few to guess the rest.
const prefixLength = len(apikeydomain.SecretPrefix) + 8

// CreateInput is a create request on behalf of UserID. ExpiresAt nil makes a key that never
// expires.
type CreateInput struct {
	UserID    string
	Name      string
	ExpiresAt *time.Time
}

// Service owns API Key lifecycle and verification. Authorization (admin, session-only create) is
// enforced by the HTTP routes before these methods run; every method scopes to the userID it is
// given, so one caller can never read or delete another's key. It is safe for concurrent use.
type Service struct {
	keys   apikeydomain.Repository
	owners apikeydomain.OwnerDirectory
	now    func() time.Time
}

// NewService wires the service.
func NewService(keys apikeydomain.Repository, owners apikeydomain.OwnerDirectory) *Service {
	return &Service{keys: keys, owners: owners, now: time.Now}
}

// List returns userID's keys oldest first. Expired keys are included so they can be deleted.
func (s *Service) List(ctx context.Context, userID string) ([]*apikeydomain.APIKey, error) {
	return s.keys.List(ctx, userID)
}

// Create makes a new key and returns it with its secret. The secret is returned only here and is
// never stored or logged. Errors: ErrInvalidName, ErrInvalidExpiry, ErrKeyLimit,
// ErrDuplicateName, or a wrapped repository failure.
//
// The key-count check is a read before the insert, so two concurrent creates at 49 keys can both
// succeed; the limit is a guard against runaway clients, not an exact quota.
func (s *Service) Create(ctx context.Context, input CreateInput) (*apikeydomain.APIKey, string, error) {
	name, err := apikeydomain.NormalizeName(input.Name)
	if err != nil {
		return nil, "", err
	}
	now := s.now()
	if input.ExpiresAt != nil && !input.ExpiresAt.After(now) {
		return nil, "", apikeydomain.ErrInvalidExpiry
	}
	count, err := s.keys.Count(ctx, input.UserID)
	if err != nil {
		return nil, "", fmt.Errorf("count api keys: %w", err)
	}
	if count >= apikeydomain.MaxKeysPerUser {
		return nil, "", apikeydomain.ErrKeyLimit
	}

	secret, err := newSecret()
	if err != nil {
		return nil, "", err
	}
	key := &apikeydomain.APIKey{
		ID:         uuid.NewString(),
		UserID:     input.UserID,
		Name:       name,
		Prefix:     secret[:prefixLength],
		SecretHash: HashSecret(secret),
		CreatedAt:  now,
	}
	if input.ExpiresAt != nil {
		expires := input.ExpiresAt.UTC()
		key.ExpiresAt = &expires
	}
	if err := s.keys.Create(ctx, key); err != nil {
		if errors.Is(err, apikeydomain.ErrDuplicateName) {
			return nil, "", err
		}
		return nil, "", fmt.Errorf("create api key: %w", err)
	}
	return key, secret, nil
}

// Delete revokes userID's key id immediately. It returns ErrKeyNotFound for an unknown key or one
// owned by someone else.
func (s *Service) Delete(ctx context.Context, userID, id string) error {
	return s.keys.Delete(ctx, userID, id)
}

// VerifyAPIKey implements the auth middleware's APIKeyVerifier. It returns (nil, nil) for an
// unknown, expired, or orphaned key and an error only for infrastructure failures. A successful
// verification records LastUsedAt at most once per LastUsedResolution; a failure to record it is
// logged and does not fail the request, since usage tracking must not take the API down.
func (s *Service) VerifyAPIKey(ctx context.Context, secret string) (*identity.Principal, error) {
	key, err := s.keys.FindBySecretHash(ctx, HashSecret(secret))
	if errors.Is(err, apikeydomain.ErrKeyNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find api key: %w", err)
	}
	now := s.now()
	if key.Expired(now) {
		return nil, nil
	}
	owner, err := s.owners.FindOwner(ctx, key.UserID)
	if errors.Is(err, apikeydomain.ErrOwnerNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find api key owner: %w", err)
	}
	if key.LastUsedAt == nil || now.Sub(*key.LastUsedAt) >= apikeydomain.LastUsedResolution {
		if err := s.keys.TouchLastUsed(ctx, key.ID, now); err != nil {
			slog.Warn("record api key use", "apiKeyId", key.ID, "error", err)
		}
	}
	return &identity.Principal{
		UserID:   owner.ID,
		Username: owner.Username,
		Role:     owner.Role,
		Method:   identity.MethodAPIKey,
		APIKeyID: key.ID,
	}, nil
}

// HashSecret is the storage and lookup hash of a secret. A fast hash is safe because secrets
// carry 256 bits of randomness; it must stay stable, or every existing key stops working.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func newSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate api key secret: %w", err)
	}
	return apikeydomain.SecretPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}
