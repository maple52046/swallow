package application

import (
	"context"
	"errors"
	"testing"

	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// memoryCredentialRepo is an in-memory RegistryCredentialRepository enforcing registry uniqueness.
type memoryCredentialRepo struct {
	items     map[string]*softwaredomain.RegistryCredential
	passwords map[string]string
}

func newMemoryCredentialRepo() *memoryCredentialRepo {
	return &memoryCredentialRepo{items: map[string]*softwaredomain.RegistryCredential{}, passwords: map[string]string{}}
}

func (r *memoryCredentialRepo) List(context.Context) ([]*softwaredomain.RegistryCredential, error) {
	out := []*softwaredomain.RegistryCredential{}
	for _, item := range r.items {
		out = append(out, item)
	}
	return out, nil
}

func (r *memoryCredentialRepo) FindByID(_ context.Context, id string) (*softwaredomain.RegistryCredential, error) {
	if item, ok := r.items[id]; ok {
		clone := *item
		return &clone, nil
	}
	return nil, softwaredomain.ErrRegistryCredentialNotFound
}

func (r *memoryCredentialRepo) FindAuth(_ context.Context, registry string) (*softwaredomain.RegistryCredential, string, error) {
	for id, item := range r.items {
		if item.Registry == registry {
			return item, r.passwords[id], nil
		}
	}
	return nil, "", softwaredomain.ErrRegistryCredentialNotFound
}

func (r *memoryCredentialRepo) Create(_ context.Context, credential *softwaredomain.RegistryCredential, password string) error {
	for _, item := range r.items {
		if item.Registry == credential.Registry {
			return softwaredomain.ErrRegistryCredentialExists
		}
	}
	clone := *credential
	r.items[credential.ID] = &clone
	r.passwords[credential.ID] = password
	return nil
}

func (r *memoryCredentialRepo) Replace(_ context.Context, credential *softwaredomain.RegistryCredential, password string) error {
	if _, ok := r.items[credential.ID]; !ok {
		return softwaredomain.ErrRegistryCredentialNotFound
	}
	clone := *credential
	r.items[credential.ID] = &clone
	r.passwords[credential.ID] = password
	return nil
}

func (r *memoryCredentialRepo) Delete(_ context.Context, id string) error {
	if _, ok := r.items[id]; !ok {
		return softwaredomain.ErrRegistryCredentialNotFound
	}
	delete(r.items, id)
	delete(r.passwords, id)
	return nil
}

func TestRegistryCredentialCreateNormalizesAndRejectsDuplicates(t *testing.T) {
	repo := newMemoryCredentialRepo()
	service := NewRegistryCredentialService(repo)
	created, err := service.Create(context.Background(), CreateRegistryCredentialInput{
		Registry: "https://Harbor.Lab.Local/", Username: " robot ", Password: " token ", RequestedBy: "admin",
	})
	if err != nil {
		t.Fatalf("Create error = %v", err)
	}
	if created.Registry != "harbor.lab.local" || created.Username != "robot" || created.UpdatedBy != "admin" {
		t.Errorf("Create = %+v, want normalized registry, trimmed username, updatedBy admin", created)
	}
	if _, password, _ := repo.FindAuth(context.Background(), "harbor.lab.local"); password != " token " {
		t.Errorf("stored password = %q, want it exactly as given", password)
	}

	_, err = service.Create(context.Background(), CreateRegistryCredentialInput{
		Registry: "harbor.lab.local", Username: "other", Password: "x",
	})
	if !errors.Is(err, softwaredomain.ErrRegistryCredentialExists) {
		t.Errorf("Create duplicate registry error = %v, want ErrRegistryCredentialExists", err)
	}
}

func TestRegistryCredentialRequiresFields(t *testing.T) {
	service := NewRegistryCredentialService(newMemoryCredentialRepo())
	tests := []struct {
		name  string
		input CreateRegistryCredentialInput
	}{
		{"bad registry", CreateRegistryCredentialInput{Registry: "harbor/team", Username: "u", Password: "p"}},
		{"missing username", CreateRegistryCredentialInput{Registry: "harbor.lab.local", Username: " ", Password: "p"}},
		{"missing password", CreateRegistryCredentialInput{Registry: "harbor.lab.local", Username: "u"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.Create(context.Background(), tc.input); !errors.Is(err, softwaredomain.ErrInvalidRegistryCredential) {
				t.Errorf("Create(%+v) error = %v, want ErrInvalidRegistryCredential", tc.input, err)
			}
		})
	}
}

func TestRegistryCredentialReplaceKeepsRegistry(t *testing.T) {
	repo := newMemoryCredentialRepo()
	service := NewRegistryCredentialService(repo)
	created, err := service.Create(context.Background(), CreateRegistryCredentialInput{
		Registry: "harbor.lab.local", Username: "robot", Password: "old",
	})
	if err != nil {
		t.Fatalf("Create error = %v", err)
	}
	replaced, err := service.Replace(context.Background(), ReplaceRegistryCredentialInput{
		ID: created.ID, Username: "robot2", Password: "new", RequestedBy: "ops",
	})
	if err != nil {
		t.Fatalf("Replace error = %v", err)
	}
	if replaced.Registry != "harbor.lab.local" || replaced.Username != "robot2" || replaced.UpdatedBy != "ops" {
		t.Errorf("Replace = %+v, want same registry with the new username", replaced)
	}
	if _, password, _ := repo.FindAuth(context.Background(), "harbor.lab.local"); password != "new" {
		t.Errorf("password after Replace = %q, want new", password)
	}
	if _, err := service.Replace(context.Background(), ReplaceRegistryCredentialInput{ID: "missing", Username: "u", Password: "p"}); !errors.Is(err, softwaredomain.ErrRegistryCredentialNotFound) {
		t.Errorf("Replace(missing) error = %v, want ErrRegistryCredentialNotFound", err)
	}
}
