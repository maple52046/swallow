package migration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// testDatabase connects to the MongoDB named by SWALLOW_TEST_MONGO_URI and returns a
// throwaway database that is dropped when the test ends. Tests skip when the variable is
// unset so `go test ./...` stays green in environments without a Mongo instance; CI and
// local verification set it to an ephemeral container.
func testDatabase(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("SWALLOW_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("SWALLOW_TEST_MONGO_URI not set; skipping migration integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect mongo: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatalf("ping mongo: %v", err)
	}
	name := fmt.Sprintf("swallow_migration_test_%d", time.Now().UnixNano())
	db := client.Database(name)
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ccancel()
		_ = db.Drop(cctx)
		_ = client.Disconnect(cctx)
	})
	return db
}

func ctxFor(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// seedV2 lays down a representative v2 database: two clusters, a server whose membership
// axis still uses clusterId, an operation carrying clusterId, and a cluster-kind
// integration, plus the v2 schema marker and the legacy indexes.
func seedV2(t *testing.T, ctx context.Context, db *mongo.Database) {
	t.Helper()
	if _, err := db.Collection("clusters").InsertMany(ctx, []any{
		bson.M{"_id": "platform-1", "siteId": "site-1", "name": "prod-k8s", "type": "kubernetes"},
		bson.M{"_id": "platform-2", "siteId": "site-1", "name": "lab-k8s", "type": "kubernetes"},
	}); err != nil {
		t.Fatalf("seed clusters: %v", err)
	}
	if _, err := db.Collection("servers").InsertOne(ctx, bson.M{
		"_id": "srv-1", "membership": bson.M{"clusterId": "platform-1", "nodeName": "node-01"},
	}); err != nil {
		t.Fatalf("seed servers: %v", err)
	}
	if _, err := db.Collection("operations").InsertOne(ctx, bson.M{
		"_id": "op-1", "schemaVersion": 1, "clusterId": "platform-1", "kind": "deploy-kubernetes",
	}); err != nil {
		t.Fatalf("seed operations: %v", err)
	}
	if _, err := db.Collection("integrations").InsertOne(ctx, bson.M{
		"_id": "int-1", "kind": "cluster", "name": "prod-k8s-reader",
	}); err != nil {
		t.Fatalf("seed integrations: %v", err)
	}
	mustIndex(t, ctx, db.Collection("clusters"), mongo.IndexModel{
		Keys: bson.D{{Key: "siteId", Value: 1}, {Key: "name", Value: 1}}, Options: options.Index().SetName("site_cluster_name"),
	})
	mustIndex(t, ctx, db.Collection("servers"), mongo.IndexModel{
		Keys: bson.D{{Key: "membership.clusterId", Value: 1}}, Options: options.Index().SetSparse(true).SetName("membership_cluster"),
	})
	mustIndex(t, ctx, db.Collection("operations"), mongo.IndexModel{
		Keys: bson.D{{Key: "schemaVersion", Value: 1}, {Key: "clusterId", Value: 1}}, Options: options.Index().SetName("execution_cluster_lifecycle"),
	})
	if _, err := db.Collection("schema_metadata").InsertOne(ctx, bson.M{
		"_id": "database", "version": 2, "updatedAt": time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed schema marker: %v", err)
	}
}

func mustIndex(t *testing.T, ctx context.Context, col *mongo.Collection, model mongo.IndexModel) {
	t.Helper()
	if _, err := col.Indexes().CreateOne(ctx, model); err != nil {
		t.Fatalf("create index on %s: %v", col.Name(), err)
	}
}

func indexNames(t *testing.T, ctx context.Context, col *mongo.Collection) map[string]bool {
	t.Helper()
	cursor, err := col.Indexes().List(ctx)
	if err != nil {
		t.Fatalf("list indexes on %s: %v", col.Name(), err)
	}
	names := map[string]bool{}
	var specs []bson.M
	if err := cursor.All(ctx, &specs); err != nil {
		t.Fatalf("decode indexes on %s: %v", col.Name(), err)
	}
	for _, spec := range specs {
		if name, ok := spec["name"].(string); ok {
			names[name] = true
		}
	}
	return names
}

func count(t *testing.T, ctx context.Context, col *mongo.Collection, filter bson.M) int64 {
	t.Helper()
	n, err := col.CountDocuments(ctx, filter)
	if err != nil {
		t.Fatalf("count %s: %v", col.Name(), err)
	}
	return n
}

func schemaVersion(t *testing.T, ctx context.Context, db *mongo.Database) int {
	t.Helper()
	var state metadata
	if err := db.Collection("schema_metadata").FindOne(ctx, bson.M{"_id": "database"}).Decode(&state); err != nil {
		t.Fatalf("read schema marker: %v", err)
	}
	return state.Version
}

// TestMigrateV2ToV3_RenamesAndRewrites verifies the full happy path: the collection is
// renamed preserving IDs, reference fields and integration kind are rewritten, the
// indexes are replaced, and no legacy reference survives.
func TestMigrateV2ToV3_RenamesAndRewrites(t *testing.T) {
	db := testDatabase(t)
	ctx := ctxFor(t)
	seedV2(t, ctx, db)

	if err := Migrate(ctx, db, true); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if got := schemaVersion(t, ctx, db); got != CurrentSchemaVersion {
		t.Errorf("schema version = %d, want %d", got, CurrentSchemaVersion)
	}
	if got := count(t, ctx, db.Collection("platforms"), bson.M{}); got != 2 {
		t.Errorf("platforms count = %d, want 2", got)
	}
	names, err := db.ListCollectionNames(ctx, bson.M{"name": "clusters"})
	if err != nil {
		t.Fatalf("list collections: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("clusters collection still present: %v", names)
	}
	// Document identity is preserved by the rename.
	if got := count(t, ctx, db.Collection("platforms"), bson.M{"_id": "platform-1"}); got != 1 {
		t.Errorf("platform-1 not found after rename")
	}
	// Reference fields are rewritten, not duplicated.
	if got := count(t, ctx, db.Collection("servers"), bson.M{"membership.platformId": "platform-1"}); got != 1 {
		t.Errorf("server membership.platformId not rewritten")
	}
	if got := count(t, ctx, db.Collection("servers"), bson.M{"membership.clusterId": bson.M{"$exists": true}}); got != 0 {
		t.Errorf("legacy membership.clusterId still present")
	}
	if got := count(t, ctx, db.Collection("operations"), bson.M{"platformId": "platform-1"}); got != 1 {
		t.Errorf("operation platformId not rewritten")
	}
	if got := count(t, ctx, db.Collection("operations"), bson.M{"clusterId": bson.M{"$exists": true}}); got != 0 {
		t.Errorf("legacy operation clusterId still present")
	}
	if got := count(t, ctx, db.Collection("integrations"), bson.M{"kind": "platform"}); got != 1 {
		t.Errorf("integration kind not rewritten to platform")
	}
	if got := count(t, ctx, db.Collection("integrations"), bson.M{"kind": "cluster"}); got != 0 {
		t.Errorf("legacy integration kind=cluster still present")
	}

	platformIdx := indexNames(t, ctx, db.Collection("platforms"))
	if !platformIdx["site_platform_name"] || platformIdx["site_cluster_name"] {
		t.Errorf("platform indexes not renamed: %v", platformIdx)
	}
	serverIdx := indexNames(t, ctx, db.Collection("servers"))
	if !serverIdx["membership_platform"] || serverIdx["membership_cluster"] {
		t.Errorf("server indexes not renamed: %v", serverIdx)
	}
	operationIdx := indexNames(t, ctx, db.Collection("operations"))
	if !operationIdx["execution_platform_lifecycle"] || operationIdx["execution_cluster_lifecycle"] {
		t.Errorf("operation indexes not renamed: %v", operationIdx)
	}
}

// TestMigrate_Idempotent verifies a second run after a completed migration is a safe
// no-op rather than an error or a double-apply.
func TestMigrate_Idempotent(t *testing.T) {
	db := testDatabase(t)
	ctx := ctxFor(t)
	seedV2(t, ctx, db)

	if err := Migrate(ctx, db, true); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := Migrate(ctx, db, true); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if got := count(t, ctx, db.Collection("platforms"), bson.M{}); got != 2 {
		t.Errorf("platforms count after rerun = %d, want 2", got)
	}
	if got := schemaVersion(t, ctx, db); got != CurrentSchemaVersion {
		t.Errorf("schema version after rerun = %d, want %d", got, CurrentSchemaVersion)
	}
}

// TestMigrate_ResumesAfterInterruptedRename simulates a crash after the collection was
// renamed but before reference fields were rewritten and the marker advanced. The rerun
// must finish the field rewrites and reach v3.
func TestMigrate_ResumesAfterInterruptedRename(t *testing.T) {
	db := testDatabase(t)
	ctx := ctxFor(t)
	seedV2(t, ctx, db)

	// Simulate the collection rename having already happened while the marker is still v2
	// and reference fields are still legacy.
	if err := renamePlatformCollection(ctx, db); err != nil {
		t.Fatalf("simulate rename: %v", err)
	}

	if err := Migrate(ctx, db, true); err != nil {
		t.Fatalf("resume Migrate: %v", err)
	}
	if got := count(t, ctx, db.Collection("servers"), bson.M{"membership.platformId": "platform-1"}); got != 1 {
		t.Errorf("resume did not rewrite membership")
	}
	if got := schemaVersion(t, ctx, db); got != CurrentSchemaVersion {
		t.Errorf("schema version after resume = %d, want %d", got, CurrentSchemaVersion)
	}
}

// TestMigrate_RequiresBackupConfirmation verifies the destructive rename refuses to run
// without an explicit backup acknowledgement.
func TestMigrate_RequiresBackupConfirmation(t *testing.T) {
	db := testDatabase(t)
	ctx := ctxFor(t)
	seedV2(t, ctx, db)

	err := Migrate(ctx, db)
	if err == nil {
		t.Fatalf("Migrate without backup confirmation should fail")
	}
	if got := count(t, ctx, db.Collection("clusters"), bson.M{}); got != 2 {
		t.Errorf("clusters were modified despite refusal: %d", got)
	}
	if got := schemaVersion(t, ctx, db); got != 2 {
		t.Errorf("schema advanced despite refusal: %d", got)
	}
}

// TestMigrate_DualCollectionWithDataFails verifies the migration refuses to guess when
// both the legacy and the renamed collection hold data.
func TestMigrate_DualCollectionWithDataFails(t *testing.T) {
	db := testDatabase(t)
	ctx := ctxFor(t)
	seedV2(t, ctx, db)
	if _, err := db.Collection("platforms").InsertOne(ctx, bson.M{"_id": "platform-9", "name": "stray"}); err != nil {
		t.Fatalf("seed stray platform: %v", err)
	}

	if err := Migrate(ctx, db, true); err == nil {
		t.Fatalf("Migrate with populated clusters and platforms should fail")
	}
	if got := schemaVersion(t, ctx, db); got != 2 {
		t.Errorf("schema advanced despite ambiguous state: %d", got)
	}
}

// TestMigrate_FreshDatabaseInitializesAtCurrent verifies an empty database is stamped at
// the current version without running upgrade steps.
func TestMigrate_FreshDatabaseInitializesAtCurrent(t *testing.T) {
	db := testDatabase(t)
	ctx := ctxFor(t)

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate fresh: %v", err)
	}
	if got := schemaVersion(t, ctx, db); got != CurrentSchemaVersion {
		t.Errorf("fresh schema version = %d, want %d", got, CurrentSchemaVersion)
	}
}

// TestCopyVerifyDrop covers the non-admin fallback used when renameCollection is denied:
// documents are copied preserving _id, the source is dropped, and a partial rerun
// converges instead of failing on duplicate keys.
func TestCopyVerifyDrop(t *testing.T) {
	db := testDatabase(t)
	ctx := ctxFor(t)
	if _, err := db.Collection("clusters").InsertMany(ctx, []any{
		bson.M{"_id": "platform-1", "name": "a"},
		bson.M{"_id": "platform-2", "name": "b"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Pre-populate one destination document to exercise the upsert-on-rerun path.
	if _, err := db.Collection("platforms").InsertOne(ctx, bson.M{"_id": "platform-1", "name": "stale"}); err != nil {
		t.Fatalf("seed dst: %v", err)
	}

	if err := copyVerifyDrop(ctx, db, "clusters", "platforms"); err != nil {
		t.Fatalf("copyVerifyDrop: %v", err)
	}
	if got := count(t, ctx, db.Collection("platforms"), bson.M{}); got != 2 {
		t.Errorf("platforms count = %d, want 2", got)
	}
	var doc bson.M
	if err := db.Collection("platforms").FindOne(ctx, bson.M{"_id": "platform-1"}).Decode(&doc); err != nil {
		t.Fatalf("find migrated doc: %v", err)
	}
	if doc["name"] != "a" {
		t.Errorf("destination doc not replaced from source: %v", doc["name"])
	}
	names, err := db.ListCollectionNames(ctx, bson.M{"name": "clusters"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("source collection not dropped: %v", names)
	}
}
