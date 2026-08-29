package infra

import (
	"context"
	"encoding/json"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/shared/secret"
)

type executionDoc struct {
	RunID          string     `bson:"runId"`
	Playbook       string     `bson:"playbook"`
	Status         string     `bson:"status"`
	StatusReason   string     `bson:"statusReason,omitempty"`
	StartedAt      *time.Time `bson:"startedAt,omitempty"`
	FinishedAt     *time.Time `bson:"finishedAt,omitempty"`
	LeaseOwner     string     `bson:"leaseOwner,omitempty"`
	LeaseExpiresAt *time.Time `bson:"leaseExpiresAt,omitempty"`
}

type executionOperationDoc struct {
	ID              string         `bson:"_id"`
	SchemaVersion   int            `bson:"schemaVersion"`
	Kind            string         `bson:"kind"`
	Intent          string         `bson:"intent,omitempty"`
	SiteID          string         `bson:"siteId"`
	ClusterID       string         `bson:"clusterId,omitempty"`
	TargetServerIDs []string       `bson:"targetServerIds"`
	ExtraVars       map[string]any `bson:"extraVars,omitempty"`
	// SealedSecretVars is an AES-GCM sealed JSON object of run-time secret vars, so a
	// value like a VRRP password is never stored in plain text alongside extraVars.
	SealedSecretVars   string       `bson:"sealedSecretVars,omitempty"`
	RetryOfOperationID string       `bson:"retryOfOperationId,omitempty"`
	Execution          executionDoc `bson:"execution"`
	Terminal           bool         `bson:"terminal"`
	RequestedBy        string       `bson:"requestedBy"`
	RequestedAt        time.Time    `bson:"requestedAt"`
	UpdatedAt          time.Time    `bson:"updatedAt"`
}

// MongoExecutionRepo stores the v2 locally-owned operation schema.
type MongoExecutionRepo struct {
	col    *mongo.Collection
	sealer *secret.Sealer
}

// NewMongoExecutionRepo creates the execution indexes. The sealer protects run-time
// secret vars at rest; it is the same key used for integration credentials.
func NewMongoExecutionRepo(db *mongo.Database, sealer *secret.Sealer) (*MongoExecutionRepo, error) {
	col := db.Collection("operations")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "schemaVersion", Value: 1}, {Key: "requestedAt", Value: -1}},
			Options: options.Index().SetName("execution_requested_at")},
		{Keys: bson.D{{Key: "schemaVersion", Value: 1}, {Key: "terminal", Value: 1}, {Key: "targetServerIds", Value: 1}},
			Options: options.Index().SetName("execution_active_targets")},
		{Keys: bson.D{{Key: "schemaVersion", Value: 1}, {Key: "execution.status", Value: 1}, {Key: "requestedAt", Value: 1}},
			Options: options.Index().SetName("execution_dispatch")},
		{Keys: bson.D{{Key: "schemaVersion", Value: 1}, {Key: "clusterId", Value: 1},
			{Key: "kind", Value: 1}, {Key: "requestedAt", Value: -1}},
			Options: options.Index().SetName("execution_cluster_lifecycle")},
	})
	if err != nil {
		return nil, err
	}
	return &MongoExecutionRepo{col: col, sealer: sealer}, nil
}

// Create persists pending intent before any runner is started. Secret vars are sealed
// here so they never touch the database in plain text.
func (r *MongoExecutionRepo) Create(ctx context.Context, operation *operationdomain.ExecutionOperation) error {
	doc := toExecutionDoc(operation)
	if len(operation.SecretVars) > 0 {
		raw, err := json.Marshal(operation.SecretVars)
		if err != nil {
			return err
		}
		sealed, err := r.sealer.Seal(string(raw))
		if err != nil {
			return err
		}
		doc.SealedSecretVars = sealed
	}
	_, err := r.col.InsertOne(ctx, doc)
	return err
}

