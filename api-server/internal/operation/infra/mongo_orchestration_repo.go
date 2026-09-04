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

// MongoOrchestrationRepo stores v3 Operation intent and its Temporal-owned projection.
type MongoOrchestrationRepo struct {
	operations *mongo.Collection
	events     *mongo.Collection
}

// NewMongoOrchestrationRepo creates indexes used by starter reconciliation and queries.
func NewMongoOrchestrationRepo(db *mongo.Database) (*MongoOrchestrationRepo, error) {
	repo := &MongoOrchestrationRepo{
		operations: db.Collection("operations"),
		events:     db.Collection("operation_events"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := repo.operations.Indexes().CreateMany(ctx, []mongo.IndexModel{
		// v2 and v3 share this key shape. MongoDB rejects duplicate key patterns with
		// different names, so retain the compatibility index name until v2 is retired.
		{Keys: bson.D{{Key: "schemaVersion", Value: 1}, {Key: "requestedAt", Value: -1}}, Options: options.Index().SetName("execution_requested_at")},
		{Keys: bson.D{{Key: "schemaVersion", Value: 1}, {Key: "startState", Value: 1}, {Key: "requestedAt", Value: 1}}, Options: options.Index().SetName("operation_v3_starter")},
		{Keys: bson.D{{Key: "schemaVersion", Value: 1}, {Key: "status", Value: 1}, {Key: "targetServerIds", Value: 1}}, Options: options.Index().SetName("operation_v3_active_targets")},
		{Keys: bson.D{{Key: "schemaVersion", Value: 1}, {Key: "platformId", Value: 1}, {Key: "requestedAt", Value: -1}}, Options: options.Index().SetName("operation_v3_platform")},
	})
	if err != nil {
		return nil, err
	}
	_, err = repo.events.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "operationId", Value: 1}, {Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}, Options: options.Index().SetName("operation_timeline")},
	})
	if err != nil {
		return nil, err
	}
	return repo, nil
}

func (r *MongoOrchestrationRepo) Create(ctx context.Context, operation *operationdomain.OperationV3) error {
	operation.SchemaVersion = 3
	_, err := r.operations.InsertOne(ctx, operation)
	return err
}

func (r *MongoOrchestrationRepo) FindByID(ctx context.Context, id string) (*operationdomain.OperationV3, error) {
	var operation operationdomain.OperationV3
	err := r.operations.FindOne(ctx, bson.M{"_id": id, "schemaVersion": 3}).Decode(&operation)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, operationdomain.ErrOperationNotV3
	}
	return &operation, err
}

func (r *MongoOrchestrationRepo) List(ctx context.Context, filter operationdomain.OrchestrationFilter) ([]*operationdomain.OperationV3, int, error) {
	query := bson.M{"schemaVersion": 3}
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
			string(operationdomain.OrchestrationSucceeded), string(operationdomain.OrchestrationFailed),
			string(operationdomain.OrchestrationPartiallySucceeded), string(operationdomain.OrchestrationCanceled),
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
	var operations []*operationdomain.OperationV3
	if err := cursor.All(ctx, &operations); err != nil {
		return nil, 0, err
	}
	return operations, int(total), nil
}

func (r *MongoOrchestrationRepo) ListPendingStart(ctx context.Context, limit int) ([]*operationdomain.OperationV3, error) {
	if limit <= 0 {
		limit = 100
	}
	cursor, err := r.operations.Find(ctx,
		bson.M{"schemaVersion": 3, "startState": "pending"},
		options.Find().SetSort(bson.D{{Key: "requestedAt", Value: 1}}).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var operations []*operationdomain.OperationV3
	if err := cursor.All(ctx, &operations); err != nil {
		return nil, err
	}
	return operations, nil
}

func (r *MongoOrchestrationRepo) MarkWorkflowStarted(ctx context.Context, id, runID string) error {
	now := time.Now().UTC()
	result, err := r.operations.UpdateOne(ctx,
		bson.M{"_id": id, "schemaVersion": 3, "startState": "pending"},
		bson.M{"$set": bson.M{"startState": "started", "temporal.runId": runID, "updatedAt": now}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		var doc bson.M
		if err := r.operations.FindOne(ctx, bson.M{"_id": id, "schemaVersion": 3}).Decode(&doc); err != nil {
			return err
		}
	}
	return nil
}

func (r *MongoOrchestrationRepo) UpdateState(ctx context.Context, id string, status operationdomain.OrchestrationStatus, reason string, startedAt, finishedAt *time.Time) error {
	set := bson.M{"status": string(status), "statusReason": reason, "updatedAt": time.Now().UTC()}
	if startedAt != nil {
		set["startedAt"] = startedAt
	}
	if finishedAt != nil {
		set["finishedAt"] = finishedAt
	}
	result, err := r.operations.UpdateOne(ctx, bson.M{"_id": id, "schemaVersion": 3}, bson.M{"$set": set})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return operationdomain.ErrOperationNotFound
	}
	return nil
}

func (r *MongoOrchestrationRepo) UpdateStep(ctx context.Context, operationID string, step operationdomain.OperationStep) error {
	result, err := r.operations.UpdateOne(ctx,
		bson.M{"_id": operationID, "schemaVersion": 3, "steps.id": step.ID},
		bson.M{"$set": bson.M{"steps.$": step, "updatedAt": time.Now().UTC()}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return operationdomain.ErrStepNotFound
	}
	return nil
}

func (r *MongoOrchestrationRepo) AppendEvent(ctx context.Context, event operationdomain.TimelineEvent) error {
	_, err := r.events.InsertOne(ctx, event)
	return err
}

func (r *MongoOrchestrationRepo) Timeline(ctx context.Context, operationID string) ([]operationdomain.TimelineEvent, error) {
	cursor, err := r.events.Find(ctx, bson.M{"operationId": operationID}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}))
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
