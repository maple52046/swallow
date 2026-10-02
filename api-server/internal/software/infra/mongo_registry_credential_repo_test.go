package infra

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/maple52046/swallow/internal/shared/secret"
	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// credentialTestDatabase connects to SWALLOW_TEST_MONGO_URI and returns a throwaway database dropped
// when the test ends; the test skips without it so `go test ./...` needs no Mongo.
func credentialTestDatabase(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("SWALLOW_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("SWALLOW_TEST_MONGO_URI not set; skipping registry credential repo integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect mongo: %v", err)
	}
	db := client.Database(fmt.Sprintf("swallow_registry_credential_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ccancel()
		_ = db.Drop(cctx)
		_ = client.Disconnect(cctx)
	})
	return db
}

// The password must be sealed at rest, readable only through FindAuth, replaceable, and the
// registry must stay unique.
func TestMongoRegistryCredentialRepoSealsAndEnforcesUniqueness(t *testing.T) {
	db := credentialTestDatabase(t)
	// A fixed 32-byte test key; never a production key.
	sealer, err := secret.NewSealer("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	repo, err := NewMongoRegistryCredentialRepo(db, sealer)
	if err != nil {
		t.Fatalf("NewMongoRegistryCredentialRepo: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	credential := &softwaredomain.RegistryCredential{
		ID: "cred-1", Registry: "harbor.lab.local", Username: "robot", CreatedAt: now, UpdatedAt: now, UpdatedBy: "admin",
	}
	if err := repo.Create(ctx, credential, "plain-secret"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var raw bson.M
	if err := db.Collection("software_registry_credentials").FindOne(ctx, bson.M{"_id": "cred-1"}).Decode(&raw); err != nil {
		t.Fatalf("read raw document: %v", err)
	}
	if strings.Contains(fmt.Sprint(raw), "plain-secret") {
		t.Errorf("stored document contains the plaintext password: %v", raw)
	}

	duplicate := *credential
	duplicate.ID = "cred-2"
	if err := repo.Create(ctx, &duplicate, "other"); !errors.Is(err, softwaredomain.ErrRegistryCredentialExists) {
		t.Errorf("Create duplicate registry error = %v, want ErrRegistryCredentialExists", err)
	}

	found, password, err := repo.FindAuth(ctx, "harbor.lab.local")
	if err != nil || password != "plain-secret" || found.Username != "robot" {
		t.Fatalf("FindAuth = %+v, %q, %v; want robot / plain-secret", found, password, err)
	}

	credential.Username = "robot2"
	if err := repo.Replace(ctx, credential, "rotated"); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if _, password, _ := repo.FindAuth(ctx, "harbor.lab.local"); password != "rotated" {
		t.Errorf("password after Replace = %q, want rotated", password)
	}
	listed, err := repo.List(ctx)
	if err != nil || len(listed) != 1 || listed[0].Username != "robot2" {
		t.Errorf("List = %+v, %v; want one credential for robot2", listed, err)
	}

	if err := repo.Delete(ctx, "cred-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := repo.FindAuth(ctx, "harbor.lab.local"); !errors.Is(err, softwaredomain.ErrRegistryCredentialNotFound) {
		t.Errorf("FindAuth after Delete error = %v, want ErrRegistryCredentialNotFound", err)
	}
	if err := repo.Delete(ctx, "cred-1"); !errors.Is(err, softwaredomain.ErrRegistryCredentialNotFound) {
		t.Errorf("Delete twice error = %v, want ErrRegistryCredentialNotFound", err)
	}
}
