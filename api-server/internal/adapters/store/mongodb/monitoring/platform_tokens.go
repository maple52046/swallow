// Package monitoring is the MongoDB store of the monitoring context: the hashes of the Platform
// monitoring tokens (`platform_monitoring_tokens`). A token itself never reaches the database.
package monitoring

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
)

const collection = "platform_monitoring_tokens"

// tokenDoc is one Platform's token hash, keyed by the Platform id.
type tokenDoc struct {
	PlatformID string    `bson:"_id"`
	TokenHash  []byte    `bson:"tokenHash"`
	IssuedAt   time.Time `bson:"issuedAt"`
}

// MongoPlatformTokenStore implements monitoringdomain.PlatformTokenStore.
type MongoPlatformTokenStore struct {
	col *mongo.Collection
}

// NewMongoPlatformTokenStore returns the store; the Platform id is the document id, so it needs no
// index of its own.
func NewMongoPlatformTokenStore(db *mongo.Database) *MongoPlatformTokenStore {
	return &MongoPlatformTokenStore{col: db.Collection(collection)}
}

// SaveToken replaces the Platform's token hash.
func (s *MongoPlatformTokenStore) SaveToken(ctx context.Context, platformID string, hash []byte, issuedAt time.Time) error {
	_, err := s.col.ReplaceOne(ctx, bson.M{"_id": platformID},
		tokenDoc{PlatformID: platformID, TokenHash: hash, IssuedAt: issuedAt.UTC()},
		options.Replace().SetUpsert(true))
	return err
}

// TokenHash returns the Platform's token hash, or monitoringdomain.ErrPlatformTokenNotFound.
func (s *MongoPlatformTokenStore) TokenHash(ctx context.Context, platformID string) ([]byte, error) {
	var doc tokenDoc
	err := s.col.FindOne(ctx, bson.M{"_id": platformID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, monitoringdomain.ErrPlatformTokenNotFound
	}
	if err != nil {
		return nil, err
	}
	return doc.TokenHash, nil
}
