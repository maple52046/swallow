package infra

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/maple52046/swallow/internal/shared/secret"
	sshkeydomain "github.com/maple52046/swallow/internal/sshkey/domain"
)

// Index names. They are matched against duplicate-key errors to tell which invariant a write
// violated, so renaming one requires updating duplicateKeyError.
const (
	indexFingerprint      = "ssh_key_fingerprint"
	indexSingleDeployment = "ssh_key_single_deployment"
	indexOwnerName        = "ssh_key_owner_name"
)

// sshKeyDoc is the MongoDB shape of an SSH Key. SealedPrivateKey is set only for the Deployment
// Key and is excluded from every read projection except DeploymentPrivateKey. NameKey is the
// case-folded name that backs per-owner name uniqueness.
type sshKeyDoc struct {
	ID               string    `bson:"_id"`
	Name             string    `bson:"name"`
	NameKey          string    `bson:"nameKey"`
	Purpose          string    `bson:"purpose"`
	OwnerUserID      string    `bson:"ownerUserId,omitempty"`
	KeyType          string    `bson:"keyType"`
	Fingerprint      string    `bson:"fingerprint"`
	PublicKey        string    `bson:"publicKey"`
	SealedPrivateKey string    `bson:"sealedPrivateKey,omitempty"`
	CreatedAt        time.Time `bson:"createdAt"`
	UpdatedAt        time.Time `bson:"updatedAt"`
}

// withoutPrivateKey is the projection every ordinary read uses, so sealed key material is never
// decoded outside DeploymentPrivateKey.
var withoutPrivateKey = bson.M{"sealedPrivateKey": 0}

// MongoKeyRepo is the durable sshkeydomain.KeyRepository over the `ssh_keys` collection.
//
// The SSH Key invariants are enforced by unique indexes rather than read-then-write checks, so
// concurrent API processes cannot violate them: fingerprint is unique across all keys, a partial
// index admits at most one purpose=deployment document, and a partial compound index keeps Access
// Key names unique per owner. The Deployment Key private key is sealed with the installation's
// credential key; a database dump alone does not yield it.
type MongoKeyRepo struct {
	col    *mongo.Collection
	sealer *secret.Sealer
}

// NewMongoKeyRepo creates the repository and its uniqueness indexes.
func NewMongoKeyRepo(db *mongo.Database, sealer *secret.Sealer) (*MongoKeyRepo, error) {
	col := db.Collection("ssh_keys")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "fingerprint", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexFingerprint),
		},
		{
			Keys: bson.D{{Key: "purpose", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexSingleDeployment).
				SetPartialFilterExpression(bson.M{"purpose": string(sshkeydomain.PurposeDeployment)}),
		},
		{
			Keys: bson.D{{Key: "ownerUserId", Value: 1}, {Key: "nameKey", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexOwnerName).
				SetPartialFilterExpression(bson.M{"purpose": string(sshkeydomain.PurposeAccess)}),
		},
	})
	if err != nil {
		return nil, err
	}
	return &MongoKeyRepo{col: col, sealer: sealer}, nil
}

// List returns the Deployment Key first, then ownerUserID's Access Keys oldest first.
func (r *MongoKeyRepo) List(ctx context.Context, ownerUserID string) ([]*sshkeydomain.SSHKey, error) {
	return r.find(ctx, bson.M{"$or": bson.A{
		bson.M{"purpose": string(sshkeydomain.PurposeDeployment)},
		bson.M{"purpose": string(sshkeydomain.PurposeAccess), "ownerUserId": ownerUserID},
	}})
}

// ListAll returns every SSH Key in the same order as List.
func (r *MongoKeyRepo) ListAll(ctx context.Context) ([]*sshkeydomain.SSHKey, error) {
	return r.find(ctx, bson.M{})
}

// find lists keys oldest first with the private-key-free projection, then moves the Deployment
// Key to the front so every list starts with it.
func (r *MongoKeyRepo) find(ctx context.Context, filter bson.M) ([]*sshkeydomain.SSHKey, error) {
	cursor, err := r.col.Find(ctx, filter, options.Find().
		SetProjection(withoutPrivateKey).
		SetSort(bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []sshKeyDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	keys := make([]*sshkeydomain.SSHKey, len(docs))
	for i := range docs {
		keys[i] = toSSHKey(&docs[i])
	}
	sort.SliceStable(keys, func(i, j int) bool {
		return keys[i].Purpose == sshkeydomain.PurposeDeployment && keys[j].Purpose != sshkeydomain.PurposeDeployment
	})
	return keys, nil
}

// FindByID returns one key of any purpose, or ErrKeyNotFound. Ownership is checked by the caller.
func (r *MongoKeyRepo) FindByID(ctx context.Context, id string) (*sshkeydomain.SSHKey, error) {
	key, err := r.findOne(ctx, bson.M{"_id": id})
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, sshkeydomain.ErrKeyNotFound
	}
	return key, err
}

// FindDeployment returns the Deployment Key without its private key, or ErrDeploymentKeyNotFound.
func (r *MongoKeyRepo) FindDeployment(ctx context.Context) (*sshkeydomain.SSHKey, error) {
	key, err := r.findOne(ctx, bson.M{"purpose": string(sshkeydomain.PurposeDeployment)})
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, sshkeydomain.ErrDeploymentKeyNotFound
	}
	return key, err
}

