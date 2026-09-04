package infra

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
)

type platformDoc struct {
	ID                 string `bson:"_id"`
	SiteID             string `bson:"siteId"`
	Name               string `bson:"name"`
	Type               string `bson:"type"`
	IntegrationID      string `bson:"integrationId,omitempty"`
	OwnedIntegrationID string `bson:"ownedIntegrationId,omitempty"`
	GPUStackOwner      string `bson:"gpuStackOwner"`
	// ExporterOwner is omitempty so a document written before this field existed
	// decodes as empty and is defaulted to ansible on read.
	ExporterOwner string       `bson:"exporterOwner,omitempty"`
	Sync          platformSync `bson:"sync"`
	CreatedAt     time.Time    `bson:"createdAt"`
	UpdatedAt     time.Time    `bson:"updatedAt"`
}

type platformSync struct {
	LastStartedAt   *time.Time `bson:"lastStartedAt,omitempty"`
	LastSucceededAt *time.Time `bson:"lastSucceededAt,omitempty"`
	LastError       string     `bson:"lastError,omitempty"`
	MemberCount     int        `bson:"memberCount"`
	MatchedCount    int        `bson:"matchedCount"`
}

type MongoPlatformRepo struct {
	col *mongo.Collection
}

func NewMongoPlatformRepo(db *mongo.Database) (*MongoPlatformRepo, error) {
	col := db.Collection("platforms")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "siteId", Value: 1}, {Key: "name", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("site_platform_name"),
	})
	if err != nil {
		return nil, err
	}

	return &MongoPlatformRepo{col: col}, nil
}

func (r *MongoPlatformRepo) Create(ctx context.Context, platform *platformdomain.Platform) error {
	_, err := r.col.InsertOne(ctx, toDoc(platform))
	if mongo.IsDuplicateKeyError(err) {
		return platformdomain.ErrPlatformNameTaken
	}
	return err
}

func (r *MongoPlatformRepo) FindByID(ctx context.Context, id string) (*platformdomain.Platform, error) {
	var doc platformDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, platformdomain.ErrPlatformNotFound
	}
	if err != nil {
		return nil, err
	}
	return toPlatform(&doc), nil
}

func (r *MongoPlatformRepo) List(ctx context.Context, siteID string) ([]*platformdomain.Platform, error) {
	query := bson.M{}
	if siteID != "" {
		query["siteId"] = siteID
	}

	cursor, err := r.col.Find(ctx, query, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []platformDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	platforms := make([]*platformdomain.Platform, len(docs))
	for i := range docs {
		platforms[i] = toPlatform(&docs[i])
	}
	return platforms, nil
}

func (r *MongoPlatformRepo) Update(ctx context.Context, platform *platformdomain.Platform) error {
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": platform.ID},
		bson.M{"$set": bson.M{
			"name":               platform.Name,
			"integrationId":      platform.IntegrationID,
			"ownedIntegrationId": platform.OwnedIntegrationID,
			"gpuStackOwner":      string(platform.GPUStackOwner),
			"exporterOwner":      string(platform.ExporterOwner),
			"updatedAt":          platform.UpdatedAt,
		}},
	)
	if mongo.IsDuplicateKeyError(err) {
		return platformdomain.ErrPlatformNameTaken
	}
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return platformdomain.ErrPlatformNotFound
	}
	return nil
}

func (r *MongoPlatformRepo) UpdateSyncState(
	ctx context.Context,
	id, integrationID string,
	state platformdomain.SyncState,
) error {
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id, "integrationId": integrationID},
		bson.M{"$set": bson.M{"sync": platformSync{
			LastStartedAt:   state.LastStartedAt,
			LastSucceededAt: state.LastSucceededAt,
			LastError:       state.LastError,
			MemberCount:     state.MemberCount,
			MatchedCount:    state.MatchedCount,
		}}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return platformdomain.ErrPlatformNotFound
	}
	return nil
}

func (r *MongoPlatformRepo) Delete(ctx context.Context, id string) error {
	result, err := r.col.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return platformdomain.ErrPlatformNotFound
	}
	return nil
}

func toDoc(platform *platformdomain.Platform) *platformDoc {
	return &platformDoc{
		ID:                 platform.ID,
		SiteID:             platform.SiteID,
		Name:               platform.Name,
		Type:               string(platform.Type),
		IntegrationID:      platform.IntegrationID,
		OwnedIntegrationID: platform.OwnedIntegrationID,
		GPUStackOwner:      string(platform.GPUStackOwner),
		ExporterOwner:      string(platform.ExporterOwner),
		Sync: platformSync{
			LastStartedAt:   platform.Sync.LastStartedAt,
			LastSucceededAt: platform.Sync.LastSucceededAt,
			LastError:       platform.Sync.LastError,
			MemberCount:     platform.Sync.MemberCount,
			MatchedCount:    platform.Sync.MatchedCount,
		},
		CreatedAt: platform.CreatedAt,
		UpdatedAt: platform.UpdatedAt,
	}
}

func toPlatform(doc *platformDoc) *platformdomain.Platform {
	// A platform stored before exporterOwner existed decodes as empty; treat that as the
	// default ansible owner rather than an invalid empty value.
	exporterOwner := platformdomain.ExporterOwner(doc.ExporterOwner)
	if exporterOwner == "" {
		exporterOwner = platformdomain.ExporterOwnerAnsible
	}
	return &platformdomain.Platform{
		ID:                 doc.ID,
		SiteID:             doc.SiteID,
		Name:               doc.Name,
		Type:               platformdomain.PlatformType(doc.Type),
		IntegrationID:      doc.IntegrationID,
		OwnedIntegrationID: doc.OwnedIntegrationID,
		GPUStackOwner:      platformdomain.GPUStackOwner(doc.GPUStackOwner),
		ExporterOwner:      exporterOwner,
		Sync: platformdomain.SyncState{
			LastStartedAt:   doc.Sync.LastStartedAt,
			LastSucceededAt: doc.Sync.LastSucceededAt,
			LastError:       doc.Sync.LastError,
			MemberCount:     doc.Sync.MemberCount,
			MatchedCount:    doc.Sync.MatchedCount,
		},
		CreatedAt: doc.CreatedAt,
		UpdatedAt: doc.UpdatedAt,
	}
}