// SecretVars decrypts one operation's run-time secret vars for the dispatcher. An
// operation with none returns a nil map and no error.
func (r *MongoExecutionRepo) SecretVars(ctx context.Context, id string) (map[string]any, error) {
	var doc executionOperationDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id, "schemaVersion": 2},
		options.FindOne().SetProjection(bson.M{"sealedSecretVars": 1})).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, operationdomain.ErrOperationNotFound
	}
	if err != nil {
		return nil, err
	}
	if doc.SealedSecretVars == "" {
		return nil, nil
	}
	raw, err := r.sealer.Open(doc.SealedSecretVars)
	if err != nil {
		return nil, err
	}
	var vars map[string]any
	if err := json.Unmarshal([]byte(raw), &vars); err != nil {
		return nil, err
	}
	return vars, nil
}

// FindByID reads one v2 operation.
func (r *MongoExecutionRepo) FindByID(ctx context.Context, id string) (*operationdomain.ExecutionOperation, error) {
	var doc executionOperationDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id, "schemaVersion": 2}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, operationdomain.ErrOperationNotFound
	}
	if err != nil {
		return nil, err
	}
	return fromExecutionDoc(&doc), nil
}

// List returns v2 operations only.
func (r *MongoExecutionRepo) List(ctx context.Context, filter operationdomain.ExecutionListFilter) (operationdomain.ExecutionListResult, error) {
	query := bson.M{"schemaVersion": 2}
	if filter.SiteID != "" {
		query["siteId"] = filter.SiteID
	}
	if filter.ClusterID != "" {
		query["clusterId"] = filter.ClusterID
	} else if len(filter.ClusterIDs) > 0 {
		query["clusterId"] = bson.M{"$in": filter.ClusterIDs}
	}
	if filter.ServerID != "" {
		query["targetServerIds"] = filter.ServerID
	}
	if filter.Kind != "" {
		query["kind"] = string(filter.Kind)
	} else if len(filter.Kinds) > 0 {
		query["kind"] = bson.M{"$in": filter.Kinds}
	}
	if filter.Status != "" {
		query["execution.status"] = string(filter.Status)
	}
	if filter.ActiveOnly {
		query["terminal"] = false
	}

	total, err := r.col.CountDocuments(ctx, query)
	if err != nil {
		return operationdomain.ExecutionListResult{}, err
	}
	opts := options.Find().SetSort(bson.D{{Key: "requestedAt", Value: -1}}).SetSkip(int64(filter.Offset))
	if filter.Limit > 0 {
		opts.SetLimit(int64(filter.Limit))
	}
	cursor, err := r.col.Find(ctx, query, opts)
	if err != nil {
		return operationdomain.ExecutionListResult{}, err
	}
	defer cursor.Close(ctx)
	var docs []executionOperationDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return operationdomain.ExecutionListResult{}, err
	}
	items := make([]*operationdomain.ExecutionOperation, len(docs))
	for i := range docs {
		items[i] = fromExecutionDoc(&docs[i])
	}
	return operationdomain.ExecutionListResult{Operations: items, Total: int(total)}, nil
}

// FindActiveByServerIDs enforces target-overlap guards.
func (r *MongoExecutionRepo) FindActiveByServerIDs(ctx context.Context, ids []string) ([]*operationdomain.ExecutionOperation, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	cursor, err := r.col.Find(ctx, bson.M{
		"schemaVersion": 2, "terminal": false, "targetServerIds": bson.M{"$in": ids},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []executionOperationDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	items := make([]*operationdomain.ExecutionOperation, len(docs))
	for i := range docs {
		items[i] = fromExecutionDoc(&docs[i])
	}
	return items, nil
}

// Claim atomically moves one pending operation to running.
func (r *MongoExecutionRepo) Claim(ctx context.Context, id, owner string, expiresAt time.Time) (bool, error) {
	now := time.Now().UTC()
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id, "schemaVersion": 2, "execution.status": string(operationdomain.StatusPending)},
		bson.M{"$set": bson.M{
			"execution.status":         string(operationdomain.StatusRunning),
			"execution.startedAt":      now,
			"execution.leaseOwner":     owner,
			"execution.leaseExpiresAt": expiresAt,
			"updatedAt":                now,
		}},
	)
	return result != nil && result.ModifiedCount == 1, err
}

