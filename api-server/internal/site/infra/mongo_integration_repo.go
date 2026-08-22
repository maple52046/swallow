package infra

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/AFDEAPAC/swallow/internal/shared/secret"
	sitedomain "github.com/AFDEAPAC/swallow/internal/site/domain"
)

// integrationDoc stores an integration and its sealed credential.
//
// The credential lives in this document but never in the domain struct, so no read
// path can return it by accident: it leaves this package only through Credential.
type integrationDoc struct {
	ID           string            `bson:"_id"`
	SiteID       string            `bson:"siteId"`
	Kind         string            `bson:"kind"`
	ProviderKind string            `bson:"providerKind"`
	Name         string            `bson:"name"`
	Endpoint     string            `bson:"endpoint"`
	Enabled      bool              `bson:"enabled"`
	Settings     map[string]string `bson:"settings,omitempty"`

	// SealedCredential is AES-GCM ciphertext, base64 encoded.
	SealedCredential string `bson:"sealedCredential,omitempty"`

	Sync syncDoc `bson:"sync"`

	CreatedAt time.Time `bson:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt"`
}

type syncDoc struct {
	LastStartedAt   *time.Time `bson:"lastStartedAt,omitempty"`
	LastSucceededAt *time.Time `bson:"lastSucceededAt,omitempty"`
	LastError       string     `bson:"lastError,omitempty"`
}

type MongoIntegrationRepo struct {
	col    *mongo.Collection
	sealer *secret.Sealer
}

func NewMongoIntegrationRepo(db *mongo.Database, sealer *secret.Sealer) (*MongoIntegrationRepo, error) {
	col := db.Collection("integrations")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "siteId", Value: 1}, {Key: "kind", Value: 1}},
			Options: options.Index().SetName("site_kind"),
		},
		{
			Keys:    bson.D{{Key: "siteId", Value: 1}, {Key: "name", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("site_name"),
		},
	})
	if err != nil {
		return nil, err
	}

	return &MongoIntegrationRepo{col: col, sealer: sealer}, nil
}

func (r *MongoIntegrationRepo) Create(ctx context.Context, integration *sitedomain.Integration, credential string) error {
	doc := integrationDoc{
		ID:           integration.ID,
		SiteID:       integration.SiteID,
		Kind:         string(integration.Kind),
		ProviderKind: integration.ProviderKind,
		Name:         integration.Name,
		Endpoint:     integration.Endpoint,
		Enabled:      integration.Enabled,
		Settings:     integration.Settings,
		CreatedAt:    integration.CreatedAt,
		UpdatedAt:    integration.UpdatedAt,
	}

	if credential != "" {
		sealed, err := r.sealer.Seal(credential)
		if err != nil {
			return err
		}
		doc.SealedCredential = sealed
	}

	_, err := r.col.InsertOne(ctx, doc)
	return err
}

func (r *MongoIntegrationRepo) FindByID(ctx context.Context, id string) (*sitedomain.Integration, error) {
	var doc integrationDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, sitedomain.ErrIntegrationNotFound
	}
	if err != nil {
		return nil, err
	}
	return toIntegration(&doc), nil
}

func (r *MongoIntegrationRepo) List(ctx context.Context, filter sitedomain.IntegrationFilter) ([]*sitedomain.Integration, error) {
	query := bson.M{}
	if filter.SiteID != "" {
		query["siteId"] = filter.SiteID
	}
	if filter.Kind != "" {
		query["kind"] = string(filter.Kind)
	}
	if filter.EnabledOnly {
		query["enabled"] = true
	}

	cursor, err := r.col.Find(ctx, query, options.Find().SetSort(bson.D{
		{Key: "siteId", Value: 1}, {Key: "name", Value: 1},
	}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []integrationDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	integrations := make([]*sitedomain.Integration, len(docs))
	for i := range docs {
		integrations[i] = toIntegration(&docs[i])
	}
	return integrations, nil
}

// Update replaces the mutable fields only. The credential and sync state are
// untouched, so an operator renaming an integration cannot clear either.
func (r *MongoIntegrationRepo) Update(ctx context.Context, integration *sitedomain.Integration) error {
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": integration.ID},
		bson.M{"$set": bson.M{
			"name":      integration.Name,
			"endpoint":  integration.Endpoint,
			"enabled":   integration.Enabled,
			"settings":  integration.Settings,
			"updatedAt": integration.UpdatedAt,
		}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return sitedomain.ErrIntegrationNotFound
	}
	return nil
}

func (r *MongoIntegrationRepo) ReplaceCredential(ctx context.Context, id, credential string) error {
	sealed, err := r.sealer.Seal(credential)
	if err != nil {
		return err
	}

	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"sealedCredential": sealed, "updatedAt": time.Now().UTC()}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return sitedomain.ErrIntegrationNotFound
	}
	return nil
}

func (r *MongoIntegrationRepo) Credential(ctx context.Context, id string) (string, error) {
	var doc integrationDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id},
		options.FindOne().SetProjection(bson.M{"sealedCredential": 1})).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return "", sitedomain.ErrIntegrationNotFound
	}
	if err != nil {
		return "", err
	}
	if doc.SealedCredential == "" {
		return "", sitedomain.ErrCredentialNotSet
	}
	return r.sealer.Open(doc.SealedCredential)
}

func (r *MongoIntegrationRepo) UpdateSyncState(ctx context.Context, id string, state sitedomain.SyncState) error {
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"sync": syncDoc{
			LastStartedAt:   state.LastStartedAt,
			LastSucceededAt: state.LastSucceededAt,
			LastError:       state.LastError,
		}}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return sitedomain.ErrIntegrationNotFound
	}
	return nil
}

func (r *MongoIntegrationRepo) Delete(ctx context.Context, id string) error {
	result, err := r.col.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return sitedomain.ErrIntegrationNotFound
	}
	return nil
}

func toIntegration(doc *integrationDoc) *sitedomain.Integration {
	return &sitedomain.Integration{
		ID:           doc.ID,
		SiteID:       doc.SiteID,
		Kind:         sitedomain.IntegrationKind(doc.Kind),
		ProviderKind: doc.ProviderKind,
		Name:         doc.Name,
		Endpoint:     doc.Endpoint,
		Enabled:      doc.Enabled,
		Settings:     doc.Settings,
		Sync: sitedomain.SyncState{
			LastStartedAt:   doc.Sync.LastStartedAt,
			LastSucceededAt: doc.Sync.LastSucceededAt,
			LastError:       doc.Sync.LastError,
		},
		CreatedAt: doc.CreatedAt,
		UpdatedAt: doc.UpdatedAt,
	}
}
