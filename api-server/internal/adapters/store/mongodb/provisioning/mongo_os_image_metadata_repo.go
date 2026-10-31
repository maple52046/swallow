package provisioning

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// osImageMetadataDoc is the MongoDB shape of a swallow-owned OS Image metadata.
//
// The document has no synthetic _id: the metadata's identity is the provider image identity
// (integrationId, imageId, architecture), enforced by a unique compound index, so relying on
// that natural key avoids a second uniqueness concern. bson tags live only in this store type.
type osImageMetadataDoc struct {
	IntegrationID string    `bson:"integrationId"`
	ImageID       string    `bson:"imageId"`
	Architecture  string    `bson:"architecture"`
	DisplayName   string    `bson:"displayName,omitempty"`
	OSSystem      string    `bson:"osSystem,omitempty"`
	Release       string    `bson:"release,omitempty"`
	Tags          []string  `bson:"tags,omitempty"`
	DefaultUser   string    `bson:"defaultUser,omitempty"`
	UpdatedAt     time.Time `bson:"updatedAt"`
}

// MongoOSImageMetadataRepo stores swallow's OS Image metadata in the os_image_metadata collection.
//
// The metadata is owned desired state (decision 071), so there is no source or staleness column
// and no cross-site fan-out concern: reading it back is authoritative for what it holds.
type MongoOSImageMetadataRepo struct {
	col *mongo.Collection
}

// NewMongoOSImageMetadataRepo creates the unique metadata-key index and returns the repository.
//
// The unique index on (integrationId, imageId, architecture) is the persistence guarantee
// behind the domain repository's key: one image within one Integration never carries two
// conflicting names. Index creation uses its own bounded context so a slow index build cannot
// block process start indefinitely.
func NewMongoOSImageMetadataRepo(db *mongo.Database) (*MongoOSImageMetadataRepo, error) {
	col := db.Collection("os_image_metadata")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "integrationId", Value: 1},
			{Key: "imageId", Value: 1},
			{Key: "architecture", Value: 1},
		},
		Options: options.Index().SetUnique(true).SetName("integration_image_architecture"),
	})
	if err != nil {
		return nil, err
	}
	return &MongoOSImageMetadataRepo{col: col}, nil
}

// ListByIntegration returns every metadata stored for one Integration, or an empty slice.
func (r *MongoOSImageMetadataRepo) ListByIntegration(
	ctx context.Context,
	integrationID string,
) ([]*provisioningdomain.OSImageMetadata, error) {
	cursor, err := r.col.Find(ctx, bson.M{"integrationId": integrationID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []osImageMetadataDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	metadataRecords := make([]*provisioningdomain.OSImageMetadata, len(docs))
	for i := range docs {
		metadataRecords[i] = toOSImageMetadata(&docs[i])
	}
	return metadataRecords, nil
}

// Upsert stores or replaces the metadata for its natural key, writing the metadata's UpdatedAt.
//
// SetUpsert makes the first rename an insert and later renames an update against the same
// unique key, so a repeated rename never creates a duplicate.
func (r *MongoOSImageMetadataRepo) Upsert(
	ctx context.Context,
	metadata *provisioningdomain.OSImageMetadata,
) error {
	filter := bson.M{
		"integrationId": metadata.IntegrationID,
		"imageId":       metadata.ImageID,
		"architecture":  metadata.Architecture,
	}
	// Every override field is replaced so a cleared field (empty string) is persisted rather
	// than leaving a stale prior value; the write time is refreshed on each change.
	update := bson.M{"$set": bson.M{
		"displayName": metadata.DisplayName,
		"osSystem":    metadata.OSSystem,
		"release":     metadata.Release,
		"tags":        metadata.Tags,
		"defaultUser": metadata.DefaultUser,
		"updatedAt":   metadata.UpdatedAt,
	}}
	_, err := r.col.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// Delete removes the metadata for the given key. An absent metadata is treated as success,
// because the caller's intended end state — the image showing its provider label — already
// holds.
func (r *MongoOSImageMetadataRepo) Delete(
	ctx context.Context,
	integrationID, imageID, architecture string,
) error {
	_, err := r.col.DeleteOne(ctx, bson.M{
		"integrationId": integrationID,
		"imageId":       imageID,
		"architecture":  architecture,
	})
	return err
}

// toOSImageMetadata maps a stored document onto the domain metadata, keeping bson concerns in
// this store package.
func toOSImageMetadata(doc *osImageMetadataDoc) *provisioningdomain.OSImageMetadata {
	return &provisioningdomain.OSImageMetadata{
		IntegrationID: doc.IntegrationID,
		ImageID:       doc.ImageID,
		Architecture:  doc.Architecture,
		DisplayName:   doc.DisplayName,
		OSSystem:      doc.OSSystem,
		Release:       doc.Release,
		Tags:          doc.Tags,
		DefaultUser:   doc.DefaultUser,
		UpdatedAt:     doc.UpdatedAt,
	}
}