// findOne decodes one key with the private-key-free projection; a miss is mongo.ErrNoDocuments.
func (r *MongoKeyRepo) findOne(ctx context.Context, filter bson.M) (*sshkeydomain.SSHKey, error) {
	var doc sshKeyDoc
	if err := r.col.FindOne(ctx, filter, options.FindOne().SetProjection(withoutPrivateKey)).Decode(&doc); err != nil {
		return nil, err
	}
	return toSSHKey(&doc), nil
}

// CreateAccess inserts an Access Key; a duplicate fingerprint is ErrDuplicateKey and a duplicate
// name for the same owner is ErrDuplicateName. No private key is ever stored for it.
func (r *MongoKeyRepo) CreateAccess(ctx context.Context, key *sshkeydomain.SSHKey) error {
	_, err := r.col.InsertOne(ctx, toSSHKeyDoc(key, ""))
	return duplicateKeyError(err)
}

// CreateDeployment seals privateKeyPEM and inserts the Deployment Key. A second Deployment Key
// violates the partial unique index and returns ErrDeploymentKeyExists.
func (r *MongoKeyRepo) CreateDeployment(ctx context.Context, key *sshkeydomain.SSHKey, privateKeyPEM string) error {
	sealed, err := r.sealer.Seal(privateKeyPEM)
	if err != nil {
		return err
	}
	_, err = r.col.InsertOne(ctx, toSSHKeyDoc(key, sealed))
	return duplicateKeyError(err)
}

// ReplaceDeployment overwrites the Deployment Key's name, material, and sealed private key in one
// document update, so there is never a moment without a usable Deployment Key.
func (r *MongoKeyRepo) ReplaceDeployment(ctx context.Context, key *sshkeydomain.SSHKey, privateKeyPEM string) error {
	sealed, err := r.sealer.Seal(privateKeyPEM)
	if err != nil {
		return err
	}
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": key.ID, "purpose": string(sshkeydomain.PurposeDeployment)},
		bson.M{"$set": bson.M{
			"name":             key.Name,
			"nameKey":          nameKey(key.Name),
			"keyType":          key.KeyType,
			"fingerprint":      key.Fingerprint,
			"publicKey":        key.PublicKey,
			"sealedPrivateKey": sealed,
			"updatedAt":        key.UpdatedAt,
		}},
	)
	if err != nil {
		return duplicateKeyError(err)
	}
	if result.MatchedCount == 0 {
		return sshkeydomain.ErrDeploymentKeyNotFound
	}
	return nil
}

// DeploymentPrivateKey is the only read of sealed key material.
func (r *MongoKeyRepo) DeploymentPrivateKey(ctx context.Context) (string, error) {
	var doc sshKeyDoc
	err := r.col.FindOne(ctx,
		bson.M{"purpose": string(sshkeydomain.PurposeDeployment)},
		options.FindOne().SetProjection(bson.M{"sealedPrivateKey": 1}),
	).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) || (err == nil && doc.SealedPrivateKey == "") {
		return "", sshkeydomain.ErrDeploymentKeyNotFound
	}
	if err != nil {
		return "", err
	}
	return r.sealer.Open(doc.SealedPrivateKey)
}

// DeleteAccess removes one Access Key only when it belongs to ownerUserID, so the filter itself
// enforces ownership; anything else (another owner's key, the Deployment Key) is ErrKeyNotFound.
func (r *MongoKeyRepo) DeleteAccess(ctx context.Context, ownerUserID, id string) error {
	result, err := r.col.DeleteOne(ctx, bson.M{
		"_id": id, "purpose": string(sshkeydomain.PurposeAccess), "ownerUserId": ownerUserID,
	})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return sshkeydomain.ErrKeyNotFound
	}
	return nil
}

// duplicateKeyError maps a unique-index violation onto the invariant it protects. The driver
// reports the violated index only in the server message, so the index name is matched there.
func duplicateKeyError(err error) error {
	if err == nil || !mongo.IsDuplicateKeyError(err) {
		return err
	}
	message := err.Error()
	switch {
	case strings.Contains(message, indexSingleDeployment):
		return sshkeydomain.ErrDeploymentKeyExists
	case strings.Contains(message, indexOwnerName):
		return sshkeydomain.ErrDuplicateName
	default:
		return sshkeydomain.ErrDuplicateKey
	}
}

// nameKey case-folds a name for the per-owner uniqueness index, so "Laptop" and "laptop" collide.
func nameKey(name string) string {
	return strings.ToLower(name)
}

func toSSHKeyDoc(key *sshkeydomain.SSHKey, sealedPrivateKey string) sshKeyDoc {
	return sshKeyDoc{
		ID:               key.ID,
		Name:             key.Name,
		NameKey:          nameKey(key.Name),
		Purpose:          string(key.Purpose),
		OwnerUserID:      key.OwnerUserID,
		KeyType:          key.KeyType,
		Fingerprint:      key.Fingerprint,
		PublicKey:        key.PublicKey,
		SealedPrivateKey: sealedPrivateKey,
		CreatedAt:        key.CreatedAt,
		UpdatedAt:        key.UpdatedAt,
	}
}

func toSSHKey(doc *sshKeyDoc) *sshkeydomain.SSHKey {
	return &sshkeydomain.SSHKey{
		ID:          doc.ID,
		Name:        doc.Name,
		Purpose:     sshkeydomain.Purpose(doc.Purpose),
		OwnerUserID: doc.OwnerUserID,
		KeyType:     doc.KeyType,
		Fingerprint: doc.Fingerprint,
		PublicKey:   doc.PublicKey,
		CreatedAt:   doc.CreatedAt,
		UpdatedAt:   doc.UpdatedAt,
	}
}
