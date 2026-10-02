package infra

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/maple52046/swallow/internal/shared/secret"
	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// registryCredentialDoc is the BSON shape of a Registry Credential. SealedPassword is excluded from
// every read projection except FindAuth.
type registryCredentialDoc struct {
	ID             string    `bson:"_id"`
	Registry       string    `bson:"registry"`
	Username       string    `bson:"username"`
	SealedPassword string    `bson:"sealedPassword"`
	CreatedAt      time.Time `bson:"createdAt"`
	UpdatedAt      time.Time `bson:"updatedAt"`
	UpdatedBy      string    `bson:"updatedBy,omitempty"`
}

// withoutPassword is the projection of every ordinary read, so sealed material is never decoded
// outside FindAuth.
var withoutPassword = bson.M{"sealedPassword": 0}

// MongoRegistryCredentialRepo is the durable softwaredomain.RegistryCredentialRepository over the
// `software_registry_credentials` collection (decision 044).
//
// Registry uniqueness is a unique index, so concurrent creates cannot store two credentials for one
// registry. Passwords are sealed with the installation credential key before they are written; a
// database dump alone does not yield them, and a key rotation makes them unreadable (FindAuth then
// fails rather than sending a wrong credential).
type MongoRegistryCredentialRepo struct {
	col    *mongo.Collection
	sealer *secret.Sealer
}

var _ softwaredomain.RegistryCredentialRepository = (*MongoRegistryCredentialRepo)(nil)

// NewMongoRegistryCredentialRepo creates the repository and its unique registry index.
func NewMongoRegistryCredentialRepo(db *mongo.Database, sealer *secret.Sealer) (*MongoRegistryCredentialRepo, error) {
	col := db.Collection("software_registry_credentials")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "registry", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("registry"),
	})
	if err != nil {
		return nil, err
	}
	return &MongoRegistryCredentialRepo{col: col, sealer: sealer}, nil
}

// List returns every credential ordered by registry, without passwords.
func (r *MongoRegistryCredentialRepo) List(ctx context.Context) ([]*softwaredomain.RegistryCredential, error) {
	cursor, err := r.col.Find(ctx, bson.M{}, options.Find().
		SetProjection(withoutPassword).
		SetSort(bson.D{{Key: "registry", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []registryCredentialDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	credentials := make([]*softwaredomain.RegistryCredential, len(docs))
	for i := range docs {
		credentials[i] = toRegistryCredential(&docs[i])
	}
	return credentials, nil
}

// FindByID returns one credential without its password, or ErrRegistryCredentialNotFound.
func (r *MongoRegistryCredentialRepo) FindByID(ctx context.Context, id string) (*softwaredomain.RegistryCredential, error) {
	var doc registryCredentialDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id}, options.FindOne().SetProjection(withoutPassword)).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, softwaredomain.ErrRegistryCredentialNotFound
	}
	if err != nil {
		return nil, err
	}
	return toRegistryCredential(&doc), nil
}

// FindAuth is the only read that decodes a password: the credential for a normalized registry and
// its unsealed password, or ErrRegistryCredentialNotFound.
func (r *MongoRegistryCredentialRepo) FindAuth(ctx context.Context, registry string) (*softwaredomain.RegistryCredential, string, error) {
	var doc registryCredentialDoc
	err := r.col.FindOne(ctx, bson.M{"registry": registry}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, "", softwaredomain.ErrRegistryCredentialNotFound
	}
	if err != nil {
		return nil, "", err
	}
	password, err := r.sealer.Open(doc.SealedPassword)
	if err != nil {
		return nil, "", err
	}
	return toRegistryCredential(&doc), password, nil
}

// Create seals the password and inserts the credential; a duplicate registry is
// ErrRegistryCredentialExists.
func (r *MongoRegistryCredentialRepo) Create(ctx context.Context, credential *softwaredomain.RegistryCredential, password string) error {
	sealed, err := r.sealer.Seal(password)
	if err != nil {
		return err
	}
	_, err = r.col.InsertOne(ctx, registryCredentialDoc{
		ID: credential.ID, Registry: credential.Registry, Username: credential.Username, SealedPassword: sealed,
		CreatedAt: credential.CreatedAt, UpdatedAt: credential.UpdatedAt, UpdatedBy: credential.UpdatedBy,
	})
	if mongo.IsDuplicateKeyError(err) {
		return softwaredomain.ErrRegistryCredentialExists
	}
	return err
}

// Replace overwrites the username, sealed password, and update stamp in one document update; the
// registry and creation time are never changed. A missing id is ErrRegistryCredentialNotFound.
func (r *MongoRegistryCredentialRepo) Replace(ctx context.Context, credential *softwaredomain.RegistryCredential, password string) error {
	sealed, err := r.sealer.Seal(password)
	if err != nil {
		return err
	}
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": credential.ID}, bson.M{"$set": bson.M{
		"username": credential.Username, "sealedPassword": sealed,
		"updatedAt": credential.UpdatedAt, "updatedBy": credential.UpdatedBy,
	}})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return softwaredomain.ErrRegistryCredentialNotFound
	}
	return nil
}

// Delete removes one credential, or returns ErrRegistryCredentialNotFound.
func (r *MongoRegistryCredentialRepo) Delete(ctx context.Context, id string) error {
	result, err := r.col.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return softwaredomain.ErrRegistryCredentialNotFound
	}
	return nil
}

func toRegistryCredential(doc *registryCredentialDoc) *softwaredomain.RegistryCredential {
	return &softwaredomain.RegistryCredential{
		ID: doc.ID, Registry: doc.Registry, Username: doc.Username,
		CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt, UpdatedBy: doc.UpdatedBy,
	}
}
