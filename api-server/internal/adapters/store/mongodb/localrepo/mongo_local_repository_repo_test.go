package localrepo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	storemongo "github.com/maple52046/swallow/internal/adapters/store/mongodb"
	"github.com/maple52046/swallow/internal/core/convergence"
	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
)

// localRepositoryTestDatabase returns a fresh database dropped when the test ends. Skipped without
// SWALLOW_TEST_MONGO_URI.
func localRepositoryTestDatabase(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("SWALLOW_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("SWALLOW_TEST_MONGO_URI is not set")
	}
	client, err := storemongo.Connect(context.Background(), uri)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	db := client.Database(fmt.Sprintf("swallow_localrepo_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return db
}

// The spec is written only at the generation the writer read, the first write creates the record,
// and a status write neither changes the spec nor its generation.
func TestLocalRepositorySpecWritesAreConditionalOnTheGeneration(t *testing.T) {
	ctx := context.Background()
	repo := NewMongoLocalRepositoryRepo(localRepositoryTestDatabase(t))
	ubuntu := localrepodomain.Spec{Sources: []localrepodomain.Source{{
		Name: "ubuntu", Format: localrepodomain.FormatDeb, URL: "http://archive.example/ubuntu/",
		Suites: []string{"noble"}, Components: []string{"main"}, Architectures: []string{"amd64"},
	}}}

	if _, err := repo.Get(ctx, "site-a"); !errors.Is(err, localrepodomain.ErrLocalRepositoryNotFound) {
		t.Fatalf("Get of a new Site = %v, want ErrLocalRepositoryNotFound", err)
	}
	if _, err := repo.PutSpec(ctx, "site-a", 1, ubuntu); !errors.Is(err, localrepodomain.ErrGenerationConflict) {
		t.Fatalf("PutSpec expecting 1 without a record = %v, want ErrGenerationConflict", err)
	}
	created, err := repo.PutSpec(ctx, "site-a", 0, ubuntu)
	if err != nil || created.Generation != 1 || !reflect.DeepEqual(created.Spec, ubuntu) {
		t.Fatalf("first PutSpec = %+v, %v; want generation 1 with the spec", created, err)
	}
	if _, err := repo.PutSpec(ctx, "site-a", 0, ubuntu); !errors.Is(err, localrepodomain.ErrGenerationConflict) {
		t.Fatalf("PutSpec expecting 0 after a write = %v, want ErrGenerationConflict", err)
	}
	if err := repo.SetStatus(ctx, "site-a", localrepodomain.Status{IntegrationID: "repo-1", ObservedGeneration: 1}); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	replaced, err := repo.PutSpec(ctx, "site-a", 1, localrepodomain.Spec{})
	if err != nil || replaced.Generation != 2 || len(replaced.Spec.Sources) != 0 || replaced.Status.IntegrationID != "repo-1" {
		t.Fatalf("second PutSpec = %+v, %v; want generation 2, no sources, the status kept", replaced, err)
	}
}

// A status written for a Site without a record creates one at generation 0, and every observed
// field and condition round trips.
func TestLocalRepositoryStatusRoundTrips(t *testing.T) {
	ctx := context.Background()
	repo := NewMongoLocalRepositoryRepo(localRepositoryTestDatabase(t))
	now := time.Now().UTC().Truncate(time.Millisecond)
	source := localrepodomain.Source{
		Name: "docker-ce", Format: localrepodomain.FormatDeb, URL: "https://download.example/linux/ubuntu",
		Suites: []string{"noble"}, Components: []string{"stable"},
	}
	file := localrepodomain.File{Path: "tools/tool-1.0", URL: "https://files.example/tool-1.0", SHA256: "ab12"}
	status := localrepodomain.Status{
		IntegrationID: "repo-1", ObservedGeneration: 0,
		Sources: []localrepodomain.ObservedSource{{Source: source, Status: localrepodomain.SourceStatus{
			Name: source.Name, Format: source.Format, State: localrepodomain.SyncReady, LastSyncedAt: &now, ContentCount: 12,
		}}},
		Files: []localrepodomain.ObservedFile{{File: file, Status: localrepodomain.FileStatus{
			Path: file.Path, State: localrepodomain.SyncFailed, Message: "checksum mismatch",
		}}},
		Images: []localrepodomain.ObservedImage{
			{Reference: "registry.example/app:1", Status: localrepodomain.ImageStatus{
				Reference: "registry.example/app:1", State: localrepodomain.SyncReady, SizeBytes: 83_800_000,
			}},
			{Reference: "registry.example/tool:2", Status: localrepodomain.ImageStatus{
				Reference: "registry.example/tool:2", State: localrepodomain.SyncFailed, Message: "Pulp reported an internal error (HTTP 502).",
			}},
		},
		Endpoints: &localrepodomain.HostEndpoints{
			Deb:           []localrepodomain.DebSource{{Name: "docker-ce", URI: "http://repo/deb/docker-ce/", Suites: []string{"noble"}, Components: []string{"stable"}}},
			DebSigningKey: "-----BEGIN PGP PUBLIC KEY BLOCK-----",
			Registry:      "repo:8080",
			RegistryMirrors: map[string]string{
				"registry.example": "http://repo:8080/v2/registry.example", "quay.example": "http://repo:8080/v2/quay.example",
			},
			PythonIndex: "http://repo:8080/pypi/pypi/simple/", Files: "http://repo:8080/files/",
		},
		ObservedAt: &now,
		Conditions: []convergence.Condition{{
			Type: localrepodomain.ConditionReady, Status: convergence.ConditionFalse, Reason: localrepodomain.ReasonFileFailed,
			Message: "file tools/tool-1.0 failed: checksum mismatch", LastTransitionTime: now,
		}},
	}
	if err := repo.SetStatus(ctx, "site-b", status); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	stored, err := repo.Get(ctx, "site-b")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Generation != 0 || len(stored.Spec.Sources) != 0 {
		t.Errorf("Get = generation %d spec %+v; want generation 0 and an empty spec", stored.Generation, stored.Spec)
	}
	if !reflect.DeepEqual(stored.Status, status) {
		t.Errorf("Get status = %+v, want %+v", stored.Status, status)
	}
	listed, err := repo.List(ctx)
	if err != nil || len(listed) != 1 || listed[0].SiteID != "site-b" {
		t.Fatalf("List = %+v, %v; want site-b", listed, err)
	}
	if err := repo.Delete(ctx, "site-b"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := repo.Delete(ctx, "site-b"); err != nil {
		t.Fatalf("Delete of a missing record: %v", err)
	}
	if _, err := repo.Get(ctx, "site-b"); !errors.Is(err, localrepodomain.ErrLocalRepositoryNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrLocalRepositoryNotFound", err)
	}
}

// A record written before declared images were observed items holds prefetchedImages instead of
// images: it still reads, with no image observed, and the next status write replaces the field.
func TestLocalRepositoryStatusWithPrefetchedImagesStillReads(t *testing.T) {
	ctx := context.Background()
	repo := NewMongoLocalRepositoryRepo(localRepositoryTestDatabase(t))
	if _, err := repo.col.InsertOne(ctx, bson.M{
		"_id": "site-c", "generation": int64(1), "spec": bson.M{"sources": bson.A{}},
		"status": bson.M{
			"integrationId": "repo-1", "observedGeneration": int64(1),
			"prefetchedImages": bson.A{"quay.io/k0sproject/pause:3.10.2-0", "registry.k8s.io/nfd/node-feature-discovery:v0.18.3"},
		},
	}); err != nil {
		t.Fatalf("InsertOne: %v", err)
	}
	stored, err := repo.Get(ctx, "site-c")
	if err != nil {
		t.Fatalf("Get of a record with prefetchedImages: %v", err)
	}
	if stored.Status.IntegrationID != "repo-1" || stored.Status.ObservedGeneration != 1 || len(stored.Status.Images) != 0 {
		t.Errorf("Get status = %+v, want repo-1 at generation 1 with no image observed", stored.Status)
	}

	if err := repo.SetStatus(ctx, "site-c", stored.Status); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	var raw bson.M
	if err := repo.col.FindOne(ctx, bson.M{"_id": "site-c"}).Decode(&raw); err != nil {
		t.Fatalf("FindOne: %v", err)
	}
	if status, _ := raw["status"].(bson.M); status == nil || status["prefetchedImages"] != nil {
		t.Errorf("status after a write = %v, want it without prefetchedImages", raw["status"])
	}
}
