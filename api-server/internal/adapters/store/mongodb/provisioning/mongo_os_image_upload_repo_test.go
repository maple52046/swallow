package provisioning

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/maple52046/swallow/internal/adapters/store/mongodb/mongotest"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// The store mapper carries every upload field both ways, so a claim returns exactly the intent
// the create stored.
func TestOSImageUploadDocRoundTrips(t *testing.T) {
	created := time.Date(2026, 11, 5, 7, 0, 0, 0, time.UTC)
	upload := &provisioningdomain.OSImageUpload{
		ID: "upload-1", IntegrationID: "integration-1", Name: "rocky-10", Architecture: "amd64",
		Title: "Rocky 10", FileType: "ddgz", SizeBytes: 4096, SHA256: "ab", DefaultUser: "cloud-user",
		State: provisioningdomain.OSImageUploadPending, CreatedAt: created, ExpiresAt: created.Add(time.Hour),
	}
	doc := toOSImageUploadDoc(upload)
	if got := toOSImageUpload(&doc); !reflect.DeepEqual(got, upload) {
		t.Errorf("round trip = %+v, want %+v", got, upload)
	}
}

// uploadTestRepo connects to the test replica set and returns a repository over a database that is
// dropped when the test ends.
func uploadTestRepo(t *testing.T) (*MongoOSImageUploadRepo, *mongo.Database) {
	t.Helper()
	_, db := mongotest.Database(t, "os_image_upload")
	repo, err := NewMongoOSImageUploadRepo(db)
	if err != nil {
		t.Fatalf("NewMongoOSImageUploadRepo() error = %v", err)
	}
	return repo, db
}

// A pending upload is claimed exactly once; a claimed one is in progress, an expired or unknown
// one is not found, and deleting is idempotent.
func TestMongoOSImageUploadRepoClaim(t *testing.T) {
	repo, _ := uploadTestRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, upload := range []*provisioningdomain.OSImageUpload{
		{ID: "live", IntegrationID: "integration-1", Name: "rocky-10", Architecture: "amd64", SizeBytes: 4,
			SHA256: "ab", State: provisioningdomain.OSImageUploadPending, CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		{ID: "expired", IntegrationID: "integration-1", Name: "old", Architecture: "amd64", SizeBytes: 4,
			SHA256: "ab", State: provisioningdomain.OSImageUploadPending, CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour)},
	} {
		if err := repo.Create(ctx, upload); err != nil {
			t.Fatalf("Create(%s) error = %v", upload.ID, err)
		}
	}

	claimed, err := repo.Claim(ctx, "live", now)
	if err != nil {
		t.Fatalf("Claim(live) error = %v", err)
	}
	if claimed.State != provisioningdomain.OSImageUploadSending || claimed.Name != "rocky-10" || !claimed.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Errorf("Claim(live) = %+v, want the stored intent in state sending", claimed)
	}
	if _, err := repo.Claim(ctx, "live", now); !errors.Is(err, provisioningdomain.ErrOSImageUploadInProgress) {
		t.Errorf("second Claim(live) error = %v, want ErrOSImageUploadInProgress", err)
	}
	if _, err := repo.Claim(ctx, "expired", now); !errors.Is(err, provisioningdomain.ErrOSImageUploadNotFound) {
		t.Errorf("Claim(expired) error = %v, want ErrOSImageUploadNotFound", err)
	}
	if _, err := repo.Claim(ctx, "unknown", now); !errors.Is(err, provisioningdomain.ErrOSImageUploadNotFound) {
		t.Errorf("Claim(unknown) error = %v, want ErrOSImageUploadNotFound", err)
	}

	for range 2 {
		if err := repo.Delete(ctx, "live"); err != nil {
			t.Errorf("Delete(live) error = %v, want nil even when already deleted", err)
		}
	}
	if _, err := repo.Claim(ctx, "live", now); !errors.Is(err, provisioningdomain.ErrOSImageUploadNotFound) {
		t.Errorf("Claim(deleted) error = %v, want ErrOSImageUploadNotFound", err)
	}
}

// Uploads nobody claims are discarded by the store: expiresAt carries a TTL index that expires a
// document at that time.
func TestMongoOSImageUploadRepoHasExpiryIndex(t *testing.T) {
	_, db := uploadTestRepo(t)
	cursor, err := db.Collection(osImageUploadCollection).Indexes().List(context.Background())
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	var indexes []bson.M
	if err := cursor.All(context.Background(), &indexes); err != nil {
		t.Fatalf("decode indexes: %v", err)
	}
	for _, index := range indexes {
		if index["name"] != "os_image_upload_expiry" {
			continue
		}
		if seconds, ok := index["expireAfterSeconds"].(int32); !ok || seconds != 0 {
			t.Errorf("expiry index expireAfterSeconds = %v, want 0", index["expireAfterSeconds"])
		}
		return
	}
	t.Errorf("indexes = %v, want os_image_upload_expiry on expiresAt", indexes)
}
