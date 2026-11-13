package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
)

const (
	userCollection     = "users"
	indexUserUsername  = "username_unique"
	indexUserUIDNumber = "uid_number_unique"
)

// userDoc is the stored shape of a User. uidNumber is absent (0) on a User created before POSIX
// identities existed, until the API's start allocates one.
type userDoc struct {
	ID           string    `bson:"_id"`
	Username     string    `bson:"username"`
	PasswordHash string    `bson:"passwordHash"`
	Role         string    `bson:"role"`
	UIDNumber    int       `bson:"uidNumber,omitempty"`
	CreatedAt    time.Time `bson:"createdAt"`
	UpdatedAt    time.Time `bson:"updatedAt,omitempty"`
}

// MongoUserRepo stores Users in `users`. The unique username index enforces the username invariant
// across concurrent creators, and the partial unique uidNumber index keeps every allocated number on
// one User; both violations come back as domain errors.
type MongoUserRepo struct {
	col *mongo.Collection
}

// NewMongoUserRepo returns the repository without touching its indexes, for assembly code that only
// reads Users; EnsureUserIndexes creates them at the API's start.
func NewMongoUserRepo(db *mongo.Database) *MongoUserRepo {
	return &MongoUserRepo{col: db.Collection(userCollection)}
}

// EnsureUserIndexes creates the users collection's unique indexes. It is idempotent, so every API
// start calls it before the bootstrap admin is ensured.
func EnsureUserIndexes(ctx context.Context, db *mongo.Database) error {
	_, err := db.Collection(userCollection).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "username", Value: 1}}, Options: options.Index().SetUnique(true).SetName(indexUserUsername)},
		{
			Keys: bson.D{{Key: "uidNumber", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexUserUIDNumber).
				SetPartialFilterExpression(bson.M{"uidNumber": bson.M{"$gt": 0}}),
		},
	})
	if err != nil {
		return fmt.Errorf("users indexes: %w", err)
	}
	return nil
}

// FindByUsername implements authdomain.UserRepository.
func (r *MongoUserRepo) FindByUsername(ctx context.Context, username string) (*authdomain.User, error) {
	return r.findOne(ctx, bson.M{"username": username})
}

// FindByID implements authdomain.UserRepository.
func (r *MongoUserRepo) FindByID(ctx context.Context, id string) (*authdomain.User, error) {
	return r.findOne(ctx, bson.M{"_id": id})
}

func (r *MongoUserRepo) findOne(ctx context.Context, filter bson.M) (*authdomain.User, error) {
	var doc userDoc
	err := r.col.FindOne(ctx, filter).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, authdomain.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return toUser(&doc), nil
}

// Create implements authdomain.UserRepository; a duplicate username is ErrUsernameTaken.
func (r *MongoUserRepo) Create(ctx context.Context, user *authdomain.User) error {
	_, err := r.col.InsertOne(ctx, toUserDoc(user))
	if mongo.IsDuplicateKeyError(err) {
		return authdomain.ErrUsernameTaken
	}
	return err
}

// ExistsByUsername implements authdomain.UserRepository.
func (r *MongoUserRepo) ExistsByUsername(ctx context.Context, username string) (bool, error) {
	count, err := r.col.CountDocuments(ctx, bson.M{"username": username})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// List implements authdomain.UserRepository.
func (r *MongoUserRepo) List(ctx context.Context) ([]*authdomain.User, error) {
	cursor, err := r.col.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "username", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []userDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	users := make([]*authdomain.User, 0, len(docs))
	for i := range docs {
		users = append(users, toUser(&docs[i]))
	}
	return users, nil
}

// Update implements authdomain.UserRepository: it rewrites only the mutable fields.
func (r *MongoUserRepo) Update(ctx context.Context, user *authdomain.User) error {
	set := bson.M{"role": string(user.Role), "passwordHash": string(user.PasswordHash), "updatedAt": user.UpdatedAt}
	if user.UIDNumber > 0 {
		set["uidNumber"] = user.UIDNumber
	}
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": user.ID}, bson.M{"$set": set})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return authdomain.ErrUserNotFound
	}
	return nil
}

// Delete implements authdomain.UserRepository.
func (r *MongoUserRepo) Delete(ctx context.Context, id string) error {
	result, err := r.col.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return authdomain.ErrUserNotFound
	}
	return nil
}

// CountByRole implements authdomain.UserRepository.
func (r *MongoUserRepo) CountByRole(ctx context.Context, role authdomain.Role) (int, error) {
	count, err := r.col.CountDocuments(ctx, bson.M{"role": string(role)})
	return int(count), err
}

func toUserDoc(user *authdomain.User) userDoc {
	return userDoc{
		ID: user.ID, Username: user.Username, PasswordHash: string(user.PasswordHash), Role: string(user.Role),
		UIDNumber: user.UIDNumber, CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt,
	}
}

func toUser(doc *userDoc) *authdomain.User {
	updated := doc.UpdatedAt
	if updated.IsZero() {
		updated = doc.CreatedAt
	}
	return &authdomain.User{
		ID:           doc.ID,
		Username:     doc.Username,
		PasswordHash: authdomain.PasswordHash(doc.PasswordHash),
		Role:         authdomain.Role(doc.Role),
		UIDNumber:    doc.UIDNumber,
		CreatedAt:    doc.CreatedAt,
		UpdatedAt:    updated,
	}
}
