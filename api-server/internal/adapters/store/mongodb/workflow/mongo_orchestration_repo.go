package workflow

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsoncodec"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// MongoWorkflowRepo stores Workflows: their accepted plan and their execution state, of which it
// is the only record (decision 072).
type MongoWorkflowRepo struct {
	operations *mongo.Collection
	events     *mongo.Collection
}

// workflowRegistry decodes an array held in an untyped field (Task parameters, completion params,
// intent) as []any, the shape Runners and completion handlers read it in, instead of the driver's
// primitive.A. The Engine hands Runners Tasks read from the store, not ones that crossed JSON.
var workflowRegistry = func() *bsoncodec.Registry {
	registry := bson.NewRegistry()
	registry.RegisterTypeMapEntry(bson.TypeArray, reflect.TypeOf([]any{}))
	return registry
}()

// NewMongoWorkflowRepo creates the indexes of the Workflow queries.
func NewMongoWorkflowRepo(db *mongo.Database) (*MongoWorkflowRepo, error) {
	repo := &MongoWorkflowRepo{
		operations: db.Collection("workflows", options.Collection().SetRegistry(workflowRegistry)),
		events:     db.Collection("workflow_events"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := repo.operations.Indexes().CreateMany(ctx, []mongo.IndexModel{
		// v2 and v3 share this key shape. MongoDB rejects duplicate key patterns with
		// different names, so retain the compatibility index name until v2 is retired.
		{Keys: bson.D{{Key: "schemaVersion", Value: 1}, {Key: "requestedAt", Value: -1}}, Options: options.Index().SetName("execution_requested_at")},
		{Keys: bson.D{{Key: "schemaVersion", Value: 1}, {Key: "status", Value: 1}, {Key: "targetServerIds", Value: 1}}, Options: options.Index().SetName("operation_v3_active_targets")},
		{Keys: bson.D{{Key: "schemaVersion", Value: 1}, {Key: "platformId", Value: 1}, {Key: "requestedAt", Value: -1}}, Options: options.Index().SetName("operation_v3_platform")},
	})
	if err != nil {
		return nil, err
	}
	_, err = repo.events.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "workflowId", Value: 1}, {Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}, Options: options.Index().SetName("operation_timeline")},
	})
	if err != nil {
		return nil, err
	}
	return repo, nil
}

func (r *MongoWorkflowRepo) Create(ctx context.Context, operation *workflowdomain.Workflow) error {
	operation.SchemaVersion = 4
	_, err := r.operations.InsertOne(ctx, operation)
	return err
}

func (r *MongoWorkflowRepo) FindByID(ctx context.Context, id string) (*workflowdomain.Workflow, error) {
	var operation workflowdomain.Workflow
	err := r.operations.FindOne(ctx, bson.M{"_id": id, "schemaVersion": 4}).Decode(&operation)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, workflowdomain.ErrWorkflowNotV3
	}
	return &operation, err
}

func (r *MongoWorkflowRepo) List(ctx context.Context, filter workflowdomain.WorkflowFilter) ([]*workflowdomain.Workflow, int, error) {
	query := bson.M{"schemaVersion": 4}
	if filter.SiteID != "" {
		query["siteId"] = filter.SiteID
	}
	if filter.PlatformID != "" {
		query["platformId"] = filter.PlatformID
	} else if len(filter.PlatformIDs) > 0 {
		query["platformId"] = bson.M{"$in": filter.PlatformIDs}
	}
	if filter.ServerID != "" {
		query["targetServerIds"] = filter.ServerID
	}
	if filter.Kind != "" {
		query["kind"] = string(filter.Kind)
	}
	if filter.Status != "" {
		query["status"] = string(filter.Status)
	}
	if filter.ActiveOnly {
		query["status"] = bson.M{"$nin": []string{
			string(workflowdomain.WorkflowSucceeded), string(workflowdomain.WorkflowFailed),
			string(workflowdomain.WorkflowPartiallySucceeded), string(workflowdomain.WorkflowCanceled),
		}}
	}
	total, err := r.operations.CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, err
	}
	opts := options.Find().SetSort(bson.D{{Key: "requestedAt", Value: -1}}).SetSkip(int64(filter.Offset))
	if filter.Limit > 0 {
		opts.SetLimit(int64(filter.Limit))
	}
	cursor, err := r.operations.Find(ctx, query, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	var operations []*workflowdomain.Workflow
	if err := cursor.All(ctx, &operations); err != nil {
		return nil, 0, err
	}
	return operations, int(total), nil
}

// AppendEvent records a timeline event idempotently. Projection activities can be retried
// (they run with unlimited attempts), so an event whose ID is deterministic for its
// transition is upserted by _id: a retry re-writes the same document instead of appending a
// duplicate. Callers must supply a stable ID for a given logical transition.
func (r *MongoWorkflowRepo) AppendEvent(ctx context.Context, event workflowdomain.TimelineEvent) error {
	_, err := r.events.ReplaceOne(ctx, bson.M{"_id": event.ID}, event, options.Replace().SetUpsert(true))
	return err
}

func (r *MongoWorkflowRepo) Timeline(ctx context.Context, operationID string) ([]workflowdomain.TimelineEvent, error) {
	cursor, err := r.events.Find(ctx, bson.M{"workflowId": operationID}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var events []workflowdomain.TimelineEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, err
	}
	if events == nil {
		events = []workflowdomain.TimelineEvent{}
	}
	return events, nil
}

