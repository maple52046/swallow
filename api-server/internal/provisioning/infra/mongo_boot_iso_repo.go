package infra

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// bootISODoc is the stored Boot ISO record (decision 049). The file itself lives in the Boot
// Media directory; this document holds what was built and from what input.
type bootISODoc struct {
	ID             string    `bson:"_id"`
	IntegrationID  string    `bson:"integrationId"`
	NormalizedName string    `bson:"normalizedName"`
	Name           string    `bson:"name"`
	RackAddress    string    `bson:"rackAddress"`
	ChainURL       string    `bson:"chainUrl"`
	Script         string    `bson:"script"`
	IPXEVersion    string    `bson:"ipxeVersion"`
	SizeBytes      int64     `bson:"sizeBytes"`
	SHA256         string    `bson:"sha256"`
	CreatedAt      time.Time `bson:"createdAt"`
	CreatedBy      string    `bson:"createdBy,omitempty"`
}

// MongoBootISORepo stores Boot ISO records in the boot_isos collection. A unique index on
// (integrationId, normalizedName) enforces the per-Integration name uniqueness the contract
// promises even under concurrent builds.
type MongoBootISORepo struct {
	col *mongo.Collection
}

// NewMongoBootISORepo creates the indexes and returns the repository.
func NewMongoBootISORepo(db *mongo.Database) (*MongoBootISORepo, error) {
	col := db.Collection("boot_isos")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "integrationId", Value: 1}, {Key: "normalizedName", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("integration_normalized_name"),
	})
	if err != nil {
		return nil, err
	}
	return &MongoBootISORepo{col: col}, nil
}

// Create implements provisioningdomain.BootISORepository.
func (r *MongoBootISORepo) Create(ctx context.Context, iso *provisioningdomain.BootISO) error {
	_, err := r.col.InsertOne(ctx, newBootISODoc(iso))
	if mongo.IsDuplicateKeyError(err) {
		return provisioningdomain.ErrBootISONameTaken
	}
	return err
}

// FindByID implements provisioningdomain.BootISORepository.
func (r *MongoBootISORepo) FindByID(ctx context.Context, id string) (*provisioningdomain.BootISO, error) {
	var doc bootISODoc
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, provisioningdomain.ErrBootISONotFound
	}
	if err != nil {
		return nil, err
	}
	return toBootISO(&doc), nil
}

// List implements provisioningdomain.BootISORepository.
func (r *MongoBootISORepo) List(ctx context.Context, filter provisioningdomain.BootISOFilter) ([]*provisioningdomain.BootISO, error) {
	query := bson.M{}
	if filter.IntegrationID != "" {
		query["integrationId"] = filter.IntegrationID
	}
	if len(filter.IntegrationIDs) > 0 {
		query["integrationId"] = bson.M{"$in": filter.IntegrationIDs}
	}
	cursor, err := r.col.Find(ctx, query, options.Find().SetSort(bson.D{
		{Key: "normalizedName", Value: 1}, {Key: "integrationId", Value: 1},
	}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []bootISODoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	items := make([]*provisioningdomain.BootISO, len(docs))
	for i := range docs {
		items[i] = toBootISO(&docs[i])
	}
	return items, nil
}

// Delete implements provisioningdomain.BootISORepository.
func (r *MongoBootISORepo) Delete(ctx context.Context, id string) error {
	result, err := r.col.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return provisioningdomain.ErrBootISONotFound
	}
	return nil
}

func newBootISODoc(iso *provisioningdomain.BootISO) bootISODoc {
	return bootISODoc{
		ID: iso.ID, IntegrationID: iso.IntegrationID,
		NormalizedName: strings.ToLower(strings.TrimSpace(iso.Name)), Name: iso.Name,
		RackAddress: iso.RackAddress, ChainURL: iso.ChainURL, Script: iso.Script, IPXEVersion: iso.IPXEVersion,
		SizeBytes: iso.SizeBytes, SHA256: iso.SHA256, CreatedAt: iso.CreatedAt, CreatedBy: iso.CreatedBy,
	}
}

func toBootISO(doc *bootISODoc) *provisioningdomain.BootISO {
	return &provisioningdomain.BootISO{
		ID: doc.ID, Name: doc.Name, IntegrationID: doc.IntegrationID,
		RackAddress: doc.RackAddress, ChainURL: doc.ChainURL, Script: doc.Script, IPXEVersion: doc.IPXEVersion,
		SizeBytes: doc.SizeBytes, SHA256: doc.SHA256, CreatedAt: doc.CreatedAt, CreatedBy: doc.CreatedBy,
	}
}
