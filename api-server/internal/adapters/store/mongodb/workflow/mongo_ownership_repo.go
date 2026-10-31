package workflow

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// MongoOwnershipRepo implements workflowdomain.OwnershipRepository over `resource_leases`, one
// document per resource key whose `owner` is the owning Workflow's id (decision 072). The
// collection keeps its name so the API's `leases` and the Server controller's hold read one place.
//
// A document of the pre-ownership lease shape (an `owner` with an `expiresAt`) counts as owned
// until it expires, so ownership and an old run's lease exclude each other while both exist;
// schema v9 converts the old shape.
type MongoOwnershipRepo struct {
	col *mongo.Collection
}

var _ workflowdomain.OwnershipRepository = (*MongoOwnershipRepo)(nil)

// NewMongoOwnershipRepo returns the repository and ensures its owner index.
func NewMongoOwnershipRepo(db *mongo.Database) (*MongoOwnershipRepo, error) {
	col := db.Collection("resource_leases")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "owner", Value: 1}}, Options: options.Index().SetName("resource_owner"),
	}); err != nil {
		return nil, err
	}
	return &MongoOwnershipRepo{col: col}, nil
}

type ownershipDoc struct {
	ResourceKey string     `bson:"_id"`
	Owner       string     `bson:"owner,omitempty"`
	AcquiredAt  time.Time  `bson:"acquiredAt,omitempty"`
	UpdatedAt   time.Time  `bson:"updatedAt,omitempty"`
	ExpiresAt   *time.Time `bson:"expiresAt,omitempty"`
}

// Acquire implements workflowdomain.OwnershipRepository. It reads before it writes, so a resource
// held by another Workflow makes it return false without aborting the caller's transaction; two
// Workflows racing for a free resource conflict on the write, and the losing transaction retries
// and then sees the resource owned.
func (r *MongoOwnershipRepo) Acquire(ctx context.Context, owner string, resourceKeys []string, at time.Time) (bool, error) {
	if len(resourceKeys) == 0 {
		return true, nil
	}
	cursor, err := r.col.Find(ctx, bson.M{"_id": bson.M{"$in": resourceKeys}})
	if err != nil {
		return false, err
	}
	var docs []ownershipDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return false, err
	}
	for _, doc := range docs {
		if doc.Owner == "" || doc.Owner == owner {
			continue
		}
		if doc.ExpiresAt != nil && !doc.ExpiresAt.After(at) {
			continue
		}
		return false, nil
	}
	for _, key := range resourceKeys {
		if _, err := r.col.UpdateOne(ctx, bson.M{"_id": key}, bson.M{
			"$set":   bson.M{"owner": owner, "acquiredAt": at, "updatedAt": at},
			"$unset": bson.M{"expiresAt": ""},
		}, options.Update().SetUpsert(true)); err != nil {
			return false, err
		}
	}
	return true, nil
}

// Release implements workflowdomain.OwnershipRepository.
func (r *MongoOwnershipRepo) Release(ctx context.Context, owner string) error {
	_, err := r.col.DeleteMany(ctx, bson.M{"owner": owner, "expiresAt": bson.M{"$exists": false}})
	return err
}

// FindByOwner implements workflowdomain.OwnershipRepository.
func (r *MongoOwnershipRepo) FindByOwner(ctx context.Context, owner string) ([]workflowdomain.ResourceOwnership, error) {
	cursor, err := r.col.Find(ctx, bson.M{"owner": owner, "expiresAt": bson.M{"$exists": false}},
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var docs []ownershipDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	owned := make([]workflowdomain.ResourceOwnership, len(docs))
	for i, doc := range docs {
		owned[i] = workflowdomain.ResourceOwnership{ResourceKey: doc.ResourceKey, Owner: doc.Owner, AcquiredAt: doc.AcquiredAt}
	}
	return owned, nil
}

// IsHeld implements workflowdomain.OwnershipRepository.
func (r *MongoOwnershipRepo) IsHeld(ctx context.Context, resourceKey string) (bool, error) {
	n, err := r.col.CountDocuments(ctx, bson.M{
		"_id": resourceKey, "owner": bson.M{"$nin": bson.A{"", nil}},
		"$or": bson.A{bson.M{"expiresAt": bson.M{"$exists": false}}, bson.M{"expiresAt": bson.M{"$gt": time.Now().UTC()}}},
	}, options.Count().SetLimit(1))
	return n > 0, err
}