// terminalStatuses are the Workflow statuses after which no command or dispatch applies.
var terminalStatuses = bson.A{
	string(workflowdomain.WorkflowSucceeded), string(workflowdomain.WorkflowFailed),
	string(workflowdomain.WorkflowPartiallySucceeded), string(workflowdomain.WorkflowCanceled),
}

// Save implements workflowdomain.WorkflowRepository. It replaces the whole document, so it must
// run inside a transaction that read the Workflow: a concurrent write then aborts and retries the
// transition instead of being overwritten.
func (r *MongoWorkflowRepo) Save(ctx context.Context, workflow *workflowdomain.Workflow) error {
	workflow.UpdatedAt = time.Now().UTC()
	result, err := r.operations.ReplaceOne(ctx, bson.M{"_id": workflow.ID, "schemaVersion": 4}, workflow)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return workflowdomain.ErrWorkflowNotFound
	}
	return nil
}

// UpdateTaskLive implements workflowdomain.WorkflowRepository with one targeted update guarded by
// the dispatch generation and the Task's running attempt.
func (r *MongoWorkflowRepo) UpdateTaskLive(ctx context.Context, id string, generation int64, taskID string, attempt int,
	ref *workflowdomain.ExternalExecutionReference, startedAt *time.Time, live *workflowdomain.TaskLive) error {
	set := bson.M{"updatedAt": time.Now().UTC()}
	if ref != nil {
		set["tasks.$.externalExecution"] = ref
	}
	if startedAt != nil {
		set["tasks.$.startedAt"] = startedAt
	}
	if live != nil {
		set["tasks.$.live"] = live
	}
	result, err := r.operations.UpdateOne(ctx, bson.M{
		"_id": id, "schemaVersion": 4, "dispatch.generation": generation,
		"tasks": bson.M{"$elemMatch": bson.M{
			"id": taskID, "attempt": attempt,
			"status": bson.M{"$in": bson.A{string(workflowdomain.TaskRunning), string(workflowdomain.TaskWaitingExternal)}},
		}},
	}, bson.M{"$set": set})
	if err != nil {
		return err
	}
	if result.MatchedCount == 1 {
		return nil
	}
	workflow, err := r.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if workflow.Dispatch.Generation != generation {
		return workflowdomain.ErrDispatchFenced
	}
	return workflowdomain.ErrTaskNotRunning
}

// RequestCancel implements workflowdomain.WorkflowRepository.
func (r *MongoWorkflowRepo) RequestCancel(ctx context.Context, id string, command workflowdomain.CancelCommand) error {
	result, err := r.operations.UpdateOne(ctx, bson.M{
		"_id": id, "schemaVersion": 4, "status": bson.M{"$nin": terminalStatuses}, "commands.cancel": bson.M{"$exists": false},
	}, bson.M{
		"$set": bson.M{"commands.cancel": command, "updatedAt": time.Now().UTC()},
		"$inc": bson.M{"commands.sequence": 1},
	})
	if err != nil || result.MatchedCount == 1 {
		return err
	}
	workflow, err := r.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if workflow.Status.Terminal() {
		return fmt.Errorf("%w: a finished Operation cannot be canceled", workflowdomain.ErrWorkflowControlConflict)
	}
	return nil
}

// RequestRetry implements workflowdomain.WorkflowRepository.
func (r *MongoWorkflowRepo) RequestRetry(ctx context.Context, id string, command workflowdomain.RetryCommand) error {
	result, err := r.operations.UpdateOne(ctx, bson.M{
		"_id": id, "schemaVersion": 4, "status": bson.M{"$nin": terminalStatuses},
	}, bson.M{
		"$push": bson.M{"commands.retries": command},
		"$set":  bson.M{"updatedAt": time.Now().UTC()},
		"$inc":  bson.M{"commands.sequence": 1},
	})
	if err != nil || result.MatchedCount == 1 {
		return err
	}
	if _, err := r.FindByID(ctx, id); err != nil {
		return err
	}
	return fmt.Errorf("%w: a finished Operation cannot be retried", workflowdomain.ErrWorkflowControlConflict)
}

// MarkCommandsDelivered implements workflowdomain.WorkflowRepository; it never moves the
// delivered sequence backwards.
func (r *MongoWorkflowRepo) MarkCommandsDelivered(ctx context.Context, id string, sequence int64) error {
	_, err := r.operations.UpdateOne(ctx,
		bson.M{"_id": id, "schemaVersion": 4, "dispatch.deliveredCommand": bson.M{"$lt": sequence}},
		bson.M{"$set": bson.M{"dispatch.deliveredCommand": sequence}})
	return err
}

// RecordRun implements workflowdomain.WorkflowRepository.
func (r *MongoWorkflowRepo) RecordRun(ctx context.Context, id string, generation int64, runID string) error {
	_, err := r.operations.UpdateOne(ctx,
		bson.M{"_id": id, "schemaVersion": 4, "dispatch.generation": generation},
		bson.M{"$set": bson.M{"temporal.runId": runID}})
	return err
}

// ListUnfinishedIDs implements workflowdomain.WorkflowRepository.
func (r *MongoWorkflowRepo) ListUnfinishedIDs(ctx context.Context) ([]string, error) {
	cursor, err := r.operations.Find(ctx,
		bson.M{"schemaVersion": 4, "status": bson.M{"$nin": terminalStatuses}},
		options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []struct {
		ID string `bson:"_id"`
	}
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	ids := make([]string, len(docs))
	for i, doc := range docs {
		ids[i] = doc.ID
	}
	return ids, nil
}
