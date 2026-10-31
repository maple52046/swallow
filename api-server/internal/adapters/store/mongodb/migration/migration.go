// Package migration owns the explicit MongoDB schema version and the versioned,
// resumable upgrade steps that move a database between schema versions.
//
// The binary refuses to serve any database whose recorded schema version differs
// from CurrentSchemaVersion (see Check). Upgrades are applied only by the explicit
// `swallow-api migrate` command, never implicitly at startup, so an operator always
// decides when a destructive schema change runs.
//
// Note on naming: the command is invoked as `swallow-api migrate`; the service
// binary was renamed from `swallow` to `swallow-api` so the CLI binary can own
// the `swallow` name (see repository codebase-structure).
//
// Recovery model: every upgrade step is written to be idempotent and safe to retry.
// A step's version marker is advanced only after its data changes complete, so an
// interrupted migration can be rerun and will resume from the last completed version.
// The v2 -> v3 rename is gated behind an explicit backup confirmation because there
// is no automatic rollback: if a step fails partway, the operator restores the
// verified backup and reruns, rather than the migration attempting to reverse
// partially applied writes.
package migration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CurrentSchemaVersion is the only schema this binary can serve.
//
// v3 renames the managed runtime aggregate from Cluster to Platform: the `clusters`
// collection becomes `platforms`, the `clusterId` reference fields become
// `platformId`, and the `cluster` integration kind becomes `platform`. Document IDs
// are preserved.
//
// v4 renames the durable Operation persistence to Workflow (ADR 017): schema-v3 operation
// documents move from the shared `operations` collection into a dedicated `workflows`
// collection with `steps`->`tasks` and each task's `executor`->`runner`, and the
// `operation_events` / `operation_secrets` collections become `workflow_events` /
// `workflow_secrets` with their `operationId` reference becoming `workflowId`. Schema-v2
// documents stay in `operations` and remain readable for one release.
const CurrentSchemaVersion = 4

type metadata struct {
	ID        string    `bson:"_id"`
	Version   int       `bson:"version"`
	UpdatedAt time.Time `bson:"updatedAt"`
}

// EnsureInitialized creates the version marker for a fresh database but never upgrades
// an existing one.
//
// A database that already holds application collections but has no schema marker is
// treated as an error rather than silently stamped as current: stamping it could mask
// an un-migrated older schema. Such a database must be backed up and given its missing
// schema_metadata record before migration.
func EnsureInitialized(ctx context.Context, db *mongo.Database) error {
	var state metadata
	err := db.Collection("schema_metadata").FindOne(ctx, bson.M{"_id": "database"}).Decode(&state)
	if err == nil {
		return Check(ctx, db)
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return err
	}
	fresh, err := isFreshDatabase(ctx, db)
	if err != nil {
		return err
	}
	if !fresh {
		return errors.New("database has application collections but no schema marker; back it up and restore the missing schema_metadata record before migration")
	}
	col := db.Collection("schema_metadata")
	_, err = col.UpdateOne(ctx, bson.M{"_id": "database"}, bson.M{
		"$setOnInsert": metadata{
			ID: "database", Version: CurrentSchemaVersion, UpdatedAt: time.Now().UTC(),
		},
	}, options.Update().SetUpsert(true))
	if err != nil {
		return err
	}
	return Check(ctx, db)
}

// Check refuses newer, older, or missing schemas so a mismatched binary never serves
// a database it cannot correctly interpret.
func Check(ctx context.Context, db *mongo.Database) error {
	var state metadata
	err := db.Collection("schema_metadata").FindOne(ctx, bson.M{"_id": "database"}).Decode(&state)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return fmt.Errorf("database schema is not initialized; run swallow-api migrate")
	}
	if err != nil {
		return err
	}
	if state.Version != CurrentSchemaVersion {
		return fmt.Errorf("database schema version %d is incompatible with binary schema version %d",
			state.Version, CurrentSchemaVersion)
	}
	return nil
}

