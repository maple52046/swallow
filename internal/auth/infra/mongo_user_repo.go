package infra

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	authdomain "github.com/AFDEAPAC/swallow/internal/auth/domain"
)

type userDoc struct {
	ID           string    `bson:"_id"`
	Username     string    `bson:"username"`
	PasswordHash string    `bson:"passwordHash"`
	Role         string    `bson:"role"`
	CreatedAt    time.Time `bson:"createdAt"`
}

type MongoUserRepo struct {
	col *mongo.Collection
}

func NewMongoUserRepo(db *mongo.Database) *MongoUserRepo {
	return &MongoUserRepo{col: db.Collection("users")}
}

func (r *MongoUserRepo) FindByUsername(ctx context.Context, username string) (*authdomain.User, error) {
	var doc userDoc
	err := r.col.FindOne(ctx, bson.M{"username": username}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, authdomain.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return toUser(&doc), nil
}

func (r *MongoUserRepo) FindByID(ctx context.Context, id string) (*authdomain.User, error) {
	var doc userDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, authdomain.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return toUser(&doc), nil
}

func (r *MongoUserRepo) Create(ctx context.Context, user *authdomain.User) error {
	doc := userDoc{
		ID:           user.ID,
		Username:     user.Username,
		PasswordHash: string(user.PasswordHash),
		Role:         string(user.Role),
		CreatedAt:    user.CreatedAt,
	}
	_, err := r.col.InsertOne(ctx, doc)
	return err
}

func (r *MongoUserRepo) ExistsByUsername(ctx context.Context, username string) (bool, error) {
	count, err := r.col.CountDocuments(ctx, bson.M{"username": username})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func toUser(doc *userDoc) *authdomain.User {
	return &authdomain.User{
		ID:           doc.ID,
		Username:     doc.Username,
		PasswordHash: authdomain.PasswordHash(doc.PasswordHash),
		Role:         authdomain.Role(doc.Role),
		CreatedAt:    doc.CreatedAt,
	}
}
