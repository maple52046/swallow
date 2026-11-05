package site

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

	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	"github.com/maple52046/swallow/internal/shared/secret"
)

// siteTestDatabase connects to the MongoDB named by SWALLOW_TEST_MONGO_URI and returns a
// throwaway database dropped when the test ends; without the variable the test is skipped.
func siteTestDatabase(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("SWALLOW_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("SWALLOW_TEST_MONGO_URI not set; skipping site repo integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	db := client.Database(fmt.Sprintf("swallow_site_repo_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return db
}

// The unique (siteId, name) index reports a duplicate as ErrIntegrationNameTaken, on create
// and on rename, so the API answers 409 instead of an internal error.
func TestMongoIntegrationRepo_DuplicateNameIsTaken(t *testing.T) {
	db := siteTestDatabase(t)
	ctx := context.Background()
	sealer, err := secret.NewSealer("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	repo, err := NewMongoIntegrationRepo(db, sealer)
	if err != nil {
		t.Fatalf("NewMongoIntegrationRepo: %v", err)
	}
	integration := func(id, name string) *sitedomain.Integration {
		return &sitedomain.Integration{ID: id, SiteID: "site-1", Kind: sitedomain.IntegrationKindProvisioner, Name: name, Endpoint: "http://maas"}
	}
	if err := repo.Create(ctx, integration("i-1", "maas-a"), ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.Create(ctx, integration("i-2", "maas-a"), ""); !errors.Is(err, sitedomain.ErrIntegrationNameTaken) {
		t.Errorf("duplicate create err = %v, want ErrIntegrationNameTaken", err)
	}
	if err := repo.Create(ctx, integration("i-3", "maas-b"), ""); err != nil {
		t.Fatalf("create second: %v", err)
	}
	if err := repo.Update(ctx, integration("i-3", "maas-a")); !errors.Is(err, sitedomain.ErrIntegrationNameTaken) {
		t.Errorf("rename onto a taken name err = %v, want ErrIntegrationNameTaken", err)
	}
}

// The operator credential is optional and sealed at rest, separate from the credential: writing,
// replacing, or updating one leaves the other alone, an empty value removes it, and an unknown
// Integration is ErrIntegrationNotFound.
func TestMongoIntegrationRepo_OperatorCredential(t *testing.T) {
	db := siteTestDatabase(t)
	ctx := context.Background()
	sealer, err := secret.NewSealer("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	repo, err := NewMongoIntegrationRepo(db, sealer)
	if err != nil {
		t.Fatalf("NewMongoIntegrationRepo: %v", err)
	}
	integration := &sitedomain.Integration{ID: "i-1", SiteID: "site-1", Kind: sitedomain.IntegrationKindPlatform, Name: "lab (deployed)", Endpoint: "https://10.0.0.1:6443"}
	if err := repo.Create(ctx, integration, "read-token"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := repo.OperatorCredential(ctx, "i-1"); !errors.Is(err, sitedomain.ErrCredentialNotSet) {
		t.Errorf("OperatorCredential before one is written: err = %v, want ErrCredentialNotSet", err)
	}

	if err := repo.ReplaceOperatorCredential(ctx, "i-1", "operator-token"); err != nil {
		t.Fatalf("ReplaceOperatorCredential: %v", err)
	}
	var raw bson.M
	if err := db.Collection("integrations").FindOne(ctx, bson.M{"_id": "i-1"}).Decode(&raw); err != nil {
		t.Fatalf("read raw document: %v", err)
	}
	if sealed, _ := raw["sealedOperatorCredential"].(string); sealed == "" || strings.Contains(sealed, "operator-token") {
		t.Errorf("sealedOperatorCredential = %q, want ciphertext", sealed)
	}
	if got, err := repo.OperatorCredential(ctx, "i-1"); err != nil || got != "operator-token" {
		t.Errorf("OperatorCredential = %q, %v; want operator-token", got, err)
	}
	if got, err := repo.Credential(ctx, "i-1"); err != nil || got != "read-token" {
		t.Errorf("Credential = %q, %v; want the read credential untouched", got, err)
	}

	if err := repo.ReplaceCredential(ctx, "i-1", "read-token-2"); err != nil {
		t.Fatalf("ReplaceCredential: %v", err)
	}
	integration.Name = "renamed"
	if err := repo.Update(ctx, integration); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got, err := repo.OperatorCredential(ctx, "i-1"); err != nil || got != "operator-token" {
		t.Errorf("OperatorCredential after ReplaceCredential and Update = %q, %v; want it kept", got, err)
	}

	if err := repo.ReplaceOperatorCredential(ctx, "i-1", ""); err != nil {
		t.Fatalf("ReplaceOperatorCredential(empty): %v", err)
	}
	if _, err := repo.OperatorCredential(ctx, "i-1"); !errors.Is(err, sitedomain.ErrCredentialNotSet) {
		t.Errorf("OperatorCredential after removal: err = %v, want ErrCredentialNotSet", err)
	}
	if got, err := repo.Credential(ctx, "i-1"); err != nil || got != "read-token-2" {
		t.Errorf("Credential after removing the operator credential = %q, %v", got, err)
	}

	if err := repo.ReplaceOperatorCredential(ctx, "missing", "x"); !errors.Is(err, sitedomain.ErrIntegrationNotFound) {
		t.Errorf("ReplaceOperatorCredential(missing): err = %v, want ErrIntegrationNotFound", err)
	}
	if _, err := repo.OperatorCredential(ctx, "missing"); !errors.Is(err, sitedomain.ErrIntegrationNotFound) {
		t.Errorf("OperatorCredential(missing): err = %v, want ErrIntegrationNotFound", err)
	}
}

// Every read reports whether an operator credential is stored without opening it: a document
// written before the field existed reads false, a value the key cannot open still reads true, and
// neither Create nor Update writes the flag a caller set on the Integration.
func TestMongoIntegrationRepo_ReportsOperatorCredentialPresence(t *testing.T) {
	db := siteTestDatabase(t)
	ctx := context.Background()
	sealer, err := secret.NewSealer("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	repo, err := NewMongoIntegrationRepo(db, sealer)
	if err != nil {
		t.Fatalf("NewMongoIntegrationRepo: %v", err)
	}
	presence := func(when string, want bool) {
		t.Helper()
		found, err := repo.FindByID(ctx, "i-1")
		if err != nil {
			t.Fatalf("FindByID %s: %v", when, err)
		}
		if found.HasOperatorCredential != want {
			t.Errorf("FindByID %s: HasOperatorCredential = %v, want %v", when, found.HasOperatorCredential, want)
		}
		listed, err := repo.List(ctx, sitedomain.IntegrationFilter{SiteID: "site-1"})
		if err != nil {
			t.Fatalf("List %s: %v", when, err)
		}
		if len(listed) != 1 || listed[0].HasOperatorCredential != want {
			t.Errorf("List %s: got %d Integrations, want 1 with HasOperatorCredential = %v", when, len(listed), want)
		}
	}

	integration := &sitedomain.Integration{
		ID: "i-1", SiteID: "site-1", Kind: sitedomain.IntegrationKindPlatform, Name: "lab (deployed)",
		Endpoint: "https://10.0.0.1:6443", HasOperatorCredential: true,
	}
	if err := repo.Create(ctx, integration, "read-token"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	presence("after Create", false)

	if err := repo.ReplaceOperatorCredential(ctx, "i-1", "operator-token"); err != nil {
		t.Fatalf("ReplaceOperatorCredential: %v", err)
	}
	presence("after ReplaceOperatorCredential", true)

	integration.Name = "renamed"
	integration.HasOperatorCredential = false
	if err := repo.Update(ctx, integration); err != nil {
		t.Fatalf("Update: %v", err)
	}
	presence("after Update", true)

	if _, err := db.Collection("integrations").UpdateOne(ctx, bson.M{"_id": "i-1"},
		bson.M{"$set": bson.M{"sealedOperatorCredential": "not-ciphertext"}}); err != nil {
		t.Fatalf("overwrite sealedOperatorCredential: %v", err)
	}
	presence("with a value the key cannot open", true)

	if err := repo.ReplaceOperatorCredential(ctx, "i-1", ""); err != nil {
		t.Fatalf("ReplaceOperatorCredential(empty): %v", err)
	}
	presence("after removal", false)
}
