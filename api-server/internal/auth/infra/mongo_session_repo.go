package infra

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	authdomain "github.com/maple52046/swallow/internal/auth/domain"
)

const (
	sessionCollection        = "auth_sessions"
	indexSessionTokenHash    = "token_hash_unique"
	indexSessionPreviousHash = "previous_token_hash"
	indexSessionPurge        = "absolute_expiry_ttl"
)

// sessionDoc is the stored shape of a Session. Only token hashes are stored; a refresh token
// never reaches the database.
type sessionDoc struct {
	ID                string     `bson:"_id"`
	UserID            string     `bson:"userId"`
	Client            string     `bson:"client"`
	TokenHash         string     `bson:"tokenHash"`
	PreviousTokenHash string     `bson:"previousTokenHash,omitempty"`
	RotatedAt         time.Time  `bson:"rotatedAt,omitempty"`
	CreatedAt         time.Time  `bson:"createdAt"`
	LastUsedAt        time.Time  `bson:"lastUsedAt"`
	ExpiresAt         time.Time  `bson:"expiresAt"`
	AbsoluteExpiresAt time.Time  `bson:"absoluteExpiresAt"`
	RevokedAt         *time.Time `bson:"revokedAt,omitempty"`
}

// MongoSessionRepo stores Sessions in `auth_sessions`.
//
// Its indexes are part of the security model: tokenHash is unique so a hash maps to at most one
// Session, previousTokenHash is indexed for the reuse check, and a TTL index on absoluteExpiresAt
// removes Sessions once they can never be used again (revoked Sessions are kept until then, which
// is harmless because Active rejects them).
type MongoSessionRepo struct {
	col *mongo.Collection
}

// NewMongoSessionRepo creates the repository and its indexes. Index creation is idempotent, so
// every API start may call it.
func NewMongoSessionRepo(db *mongo.Database) (*MongoSessionRepo, error) {
	col := db.Collection(sessionCollection)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "tokenHash", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexSessionTokenHash),
		},
		{
			Keys:    bson.D{{Key: "previousTokenHash", Value: 1}},
			Options: options.Index().SetName(indexSessionPreviousHash).SetSparse(true),
		},
		{
			Keys:    bson.D{{Key: "absoluteExpiresAt", Value: 1}},
			Options: options.Index().SetName(indexSessionPurge).SetExpireAfterSeconds(0),
		},
	})
	if err != nil {
		return nil, err
	}
	return &MongoSessionRepo{col: col}, nil
}

// Create inserts a new Session.
func (r *MongoSessionRepo) Create(ctx context.Context, session *authdomain.Session) error {
	_, err := r.col.InsertOne(ctx, toSessionDoc(session))
	return err
}

// FindByTokenHash returns the Session whose current refresh token hashes to hash.
func (r *MongoSessionRepo) FindByTokenHash(ctx context.Context, hash string) (*authdomain.Session, error) {
	return r.findOne(ctx, bson.M{"tokenHash": hash})
}

// FindByPreviousTokenHash returns the Session whose replaced refresh token hashes to hash.
func (r *MongoSessionRepo) FindByPreviousTokenHash(ctx context.Context, hash string) (*authdomain.Session, error) {
	return r.findOne(ctx, bson.M{"previousTokenHash": hash})
}

// FindByID returns the Session with this ID.
func (r *MongoSessionRepo) FindByID(ctx context.Context, id string) (*authdomain.Session, error) {
	return r.findOne(ctx, bson.M{"_id": id})
}

// Rotate is a single conditional update: it matches only while tokenHash still equals fromHash
// and the Session is unrevoked, so of two concurrent refreshes exactly one rotates.
func (r *MongoSessionRepo) Rotate(ctx context.Context, id, fromHash, toHash string, rotatedAt, expiresAt time.Time) error {
	res, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id, "tokenHash": fromHash, "revokedAt": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{
			"tokenHash":         toHash,
			"previousTokenHash": fromHash,
			"rotatedAt":         rotatedAt,
			"lastUsedAt":        rotatedAt,
			"expiresAt":         expiresAt,
		}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return authdomain.ErrSessionNotFound
	}
	return nil
}

// Touch records a use on the grace path; it does not move the idle expiry.
func (r *MongoSessionRepo) Touch(ctx context.Context, id string, usedAt time.Time) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"lastUsedAt": usedAt}})
	return err
}

// Revoke sets revokedAt once; later calls keep the first revocation time.
func (r *MongoSessionRepo) Revoke(ctx context.Context, id string, revokedAt time.Time) error {
	_, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id, "revokedAt": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"revokedAt": revokedAt}},
	)
	return err
}

func (r *MongoSessionRepo) findOne(ctx context.Context, filter bson.M) (*authdomain.Session, error) {
	var doc sessionDoc
	err := r.col.FindOne(ctx, filter).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, authdomain.ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	return toSession(&doc), nil
}

func toSessionDoc(s *authdomain.Session) *sessionDoc {
	return &sessionDoc{
		ID:                s.ID,
		UserID:            s.UserID,
		Client:            string(s.Client),
		TokenHash:         s.TokenHash,
		PreviousTokenHash: s.PreviousTokenHash,
		RotatedAt:         s.RotatedAt,
		CreatedAt:         s.CreatedAt,
		LastUsedAt:        s.LastUsedAt,
		ExpiresAt:         s.ExpiresAt,
		AbsoluteExpiresAt: s.AbsoluteExpiresAt,
		RevokedAt:         s.RevokedAt,
	}
}

func toSession(d *sessionDoc) *authdomain.Session {
	return &authdomain.Session{
		ID:                d.ID,
		UserID:            d.UserID,
		Client:            authdomain.SessionClient(d.Client),
		TokenHash:         d.TokenHash,
		PreviousTokenHash: d.PreviousTokenHash,
		RotatedAt:         d.RotatedAt,
		CreatedAt:         d.CreatedAt,
		LastUsedAt:        d.LastUsedAt,
		ExpiresAt:         d.ExpiresAt,
		AbsoluteExpiresAt: d.AbsoluteExpiresAt,
		RevokedAt:         d.RevokedAt,
	}
}
