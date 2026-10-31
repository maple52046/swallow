package workflow

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// MongoAnsibleExecutionRepo stores the executor queue and its unique idempotency key.
type MongoAnsibleExecutionRepo struct{ col *mongo.Collection }

func NewMongoAnsibleExecutionRepo(db *mongo.Database) (*MongoAnsibleExecutionRepo, error) {
	repo := &MongoAnsibleExecutionRepo{col: db.Collection("ansible_executions")}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := repo.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "idempotencyKey", Value: 1}}, Options: options.Index().SetName("ansible_idempotency").SetUnique(true)},
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "createdAt", Value: 1}}, Options: options.Index().SetName("ansible_queue")},
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "leaseExpiresAt", Value: 1}}, Options: options.Index().SetName("ansible_executor_lease")},
	})
	return repo, err
}

func (r *MongoAnsibleExecutionRepo) CreateOrGet(ctx context.Context, execution *workflowdomain.AnsibleExecution) (*workflowdomain.AnsibleExecution, error) {
	_, err := r.col.InsertOne(ctx, execution)
	if err == nil {
		copy := *execution
		return &copy, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return nil, err
	}
	var existing workflowdomain.AnsibleExecution
	if err := r.col.FindOne(ctx, bson.M{"idempotencyKey": execution.IdempotencyKey}).Decode(&existing); err != nil {
		return nil, err
	}
	return &existing, nil
}

func (r *MongoAnsibleExecutionRepo) FindByID(ctx context.Context, id string) (*workflowdomain.AnsibleExecution, error) {
	var execution workflowdomain.AnsibleExecution
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&execution)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, workflowdomain.ErrAnsibleExecutionNotFound
	}
	return &execution, err
}

func (r *MongoAnsibleExecutionRepo) ClaimNext(ctx context.Context, owner string, expiresAt time.Time) (*workflowdomain.AnsibleExecution, error) {
	now := time.Now().UTC()
	var execution workflowdomain.AnsibleExecution
	err := r.col.FindOneAndUpdate(ctx,
		bson.M{"status": string(workflowdomain.AnsibleQueued), "cancelRequested": bson.M{"$ne": true}},
		bson.M{"$set": bson.M{"status": string(workflowdomain.AnsibleRunning), "leaseOwner": owner,
			"leaseExpiresAt": expiresAt, "startedAt": now, "updatedAt": now}},
		options.FindOneAndUpdate().SetSort(bson.D{{Key: "createdAt", Value: 1}}).SetReturnDocument(options.After),
	).Decode(&execution)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	return &execution, err
}

func (r *MongoAnsibleExecutionRepo) Renew(ctx context.Context, id, owner string, expiresAt time.Time) error {
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id, "status": string(workflowdomain.AnsibleRunning), "leaseOwner": owner},
		bson.M{"$set": bson.M{"leaseExpiresAt": expiresAt, "updatedAt": time.Now().UTC()}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount != 1 {
		return workflowdomain.ErrAnsibleExecutionLeaseLost
	}
	return nil
}

// UpdateProgress records live run progress for the lease-holding executor. It is best-effort: a
// zero MatchedCount (lease lost or execution already finished) is not an error, because progress is
// advisory and the authoritative outcome is written by Finish under the same owner guard.
func (r *MongoAnsibleExecutionRepo) UpdateProgress(ctx context.Context, id, owner string, progress workflowdomain.AnsibleRunProgress) error {
	progress.UpdatedAt = time.Now().UTC()
	_, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id, "status": string(workflowdomain.AnsibleRunning), "leaseOwner": owner},
		bson.M{"$set": bson.M{"progress": progress, "updatedAt": progress.UpdatedAt}},
	)
	return err
}

func (r *MongoAnsibleExecutionRepo) Finish(ctx context.Context, id, owner string, status workflowdomain.AnsibleExecutionStatus, reason string) error {
	now := time.Now().UTC()
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id, "status": string(workflowdomain.AnsibleRunning), "leaseOwner": owner},
		bson.M{"$set": bson.M{"status": string(status), "statusReason": reason, "finishedAt": now,
			"updatedAt": now}, "$unset": bson.M{"leaseOwner": "", "leaseExpiresAt": ""}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount != 1 {
		return workflowdomain.ErrAnsibleExecutionLeaseLost
	}
	return nil
}

func (r *MongoAnsibleExecutionRepo) RequestCancel(ctx context.Context, id string) error {
	now := time.Now().UTC()
	queued, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "status": string(workflowdomain.AnsibleQueued)},
		bson.M{"$set": bson.M{"status": string(workflowdomain.AnsibleCanceled), "cancelRequested": true, "finishedAt": now, "updatedAt": now}})
	if err != nil {
		return err
	}
	if queued.MatchedCount == 1 {
		return nil
	}
	running, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "status": string(workflowdomain.AnsibleRunning)},
		bson.M{"$set": bson.M{"cancelRequested": true, "updatedAt": now}})
	if err != nil {
		return err
	}
	if running.MatchedCount == 0 {
		if _, findErr := r.FindByID(ctx, id); findErr != nil {
			return findErr
		}
	}
	return nil
}

func (r *MongoAnsibleExecutionRepo) MarkExpiredRequiresAttention(ctx context.Context, now time.Time) (int64, error) {
	result, err := r.col.UpdateMany(ctx, bson.M{
		"status": string(workflowdomain.AnsibleRunning), "leaseExpiresAt": bson.M{"$lt": now},
	}, bson.M{"$set": bson.M{
		"status":       string(workflowdomain.AnsibleRequiresAttention),
		"statusReason": "The Ansible executor stopped before it recorded an outcome. Verify the hosts and retry this Step explicitly.",
		"finishedAt":   now, "updatedAt": now,
	}, "$unset": bson.M{"leaseOwner": "", "leaseExpiresAt": ""}})
	if err != nil {
		return 0, err
	}
	return result.ModifiedCount, nil
}
