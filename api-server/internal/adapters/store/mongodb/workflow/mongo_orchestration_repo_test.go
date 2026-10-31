package workflow

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

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
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

// Commands are only stored (decision 072): a cancel keeps the first request and its sequence, a
// retry appends, and a finished Workflow refuses both so a late control cannot reopen it.
func TestCommandsKeepTheFirstCancelAndRefuseFinishedWorkflows(t *testing.T) {
	db := workflowTestDatabase(t)
	ctx := context.Background()
	repo, err := NewMongoWorkflowRepo(db)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	insert := func(id string, status workflowdomain.WorkflowStatus) {
		if _, err := db.Collection("workflows").InsertOne(ctx, bson.M{
			"_id": id, "schemaVersion": 4, "status": string(status),
			"tasks": bson.A{bson.M{"id": "provision-a", "kind": "provision-os", "status": "running", "attempt": 1}},
		}); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	now := time.Now().UTC()

	insert("op-running", workflowdomain.WorkflowRunning)
	if err := repo.RequestRetry(ctx, "op-running", workflowdomain.RetryCommand{TaskID: "provision-a", RequestedAt: now}); err != nil {
		t.Fatalf("retry: %v", err)
	}
	for _, reason := range []string{"first", "second"} {
		if err := repo.RequestCancel(ctx, "op-running", workflowdomain.CancelCommand{Reason: reason, RequestedAt: now}); err != nil {
			t.Fatalf("cancel %s: %v", reason, err)
		}
	}
	running, err := repo.FindByID(ctx, "op-running")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if running.Commands.Cancel == nil || running.Commands.Cancel.Reason != "first" || running.Commands.Sequence != 2 ||
		len(running.Commands.Retries) != 1 {
		t.Fatalf("commands = %+v, want the retry and the first cancel at sequence 2", running.Commands)
	}

	insert("op-finished", workflowdomain.WorkflowSucceeded)
	if err := repo.RequestCancel(ctx, "op-finished", workflowdomain.CancelCommand{Reason: "late"}); !errors.Is(err, workflowdomain.ErrWorkflowControlConflict) {
		t.Errorf("cancel of a finished Workflow err = %v, want a control conflict", err)
	}
	if err := repo.RequestRetry(ctx, "op-finished", workflowdomain.RetryCommand{TaskID: "provision-a"}); !errors.Is(err, workflowdomain.ErrWorkflowControlConflict) {
		t.Errorf("retry of a finished Workflow err = %v, want a control conflict", err)
	}
	if err := repo.RequestCancel(ctx, "op-missing", workflowdomain.CancelCommand{Reason: "x"}); err == nil {
		t.Error("cancel of a missing Workflow err = nil, want not found")
	}
}

// Runners and completion handlers read the Tasks the Engine loads from the store, so an array in a
// Task's parameters or a completion's params must decode as []any, not as the driver's primitive.A
// (a software install's serverIds would otherwise read as no Servers).
func TestFindByIDDecodesUntypedArraysAsSlices(t *testing.T) {
	db := workflowTestDatabase(t)
	ctx := context.Background()
	repo, err := NewMongoWorkflowRepo(db)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	if err := repo.Create(ctx, &workflowdomain.Workflow{ID: "op-arrays", Steps: []workflowdomain.Task{{
		ID: "configure-software", Parameters: map[string]any{"serverIds": []string{"s1"}},
		Completion: []workflowdomain.Completion{{Type: "software-assignment-failure", Params: map[string]any{"serverIds": []string{"s1"}}}},
	}}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.FindByID(ctx, "op-arrays")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	task := got.Steps[0]
	if _, ok := task.Parameters["serverIds"].([]any); !ok {
		t.Errorf("parameters serverIds = %T, want []any", task.Parameters["serverIds"])
	}
	if _, ok := task.Completion[0].Params["serverIds"].([]any); !ok {
		t.Errorf("completion serverIds = %T, want []any", task.Completion[0].Params["serverIds"])
	}
}

// DeleteRuns removes only the streamed events of the named runs (DF-10).
func TestDeleteRunsRemovesOnlyTheNamedRunsEvents(t *testing.T) {
	db := workflowTestDatabase(t)
	ctx := context.Background()
	repo, err := NewMongoAnsibleEventRepo(db)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	var events []workflowdomain.StreamedTaskEvent
	for _, run := range []string{"run-old", "run-fresh"} {
		events = append(events, workflowdomain.StreamedTaskEvent{RunID: run, Seq: 1, TaskEvent: workflowdomain.TaskEvent{Task: "t", Status: "ok"}})
	}
	if err := repo.AppendEvents(ctx, events); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := repo.DeleteRuns(ctx, []string{"run-old"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if old, _ := repo.ListEvents(ctx, "run-old"); len(old) != 0 {
		t.Errorf("run-old events = %v, want none", old)
	}
	if fresh, _ := repo.ListEvents(ctx, "run-fresh"); len(fresh) != 1 {
		t.Errorf("run-fresh events = %v, want one", fresh)
	}
}