// UpdateExecution stores runner progress and terminal state.
func (r *MongoExecutionRepo) UpdateExecution(ctx context.Context, id, owner string, execution operationdomain.Execution) error {
	result, err := r.col.UpdateOne(ctx, bson.M{
		"_id": id, "schemaVersion": 2,
		"execution.status":     string(operationdomain.StatusRunning),
		"execution.leaseOwner": owner,
	}, bson.M{"$set": bson.M{
		"execution": toExecutionSubdoc(execution),
		"terminal":  execution.Status.Terminal(),
		"updatedAt": time.Now().UTC(),
	}})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return operationdomain.ErrExecutionLeaseLost
	}
	return nil
}

// MarkExpiredIndeterminate preserves logs and never retries an interrupted run.
func (r *MongoExecutionRepo) MarkExpiredIndeterminate(ctx context.Context, now time.Time) (int64, error) {
	result, err := r.col.UpdateMany(ctx, bson.M{
		"schemaVersion":            2,
		"execution.status":         string(operationdomain.StatusRunning),
		"execution.leaseExpiresAt": bson.M{"$lt": now},
	}, bson.M{"$set": bson.M{
		"execution.status":       string(operationdomain.StatusIndeterminate),
		"execution.statusReason": "execution lease expired before a terminal result was recorded",
		"execution.finishedAt":   now,
		"terminal":               true,
		"updatedAt":              now,
	}})
	if err != nil {
		return 0, err
	}
	return result.ModifiedCount, nil
}

func toExecutionDoc(operation *operationdomain.ExecutionOperation) executionOperationDoc {
	return executionOperationDoc{
		ID: operation.ID, SchemaVersion: 2, Kind: string(operation.Kind), Intent: operation.Intent,
		SiteID: operation.SiteID, ClusterID: operation.ClusterID, TargetServerIDs: operation.TargetServerIDs,
		ExtraVars: operation.ExtraVars, RetryOfOperationID: operation.RetryOfOperationID,
		Execution: toExecutionSubdoc(operation.Execution),
		Terminal:  operation.Execution.Status.Terminal(), RequestedBy: operation.RequestedBy,
		RequestedAt: operation.RequestedAt, UpdatedAt: operation.UpdatedAt,
	}
}

func toExecutionSubdoc(execution operationdomain.Execution) executionDoc {
	return executionDoc{
		RunID: execution.RunID, Playbook: execution.Playbook, Status: string(execution.Status),
		StatusReason: execution.StatusReason, StartedAt: execution.StartedAt,
		FinishedAt: execution.FinishedAt, LeaseOwner: execution.LeaseOwner,
		LeaseExpiresAt: execution.LeaseExpiresAt,
	}
}

func fromExecutionDoc(doc *executionOperationDoc) *operationdomain.ExecutionOperation {
	return &operationdomain.ExecutionOperation{
		ID: doc.ID, Kind: operationdomain.OperationKind(doc.Kind), Intent: doc.Intent,
		SiteID: doc.SiteID, ClusterID: doc.ClusterID, TargetServerIDs: doc.TargetServerIDs,
		ExtraVars: doc.ExtraVars, RetryOfOperationID: doc.RetryOfOperationID,
		Execution: operationdomain.Execution{
			RunID: doc.Execution.RunID, Playbook: doc.Execution.Playbook,
			Status: operationdomain.Status(doc.Execution.Status), StatusReason: doc.Execution.StatusReason,
			StartedAt: doc.Execution.StartedAt, FinishedAt: doc.Execution.FinishedAt,
			LeaseOwner: doc.Execution.LeaseOwner, LeaseExpiresAt: doc.Execution.LeaseExpiresAt,
		},
		RequestedBy: doc.RequestedBy, RequestedAt: doc.RequestedAt, UpdatedAt: doc.UpdatedAt,
	}
}
