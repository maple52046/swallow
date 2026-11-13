package access

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
)

const membershipCollection = "pool_memberships"

// membershipDoc is the stored shape of a Pool Membership.
type membershipDoc struct {
	ID               string    `bson:"_id"`
	PoolID           string    `bson:"poolId"`
	SiteID           string    `bson:"siteId"`
	UserID           string    `bson:"userId"`
	Role             string    `bson:"role"`
	Sudo             string    `bson:"sudo"`
	SudoPasswordless string    `bson:"sudoPasswordless"`
	CreatedAt        time.Time `bson:"createdAt"`
	UpdatedAt        time.Time `bson:"updatedAt"`
}

// MongoMemberships is the durable accessdomain.MembershipRepository. Its unique (poolId, userId)
// index is what makes a second membership of the same User in a Pool ErrMembershipExists, even for
// concurrent writers.
type MongoMemberships struct {
	col *mongo.Collection
}

// NewMongoMemberships creates the repository and its indexes; index creation is idempotent.
func NewMongoMemberships(ctx context.Context, db *mongo.Database) (*MongoMemberships, error) {
	col := db.Collection(membershipCollection)
	_, err := col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "poolId", Value: 1}, {Key: "userId", Value: 1}}, Options: options.Index().SetUnique(true).SetName("pool_user_unique")},
		{Keys: bson.D{{Key: "userId", Value: 1}}, Options: options.Index().SetName("user")},
	})
	if err != nil {
		return nil, fmt.Errorf("pool_memberships indexes: %w", err)
	}
	return &MongoMemberships{col: col}, nil
}

var _ accessdomain.MembershipRepository = (*MongoMemberships)(nil)

// Create implements accessdomain.MembershipRepository.
func (r *MongoMemberships) Create(ctx context.Context, m *accessdomain.Membership) error {
	_, err := r.col.InsertOne(ctx, toMembershipDoc(m))
	if mongo.IsDuplicateKeyError(err) {
		return accessdomain.ErrMembershipExists
	}
	return err
}

// Find implements accessdomain.MembershipRepository.
func (r *MongoMemberships) Find(ctx context.Context, poolID, userID string) (*accessdomain.Membership, error) {
	var doc membershipDoc
	err := r.col.FindOne(ctx, bson.M{"poolId": poolID, "userId": userID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, accessdomain.ErrMembershipNotFound
	}
	if err != nil {
		return nil, err
	}
	return toMembership(doc), nil
}

// ListByPool implements accessdomain.MembershipRepository.
func (r *MongoMemberships) ListByPool(ctx context.Context, poolID string) ([]*accessdomain.Membership, error) {
	return r.list(ctx, bson.M{"poolId": poolID})
}

// ListByUser implements accessdomain.MembershipRepository.
func (r *MongoMemberships) ListByUser(ctx context.Context, userID string) ([]*accessdomain.Membership, error) {
	return r.list(ctx, bson.M{"userId": userID})
}

// ListAll implements accessdomain.MembershipRepository.
func (r *MongoMemberships) ListAll(ctx context.Context) ([]*accessdomain.Membership, error) {
	return r.list(ctx, bson.M{})
}

func (r *MongoMemberships) list(ctx context.Context, filter bson.M) ([]*accessdomain.Membership, error) {
	cursor, err := r.col.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []membershipDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]*accessdomain.Membership, 0, len(docs))
	for _, doc := range docs {
		out = append(out, toMembership(doc))
	}
	return out, nil
}

// Update implements accessdomain.MembershipRepository.
func (r *MongoMemberships) Update(ctx context.Context, m *accessdomain.Membership) error {
	result, err := r.col.UpdateOne(ctx, bson.M{"poolId": m.PoolID, "userId": m.UserID}, bson.M{"$set": bson.M{
		"role": string(m.Role), "sudo": string(m.Sudo), "sudoPasswordless": string(m.SudoPasswordless), "updatedAt": m.UpdatedAt,
	}})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return accessdomain.ErrMembershipNotFound
	}
	return nil
}

// Delete implements accessdomain.MembershipRepository.
func (r *MongoMemberships) Delete(ctx context.Context, poolID, userID string) error {
	result, err := r.col.DeleteOne(ctx, bson.M{"poolId": poolID, "userId": userID})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return accessdomain.ErrMembershipNotFound
	}
	return nil
}

// DeleteByUser implements accessdomain.MembershipRepository.
func (r *MongoMemberships) DeleteByUser(ctx context.Context, userID string) error {
	_, err := r.col.DeleteMany(ctx, bson.M{"userId": userID})
	return err
}

// DeleteByPool implements accessdomain.MembershipRepository.
func (r *MongoMemberships) DeleteByPool(ctx context.Context, poolID string) error {
	_, err := r.col.DeleteMany(ctx, bson.M{"poolId": poolID})
	return err
}

func toMembershipDoc(m *accessdomain.Membership) membershipDoc {
	return membershipDoc{
		ID: m.ID, PoolID: m.PoolID, SiteID: m.SiteID, UserID: m.UserID, Role: string(m.Role),
		Sudo: string(m.Sudo), SudoPasswordless: string(m.SudoPasswordless), CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func toMembership(doc membershipDoc) *accessdomain.Membership {
	return &accessdomain.Membership{
		ID: doc.ID, PoolID: doc.PoolID, SiteID: doc.SiteID, UserID: doc.UserID, Role: accessdomain.PoolRole(doc.Role),
		Sudo: accessdomain.SudoSetting(doc.Sudo), SudoPasswordless: accessdomain.SudoSetting(doc.SudoPasswordless),
		CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt,
	}
}
