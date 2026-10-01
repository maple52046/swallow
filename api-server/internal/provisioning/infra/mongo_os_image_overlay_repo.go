package infra

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// osImageOverlayDoc is the MongoDB shape of a swallow-owned OS Image overlay.
//
// The document has no synthetic _id: the overlay's identity is the provider image identity
// (integrationId, imageId, architecture), enforced by a unique compound index, so relying on
// that natural key avoids a second uniqueness concern. bson tags live only in this infra type.
type osImageOverlayDoc struct {
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

// MongoOSImageOverlayRepo stores swallow-owned OS Image overlays in the os_image_overlays
// collection.
//
// Overlays are owned data (docs/decisions/025), so there is no source or staleness column and
// no cross-site fan-out concern: reading an overlay back is authoritative for the name it holds.
type MongoOSImageOverlayRepo struct {
	col *mongo.Collection
}

// NewMongoOSImageOverlayRepo creates the unique overlay-key index and returns the repository.
//
// The unique index on (integrationId, imageId, architecture) is the persistence guarantee
// behind the domain repository's key: one image within one Integration never carries two
// conflicting names. Index creation uses its own bounded context so a slow index build cannot
// block process start indefinitely.
func NewMongoOSImageOverlayRepo(db *mongo.Database) (*MongoOSImageOverlayRepo, error) {
	col := db.Collection("os_image_overlays")
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
	return &MongoOSImageOverlayRepo{col: col}, nil
}

// ListByIntegration returns every overlay stored for one Integration, or an empty slice.
func (r *MongoOSImageOverlayRepo) ListByIntegration(
	ctx context.Context,
	integrationID string,
) ([]*provisioningdomain.OSImageOverlay, error) {
	cursor, err := r.col.Find(ctx, bson.M{"integrationId": integrationID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []osImageOverlayDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	overlays := make([]*provisioningdomain.OSImageOverlay, len(docs))
	for i := range docs {
		overlays[i] = toOSImageOverlay(&docs[i])
	}
	return overlays, nil
}

// Upsert stores or replaces the overlay for its natural key, writing the overlay's UpdatedAt.
//
// SetUpsert makes the first rename an insert and later renames an update against the same
// unique key, so a repeated rename never creates a duplicate.
func (r *MongoOSImageOverlayRepo) Upsert(
	ctx context.Context,
	overlay *provisioningdomain.OSImageOverlay,
) error {
	filter := bson.M{
		"integrationId": overlay.IntegrationID,
		"imageId":       overlay.ImageID,
		"architecture":  overlay.Architecture,
	}
	// Every override field is replaced so a cleared field (empty string) is persisted rather
	// than leaving a stale prior value; the write time is refreshed on each change.
	update := bson.M{"$set": bson.M{
		"displayName": overlay.DisplayName,
		"osSystem":    overlay.OSSystem,
		"release":     overlay.Release,
		"tags":        overlay.Tags,
		"defaultUser": overlay.DefaultUser,
		"updatedAt":   overlay.UpdatedAt,
	}}
	_, err := r.col.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// Delete removes the overlay for the given key. An absent overlay is treated as success,
// because the caller's intended end state — the image showing its provider label — already
// holds.
func (r *MongoOSImageOverlayRepo) Delete(
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

// toOSImageOverlay maps a stored document onto the domain overlay, keeping bson concerns in
// this infra package.
func toOSImageOverlay(doc *osImageOverlayDoc) *provisioningdomain.OSImageOverlay {
	return &provisioningdomain.OSImageOverlay{
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
