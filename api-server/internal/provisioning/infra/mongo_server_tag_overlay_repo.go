package infra

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// serverTagOverlayDoc is the MongoDB shape of a swallow-owned Server tag overlay.
//
// The document's identity is the Server id, stored as _id, so there is no synthetic key and one
// Server can never carry two overlays. IntegrationID is denormalized alongside so reconcile can
// list every overlay for one integration in a single query. bson tags live only in this infra type.
type serverTagOverlayDoc struct {
	ServerID      string    `bson:"_id"`
	IntegrationID string    `bson:"integrationId"`
	Tags          []string  `bson:"tags"`
	UpdatedAt     time.Time `bson:"updatedAt"`
}

// MongoServerTagOverlayRepo stores swallow-owned Server tag overlays in the server_tag_overlays
// collection.
//
// Overlays are owned data (docs/decisions/025 and 031), so there is no source or staleness column:
// reading an overlay back is authoritative for the tags it holds. The fallback is inert while the
// Server's provisioner is tagging-capable (MAAS is), so this collection stays empty in that case.
type MongoServerTagOverlayRepo struct {
	col *mongo.Collection
}

// NewMongoServerTagOverlayRepo creates the integration index used by reconcile and returns the
// repository. The overlay's uniqueness is the _id (Server id) itself, so only the integration
// lookup index is added here. Index creation uses its own bounded context so a slow build cannot
// block process start indefinitely.
func NewMongoServerTagOverlayRepo(db *mongo.Database) (*MongoServerTagOverlayRepo, error) {
	col := db.Collection("server_tag_overlays")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "integrationId", Value: 1}},
		Options: options.Index().SetName("integrationId"),
	})
	if err != nil {
		return nil, err
	}
	return &MongoServerTagOverlayRepo{col: col}, nil
}

// Get returns the overlay for one Server, or ErrServerTagOverlayNotFound when none exists.
func (r *MongoServerTagOverlayRepo) Get(
	ctx context.Context,
	serverID string,
) (*provisioningdomain.ServerTagOverlay, error) {
	var doc serverTagOverlayDoc
	if err := r.col.FindOne(ctx, bson.M{"_id": serverID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, provisioningdomain.ErrServerTagOverlayNotFound
		}
		return nil, err
	}
	return toServerTagOverlay(&doc), nil
}

// ListByIntegration returns every overlay stored for one integration, or an empty slice.
func (r *MongoServerTagOverlayRepo) ListByIntegration(
	ctx context.Context,
	integrationID string,
) ([]*provisioningdomain.ServerTagOverlay, error) {
	cursor, err := r.col.Find(ctx, bson.M{"integrationId": integrationID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []serverTagOverlayDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	overlays := make([]*provisioningdomain.ServerTagOverlay, len(docs))
	for i := range docs {
		overlays[i] = toServerTagOverlay(&docs[i])
	}
	return overlays, nil
}

// Upsert stores or replaces the overlay for its Server id, writing the whole tag set and the
// overlay's UpdatedAt. SetUpsert makes the first write an insert and later writes an update against
// the same _id, so a repeated edit never creates a duplicate.
func (r *MongoServerTagOverlayRepo) Upsert(
	ctx context.Context,
	overlay *provisioningdomain.ServerTagOverlay,
) error {
	update := bson.M{"$set": bson.M{
		"integrationId": overlay.IntegrationID,
		"tags":          overlay.Tags,
		"updatedAt":     overlay.UpdatedAt,
	}}
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": overlay.ServerID}, update, options.Update().SetUpsert(true))
	return err
}

// Delete removes the overlay for the given Server. An absent overlay is treated as success, because
// the caller's intended end state — the Server having no swallow-owned tags — already holds.
func (r *MongoServerTagOverlayRepo) Delete(ctx context.Context, serverID string) error {
	_, err := r.col.DeleteOne(ctx, bson.M{"_id": serverID})
	return err
}

// toServerTagOverlay maps a stored document onto the domain overlay, keeping bson concerns in this
// infra package.
func toServerTagOverlay(doc *serverTagOverlayDoc) *provisioningdomain.ServerTagOverlay {
	return &provisioningdomain.ServerTagOverlay{
		ServerID:      doc.ServerID,
		IntegrationID: doc.IntegrationID,
		Tags:          doc.Tags,
		UpdatedAt:     doc.UpdatedAt,
	}
}
