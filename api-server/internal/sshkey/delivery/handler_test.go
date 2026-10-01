package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/shared/jwt"
	"github.com/maple52046/swallow/internal/shared/middleware"
	"github.com/maple52046/swallow/internal/sshkey/application"
	sshkeydomain "github.com/maple52046/swallow/internal/sshkey/domain"
	sshkeyinfra "github.com/maple52046/swallow/internal/sshkey/infra"
)

// memoryKeys is a minimal in-memory KeyRepository for exercising the HTTP contract; uniqueness is
// by fingerprint only, which is all these tests rely on.
type memoryKeys struct {
	mu   sync.Mutex
	keys []*sshkeydomain.SSHKey
}

func (m *memoryKeys) List(_ context.Context, owner string) ([]*sshkeydomain.SSHKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*sshkeydomain.SSHKey
	for _, key := range m.keys {
		if key.Purpose == sshkeydomain.PurposeDeployment || key.OwnerUserID == owner {
			out = append(out, key)
		}
	}
	return out, nil
}

func (m *memoryKeys) ListAll(context.Context) ([]*sshkeydomain.SSHKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*sshkeydomain.SSHKey(nil), m.keys...), nil
}

func (m *memoryKeys) FindByID(_ context.Context, id string) (*sshkeydomain.SSHKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, key := range m.keys {
		if key.ID == id {
			return key, nil
		}
	}
	return nil, sshkeydomain.ErrKeyNotFound
}

func (m *memoryKeys) FindDeployment(context.Context) (*sshkeydomain.SSHKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, key := range m.keys {
		if key.Purpose == sshkeydomain.PurposeDeployment {
			return key, nil
		}
	}
	return nil, sshkeydomain.ErrDeploymentKeyNotFound
}

func (m *memoryKeys) insert(key *sshkeydomain.SSHKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.keys {
		if existing.Fingerprint == key.Fingerprint {
			return sshkeydomain.ErrDuplicateKey
		}
	}
	m.keys = append(m.keys, key)
	return nil
}

func (m *memoryKeys) CreateAccess(_ context.Context, key *sshkeydomain.SSHKey) error {
	return m.insert(key)
}

func (m *memoryKeys) CreateDeployment(_ context.Context, key *sshkeydomain.SSHKey, _ string) error {
	return m.insert(key)
}

func (m *memoryKeys) ReplaceDeployment(context.Context, *sshkeydomain.SSHKey, string) error {
	return nil
}

func (m *memoryKeys) DeploymentPrivateKey(context.Context) (string, error) {
	return "", sshkeydomain.ErrDeploymentKeyNotFound
}

func (m *memoryKeys) DeleteAccess(_ context.Context, owner, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, key := range m.keys {
		if key.ID == id && key.OwnerUserID == owner {
			m.keys = append(m.keys[:i], m.keys[i+1:]...)
			return nil
		}
	}
	return sshkeydomain.ErrKeyNotFound
}

type memorySyncs struct{}

func (memorySyncs) List(context.Context) ([]sshkeydomain.ProviderSync, error) { return nil, nil }
func (memorySyncs) Upsert(context.Context, sshkeydomain.ProviderSync) error   { return nil }
func (memorySyncs) Delete(context.Context, string, string) error              { return nil }

type noProvisioners struct{}

func (noProvisioners) ListProvisioners(context.Context) ([]sshkeydomain.Provisioner, error) {
	return nil, nil
}

func (noProvisioners) Registrar(context.Context, string) (sshkeydomain.KeyRegistrar, bool, error) {
	return nil, false, nil
}

type fixture struct {
	app    *fiber.App
	jwtSvc *jwt.Service
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	service := application.NewService(&memoryKeys{}, memorySyncs{}, sshkeyinfra.NewKeyMaterial(), noProvisioners{})
	if _, _, err := service.EnsureDeploymentKey(context.Background()); err != nil {
		t.Fatalf("EnsureDeploymentKey: %v", err)
	}
	jwtSvc := jwt.NewService("test-secret", time.Hour)
	handler := NewSSHKeyHandler(service)
	app := fiber.New()
	group := app.Group("/api/v1/ssh-keys", middleware.Auth(jwtSvc), middleware.AdminOnly())
	group.Get("/", handler.List)
	group.Post("/", handler.Import)
	group.Post("/generate", handler.Generate)
	group.Post("/sync", handler.Sync)
	group.Put("/deployment", handler.ReplaceDeployment)
	group.Post("/deployment/regenerate", handler.RegenerateDeployment)
	group.Get("/:keyId", handler.Get)
	group.Delete("/:keyId", handler.Delete)
	return fixture{app: app, jwtSvc: jwtSvc}
}

