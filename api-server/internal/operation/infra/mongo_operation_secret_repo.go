package infra

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/shared/secret"
)

type operationSecretDoc struct {
	ID          string    `bson:"_id"`
	OperationID string    `bson:"workflowId"`
	Name        string    `bson:"name"`
	SealedValue string    `bson:"sealedValue"`
	CreatedAt   time.Time `bson:"createdAt"`
}

// MongoOperationSecretRepo seals Step inputs and exposes only opaque references.
type MongoOperationSecretRepo struct {
	col    *mongo.Collection
	sealer *secret.Sealer
}

func NewMongoOperationSecretRepo(db *mongo.Database, sealer *secret.Sealer) (*MongoOperationSecretRepo, error) {
	repo := &MongoOperationSecretRepo{col: db.Collection("workflow_secrets"), sealer: sealer}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := repo.col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "workflowId", Value: 1}, {Key: "name", Value: 1}},
		Options: options.Index().SetName("operation_secret_name").SetUnique(true),
	})
	return repo, err
}

func (r *MongoOperationSecretRepo) Store(ctx context.Context, operationID, name string, value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sealed, err := r.sealer.Seal(string(raw))
	if err != nil {
		return "", err
	}
	doc := operationSecretDoc{ID: uuid.NewString(), OperationID: operationID, Name: name, SealedValue: sealed, CreatedAt: time.Now().UTC()}
	_, err = r.col.InsertOne(ctx, doc)
	if mongo.IsDuplicateKeyError(err) {
		var existing operationSecretDoc
		if findErr := r.col.FindOne(ctx, bson.M{"workflowId": operationID, "name": name}).Decode(&existing); findErr != nil {
			return "", findErr
		}
		return existing.ID, nil
	}
	return doc.ID, err
}

func (r *MongoOperationSecretRepo) Resolve(ctx context.Context, reference string) (any, error) {
	var doc operationSecretDoc
	err := r.col.FindOne(ctx, bson.M{"_id": reference}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, operationdomain.ErrWorkflowNotFound
	}
	if err != nil {
		return nil, err
	}
	raw, err := r.sealer.Open(doc.SealedValue)
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, err
	}
	return value, nil
}

func (r *MongoOperationSecretRepo) DeleteForOperation(ctx context.Context, operationID string) error {
	_, err := r.col.DeleteMany(ctx, bson.M{"workflowId": operationID})
	return err
}
