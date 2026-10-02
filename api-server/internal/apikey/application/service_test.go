package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	apikeydomain "github.com/maple52046/swallow/internal/apikey/domain"
	"github.com/maple52046/swallow/internal/shared/identity"
)

type memKeys struct {
	mu      sync.Mutex
	keys    map[string]*apikeydomain.APIKey
	touches int
}

func (m *memKeys) List(_ context.Context, userID string) ([]*apikeydomain.APIKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*apikeydomain.APIKey
	for _, k := range m.keys {
		if k.UserID == userID {
			c := *k
			out = append(out, &c)
		}
	}
	return out, nil
}

func (m *memKeys) Count(ctx context.Context, userID string) (int, error) {
	keys, _ := m.List(ctx, userID)
	return len(keys), nil
}

func (m *memKeys) Create(_ context.Context, key *apikeydomain.APIKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range m.keys {
		if k.UserID == key.UserID && strings.EqualFold(k.Name, key.Name) {
			return apikeydomain.ErrDuplicateName
		}
	}
	c := *key
	m.keys[key.ID] = &c
	return nil
}

func (m *memKeys) FindBySecretHash(_ context.Context, hash string) (*apikeydomain.APIKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range m.keys {
		if k.SecretHash == hash {
			c := *k
			return &c, nil
		}
	}
	return nil, apikeydomain.ErrKeyNotFound
}

func (m *memKeys) Delete(_ context.Context, userID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if k, ok := m.keys[id]; ok && k.UserID == userID {
		delete(m.keys, id)
		return nil
	}
	return apikeydomain.ErrKeyNotFound
}

func (m *memKeys) TouchLastUsed(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.touches++
	if k, ok := m.keys[id]; ok {
		k.LastUsedAt = &at
	}
	return nil
}

type memOwners map[string]*apikeydomain.Owner

func (m memOwners) FindOwner(_ context.Context, id string) (*apikeydomain.Owner, error) {
	if o, ok := m[id]; ok {
		return o, nil
	}
	return nil, apikeydomain.ErrOwnerNotFound
}

type keyFixture struct {
	svc    *Service
	keys   *memKeys
	owners memOwners
	clock  time.Time
}

func newKeyFixture() *keyFixture {
	f := &keyFixture{
		keys:   &memKeys{keys: map[string]*apikeydomain.APIKey{}},
		owners: memOwners{"u1": {ID: "u1", Username: "alice", Role: "admin"}},
		clock:  time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
	}
	f.svc = NewService(f.keys, f.owners)
	f.svc.now = func() time.Time { return f.clock }
	return f
}

func TestCreate_ReturnsSecretOnceAndStoresOnlyItsHash(t *testing.T) {
	f := newKeyFixture()
	key, secret, err := f.svc.Create(context.Background(), CreateInput{UserID: "u1", Name: "  ci-runner  "})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(secret, apikeydomain.SecretPrefix) || len(secret) < 40 {
		t.Errorf("secret = %q, want swk_ followed by 32 random bytes", secret)
	}
	if key.Name != "ci-runner" {
		t.Errorf("Name = %q, want trimmed ci-runner", key.Name)
	}
	if key.Prefix != secret[:12] {
		t.Errorf("Prefix = %q, want the first 12 characters of the secret", key.Prefix)
	}
	stored, _ := f.keys.FindBySecretHash(context.Background(), HashSecret(secret))
	if stored == nil || stored.SecretHash == secret {
		t.Errorf("stored key = %+v, want it found by hash and never holding the secret", stored)
	}
}

func TestCreate_Validation(t *testing.T) {
	f := newKeyFixture()
	past := f.clock.Add(-time.Minute)
	tests := []struct {
		name  string
		input CreateInput
		want  error
	}{
		{name: "empty name", input: CreateInput{UserID: "u1", Name: "   "}, want: apikeydomain.ErrInvalidName},
		{name: "name too long", input: CreateInput{UserID: "u1", Name: strings.Repeat("a", 65)}, want: apikeydomain.ErrInvalidName},
		{name: "control character", input: CreateInput{UserID: "u1", Name: "bad\nname"}, want: apikeydomain.ErrInvalidName},
		{name: "expiry in the past", input: CreateInput{UserID: "u1", Name: "old", ExpiresAt: &past}, want: apikeydomain.ErrInvalidExpiry},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := f.svc.Create(context.Background(), tc.input); !errors.Is(err, tc.want) {
				t.Errorf("Create(%+v) error = %v, want %v", tc.input, err, tc.want)
			}
		})
	}
}