// Migrate initializes a fresh database or applies every supported upgrade in order.
//
// A version marker is advanced only after its data changes have completed and is
// updated with an optimistic guard on the previous version, so the command is safe to
// retry after an interruption and fails loudly if another migrator raced it. The
// v2 -> v3 rename rewrites Platform data in place and therefore requires an explicit
// backupConfirmed flag: callers must create and verify a MongoDB backup first, because
// a failed step is recovered by restoring that backup, not by automatic rollback.
func Migrate(ctx context.Context, db *mongo.Database, backupConfirmed ...bool) error {
	var state metadata
	err := db.Collection("schema_metadata").FindOne(ctx, bson.M{"_id": "database"}).Decode(&state)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return EnsureInitialized(ctx, db)
	}
	if err != nil {
		return err
	}
	if state.Version > CurrentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than binary schema version %d",
			state.Version, CurrentSchemaVersion)
	}
	if state.Version < 2 {
		return fmt.Errorf("database schema version %d has no supported upgrade path", state.Version)
	}
	if state.Version == 2 && (len(backupConfirmed) == 0 || !backupConfirmed[0]) {
		return errors.New("schema v3 renames Platform data; create and verify a MongoDB backup, then rerun with backup confirmation")
	}

	for state.Version < CurrentSchemaVersion {
		switch state.Version {
		case 2:
			if err := migrateV2ToV3(ctx, db); err != nil {
				return fmt.Errorf("migrate schema v2 to v3: %w", err)
			}
			state.Version = 3
		case 3:
			if err := migrateV3ToV4(ctx, db); err != nil {
				return fmt.Errorf("migrate schema v3 to v4: %w", err)
			}
			state.Version = 4
		default:
			return fmt.Errorf("database schema version %d has no supported upgrade path", state.Version)
		}
		state.UpdatedAt = time.Now().UTC()
		result, err := db.Collection("schema_metadata").UpdateOne(ctx,
			bson.M{"_id": "database", "version": state.Version - 1},
			bson.M{"$set": bson.M{"version": state.Version, "updatedAt": state.UpdatedAt}},
		)
		if err != nil {
			return err
		}
		if result.MatchedCount != 1 {
			return errors.New("database schema version changed while migration was running")
		}
	}
	return Check(ctx, db)
}

// migrateV2ToV3 renames the managed runtime aggregate from Cluster to Platform without
// regenerating IDs. Every operation is idempotent, so an interrupted command can resume
// safely, and every rewrite is followed by a count verification that fails the migration
// rather than leaving a half-renamed database.
func migrateV2ToV3(ctx context.Context, db *mongo.Database) error {
	names, err := db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return err
	}
	hasClusters, hasPlatforms := contains(names, "clusters"), contains(names, "platforms")
	var clusterCount int64
	if hasClusters {
		clusterCount, err = db.Collection("clusters").CountDocuments(ctx, bson.D{})
		if err != nil {
			return err
		}
	}
	if hasClusters && hasPlatforms {
		oldCount, err := db.Collection("clusters").CountDocuments(ctx, bson.D{})
		if err != nil {
			return err
		}
		newCount, err := db.Collection("platforms").CountDocuments(ctx, bson.D{})
		if err != nil {
			return err
		}
		switch {
		case oldCount > 0 && newCount > 0:
			return fmt.Errorf("both clusters (%d documents) and platforms (%d documents) contain data; "+
				"resolve manually from backup before rerunning", oldCount, newCount)
		case newCount == 0:
			// An empty destination is a leftover from a previous interrupted run; drop
			// it so the rename below can proceed from the authoritative source.
			if err := db.Collection("platforms").Drop(ctx); err != nil {
				return err
			}
			hasPlatforms = false
		case oldCount == 0:
			// The source is an empty leftover; the destination already holds the data.
			if err := db.Collection("clusters").Drop(ctx); err != nil {
				return err
			}
			hasClusters = false
		}
	}
	if hasClusters && !hasPlatforms {
		if err := renamePlatformCollection(ctx, db); err != nil {
			return err
		}
	}
	if hasClusters {
		platformCount, countErr := db.Collection("platforms").CountDocuments(ctx, bson.D{})
		if countErr != nil {
			return countErr
		}
		if platformCount != clusterCount {
			return fmt.Errorf("platform migration count mismatch: before=%d after=%d", clusterCount, platformCount)
		}
	}

	if err := migrateReferenceField(ctx, db.Collection("servers"), "membership.clusterId", "membership.platformId"); err != nil {
		return err
	}
	if err := migrateReferenceField(ctx, db.Collection("operations"), "clusterId", "platformId"); err != nil {
		return err
	}
	if err := migrateIntegrationKind(ctx, db); err != nil {
		return err
	}

	checks := []struct {
		collection string
		filter     bson.M
	}{
		{"servers", bson.M{"membership.clusterId": bson.M{"$exists": true}}},
		{"operations", bson.M{"clusterId": bson.M{"$exists": true}}},
		{"integrations", bson.M{"kind": "cluster"}},
	}
	for _, check := range checks {
		count, countErr := db.Collection(check.collection).CountDocuments(ctx, check.filter)
		if countErr != nil {
			return countErr
		}
		if count != 0 {
			return fmt.Errorf("legacy Cluster references remain in %s: %d", check.collection, count)
		}
	}

	return renamePlatformIndexes(ctx, db)
}

