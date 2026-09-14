// Package infra holds the Infrastructure feature's driver adapters: MongoDB repositories for
// swallow-owned Zones and Pools, and the adapters that let the grouping use cases read Sites and
// Servers and realize intent in a provisioner. It is the only place in this feature that knows
// about MongoDB, the site/server domains, or the provisioning provider port; no bson tag, Mongo
// filter, or provider type leaks out of here.
package infra

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	infradomain "github.com/maple52046/swallow/internal/infrastructure/domain"
)

// zoneDoc is the MongoDB shape of a Zone. Persistence-only tags live here, never on the domain
// entity, so the domain stays free of storage concerns.
type zoneDoc struct {
	ID               string    `bson:"_id"`
	SiteID           string    `bson:"siteId"`
	Name             string    `bson:"name"`
	Description      string    `bson:"description,omitempty"`
	ProviderRealized bool      `bson:"providerRealized"`
	CreatedAt        time.Time `bson:"createdAt"`
	UpdatedAt        time.Time `bson:"updatedAt"`
}

// MongoZoneRepo is the durable ZoneRepository.
type MongoZoneRepo struct {
	col *mongo.Collection
}

// NewMongoZoneRepo creates the Zone repository and its uniqueness index.
//
// Names are unique per Site, not globally, so the unique index is the compound (siteId, name):
// two Sites may both have a Zone "rack-a", but one Site may not have two. The index is what makes
// Create/Update return ErrZoneNameTaken instead of silently allowing a duplicate.
func NewMongoZoneRepo(db *mongo.Database) (*MongoZoneRepo, error) {
	col := db.Collection("zones")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "siteId", Value: 1}, {Key: "name", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("zone_site_name"),
	})
	if err != nil {
		return nil, err
	}

	return &MongoZoneRepo{col: col}, nil
}

func (r *MongoZoneRepo) Create(ctx context.Context, zone *infradomain.Zone) error {
	_, err := r.col.InsertOne(ctx, zoneDoc{
		ID:               zone.ID,
		SiteID:           zone.SiteID,
		Name:             zone.Name,
		Description:      zone.Description,
		ProviderRealized: zone.ProviderRealized,
		CreatedAt:        zone.CreatedAt,
		UpdatedAt:        zone.UpdatedAt,
	})
	if mongo.IsDuplicateKeyError(err) {
		return infradomain.ErrZoneNameTaken
	}
	return err
}

func (r *MongoZoneRepo) FindByID(ctx context.Context, id string) (*infradomain.Zone, error) {
	var doc zoneDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, infradomain.ErrZoneNotFound
	}
	if err != nil {
		return nil, err
	}
	return toZone(&doc), nil
}

func (r *MongoZoneRepo) List(ctx context.Context, siteID string) ([]*infradomain.Zone, error) {
	filter := bson.M{}
	if siteID != "" {
		filter["siteId"] = siteID
	}
	cursor, err := r.col.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []zoneDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	zones := make([]*infradomain.Zone, len(docs))
	for i := range docs {
		zones[i] = toZone(&docs[i])
	}
	return zones, nil
}

func (r *MongoZoneRepo) Update(ctx context.Context, zone *infradomain.Zone) error {
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": zone.ID},
		bson.M{"$set": bson.M{
			"name":             zone.Name,
			"description":      zone.Description,
			"providerRealized": zone.ProviderRealized,
			"updatedAt":        zone.UpdatedAt,
		}},
	)
	if mongo.IsDuplicateKeyError(err) {
		return infradomain.ErrZoneNameTaken
	}
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return infradomain.ErrZoneNotFound
	}
	return nil
}

func (r *MongoZoneRepo) Delete(ctx context.Context, id string) error {
	result, err := r.col.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return infradomain.ErrZoneNotFound
	}
	return nil
}

func toZone(doc *zoneDoc) *infradomain.Zone {
	return &infradomain.Zone{
		ID:               doc.ID,
		SiteID:           doc.SiteID,
		Name:             doc.Name,
		Description:      doc.Description,
		ProviderRealized: doc.ProviderRealized,
		CreatedAt:        doc.CreatedAt,
		UpdatedAt:        doc.UpdatedAt,
	}
}