func TestCreate_DuplicateNameAndLimit(t *testing.T) {
	f := newKeyFixture()
	if _, _, err := f.svc.Create(context.Background(), CreateInput{UserID: "u1", Name: "Laptop"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, _, err := f.svc.Create(context.Background(), CreateInput{UserID: "u1", Name: "laptop"}); !errors.Is(err, apikeydomain.ErrDuplicateName) {
		t.Errorf("duplicate name error = %v, want ErrDuplicateName", err)
	}
	for i := 1; i < apikeydomain.MaxKeysPerUser; i++ {
		if _, _, err := f.svc.Create(context.Background(), CreateInput{UserID: "u1", Name: "key-" + string(rune('a'+i%26)) + strings.Repeat("x", i)}); err != nil {
			t.Fatalf("Create #%d: %v", i, err)
		}
	}
	if _, _, err := f.svc.Create(context.Background(), CreateInput{UserID: "u1", Name: "one-too-many"}); !errors.Is(err, apikeydomain.ErrKeyLimit) {
		t.Errorf("Create past the limit error = %v, want ErrKeyLimit", err)
	}
}

func TestVerifyAPIKey(t *testing.T) {
	f := newKeyFixture()
	expiry := f.clock.Add(time.Hour)
	key, secret, err := f.svc.Create(context.Background(), CreateInput{UserID: "u1", Name: "ci", ExpiresAt: &expiry})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	principal, err := f.svc.VerifyAPIKey(context.Background(), secret)
	if err != nil {
		t.Fatalf("VerifyAPIKey: %v", err)
	}
	want := identity.Principal{UserID: "u1", Username: "alice", Role: "admin", Method: identity.MethodAPIKey, APIKeyID: key.ID}
	if principal == nil || *principal != want {
		t.Errorf("VerifyAPIKey = %+v, want %+v", principal, want)
	}

	if p, err := f.svc.VerifyAPIKey(context.Background(), secret+"x"); p != nil || err != nil {
		t.Errorf("VerifyAPIKey(wrong secret) = %+v, %v; want nil, nil", p, err)
	}

	f.clock = expiry
	if p, err := f.svc.VerifyAPIKey(context.Background(), secret); p != nil || err != nil {
		t.Errorf("VerifyAPIKey(expired) = %+v, %v; want nil, nil", p, err)
	}

	f.clock = expiry.Add(-time.Minute)
	delete(f.owners, "u1")
	if p, err := f.svc.VerifyAPIKey(context.Background(), secret); p != nil || err != nil {
		t.Errorf("VerifyAPIKey(owner deleted) = %+v, %v; want nil, nil", p, err)
	}
}

func TestVerifyAPIKey_ThrottlesLastUsedWrites(t *testing.T) {
	f := newKeyFixture()
	_, secret, err := f.svc.Create(context.Background(), CreateInput{UserID: "u1", Name: "ci"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for i := 0; i < 5; i++ {
		f.clock = f.clock.Add(5 * time.Second)
		if _, err := f.svc.VerifyAPIKey(context.Background(), secret); err != nil {
			t.Fatalf("VerifyAPIKey: %v", err)
		}
	}
	if f.keys.touches != 1 {
		t.Errorf("TouchLastUsed calls within one minute = %d, want 1", f.keys.touches)
	}
	f.clock = f.clock.Add(apikeydomain.LastUsedResolution)
	if _, err := f.svc.VerifyAPIKey(context.Background(), secret); err != nil {
		t.Fatalf("VerifyAPIKey: %v", err)
	}
	if f.keys.touches != 2 {
		t.Errorf("TouchLastUsed calls after a minute = %d, want 2", f.keys.touches)
	}
}

func TestDelete_IsScopedToOwner(t *testing.T) {
	f := newKeyFixture()
	key, secret, err := f.svc.Create(context.Background(), CreateInput{UserID: "u1", Name: "ci"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := f.svc.Delete(context.Background(), "someone-else", key.ID); !errors.Is(err, apikeydomain.ErrKeyNotFound) {
		t.Errorf("Delete by another user error = %v, want ErrKeyNotFound", err)
	}
	if err := f.svc.Delete(context.Background(), "u1", key.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if p, _ := f.svc.VerifyAPIKey(context.Background(), secret); p != nil {
		t.Error("a deleted key still authenticates")
	}
}
