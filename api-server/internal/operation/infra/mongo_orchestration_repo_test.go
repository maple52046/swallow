package infra

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// workflowTestDatabase connects to the MongoDB named by SWALLOW_TEST_MONGO_URI and returns a
// throwaway database dropped when the test ends. Tests skip when the variable is unset so
// `go test ./...` stays green without a Mongo instance; local verification and CI point it at
// an ephemeral instance (mirrors the migration package's harness).
func workflowTestDatabase(t *testing.T) *mongo.Database {
	t.Helper()
	uri := os.Getenv("SWALLOW_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("SWALLOW_TEST_MONGO_URI not set; skipping workflow repo integration test")
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
	db := client.Database(fmt.Sprintf("swallow_workflow_repo_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ccancel()
		_ = db.Drop(cctx)
		_ = client.Disconnect(cctx)
	})
	return db
}

// TestUpdateStepIsMonotonicForCancel verifies the durable step list never regresses a completed
// Task to canceled: cancelling the enclosing Workflow (which, in the jobbed path, finalizes from
// a possibly-stale snapshot) must not flip an already-succeeded provision-os to canceled, while a
// genuinely in-flight Task still cancels and a missing Task still surfaces ErrTaskNotFound.
func TestUpdateStepIsMonotonicForCancel(t *testing.T) {
	db := workflowTestDatabase(t)
	ctx := context.Background()
	repo, err := NewMongoWorkflowRepo(db)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}

	insert := func(id string, status operationdomain.TaskStatus) {
		if _, err := db.Collection("workflows").InsertOne(ctx, bson.M{
			"_id": id, "schemaVersion": 4,
			"tasks": bson.A{bson.M{
				"id": "provision-a", "kind": "provision-os", "status": string(status), "attempt": 1,
			}},
		}); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	statusOf := func(id string) string {
		var doc struct {
			Tasks []operationdomain.Task `bson:"tasks"`
		}
		if err := db.Collection("workflows").FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		if len(doc.Tasks) == 0 {
			t.Fatalf("%s has no tasks", id)
		}
		return string(doc.Tasks[0].Status)
	}

	cancel := operationdomain.Task{ID: "provision-a", Kind: "provision-os", Attempt: 1, Status: operationdomain.TaskCanceled}

	// A succeeded Task is not regressed to canceled (the reported bug).
	insert("op-succeeded", operationdomain.TaskSucceeded)
	if err := repo.UpdateStep(ctx, "op-succeeded", cancel); err != nil {
		t.Fatalf("cancel of succeeded task: %v", err)
	}
	if got := statusOf("op-succeeded"); got != "succeeded" {
		t.Fatalf("status = %q, want succeeded preserved", got)
	}

	// A non-terminal Task still cancels normally.
	insert("op-running", operationdomain.TaskRunning)
	if err := repo.UpdateStep(ctx, "op-running", cancel); err != nil {
		t.Fatalf("cancel of running task: %v", err)
	}
	if got := statusOf("op-running"); got != "canceled" {
		t.Fatalf("status = %q, want canceled for in-flight task", got)
	}

	// A genuinely missing Task still surfaces ErrTaskNotFound rather than a silent no-op.
	insert("op-missing", operationdomain.TaskRunning)
	err = repo.UpdateStep(ctx, "op-missing", operationdomain.Task{ID: "nonexistent", Status: operationdomain.TaskCanceled})
	if !errors.Is(err, operationdomain.ErrTaskNotFound) {
		t.Fatalf("cancel of missing task err = %v, want ErrTaskNotFound", err)
	}
}