// renamePlatformCollection renames the legacy clusters collection to platforms while
// preserving document identity.
//
// It first attempts the atomic admin renameCollection command, which is instantaneous
// and trivially resumable. Managed MongoDB deployments (for example Atlas) commonly deny
// that privileged command; when the command is unauthorized or unsupported the function
// falls back to a copy-verify-drop that preserves _id values. The fallback drops the
// source only after the destination document count matches, so a rerun after a partial
// copy re-copies by _id (ReplaceOne upsert) and then completes.
func renamePlatformCollection(ctx context.Context, db *mongo.Database) error {
	from := db.Name() + ".clusters"
	to := db.Name() + ".platforms"
	err := db.Client().Database("admin").RunCommand(ctx, bson.D{
		{Key: "renameCollection", Value: from},
		{Key: "to", Value: to},
		{Key: "dropTarget", Value: false},
	}).Err()
	if err == nil {
		return nil
	}
	if !isRenameUnsupported(err) {
		return err
	}
	return copyVerifyDrop(ctx, db, "clusters", "platforms")
}

// copyVerifyDrop copies every document from src to dst preserving _id, verifies the
// destination holds at least as many documents as the source, then drops the source.
// ReplaceOne upsert makes a partial rerun converge instead of failing on duplicate _id.
func copyVerifyDrop(ctx context.Context, db *mongo.Database, srcName, dstName string) error {
	src := db.Collection(srcName)
	dst := db.Collection(dstName)
	cursor, err := src.Find(ctx, bson.D{})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	var srcCount int64
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return err
		}
		id, ok := doc["_id"]
		if !ok {
			return fmt.Errorf("document in %s has no _id and cannot be migrated safely", srcName)
		}
		if _, err := dst.ReplaceOne(ctx, bson.M{"_id": id}, doc, options.Replace().SetUpsert(true)); err != nil {
			return err
		}
		srcCount++
	}
	if err := cursor.Err(); err != nil {
		return err
	}
	dstCount, err := dst.CountDocuments(ctx, bson.D{})
	if err != nil {
		return err
	}
	if dstCount < srcCount {
		return fmt.Errorf("copy from %s to %s incomplete: copied=%d destination=%d", srcName, dstName, srcCount, dstCount)
	}
	return src.Drop(ctx)
}

// isRenameUnsupported reports whether an admin renameCollection failure is a privilege
// or capability problem (so a copy-verify-drop fallback is appropriate) rather than a
// genuine data error that must abort the migration.
func isRenameUnsupported(err error) bool {
	var cmdErr mongo.CommandError
	if errors.As(err, &cmdErr) {
		switch cmdErr.Code {
		case 13, // Unauthorized
			59,   // CommandNotFound
			8000: // AtlasError (command not allowed on the deployment tier)
			return true
		}
		if cmdErr.HasErrorCodeWithMessage(13, "") {
			return true
		}
	}
	return false
}

// migrateIntegrationKind rewrites the `cluster` integration kind to `platform` and
// verifies the counts so no integration is lost or duplicated.
func migrateIntegrationKind(ctx context.Context, db *mongo.Database) error {
	integrations := db.Collection("integrations")
	legacyCount, err := integrations.CountDocuments(ctx, bson.M{"kind": "cluster"})
	if err != nil {
		return err
	}
	platformCount, err := integrations.CountDocuments(ctx, bson.M{"kind": "platform"})
	if err != nil {
		return err
	}
	if _, err := integrations.UpdateMany(ctx,
		bson.M{"kind": "cluster"}, bson.M{"$set": bson.M{"kind": "platform"}},
	); err != nil {
		return err
	}
	remainingLegacy, err := integrations.CountDocuments(ctx, bson.M{"kind": "cluster"})
	if err != nil {
		return err
	}
	migratedPlatform, err := integrations.CountDocuments(ctx, bson.M{"kind": "platform"})
	if err != nil {
		return err
	}
	if remainingLegacy != 0 || migratedPlatform != legacyCount+platformCount {
		return fmt.Errorf("integration role migration count mismatch: legacy=%d platform-before=%d platform-after=%d",
			legacyCount, platformCount, migratedPlatform)
	}
	return nil
}