func (f fixture) do(t *testing.T, method, path, userID, body string) (*http.Response, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		token, err := f.jwtSvc.Sign(userID, userID, "admin")
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := f.app.Test(req, -1)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}

func TestSSHKeysRequireAuthentication(t *testing.T) {
	f := newFixture(t)
	if resp, _ := f.do(t, http.MethodGet, "/api/v1/ssh-keys/", "", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET without a token = %d, want 401", resp.StatusCode)
	}
}

func TestGenerateReturnsPrivateKeyOnceAndScopesAccessKeysToCaller(t *testing.T) {
	f := newFixture(t)

	resp, raw := f.do(t, http.MethodPost, "/api/v1/ssh-keys/generate", "admin-1", `{"name":"jumpbox"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /generate = %d %s, want 201", resp.StatusCode, raw)
	}
	if got := resp.Header.Get(fiber.HeaderCacheControl); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store for a response carrying a private key", got)
	}
	var generated application.GeneratedAccessKey
	if err := json.Unmarshal(raw, &generated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(generated.PrivateKey, "-----BEGIN OPENSSH PRIVATE KEY-----") || generated.Key.Purpose != "access" {
		t.Errorf("generate response = %+v, want an access key and an OpenSSH private key", generated.Key)
	}

	_, raw = f.do(t, http.MethodGet, "/api/v1/ssh-keys/", "admin-1", "")
	var mine []application.KeyItem
	if err := json.Unmarshal(raw, &mine); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(mine) != 2 || mine[0].Purpose != "deployment" || mine[1].ID != generated.Key.ID {
		t.Errorf("owner list = %+v, want the deployment key then the generated access key", mine)
	}
	if strings.Contains(string(raw), "PRIVATE KEY") {
		t.Errorf("a list response must never carry private key material")
	}

	_, raw = f.do(t, http.MethodGet, "/api/v1/ssh-keys/", "admin-2", "")
	var theirs []application.KeyItem
	_ = json.Unmarshal(raw, &theirs)
	if len(theirs) != 1 {
		t.Errorf("another user's list = %+v, want only the deployment key", theirs)
	}
	if resp, _ := f.do(t, http.MethodDelete, "/api/v1/ssh-keys/"+generated.Key.ID, "admin-2", ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("deleting another user's key = %d, want 404", resp.StatusCode)
	}

	// The single-key read follows the same scoping: the owner sees it, another user gets 404.
	resp, raw = f.do(t, http.MethodGet, "/api/v1/ssh-keys/"+generated.Key.ID, "admin-1", "")
	var one application.KeyItem
	if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &one) != nil || one.ID != generated.Key.ID {
		t.Errorf("GET own key = %d %s, want 200 with the key", resp.StatusCode, raw)
	}
	if resp, _ := f.do(t, http.MethodGet, "/api/v1/ssh-keys/"+generated.Key.ID, "admin-2", ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET another user's key = %d, want 404", resp.StatusCode)
	}
	if resp, _ := f.do(t, http.MethodGet, "/api/v1/ssh-keys/"+mine[0].ID, "admin-2", ""); resp.StatusCode != http.StatusOK {
		t.Errorf("GET the deployment key as any admin = %d, want 200", resp.StatusCode)
	}
}

func TestDeploymentKeyCannotBeDeletedAndBadKeysAreRejected(t *testing.T) {
	f := newFixture(t)
	_, raw := f.do(t, http.MethodGet, "/api/v1/ssh-keys/", "admin-1", "")
	var keys []application.KeyItem
	if err := json.Unmarshal(raw, &keys); err != nil || len(keys) != 1 {
		t.Fatalf("list = %s, want the bootstrapped deployment key", raw)
	}

	if resp, _ := f.do(t, http.MethodDelete, "/api/v1/ssh-keys/"+keys[0].ID, "admin-1", ""); resp.StatusCode != http.StatusConflict {
		t.Errorf("deleting the deployment key = %d, want 409", resp.StatusCode)
	}
	if resp, _ := f.do(t, http.MethodPost, "/api/v1/ssh-keys/", "admin-1", `{"name":"bad","publicKey":"not a key"}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("importing garbage = %d, want 400", resp.StatusCode)
	}
	duplicate := `{"name":"copy","publicKey":"` + keys[0].PublicKey + `"}`
	if resp, _ := f.do(t, http.MethodPost, "/api/v1/ssh-keys/", "admin-1", duplicate); resp.StatusCode != http.StatusConflict {
		t.Errorf("importing the deployment key's material = %d, want 409", resp.StatusCode)
	}
	if resp, _ := f.do(t, http.MethodPost, "/api/v1/ssh-keys/sync", "admin-1", ""); resp.StatusCode != http.StatusAccepted {
		t.Errorf("POST /sync = %d, want 202", resp.StatusCode)
	}
}
