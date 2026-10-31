package site

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

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

// DF-05: the unique (siteId, name) index reports a duplicate as ErrIntegrationNameTaken, on create
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