// renamePlatformIndexes replaces the Cluster-named indexes with their Platform-named
// equivalents. Dropping a missing index is treated as success so the step is idempotent.
func renamePlatformIndexes(ctx context.Context, db *mongo.Database) error {
	if err := dropIndexIfExists(ctx, db.Collection("platforms"), "site_cluster_name"); err != nil {
		return err
	}
	if _, err := db.Collection("platforms").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "siteId", Value: 1}, {Key: "name", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("site_platform_name"),
	}); err != nil {
		return err
	}
	if err := dropIndexIfExists(ctx, db.Collection("servers"), "membership_cluster"); err != nil {
		return err
	}
	if _, err := db.Collection("servers").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "membership.platformId", Value: 1}},
		Options: options.Index().SetSparse(true).SetName("membership_platform"),
	}); err != nil {
		return err
	}
	if err := dropIndexIfExists(ctx, db.Collection("operations"), "execution_cluster_lifecycle"); err != nil {
		return err
	}
	if _, err := db.Collection("operations").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "schemaVersion", Value: 1}, {Key: "platformId", Value: 1},
			{Key: "kind", Value: 1}, {Key: "requestedAt", Value: -1}},
		Options: options.Index().SetName("execution_platform_lifecycle"),
	}); err != nil {
		return err
	}
	return nil
}

// migrateV3ToV4 renames the durable Operation persistence to Workflow (ADR 017). It is
// idempotent: schema-v3 documents are upserted into `workflows` by _id and only removed
// from `operations` after they are safely copied, so an interrupted run resumes cleanly.
// Schema-v2 documents are left in `operations` for read compatibility.
func migrateV3ToV4(ctx context.Context, db *mongo.Database) error {
	source := db.Collection("operations")
	workflows := db.Collection("workflows")

	cursor, err := source.Find(ctx, bson.M{"schemaVersion": 3})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	var movedIDs []any
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return err
		}
		if steps, ok := doc["steps"]; ok {
			doc["tasks"] = renameTaskExecutorField(steps)
			delete(doc, "steps")
		}
		doc["schemaVersion"] = 4
		id, ok := doc["_id"]
		if !ok {
			return fmt.Errorf("v3 operation document has no _id and cannot be migrated safely")
		}
		if _, err := workflows.ReplaceOne(ctx, bson.M{"_id": id}, doc, options.Replace().SetUpsert(true)); err != nil {
			return err
		}
		movedIDs = append(movedIDs, id)
	}
	if err := cursor.Err(); err != nil {
		return err
	}
	if len(movedIDs) > 0 {
		if _, err := source.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": movedIDs}, "schemaVersion": 3}); err != nil {
			return err
		}
	}
	remaining, err := source.CountDocuments(ctx, bson.M{"schemaVersion": 3})
	if err != nil {
		return err
	}
	if remaining != 0 {
		return fmt.Errorf("schema-v3 operation documents remain in operations after move: %d", remaining)
	}

	if err := renameReferenceCollection(ctx, db, "operation_events", "workflow_events", "operationId", "workflowId"); err != nil {
		return err
	}
	if err := renameReferenceCollection(ctx, db, "operation_secrets", "workflow_secrets", "operationId", "workflowId"); err != nil {
		return err
	}
	return nil
}

// renameTaskExecutorField converts a stored steps array into a tasks array, renaming each
// entry's `executor` field to `runner` while preserving its value.
func renameTaskExecutorField(steps any) any {
	arr, ok := steps.(bson.A)
	if !ok {
		return steps
	}
	out := make(bson.A, 0, len(arr))
	for _, item := range arr {
		task, ok := item.(bson.M)
		if !ok {
			if d, okD := item.(bson.D); okD {
				task = d.Map()
			} else {
				out = append(out, item)
				continue
			}
		}
		if executor, ok := task["executor"]; ok {
			task["runner"] = executor
			delete(task, "executor")
		}
		out = append(out, task)
	}
	return out
}

