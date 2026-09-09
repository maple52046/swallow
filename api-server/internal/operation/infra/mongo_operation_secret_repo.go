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

// CloneForOperation copies every sealed secret of sourceOperationID into new documents owned
// by targetOperationID and returns a source-reference to clone-reference map for the caller to
// rewrite Step SecretRefs. The sealed ciphertext is copied verbatim, so the value is never
// opened here; each clone gets a fresh document id (the reference) and the target owner, which
// keeps the (workflowId, name) uniqueness intact and makes the clones independent of the
// source's lifetime. A source with no secrets returns an empty, non-nil map.
func (r *MongoOperationSecretRepo) CloneForOperation(ctx context.Context, sourceOperationID, targetOperationID string) (map[string]string, error) {
	cursor, err := r.col.Find(ctx, bson.M{"workflowId": sourceOperationID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var sources []operationSecretDoc
	if err := cursor.All(ctx, &sources); err != nil {
		return nil, err
	}
	references := make(map[string]string, len(sources))
	now := time.Now().UTC()
	for _, source := range sources {
		clone := operationSecretDoc{
			ID: uuid.NewString(), OperationID: targetOperationID,
			Name: source.Name, SealedValue: source.SealedValue, CreatedAt: now,
		}
		if _, err := r.col.InsertOne(ctx, clone); err != nil {
			return nil, err
		}
		references[source.ID] = clone.ID
	}
	return references, nil
}

func (r *MongoOperationSecretRepo) DeleteForOperation(ctx context.Context, operationID string) error {
	_, err := r.col.DeleteMany(ctx, bson.M{"workflowId": operationID})
	return err
}
