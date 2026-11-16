package migration

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

// v17 gives each user suite its author's user id where a User has the recorded username (none where
// no User does), and keeps its current revision as its first revision, with the retired runAs read
// as sudo; a second run changes nothing.
func TestMigrateV16ToV17_RecordsAuthorsAndFirstRevisions(t *testing.T) {
	db := testDatabase(t)
	ctx := ctxFor(t)
	updated := time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)
	if _, err := db.Collection("users").InsertOne(ctx, bson.M{"_id": "user-alice", "username": "alice"}); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if _, err := db.Collection("validation_suites").InsertMany(ctx, []any{
		bson.M{"_id": "by-alice", "name": "mounts", "script": "#!/bin/sh\nexit 0\n", "scriptSha256": "aaa", "runAs": "root",
			"timeoutSeconds": 120, "impact": "observe", "revision": 4, "retired": false, "createdAt": updated, "updatedAt": updated, "createdBy": "alice"},
		bson.M{"_id": "by-gone", "name": "old", "script": "exit 1", "scriptSha256": "bbb", "runAs": "automation-user",
			"timeoutSeconds": 600, "impact": "workload", "revision": 1, "retired": true, "createdAt": updated, "updatedAt": updated, "createdBy": "gone"},
	}); err != nil {
		t.Fatalf("seed suites: %v", err)
	}
	if _, err := db.Collection("schema_metadata").InsertOne(ctx, bson.M{"_id": "database", "version": 16, "updatedAt": time.Now().UTC()}); err != nil {
		t.Fatalf("seed schema marker: %v", err)
	}
	for range 2 {
		if err := Migrate(ctx, db); err != nil {
			t.Fatalf("Migrate: %v", err)
		}
	}
	authors := map[string]any{}
	for _, id := range []string{"by-alice", "by-gone"} {
		var suite bson.M
		if err := db.Collection("validation_suites").FindOne(ctx, bson.M{"_id": id}).Decode(&suite); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		authors[id] = suite["createdById"]
		if suite["runAs"] == nil {
			t.Errorf("%s lost runAs, which the store maps at read time", id)
		}
	}
	if authors["by-alice"] != "user-alice" || authors["by-gone"] != nil {
		t.Errorf("createdById = %v, want alice's id on her suite and none on the deleted author's", authors)
	}
	for id, want := range map[string]bson.M{
		"by-alice/4": {"suiteId": "by-alice", "revision": int32(4), "script": "#!/bin/sh\nexit 0\n", "scriptSha256": "aaa", "sudo": true,
			"starterLevel": "owner", "timeoutSeconds": int32(120), "impact": "observe"},
		"by-gone/1": {"suiteId": "by-gone", "revision": int32(1), "sudo": false, "starterLevel": "owner"},
	} {
		var revision bson.M
		if err := db.Collection("validation_suite_revisions").FindOne(ctx, bson.M{"_id": id}).Decode(&revision); err != nil {
			t.Fatalf("read revision %s: %v", id, err)
		}
		for key, value := range want {
			if revision[key] != value {
				t.Errorf("revision %s %s = %v (%T), want %v", id, key, revision[key], revision[key], value)
			}
		}
	}
	if got := count(t, ctx, db.Collection("validation_suite_revisions"), bson.M{}); got != 2 {
		t.Errorf("%d revisions after two runs, want 2", got)
	}
	if got := schemaVersion(t, ctx, db); got != CurrentSchemaVersion {
		t.Errorf("schema version = %d, want %d", got, CurrentSchemaVersion)
	}
}