// renameReferenceCollection renames a collection and then renames one reference field in
// the destination. It reuses the admin renameCollection with the copy-verify-drop fallback
// used by the Platform migration, and tolerates a missing source (nothing to rename).
func renameReferenceCollection(ctx context.Context, db *mongo.Database, srcName, dstName, legacyField, newField string) error {
	names, err := db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return err
	}
	hasSrc, hasDst := contains(names, srcName), contains(names, dstName)
	switch {
	case hasSrc && !hasDst:
		from := db.Name() + "." + srcName
		to := db.Name() + "." + dstName
		renameErr := db.Client().Database("admin").RunCommand(ctx, bson.D{
			{Key: "renameCollection", Value: from},
			{Key: "to", Value: to},
			{Key: "dropTarget", Value: false},
		}).Err()
		if renameErr != nil {
			if !isRenameUnsupported(renameErr) {
				return renameErr
			}
			if err := copyVerifyDrop(ctx, db, srcName, dstName); err != nil {
				return err
			}
		}
	case hasSrc && hasDst:
		// Leftover from an interrupted run: merge the source into the destination by _id.
		if err := copyVerifyDrop(ctx, db, srcName, dstName); err != nil {
			return err
		}
	}
	if _, err := db.Collection(dstName).UpdateMany(ctx,
		bson.M{legacyField: bson.M{"$exists": true}},
		bson.M{"$rename": bson.M{legacyField: newField}},
	); err != nil {
		return err
	}
	return nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

// isFreshDatabase reports whether the database holds no application collections beyond
// the schema marker, which is the only state EnsureInitialized may stamp as current.
func isFreshDatabase(ctx context.Context, db *mongo.Database) (bool, error) {
	names, err := db.ListCollectionNames(ctx, bson.M{"name": bson.M{"$ne": "schema_metadata"}})
	if err != nil {
		return false, err
	}
	return len(names) == 0, nil
}

// migrateReferenceField renames a legacy reference field to its Platform equivalent in
// every document of a collection. It refuses to run if any document already carries both
// the legacy and the new field (an ambiguous half-migrated state) and verifies the counts
// afterwards so a rename that silently dropped or duplicated a field fails loudly.
func migrateReferenceField(ctx context.Context, collection *mongo.Collection, legacyField, platformField string) error {
	legacyFilter := bson.M{legacyField: bson.M{"$exists": true}}
	platformFilter := bson.M{platformField: bson.M{"$exists": true}}
	overlap, err := collection.CountDocuments(ctx, bson.M{
		legacyField:   bson.M{"$exists": true},
		platformField: bson.M{"$exists": true},
	})
	if err != nil {
		return err
	}
	if overlap != 0 {
		return fmt.Errorf("%s contains %d documents with both %s and %s", collection.Name(), overlap, legacyField, platformField)
	}
	legacyCount, err := collection.CountDocuments(ctx, legacyFilter)
	if err != nil {
		return err
	}
	platformCount, err := collection.CountDocuments(ctx, platformFilter)
	if err != nil {
		return err
	}
	if _, err := collection.UpdateMany(ctx, legacyFilter,
		bson.M{"$rename": bson.M{legacyField: platformField}},
	); err != nil {
		return err
	}
	remainingLegacy, err := collection.CountDocuments(ctx, legacyFilter)
	if err != nil {
		return err
	}
	migratedPlatform, err := collection.CountDocuments(ctx, platformFilter)
	if err != nil {
		return err
	}
	if remainingLegacy != 0 || migratedPlatform != legacyCount+platformCount {
		return fmt.Errorf("%s reference migration count mismatch: legacy=%d platform-before=%d platform-after=%d",
			collection.Name(), legacyCount, platformCount, migratedPlatform)
	}
	return nil
}

// dropIndexIfExists drops an index by name, treating "index not found" / "namespace not
// found" (codes 27 and 26) as success so index migration is idempotent.
func dropIndexIfExists(ctx context.Context, collection *mongo.Collection, name string) error {
	_, err := collection.Indexes().DropOne(ctx, name)
	var commandErr mongo.CommandError
	if errors.As(err, &commandErr) && (commandErr.Code == 26 || commandErr.Code == 27) {
		return nil
	}
	return err
}
