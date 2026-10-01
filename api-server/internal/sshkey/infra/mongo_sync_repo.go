package infra

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	sshkeydomain "github.com/maple52046/swallow/internal/sshkey/domain"
)

// providerSyncDoc is the MongoDB shape of one ProviderSync. The document id is derived from the
// (keyId, integrationId) pair so an upsert can never create a second record for the same pair.
type providerSyncDoc struct {
	ID                    string     `bson:"_id"`
	KeyID                 string     `bson:"keyId"`
	IntegrationID         string     `bson:"integrationId"`
	SiteID                string     `bson:"siteId"`
	Fingerprint           string     `bson:"fingerprint"`
	ProviderKeyID         string     `bson:"providerKeyId,omitempty"`
	RetiredProviderKeyIDs []string   `bson:"retiredProviderKeyIds,omitempty"`
	State                 string     `bson:"state"`
	SyncedAt              *time.Time `bson:"syncedAt,omitempty"`
	Error                 string     `bson:"error,omitempty"`
	UpdatedAt             time.Time  `bson:"updatedAt"`
}

// MongoSyncRepo is the durable sshkeydomain.SyncRepository over `ssh_key_provider_syncs`. Records
// are swallow-owned bookkeeping about provisioner realization; they hold provider key ids but no
// key material or secret.
type MongoSyncRepo struct {
	col *mongo.Collection
}

// NewMongoSyncRepo returns the sync record repository. The derived _id is the only uniqueness the
// collection needs, so no index is created.
func NewMongoSyncRepo(db *mongo.Database) *MongoSyncRepo {
	return &MongoSyncRepo{col: db.Collection("ssh_key_provider_syncs")}
}

// List returns every record, including orphans whose key or Integration is gone; the realization
// pass decides what to do with them.
func (r *MongoSyncRepo) List(ctx context.Context) ([]sshkeydomain.ProviderSync, error) {
	cursor, err := r.col.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []providerSyncDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	records := make([]sshkeydomain.ProviderSync, len(docs))
	for i, doc := range docs {
		records[i] = sshkeydomain.ProviderSync{
			KeyID:                 doc.KeyID,
			IntegrationID:         doc.IntegrationID,
			SiteID:                doc.SiteID,
			Fingerprint:           doc.Fingerprint,
			ProviderKeyID:         doc.ProviderKeyID,
			RetiredProviderKeyIDs: doc.RetiredProviderKeyIDs,
			State:                 sshkeydomain.SyncState(doc.State),
			SyncedAt:              doc.SyncedAt,
			Error:                 doc.Error,
			UpdatedAt:             doc.UpdatedAt,
		}
	}
	return records, nil
}

// Upsert replaces the whole record for its (key, Integration) pair, so a cleared field (for
// example a resolved error) is persisted as cleared.
func (r *MongoSyncRepo) Upsert(ctx context.Context, record sshkeydomain.ProviderSync) error {
	id := syncDocID(record.KeyID, record.IntegrationID)
	_, err := r.col.ReplaceOne(ctx, bson.M{"_id": id}, providerSyncDoc{
		ID:                    id,
		KeyID:                 record.KeyID,
		IntegrationID:         record.IntegrationID,
		SiteID:                record.SiteID,
		Fingerprint:           record.Fingerprint,
		ProviderKeyID:         record.ProviderKeyID,
		RetiredProviderKeyIDs: record.RetiredProviderKeyIDs,
		State:                 string(record.State),
		SyncedAt:              record.SyncedAt,
		Error:                 record.Error,
		UpdatedAt:             record.UpdatedAt,
	}, options.Replace().SetUpsert(true))
	return err
}

// Delete removes the record for the pair; a missing record is not an error.
func (r *MongoSyncRepo) Delete(ctx context.Context, keyID, integrationID string) error {
	_, err := r.col.DeleteOne(ctx, bson.M{"_id": syncDocID(keyID, integrationID)})
	return err
}

// syncDocID joins the pair with a separator that cannot occur in swallow's UUID ids.
func syncDocID(keyID, integrationID string) string {
	return keyID + ":" + integrationID
}
