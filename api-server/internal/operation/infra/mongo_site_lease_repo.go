package infra

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// MongoSiteLeaseRepo provides a cross-process one-run-per-site mutex.
type MongoSiteLeaseRepo struct {
	col *mongo.Collection
}

// NewMongoSiteLeaseRepo uses one document per site.
func NewMongoSiteLeaseRepo(db *mongo.Database) *MongoSiteLeaseRepo {
	return &MongoSiteLeaseRepo{col: db.Collection("operation_site_leases")}
}

// Acquire atomically takes a missing, expired, or already-owned lease.
func (r *MongoSiteLeaseRepo) Acquire(ctx context.Context, siteID, owner string, expiresAt time.Time) (bool, error) {
	now := time.Now().UTC()
	var result bson.M
	err := r.col.FindOneAndUpdate(ctx, bson.M{
		"_id": siteID,
		"$or": bson.A{
			bson.M{"expiresAt": bson.M{"$lte": now}},
			bson.M{"owner": owner},
		},
	}, bson.M{"$set": bson.M{"owner": owner, "expiresAt": expiresAt}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&result)
	if mongo.IsDuplicateKeyError(err) || err == mongo.ErrNoDocuments {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Renew extends a lease only for its current owner.
func (r *MongoSiteLeaseRepo) Renew(ctx context.Context, siteID, owner string, expiresAt time.Time) error {
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": siteID, "owner": owner},
		bson.M{"$set": bson.M{"expiresAt": expiresAt}})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return operationdomain.ErrExecutionLeaseLost
	}
	return nil
}

// Release removes a lease only for its current owner.
func (r *MongoSiteLeaseRepo) Release(ctx context.Context, siteID, owner string) error {
	_, err := r.col.DeleteOne(ctx, bson.M{"_id": siteID, "owner": owner})
	return err
}

// ReleaseExpired removes stale mutexes after their operations become indeterminate.
func (r *MongoSiteLeaseRepo) ReleaseExpired(ctx context.Context, now time.Time) error {
	_, err := r.col.DeleteMany(ctx, bson.M{"expiresAt": bson.M{"$lt": now}})
	return err
}
