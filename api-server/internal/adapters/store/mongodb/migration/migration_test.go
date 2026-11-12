package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/maple52046/swallow/internal/adapters/store/mongodb/mongotest"
)

// testDatabase connects to the MongoDB named by SWALLOW_TEST_MONGO_URI and returns a
// throwaway database that is dropped when the test ends. Tests skip when the variable is
// unset so `go test ./...` stays green in environments without a Mongo instance; CI and
// local verification set it to an ephemeral container.
func testDatabase(t *testing.T) *mongo.Database {
	t.Helper()
	_, db := mongotest.Database(t, "migration")
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
	// The operation v3 rewrote is a schema-v2 record, which v10 drops with its indexes.
	if names, _ := db.ListCollectionNames(ctx, bson.M{"name": "operations"}); len(names) != 0 {
		t.Errorf("operations collection still present after v10")
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

// TestMigrateV4ToV5_MovesTagOverlaysIntoTheSpec verifies a Server's swallow-owned overlay tags
// survive in its spec for adoption to keep, a Server that already has a spec keeps it, and the
// overlays are gone.
func TestMigrateV4ToV5_MovesTagOverlaysIntoTheSpec(t *testing.T) {
	db := testDatabase(t)
	ctx := ctxFor(t)
	if _, err := db.Collection("servers").InsertMany(ctx, []any{
		bson.M{"_id": "srv-overlay"},
		bson.M{"_id": "srv-spec", "generation": int64(3), "spec": bson.M{"tags": bson.A{"kept"}}},
		bson.M{"_id": "srv-plain"},
	}); err != nil {
		t.Fatalf("seed servers: %v", err)
	}
	if _, err := db.Collection("server_tag_overlays").InsertMany(ctx, []any{
		bson.M{"_id": "srv-overlay", "integrationId": "int-1", "tags": bson.A{"gpu", "ml"}},
		bson.M{"_id": "srv-spec", "integrationId": "int-1", "tags": bson.A{"stale"}},
	}); err != nil {
		t.Fatalf("seed overlays: %v", err)
	}
	if _, err := db.Collection("schema_metadata").InsertOne(ctx, bson.M{
		"_id": "database", "version": 4, "updatedAt": time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed schema marker: %v", err)
	}

	if err := Migrate(ctx, db, true); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	spec := func(id string) (int64, []string) {
		var doc struct {
			Generation int64 `bson:"generation"`
			Spec       struct {
				Tags []string `bson:"tags"`
			} `bson:"spec"`
		}
		if err := db.Collection("servers").FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return doc.Generation, doc.Spec.Tags
	}
	if generation, tags := spec("srv-overlay"); generation != 0 || fmt.Sprint(tags) != "[gpu ml]" {
		t.Errorf("srv-overlay spec = generation %d tags %v, want generation 0 (adoption pending) tags [gpu ml]", generation, tags)
	}
	if generation, tags := spec("srv-spec"); generation != 3 || fmt.Sprint(tags) != "[kept]" {
		t.Errorf("srv-spec spec = generation %d tags %v, want it untouched", generation, tags)
	}
	if generation, _ := spec("srv-plain"); generation != 0 {
		t.Errorf("srv-plain generation = %d, want 0 (adopted later by the controller)", generation)
	}
	names, err := db.ListCollectionNames(ctx, bson.M{"name": "server_tag_overlays"})
	if err != nil || len(names) != 0 {
		t.Errorf("server_tag_overlays after migration = %v (err %v), want dropped", names, err)
	}
	if got := schemaVersion(t, ctx, db); got != CurrentSchemaVersion {
		t.Errorf("schema version = %d, want %d", got, CurrentSchemaVersion)
	}
}

// TestMigrateV5ToV6_MakesZonesAndPoolsResources verifies each existing Zone and Pool loses
// providerRealized without gaining a generation (the catalog controller adopts it), and a group
// that already has a generation keeps it.
func TestMigrateV5ToV6_MakesZonesAndPoolsResources(t *testing.T) {
	db := testDatabase(t)
	ctx := ctxFor(t)
	if _, err := db.Collection("zones").InsertMany(ctx, []any{
		bson.M{"_id": "z1", "siteId": "s", "name": "rack-a", "providerRealized": true},
		bson.M{"_id": "z2", "siteId": "s", "name": "rack-b", "generation": int64(4)},
	}); err != nil {
		t.Fatalf("seed zones: %v", err)
	}
	if _, err := db.Collection("pools").InsertOne(ctx, bson.M{"_id": "p1", "siteId": "s", "name": "gpu", "providerRealized": false}); err != nil {
		t.Fatalf("seed pools: %v", err)
	}
	if _, err := db.Collection("schema_metadata").InsertOne(ctx, bson.M{
		"_id": "database", "version": 5, "updatedAt": time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed schema marker: %v", err)
	}

	if err := Migrate(ctx, db, true); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	for collection, want := range map[string]map[string]any{"zones": {"z1": nil, "z2": int64(4)}, "pools": {"p1": nil}} {
		for id, generation := range want {
			var doc bson.M
			if err := db.Collection(collection).FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
				t.Fatalf("read %s/%s: %v", collection, id, err)
			}
			if doc["generation"] != generation {
				t.Errorf("%s/%s generation = %v, want %v", collection, id, doc["generation"], generation)
			}
			if _, present := doc["providerRealized"]; present {
				t.Errorf("%s/%s still has providerRealized", collection, id)
			}
		}
	}
	if got := schemaVersion(t, ctx, db); got != CurrentSchemaVersion {
		t.Errorf("schema version = %d, want %d", got, CurrentSchemaVersion)
	}
}

// TestMigrateV6ToV7_MakesSSHKeysResources verifies each SSH Key gets generation 1 and each sync
// record keeps its provider key id but loses its state fields.
func TestMigrateV6ToV7_MakesSSHKeysResources(t *testing.T) {
	db := testDatabase(t)
	ctx := ctxFor(t)
	if _, err := db.Collection("ssh_keys").InsertOne(ctx, bson.M{"_id": "k1", "purpose": "deployment"}); err != nil {
		t.Fatalf("seed keys: %v", err)
	}
	if _, err := db.Collection("ssh_key_provider_syncs").InsertOne(ctx, bson.M{
		"_id": "k1:maas", "keyId": "k1", "providerKeyId": "7", "state": "failed", "error": "boom", "syncedAt": time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed syncs: %v", err)
	}
	if _, err := db.Collection("schema_metadata").InsertOne(ctx, bson.M{
		"_id": "database", "version": 6, "updatedAt": time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed schema marker: %v", err)
	}

	if err := Migrate(ctx, db, true); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var key bson.M
	if err := db.Collection("ssh_keys").FindOne(ctx, bson.M{"_id": "k1"}).Decode(&key); err != nil || key["generation"] != int64(1) {
		t.Errorf("key = %v, %v; want generation 1", key, err)
	}
	var record bson.M
	if err := db.Collection("ssh_key_provider_syncs").FindOne(ctx, bson.M{"_id": "k1:maas"}).Decode(&record); err != nil {
		t.Fatalf("read record: %v", err)
	}
	for _, field := range []string{"state", "error", "syncedAt"} {
		if _, present := record[field]; present {
			t.Errorf("record still has %s: %v", field, record)
		}
	}
	if record["providerKeyId"] != "7" {
		t.Errorf("record lost its provider key id: %v", record)
	}
}

// TestMigrateV7ToV8_MovesOSImageOverlaysToMetadata verifies the overlay documents arrive unchanged
// in os_image_metadata and the old collection is gone.
func TestMigrateV7ToV8_MovesOSImageOverlaysToMetadata(t *testing.T) {
	db := testDatabase(t)
	ctx := ctxFor(t)
	if _, err := db.Collection("os_image_overlays").InsertOne(ctx, bson.M{
		"integrationId": "maas-a", "imageId": "ubuntu/jammy", "architecture": "amd64", "displayName": "Golden",
	}); err != nil {
		t.Fatalf("seed overlays: %v", err)
	}
	if _, err := db.Collection("schema_metadata").InsertOne(ctx, bson.M{
		"_id": "database", "version": 7, "updatedAt": time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed schema marker: %v", err)
	}

	if err := Migrate(ctx, db, true); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var doc bson.M
	if err := db.Collection("os_image_metadata").FindOne(ctx, bson.M{"imageId": "ubuntu/jammy"}).Decode(&doc); err != nil || doc["displayName"] != "Golden" {
		t.Fatalf("metadata = %v, %v; want the overlay moved", doc, err)
	}
	if names, _ := db.ListCollectionNames(ctx, bson.M{"name": "os_image_overlays"}); len(names) != 0 {
		t.Errorf("os_image_overlays still exists")
	}
}

// TestMigrateV9ToV10_DropsTheSchemaV2Operations verifies v10 refuses to run without a backup
// confirmation, then drops `operations` and leaves the Workflows alone.
func TestMigrateV9ToV10_DropsTheSchemaV2Operations(t *testing.T) {
	db := testDatabase(t)
	ctx := ctxFor(t)
	if _, err := db.Collection("operations").InsertOne(ctx, bson.M{"_id": "op-v2", "schemaVersion": 2, "terminal": true}); err != nil {
		t.Fatalf("seed operations: %v", err)
	}
	if _, err := db.Collection("workflows").InsertOne(ctx, bson.M{"_id": "wf-1", "schemaVersion": 4}); err != nil {
		t.Fatalf("seed workflows: %v", err)
	}
	if _, err := db.Collection("schema_metadata").InsertOne(ctx, bson.M{
		"_id": "database", "version": 9, "updatedAt": time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed schema marker: %v", err)
	}

	if err := Migrate(ctx, db); err == nil {
		t.Fatal("Migrate without backup confirmation should fail")
	}
	if got := count(t, ctx, db.Collection("operations"), bson.M{}); got != 1 || schemaVersion(t, ctx, db) != 9 {
		t.Fatalf("the refused migration changed the database: operations=%d schema=%d", got, schemaVersion(t, ctx, db))
	}

	if err := Migrate(ctx, db, true); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if names, _ := db.ListCollectionNames(ctx, bson.M{"name": "operations"}); len(names) != 0 {
		t.Errorf("operations still exists")
	}
	if got := count(t, ctx, db.Collection("workflows"), bson.M{}); got != 1 {
		t.Errorf("workflows = %d, want the Workflow kept", got)
	}
	if got := schemaVersion(t, ctx, db); got != CurrentSchemaVersion {
		t.Errorf("schema version = %d, want %d", got, CurrentSchemaVersion)
	}
}

// TestMigrateV10ToV11_RemovesRetiredPlaybookMappings verifies v11 drops only the retired kinds'
// mappings, keeps the others, and needs no backup confirmation.
func TestMigrateV10ToV11_RemovesRetiredPlaybookMappings(t *testing.T) {
	db := testDatabase(t)
	ctx := ctxFor(t)
	if _, err := db.Collection("automation_configurations").InsertMany(ctx, []any{
		bson.M{"_id": "site-1", "playbookMappings": bson.M{
			"install-exporters": "install-exporters", "uninstall-exporters": "uninstall-exporters",
			"deploy-kubernetes": "deploy-kubernetes",
		}},
		bson.M{"_id": "site-2", "enabled": true},
	}); err != nil {
		t.Fatalf("seed automation configurations: %v", err)
	}
	if _, err := db.Collection("schema_metadata").InsertOne(ctx, bson.M{
		"_id": "database", "version": 10, "updatedAt": time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed schema marker: %v", err)
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var site bson.M
	if err := db.Collection("automation_configurations").FindOne(ctx, bson.M{"_id": "site-1"}).Decode(&site); err != nil {
		t.Fatalf("read site-1: %v", err)
	}
	mappings, _ := site["playbookMappings"].(bson.M)
	if len(mappings) != 1 || mappings["deploy-kubernetes"] != "deploy-kubernetes" {
		t.Errorf("playbookMappings = %v, want only deploy-kubernetes", mappings)
	}
	if got := count(t, ctx, db.Collection("automation_configurations"), bson.M{"_id": "site-2", "playbookMappings": bson.M{"$exists": true}}); got != 0 {
		t.Errorf("site-2 gained playbookMappings")
	}
	if got := schemaVersion(t, ctx, db); got != CurrentSchemaVersion {
		t.Errorf("schema version = %d, want %d", got, CurrentSchemaVersion)
	}
}

// TestMigrateV11ToV12_RetiresTheExporterOwner verifies v12 unsets exporterOwner on every Platform,
// drops only the retired Kubernetes exporter kinds' mappings, and needs no backup confirmation.
func TestMigrateV11ToV12_RetiresTheExporterOwner(t *testing.T) {
	db := testDatabase(t)
	ctx := ctxFor(t)
	if _, err := db.Collection("platforms").InsertMany(ctx, []any{
		bson.M{"_id": "platform-1", "name": "k8s", "gpuStackOwner": "provisioning", "exporterOwner": "k8s"},
		bson.M{"_id": "platform-2", "name": "slurm", "gpuStackOwner": "provisioning"},
	}); err != nil {
		t.Fatalf("seed platforms: %v", err)
	}
	if _, err := db.Collection("automation_configurations").InsertMany(ctx, []any{
		bson.M{"_id": "site-1", "playbookMappings": bson.M{
			"deploy-k8s-exporters": "deploy-k8s-exporters", "remove-k8s-exporters": "remove-k8s-exporters",
			"deploy-kubernetes": "deploy-kubernetes",
		}},
		bson.M{"_id": "site-2", "enabled": true},
	}); err != nil {
		t.Fatalf("seed automation configurations: %v", err)
	}
	if _, err := db.Collection("schema_metadata").InsertOne(ctx, bson.M{
		"_id": "database", "version": 11, "updatedAt": time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed schema marker: %v", err)
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if got := count(t, ctx, db.Collection("platforms"), bson.M{"exporterOwner": bson.M{"$exists": true}}); got != 0 {
		t.Errorf("%d platforms still have exporterOwner", got)
	}
	if got := count(t, ctx, db.Collection("platforms"), bson.M{"gpuStackOwner": "provisioning"}); got != 2 {
		t.Errorf("platforms with gpuStackOwner = %d, want both kept", got)
	}
	var site bson.M
	if err := db.Collection("automation_configurations").FindOne(ctx, bson.M{"_id": "site-1"}).Decode(&site); err != nil {
		t.Fatalf("read site-1: %v", err)
	}
	mappings, _ := site["playbookMappings"].(bson.M)
	if len(mappings) != 1 || mappings["deploy-kubernetes"] != "deploy-kubernetes" {
		t.Errorf("playbookMappings = %v, want only deploy-kubernetes", mappings)
	}
	if got := count(t, ctx, db.Collection("automation_configurations"), bson.M{"_id": "site-2", "playbookMappings": bson.M{"$exists": true}}); got != 0 {
		t.Errorf("site-2 gained playbookMappings")
	}
	if got := schemaVersion(t, ctx, db); got != CurrentSchemaVersion {
		t.Errorf("schema version = %d, want %d", got, CurrentSchemaVersion)
	}
}

// A Platform without a Platform Grafana gets monitoringDashboards false in both specs, whatever they
// said, so turning the dashboards on is a change; one with the link keeps its settings.
func TestMigrateV12ToV13_TurnsOffDashboardsNoPlatformHas(t *testing.T) {
	db := testDatabase(t)
	ctx := ctxFor(t)
	if _, err := db.Collection("platforms").InsertMany(ctx, []any{
		bson.M{"_id": "omitted", "spec": bson.M{"settings": `{"k0sVersion":"v1"}`}, "status": bson.M{"applied": bson.M{"settings": `{"k0sVersion":"v1"}`}}},
		bson.M{"_id": "applied-without-install", "spec": bson.M{"settings": `{"monitoringDashboards":true}`},
			"status": bson.M{"applied": bson.M{"settings": `{"monitoringDashboards":true}`}}, "monitoringLinks": []any{bson.M{"name": "Device Metrics Exporter Grafana", "url": "http://x"}}},
		bson.M{"_id": "has-grafana", "spec": bson.M{"settings": `{"slurm":{}}`}, "status": bson.M{"applied": bson.M{"settings": `{"slurm":{}}`}},
			"monitoringLinks": []any{bson.M{"name": "Platform Grafana", "url": "http://y:3000"}}},
		bson.M{"_id": "no-spec", "name": "legacy"},
	}); err != nil {
		t.Fatalf("seed platforms: %v", err)
	}
	if _, err := db.Collection("schema_metadata").InsertOne(ctx, bson.M{
		"_id": "database", "version": 12, "updatedAt": time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed schema marker: %v", err)
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	read := func(id string) bson.M {
		var doc bson.M
		if err := db.Collection("platforms").FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return doc
	}
	settingsOf := func(doc bson.M, path ...string) map[string]any {
		current := doc
		for _, key := range path[:len(path)-1] {
			current, _ = current[key].(bson.M)
		}
		out := map[string]any{}
		if raw, _ := current[path[len(path)-1]].(string); raw != "" {
			_ = json.Unmarshal([]byte(raw), &out)
		}
		return out
	}
	for _, id := range []string{"omitted", "applied-without-install"} {
		doc := read(id)
		for _, path := range [][]string{{"spec", "settings"}, {"status", "applied", "settings"}} {
			if got := settingsOf(doc, path...)["monitoringDashboards"]; got != false {
				t.Errorf("%s %v monitoringDashboards = %v, want false", id, path, got)
			}
		}
	}
	if got := settingsOf(read("omitted"), "spec", "settings")["k0sVersion"]; got != "v1" {
		t.Errorf("other settings were lost: k0sVersion = %v", got)
	}
	if _, ok := settingsOf(read("has-grafana"), "spec", "settings")["monitoringDashboards"]; ok {
		t.Error("a Platform with its Grafana must keep its settings")
	}
	if doc := read("no-spec"); doc["spec"] != nil {
		t.Errorf("a Platform without a spec gained one: %v", doc["spec"])
	}
	if got := schemaVersion(t, ctx, db); got != CurrentSchemaVersion {
		t.Errorf("schema version = %d, want %d", got, CurrentSchemaVersion)
	}
}

// A Platform Grafana link that still opens the Grafana root, or a landing dashboard without its
// slug, opens the landing dashboard of the bundles its Platform's applied settings give it; a link a
// user pointed at another page, the other links, and a Platform of another type keep their URLs.
func TestMigrateV13ToV14_PointsPlatformGrafanaAtItsLandingDashboard(t *testing.T) {
	db := testDatabase(t)
	ctx := ctxFor(t)
	grafana := func(url string) []any {
		return []any{bson.M{"name": "Runbook", "url": "https://wiki.example/x"}, bson.M{"name": "Platform Grafana", "url": url}}
	}
	if _, err := db.Collection("platforms").InsertMany(ctx, []any{
		bson.M{"_id": "k8s-dme", "type": "kubernetes", "monitoringLinks": grafana("http://10.0.0.2:30300"),
			"spec":   bson.M{"settings": `{"gpuOperator":{"enabled":true,"metricsExporter":false}}`},
			"status": bson.M{"applied": bson.M{"settings": `{"gpuOperator":{"enabled":true,"metricsExporter":true}}`}}},
		bson.M{"_id": "k8s-cpu", "type": "kubernetes", "monitoringLinks": grafana("http://10.0.0.3:30300/"),
			"spec": bson.M{"settings": `{"k0sVersion":"v1"}`}},
		bson.M{"_id": "slurm-omnistat", "type": "slurm", "monitoringLinks": grafana("http://10.0.0.4:3000/d/edqnz7pce7togc-rms"),
			"status": bson.M{"applied": bson.M{"settings": `{"omnistat":{"enabled":true}}`}}},
		bson.M{"_id": "slurm-dme", "type": "slurm", "monitoringLinks": grafana("http://10.0.0.5:3000"),
			"status": bson.M{"applied": bson.M{"settings": `{"deviceMetricsExporter":{"enabled":true}}`}}},
		bson.M{"_id": "user-page", "type": "kubernetes", "monitoringLinks": grafana("http://10.0.0.6:30300/d/custom/mine"),
			"status": bson.M{"applied": bson.M{"settings": `{}`}}},
		bson.M{"_id": "other-type", "type": "docker-swarm", "monitoringLinks": grafana("http://10.0.0.7:3000")},
	}); err != nil {
		t.Fatalf("seed platforms: %v", err)
	}
	if _, err := db.Collection("schema_metadata").InsertOne(ctx, bson.M{
		"_id": "database", "version": 13, "updatedAt": time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed schema marker: %v", err)
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	links := func(id string) map[string]string {
		var doc struct {
			Links []struct {
				Name string `bson:"name"`
				URL  string `bson:"url"`
			} `bson:"monitoringLinks"`
		}
		if err := db.Collection("platforms").FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		out := map[string]string{}
		for _, link := range doc.Links {
			out[link.Name] = link.URL
		}
		return out
	}
	want := map[string]string{
		"k8s-dme":        "http://10.0.0.2:30300/d/fe0ayyeeoxvy8f/overview",
		"k8s-cpu":        "http://10.0.0.3:30300/d/swallow-node-exporter/servers",
		"slurm-omnistat": "http://10.0.0.4:3000/d/edqnz7pce7togc-rms/global-rms",
		"slurm-dme":      "http://10.0.0.5:3000/d/fe0ayyeeoxvy8f/overview",
		"user-page":      "http://10.0.0.6:30300/d/custom/mine",
		"other-type":     "http://10.0.0.7:3000",
	}
	for id, url := range want {
		got := links(id)
		if got["Platform Grafana"] != url {
			t.Errorf("%s Platform Grafana = %q, want %q", id, got["Platform Grafana"], url)
		}
		if got["Runbook"] != "https://wiki.example/x" {
			t.Errorf("%s Runbook = %q, want it unchanged", id, got["Runbook"])
		}
	}
	if got := schemaVersion(t, ctx, db); got != CurrentSchemaVersion {
		t.Errorf("schema version = %d, want %d", got, CurrentSchemaVersion)
	}
}

// Each Local Repository keeps fetching where it did: serveDeployments true becomes offline, false or
// none upstream, and an observed source serves exactly when it was ready. A record that already has
// a mode keeps it.
func TestMigrateV14ToV15_TurnsServeDeploymentsIntoTheMode(t *testing.T) {
	db := testDatabase(t)
	ctx := ctxFor(t)
	if _, err := db.Collection("local_repositories").InsertMany(ctx, []any{
		bson.M{"_id": "serving", "generation": 3, "spec": bson.M{"serveDeployments": true, "sources": []any{}},
			"status": bson.M{"sources": []any{
				bson.M{"source": bson.M{"name": "ubuntu"}, "state": "ready"},
				bson.M{"source": bson.M{"name": "docker-ce"}, "state": "syncing"},
			}}},
		bson.M{"_id": "not-serving", "generation": 1, "spec": bson.M{"serveDeployments": false, "sources": []any{}}},
		bson.M{"_id": "observed-only", "generation": 0, "spec": bson.M{"sources": []any{}}, "status": bson.M{"integrationId": "repo"}},
		bson.M{"_id": "already", "generation": 2, "spec": bson.M{"mode": "cache", "sources": []any{}}},
	}); err != nil {
		t.Fatalf("seed local repositories: %v", err)
	}
	if _, err := db.Collection("schema_metadata").InsertOne(ctx, bson.M{
		"_id": "database", "version": 14, "updatedAt": time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed schema marker: %v", err)
	}

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	type record struct {
		Spec struct {
			Mode             string `bson:"mode"`
			ServeDeployments *bool  `bson:"serveDeployments"`
		} `bson:"spec"`
		Status struct {
			Sources []struct {
				Serving *bool `bson:"serving"`
			} `bson:"sources"`
		} `bson:"status"`
	}
	read := func(id string) record {
		var doc record
		if err := db.Collection("local_repositories").FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return doc
	}
	for id, mode := range map[string]string{"serving": "offline", "not-serving": "upstream", "observed-only": "upstream", "already": "cache"} {
		doc := read(id)
		if doc.Spec.Mode != mode || doc.Spec.ServeDeployments != nil {
			t.Errorf("%s spec = mode %q, serveDeployments %v; want mode %q and no serveDeployments", id, doc.Spec.Mode, doc.Spec.ServeDeployments, mode)
		}
	}
	sources := read("serving").Status.Sources
	if len(sources) != 2 || sources[0].Serving == nil || !*sources[0].Serving || sources[1].Serving == nil || *sources[1].Serving {
		t.Errorf("serving after migration = %+v, want the ready source serving and the syncing one not", sources)
	}
	if got := schemaVersion(t, ctx, db); got != CurrentSchemaVersion {
		t.Errorf("schema version = %d, want %d", got, CurrentSchemaVersion)
	}
}
