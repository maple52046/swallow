package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// RegistryCredentialService manages Registry Credentials (decision 044): validation, registry
// normalization, and the write-only password. It never contacts a registry or a host; a wrong
// credential is reported by the registry on the next pull.
//
// Callers are admin-authorized by the delivery layer. Errors are ErrInvalidRegistryCredential
// (400), ErrRegistryCredentialExists (409), ErrRegistryCredentialNotFound (404), or a storage
// failure.
type RegistryCredentialService struct {
	repo softwaredomain.RegistryCredentialRepository
	now  func() time.Time
}

// NewRegistryCredentialService wires the sealed credential repository.
func NewRegistryCredentialService(repo softwaredomain.RegistryCredentialRepository) *RegistryCredentialService {
	return &RegistryCredentialService{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

// List returns every credential ordered by registry, without passwords.
func (s *RegistryCredentialService) List(ctx context.Context) ([]*softwaredomain.RegistryCredential, error) {
	return s.repo.List(ctx)
}

// CreateRegistryCredentialInput is the accepted intent for POST /software/docker-ce/registry-credentials.
type CreateRegistryCredentialInput struct {
	Registry    string
	Username    string
	Password    string
	RequestedBy string
}

// Create normalizes the registry, requires a username and password, and stores the credential.
func (s *RegistryCredentialService) Create(ctx context.Context, input CreateRegistryCredentialInput) (*softwaredomain.RegistryCredential, error) {
	registry, err := softwaredomain.NormalizeRegistry(input.Registry)
	if err != nil {
		return nil, err
	}
	username, err := requireCredentialSecret(input.Username, input.Password)
	if err != nil {
		return nil, err
	}
	now := s.now()
	credential := &softwaredomain.RegistryCredential{
		ID: uuid.NewString(), Registry: registry, Username: username,
		CreatedAt: now, UpdatedAt: now, UpdatedBy: input.RequestedBy,
	}
	if err := s.repo.Create(ctx, credential, input.Password); err != nil {
		return nil, err
	}
	return credential, nil
}

// ReplaceRegistryCredentialInput is the accepted intent for PUT /software/docker-ce/registry-credentials/{id}.
type ReplaceRegistryCredentialInput struct {
	ID          string
	Username    string
	Password    string
	RequestedBy string
}

// Replace overwrites the username and password of an existing credential; its registry is fixed.
func (s *RegistryCredentialService) Replace(ctx context.Context, input ReplaceRegistryCredentialInput) (*softwaredomain.RegistryCredential, error) {
	username, err := requireCredentialSecret(input.Username, input.Password)
	if err != nil {
		return nil, err
	}
	credential, err := s.repo.FindByID(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	credential.Username = username
	credential.UpdatedAt = s.now()
	credential.UpdatedBy = input.RequestedBy
	if err := s.repo.Replace(ctx, credential, input.Password); err != nil {
		return nil, err
	}
	return credential, nil
}

// Delete removes one credential; later pulls from its registry are anonymous.
func (s *RegistryCredentialService) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

// requireCredentialSecret returns the trimmed username after checking both fields are present. The
// password is used as given: registry tokens can legitimately contain surrounding characters an
// operator pasted on purpose, and trimming would silently store a different secret.
func requireCredentialSecret(username, password string) (string, error) {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" {
		return "", fmt.Errorf("%w: username is required", softwaredomain.ErrInvalidRegistryCredential)
	}
	if password == "" {
		return "", fmt.Errorf("%w: password is required", softwaredomain.ErrInvalidRegistryCredential)
	}
	return trimmed, nil
}
