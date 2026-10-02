// Package infra stores API Keys in MongoDB (`api_keys`). Only secret hashes and display prefixes
// are stored; a secret never reaches the database.
package infra

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	apikeydomain "github.com/maple52046/swallow/internal/apikey/domain"
)

const (
	collection      = "api_keys"
	indexSecretHash = "secret_hash_unique"
	indexUserName   = "user_name_unique"
)

// keyDoc is the stored shape of an API Key. NameKey is the case-folded name the per-user
// uniqueness index is built on, so "Laptop" and "laptop" collide.
type keyDoc struct {
	ID         string     `bson:"_id"`
	UserID     string     `bson:"userId"`
	Name       string     `bson:"name"`
	NameKey    string     `bson:"nameKey"`
	Prefix     string     `bson:"prefix"`
	SecretHash string     `bson:"secretHash"`
	CreatedAt  time.Time  `bson:"createdAt"`
	ExpiresAt  *time.Time `bson:"expiresAt,omitempty"`
	LastUsedAt *time.Time `bson:"lastUsedAt,omitempty"`
}

// MongoKeyRepo implements apikeydomain.Repository. The unique secretHash index makes a hash map
// to one key (verification looks keys up by it on every request); the unique (userId, nameKey)
// index enforces per-user name uniqueness under concurrent creates.
type MongoKeyRepo struct {
	col *mongo.Collection
}

// NewMongoKeyRepo creates the repository and its indexes; index creation is idempotent.
func NewMongoKeyRepo(db *mongo.Database) (*MongoKeyRepo, error) {
	col := db.Collection(collection)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "secretHash", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexSecretHash),
		},
		{
			Keys:    bson.D{{Key: "userId", Value: 1}, {Key: "nameKey", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexUserName),
		},
	})
	if err != nil {
		return nil, err
	}
	return &MongoKeyRepo{col: col}, nil
}

// List returns userID's keys oldest first.
func (r *MongoKeyRepo) List(ctx context.Context, userID string) ([]*apikeydomain.APIKey, error) {
	cur, err := r.col.Find(ctx, bson.M{"userId": userID}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var docs []keyDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	keys := make([]*apikeydomain.APIKey, 0, len(docs))
	for i := range docs {
		keys = append(keys, toKey(&docs[i]))
	}
	return keys, nil
}

// Count returns how many keys userID holds.
func (r *MongoKeyRepo) Count(ctx context.Context, userID string) (int, error) {
	n, err := r.col.CountDocuments(ctx, bson.M{"userId": userID})
	return int(n), err
}

// Create inserts key, mapping a duplicate (userId, nameKey) to ErrDuplicateName. A secretHash
// collision would mean two identical 256-bit secrets and is reported as a plain error.
func (r *MongoKeyRepo) Create(ctx context.Context, key *apikeydomain.APIKey) error {
	_, err := r.col.InsertOne(ctx, keyDoc{
		ID:         key.ID,
		UserID:     key.UserID,
		Name:       key.Name,
		NameKey:    strings.ToLower(key.Name),
		Prefix:     key.Prefix,
		SecretHash: key.SecretHash,
		CreatedAt:  key.CreatedAt,
		ExpiresAt:  key.ExpiresAt,
		LastUsedAt: key.LastUsedAt,
	})
	if isDuplicate(err, indexUserName) {
		return apikeydomain.ErrDuplicateName
	}
	return err
}

// FindBySecretHash returns the key whose secret hashes to hash.
func (r *MongoKeyRepo) FindBySecretHash(ctx context.Context, hash string) (*apikeydomain.APIKey, error) {
	var doc keyDoc
	err := r.col.FindOne(ctx, bson.M{"secretHash": hash}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, apikeydomain.ErrKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	return toKey(&doc), nil
}

// Delete removes the key only when it belongs to userID.
func (r *MongoKeyRepo) Delete(ctx context.Context, userID, id string) error {
	res, err := r.col.DeleteOne(ctx, bson.M{"_id": id, "userId": userID})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return apikeydomain.ErrKeyNotFound
	}
	return nil
}

// TouchLastUsed is one conditional update, so concurrent requests with the same key write at most
// once per LastUsedResolution.
func (r *MongoKeyRepo) TouchLastUsed(ctx context.Context, id string, at time.Time) error {
	_, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id, "$or": bson.A{
			bson.M{"lastUsedAt": bson.M{"$exists": false}},
			bson.M{"lastUsedAt": bson.M{"$lt": at.Add(-apikeydomain.LastUsedResolution)}},
		}},
		bson.M{"$set": bson.M{"lastUsedAt": at}},
	)
	return err
}

func toKey(d *keyDoc) *apikeydomain.APIKey {
	return &apikeydomain.APIKey{
		ID:         d.ID,
		UserID:     d.UserID,
		Name:       d.Name,
		Prefix:     d.Prefix,
		SecretHash: d.SecretHash,
		CreatedAt:  d.CreatedAt,
		ExpiresAt:  d.ExpiresAt,
		LastUsedAt: d.LastUsedAt,
	}
}

// isDuplicate reports whether err violates the named unique index. The driver reports the index
// only in the server message, so the name is matched there.
func isDuplicate(err error, index string) bool {
	return err != nil && mongo.IsDuplicateKeyError(err) && strings.Contains(err.Error(), index)
}
