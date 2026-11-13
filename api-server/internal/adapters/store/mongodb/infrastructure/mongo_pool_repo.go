package infrastructure

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	infradomain "github.com/maple52046/swallow/internal/core/infrastructure/domain"
)

// poolDoc is the MongoDB shape of a Pool. It lives in its own collection, apart from zones, so a
// Zone and a Pool may share a name. A Pool has no Site, generation, or status (decision 082); schema
// v16 removed those fields from Pools stored before.
type poolDoc struct {
	ID          string    `bson:"_id"`
	Name        string    `bson:"name"`
	Description string    `bson:"description,omitempty"`
	CreatedAt   time.Time `bson:"createdAt"`
	UpdatedAt   time.Time `bson:"updatedAt"`
}

// MongoPoolRepo is the durable PoolRepository.
type MongoPoolRepo struct {
	col *mongo.Collection
}

// NewMongoPoolRepo creates the Pool repository and its uniqueness index.
//
// Pool names are unique across the installation, so the unique index is on name alone; it is what
// makes Create and Update return ErrPoolNameTaken instead of allowing a duplicate. The index is
// created at startup, after the schema check, so the v16 migration has already merged the per-Site
// `default` Pools that would otherwise collide.
func NewMongoPoolRepo(db *mongo.Database) (*MongoPoolRepo, error) {
	col := db.Collection("pools")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "name", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("pool_name"),
	})
	if err != nil {
		return nil, err
	}

	return &MongoPoolRepo{col: col}, nil
}

func (r *MongoPoolRepo) Create(ctx context.Context, pool *infradomain.Pool) error {
	_, err := r.col.InsertOne(ctx, poolDoc{
		ID:          pool.ID,
		Name:        pool.Name,
		Description: pool.Description,
		CreatedAt:   pool.CreatedAt,
		UpdatedAt:   pool.UpdatedAt,
	})
	if mongo.IsDuplicateKeyError(err) {
		return infradomain.ErrPoolNameTaken
	}
	return err
}

func (r *MongoPoolRepo) FindByID(ctx context.Context, id string) (*infradomain.Pool, error) {
	var doc poolDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, infradomain.ErrPoolNotFound
	}
	if err != nil {
		return nil, err
	}
	return toPool(&doc), nil
}

func (r *MongoPoolRepo) List(ctx context.Context) ([]*infradomain.Pool, error) {
	cursor, err := r.col.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []poolDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	pools := make([]*infradomain.Pool, len(docs))
	for i := range docs {
		pools[i] = toPool(&docs[i])
	}
	return pools, nil
}

func (r *MongoPoolRepo) Update(ctx context.Context, pool *infradomain.Pool) error {
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": pool.ID},
		bson.M{"$set": bson.M{"name": pool.Name, "description": pool.Description, "updatedAt": pool.UpdatedAt}},
	)
	if mongo.IsDuplicateKeyError(err) {
		return infradomain.ErrPoolNameTaken
	}
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return infradomain.ErrPoolNotFound
	}
	return nil
}

func (r *MongoPoolRepo) Delete(ctx context.Context, id string) error {
	result, err := r.col.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return infradomain.ErrPoolNotFound
	}
	return nil
}

func toPool(doc *poolDoc) *infradomain.Pool {
	return &infradomain.Pool{
		ID:          doc.ID,
		Name:        doc.Name,
		Description: doc.Description,
		CreatedAt:   doc.CreatedAt,
		UpdatedAt:   doc.UpdatedAt,
	}
}
