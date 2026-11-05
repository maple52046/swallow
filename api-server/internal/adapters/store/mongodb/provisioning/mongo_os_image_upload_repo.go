package provisioning

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// osImageUploadCollection holds the short-lived OS Image upload records. They carry no artifact
// bytes and no secret, and expire on their own, so the collection is not part of what a backup
// must preserve.
const osImageUploadCollection = "os_image_uploads"

// osImageUploadDoc is the MongoDB shape of one OS Image upload. bson tags live only in this store
// type; the domain record never sees them.
type osImageUploadDoc struct {
	ID            string    `bson:"_id"`
	IntegrationID string    `bson:"integrationId"`
	Name          string    `bson:"name"`
	Architecture  string    `bson:"architecture"`
	Title         string    `bson:"title,omitempty"`
	FileType      string    `bson:"fileType,omitempty"`
	SizeBytes     int64     `bson:"sizeBytes"`
	SHA256        string    `bson:"sha256"`
	DefaultUser   string    `bson:"defaultUser,omitempty"`
	State         string    `bson:"state"`
	CreatedAt     time.Time `bson:"createdAt"`
	ExpiresAt     time.Time `bson:"expiresAt"`
}

// MongoOSImageUploadRepo stores OS Image uploads in the os_image_uploads collection.
//
// Every API process shares the collection, so the create and the content request of one upload may
// reach different processes. A TTL index on expiresAt discards uploads nobody claimed; MongoDB's
// TTL monitor runs about once a minute, so Claim still compares expiresAt itself rather than
// trusting that an expired document is already gone. The TTL may also remove an upload whose long
// content request is still running; that is harmless, because the request already holds the
// claimed record and only deletes it at the end.
type MongoOSImageUploadRepo struct {
	col *mongo.Collection
}

// NewMongoOSImageUploadRepo creates the TTL index and returns the repository. Index creation is
// idempotent, so every API start may call it, and uses its own bounded context so a slow index
// build cannot block process start indefinitely.
func NewMongoOSImageUploadRepo(db *mongo.Database) (*MongoOSImageUploadRepo, error) {
	col := db.Collection(osImageUploadCollection)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "expiresAt", Value: 1}},
		Options: options.Index().SetName("os_image_upload_expiry").SetExpireAfterSeconds(0),
	})
	if err != nil {
		return nil, err
	}
	return &MongoOSImageUploadRepo{col: col}, nil
}

// Create inserts a new upload. The swallow-issued id is the document _id, so a repeated id is a
// duplicate-key error rather than a silent overwrite.
func (r *MongoOSImageUploadRepo) Create(ctx context.Context, upload *provisioningdomain.OSImageUpload) error {
	_, err := r.col.InsertOne(ctx, toOSImageUploadDoc(upload))
	return err
}

// Claim moves the pending, unexpired upload to sending in one conditional update, so of two
// concurrent content requests exactly one wins. When the update matches nothing, a second read
// tells an upload that another request already claimed (ErrOSImageUploadInProgress) from one that
// is absent or expired (ErrOSImageUploadNotFound); that read is only for the error and cannot let
// a losing request proceed.
func (r *MongoOSImageUploadRepo) Claim(
	ctx context.Context,
	id string,
	now time.Time,
) (*provisioningdomain.OSImageUpload, error) {
	var doc osImageUploadDoc
	err := r.col.FindOneAndUpdate(ctx,
		bson.M{
			"_id":       id,
			"state":     string(provisioningdomain.OSImageUploadPending),
			"expiresAt": bson.M{"$gt": now},
		},
		bson.M{"$set": bson.M{"state": string(provisioningdomain.OSImageUploadSending)}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&doc)
	if err == nil {
		return toOSImageUpload(&doc), nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}

	var current osImageUploadDoc
	err = r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&current)
	switch {
	case errors.Is(err, mongo.ErrNoDocuments):
		return nil, provisioningdomain.ErrOSImageUploadNotFound
	case err != nil:
		return nil, err
	case current.State == string(provisioningdomain.OSImageUploadSending):
		return nil, provisioningdomain.ErrOSImageUploadInProgress
	default:
		return nil, provisioningdomain.ErrOSImageUploadNotFound
	}
}

// Delete removes the upload; an absent upload (already deleted, or discarded by the TTL) is
// success.
func (r *MongoOSImageUploadRepo) Delete(ctx context.Context, id string) error {
	_, err := r.col.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

// toOSImageUploadDoc maps the domain record onto its stored document.
func toOSImageUploadDoc(upload *provisioningdomain.OSImageUpload) osImageUploadDoc {
	return osImageUploadDoc{
		ID:            upload.ID,
		IntegrationID: upload.IntegrationID,
		Name:          upload.Name,
		Architecture:  upload.Architecture,
		Title:         upload.Title,
		FileType:      upload.FileType,
		SizeBytes:     upload.SizeBytes,
		SHA256:        upload.SHA256,
		DefaultUser:   upload.DefaultUser,
		State:         string(upload.State),
		CreatedAt:     upload.CreatedAt,
		ExpiresAt:     upload.ExpiresAt,
	}
}

// toOSImageUpload maps a stored document onto the domain record, keeping bson concerns in this
// store package.
func toOSImageUpload(doc *osImageUploadDoc) *provisioningdomain.OSImageUpload {
	return &provisioningdomain.OSImageUpload{
		ID:            doc.ID,
		IntegrationID: doc.IntegrationID,
		Name:          doc.Name,
		Architecture:  doc.Architecture,
		Title:         doc.Title,
		FileType:      doc.FileType,
		SizeBytes:     doc.SizeBytes,
		SHA256:        doc.SHA256,
		DefaultUser:   doc.DefaultUser,
		State:         provisioningdomain.OSImageUploadState(doc.State),
		CreatedAt:     doc.CreatedAt,
		ExpiresAt:     doc.ExpiresAt,
	}
}
