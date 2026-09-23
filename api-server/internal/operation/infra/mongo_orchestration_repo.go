package infra

import (
	"context"
	"errors"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// MongoWorkflowRepo stores v3 Operation intent and its Temporal-owned projection.
type MongoWorkflowRepo struct {
	operations *mongo.Collection
	events     *mongo.Collection
}

// NewMongoWorkflowRepo creates indexes used by starter reconciliation and queries.
func NewMongoWorkflowRepo(db *mongo.Database) (*MongoWorkflowRepo, error) {
	repo := &MongoWorkflowRepo{
		operations: db.Collection("workflows"),
		events:     db.Collection("workflow_events"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := repo.operations.Indexes().CreateMany(ctx, []mongo.IndexModel{
		// v2 and v3 share this key shape. MongoDB rejects duplicate key patterns with
		// different names, so retain the compatibility index name until v2 is retired.
		{Keys: bson.D{{Key: "schemaVersion", Value: 1}, {Key: "requestedAt", Value: -1}}, Options: options.Index().SetName("execution_requested_at")},
		{Keys: bson.D{{Key: "schemaVersion", Value: 1}, {Key: "startState", Value: 1}, {Key: "requestedAt", Value: 1}}, Options: options.Index().SetName("operation_v3_starter")},
		// Backs the lost-execution reconciler sweep, which selects started records still in a
		// forward-progress status. Keeping status in the key lets the sweep skip the many
		// started-and-terminal history records instead of scanning them every interval.
		{Keys: bson.D{{Key: "schemaVersion", Value: 1}, {Key: "startState", Value: 1}, {Key: "status", Value: 1}}, Options: options.Index().SetName("operation_v3_recovery")},
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

func (r *MongoWorkflowRepo) Create(ctx context.Context, operation *operationdomain.Workflow) error {
	operation.SchemaVersion = 4
	_, err := r.operations.InsertOne(ctx, operation)
	return err
}

func (r *MongoWorkflowRepo) FindByID(ctx context.Context, id string) (*operationdomain.Workflow, error) {
	var operation operationdomain.Workflow
	err := r.operations.FindOne(ctx, bson.M{"_id": id, "schemaVersion": 4}).Decode(&operation)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, operationdomain.ErrWorkflowNotV3
	}
	return &operation, err
}

func (r *MongoWorkflowRepo) List(ctx context.Context, filter operationdomain.WorkflowFilter) ([]*operationdomain.Workflow, int, error) {
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
			string(operationdomain.WorkflowSucceeded), string(operationdomain.WorkflowFailed),
			string(operationdomain.WorkflowPartiallySucceeded), string(operationdomain.WorkflowCanceled),
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
	var operations []*operationdomain.Workflow
	if err := cursor.All(ctx, &operations); err != nil {
		return nil, 0, err
	}
	return operations, int(total), nil
}

func (r *MongoWorkflowRepo) ListPendingStart(ctx context.Context, limit int) ([]*operationdomain.Workflow, error) {
	if limit <= 0 {
		limit = 100
	}
	cursor, err := r.operations.Find(ctx,
		bson.M{"schemaVersion": 4, "startState": "pending"},
		options.Find().SetSort(bson.D{{Key: "requestedAt", Value: 1}}).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var operations []*operationdomain.Workflow
	if err := cursor.All(ctx, &operations); err != nil {
		return nil, err
	}
	return operations, nil
}

// ListNonTerminalStarted returns started Operations still in a forward-progress status so the
// lost-execution reconciler can check each against Temporal. It excludes requires_attention
// (already surfaced as repairable) and canceling (a deliberate teardown), and only ever reads
// started records, so an unstarted pending record awaiting the starter is never mistaken for a
// lost execution.
func (r *MongoWorkflowRepo) ListNonTerminalStarted(ctx context.Context, limit int) ([]*operationdomain.Workflow, error) {
	if limit <= 0 {
		limit = 100
	}
	cursor, err := r.operations.Find(ctx,
		bson.M{
			"schemaVersion": 4,
			"startState":    "started",
			"status": bson.M{"$in": []string{
				string(operationdomain.WorkflowPending),
				string(operationdomain.WorkflowWaitingDependency),
				string(operationdomain.WorkflowRunning),
				string(operationdomain.WorkflowWaitingExternal),
			}},
		},
		options.Find().SetSort(bson.D{{Key: "requestedAt", Value: 1}}).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var operations []*operationdomain.Workflow
	if err := cursor.All(ctx, &operations); err != nil {
		return nil, err
	}
	return operations, nil
}

func (r *MongoWorkflowRepo) MarkWorkflowStarted(ctx context.Context, id, runID string) error {
	now := time.Now().UTC()
	result, err := r.operations.UpdateOne(ctx,
		bson.M{"_id": id, "schemaVersion": 4, "startState": "pending"},
		bson.M{"$set": bson.M{"startState": "started", "temporal.runId": runID, "updatedAt": now}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		var doc bson.M
		if err := r.operations.FindOne(ctx, bson.M{"_id": id, "schemaVersion": 4}).Decode(&doc); err != nil {
			return err
		}
	}
	return nil
}

func (r *MongoWorkflowRepo) UpdateState(ctx context.Context, id string, status operationdomain.WorkflowStatus, reason string, startedAt, finishedAt *time.Time) error {
	set := bson.M{"status": string(status), "statusReason": reason, "updatedAt": time.Now().UTC()}
	if startedAt != nil {
		set["startedAt"] = startedAt
	}
	if finishedAt != nil {
		set["finishedAt"] = finishedAt
	}
	result, err := r.operations.UpdateOne(ctx, bson.M{"_id": id, "schemaVersion": 4}, bson.M{"$set": set})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return operationdomain.ErrWorkflowNotFound
	}
	return nil
}

func (r *MongoWorkflowRepo) UpdateStep(ctx context.Context, operationID string, step operationdomain.Task) error {
	filter := bson.M{"_id": operationID, "schemaVersion": 4, "tasks.id": step.ID}
	// Monotonic guard: a `canceled` write must never regress a Task that already reached a
	// terminal outcome (succeeded/failed/skipped). Cancelling the enclosing Workflow finalizes
	// from a step snapshot that, in the jobbed path, can be stale (the parent never merges child
	// Job outcomes), so without this a completed provision-os would be flipped to canceled in the
	// durable step list and cascade onto the Server deployment axis. Non-terminal Tasks
	// (pending/running/waiting) still cancel normally.
	if step.Status == operationdomain.TaskCanceled {
		filter = bson.M{
			"_id":           operationID,
			"schemaVersion": 4,
			"tasks": bson.M{"$elemMatch": bson.M{
				"id":     step.ID,
				"status": bson.M{"$nin": bson.A{operationdomain.TaskSucceeded, operationdomain.TaskFailed, operationdomain.TaskSkipped}},
			}},
		}
	}
	result, err := r.operations.UpdateOne(ctx, filter,
		bson.M{"$set": bson.M{"tasks.$": step, "updatedAt": time.Now().UTC()}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		// For a guarded `canceled` write, zero matches can mean the Task is already terminal (a
		// monotonic no-op, not an error) rather than genuinely missing; distinguish the two so a
		// real missing Task still surfaces ErrTaskNotFound.
		if step.Status == operationdomain.TaskCanceled {
			exists, existErr := r.taskExists(ctx, operationID, step.ID)
			if existErr != nil {
				return existErr
			}
			if exists {
				return nil
			}
		}
		return operationdomain.ErrTaskNotFound
	}
	return nil
}

// taskExists reports whether the Operation still carries a Task with this id, used to tell a
// monotonic-guard no-op (the Task is present but already terminal) apart from a genuinely
// missing Task when a guarded update matches nothing.
func (r *MongoWorkflowRepo) taskExists(ctx context.Context, operationID, stepID string) (bool, error) {
	count, err := r.operations.CountDocuments(ctx,
		bson.M{"_id": operationID, "schemaVersion": 4, "tasks.id": stepID},
		options.Count().SetLimit(1),
	)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// UpdateStepLive sets only the live fields of a running step via a positional $set, so a mid-run
// writer never overwrites the step's intent (targets, parameters, dependencies) the way the
// full-document UpdateStep would. Nil arguments are skipped; startedAt is written verbatim, so the
// caller passes it only on the first observation to avoid resetting it on every poll.
func (r *MongoWorkflowRepo) UpdateStepLive(ctx context.Context, operationID, stepID string, ref *operationdomain.ExternalExecutionReference, startedAt *time.Time, live *operationdomain.TaskLive) error {
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
	result, err := r.operations.UpdateOne(ctx,
		bson.M{"_id": operationID, "schemaVersion": 4, "tasks.id": stepID},
		bson.M{"$set": set},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return operationdomain.ErrTaskNotFound
	}
	return nil
}

// AppendEvent records a timeline event idempotently. Projection activities can be retried
// (they run with unlimited attempts), so an event whose ID is deterministic for its
// transition is upserted by _id: a retry re-writes the same document instead of appending a
// duplicate. Callers must supply a stable ID for a given logical transition.
func (r *MongoWorkflowRepo) AppendEvent(ctx context.Context, event operationdomain.TimelineEvent) error {
	_, err := r.events.ReplaceOne(ctx, bson.M{"_id": event.ID}, event, options.Replace().SetUpsert(true))
	return err
}

func (r *MongoWorkflowRepo) Timeline(ctx context.Context, operationID string) ([]operationdomain.TimelineEvent, error) {
	cursor, err := r.events.Find(ctx, bson.M{"workflowId": operationID}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var events []operationdomain.TimelineEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, err
	}
	if events == nil {
		events = []operationdomain.TimelineEvent{}
	}
	return events, nil
}

// MongoResourceLeaseRepo uses monotonically increasing fencing tokens so a worker whose
// lease expired cannot perform a late side effect after another workflow acquired it.
type MongoResourceLeaseRepo struct {
	col *mongo.Collection
}

func NewMongoResourceLeaseRepo(db *mongo.Database) *MongoResourceLeaseRepo {
	return &MongoResourceLeaseRepo{col: db.Collection("resource_leases")}
}

func (r *MongoResourceLeaseRepo) Acquire(ctx context.Context, resourceKeys []string, owner string, expiresAt time.Time) ([]operationdomain.ResourceLease, error) {
	keys := append([]string(nil), resourceKeys...)
	sort.Strings(keys)
	leases := make([]operationdomain.ResourceLease, 0, len(keys))
	now := time.Now().UTC()
	for _, key := range keys {
		var doc operationdomain.ResourceLease
		err := r.col.FindOneAndUpdate(ctx, bson.M{
			"_id": key,
			"$or": []bson.M{{"expiresAt": bson.M{"$lte": now}}, {"owner": owner}, {"owner": bson.M{"$exists": false}}},
		}, bson.M{
			"$set": bson.M{"owner": owner, "expiresAt": expiresAt, "updatedAt": now},
			"$inc": bson.M{"fencingToken": 1},
		}, options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)).Decode(&doc)
		if err != nil {
			_ = r.Release(ctx, leases)
			if errors.Is(err, mongo.ErrNoDocuments) || mongo.IsDuplicateKeyError(err) {
				return nil, operationdomain.ErrLeaseConflict
			}
			return nil, err
		}
		leases = append(leases, doc)
	}
	return leases, nil
}

func (r *MongoResourceLeaseRepo) Renew(ctx context.Context, leases []operationdomain.ResourceLease, expiresAt time.Time) error {
	for _, lease := range leases {
		result, err := r.col.UpdateOne(ctx, leaseFilter(lease), bson.M{"$set": bson.M{"expiresAt": expiresAt, "updatedAt": time.Now().UTC()}})
		if err != nil {
			return err
		}
		if result.MatchedCount != 1 {
			return operationdomain.ErrLeaseFenced
		}
	}
	return nil
}

func (r *MongoResourceLeaseRepo) Validate(ctx context.Context, lease operationdomain.ResourceLease) error {
	count, err := r.col.CountDocuments(ctx, bson.M{
		"_id": lease.ResourceKey, "owner": lease.Owner, "fencingToken": lease.FencingToken,
		"expiresAt": bson.M{"$gt": time.Now().UTC()},
	})
	if err != nil {
		return err
	}
	if count != 1 {
		return operationdomain.ErrLeaseFenced
	}
	return nil
}

func (r *MongoResourceLeaseRepo) Release(ctx context.Context, leases []operationdomain.ResourceLease) error {
	for _, lease := range leases {
		_, err := r.col.UpdateOne(ctx, leaseFilter(lease), bson.M{"$unset": bson.M{"owner": "", "expiresAt": ""}, "$set": bson.M{"updatedAt": time.Now().UTC()}})
		if err != nil {
			return err
		}
	}
	return nil
}

func leaseFilter(lease operationdomain.ResourceLease) bson.M {
	return bson.M{"_id": lease.ResourceKey, "owner": lease.Owner, "fencingToken": lease.FencingToken}
}

// FindByOwner returns currently held leases for one workflow in stable resource order.
func (r *MongoResourceLeaseRepo) FindByOwner(ctx context.Context, owner string) ([]operationdomain.ResourceLease, error) {
	cursor, err := r.col.Find(ctx, bson.M{"owner": owner, "expiresAt": bson.M{"$gt": time.Now().UTC()}},
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var leases []operationdomain.ResourceLease
	if err := cursor.All(ctx, &leases); err != nil {
		return nil, err
	}
	if leases == nil {
		leases = []operationdomain.ResourceLease{}
	}
	return leases, nil
}
