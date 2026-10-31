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

// poolDoc is the MongoDB shape of a Pool. It mirrors zoneDoc but lives in its own collection so
// a Zone and a Pool may share a name within a Site without colliding.
type poolDoc struct {
	ID          string         `bson:"_id"`
	SiteID      string         `bson:"siteId"`
	Generation  int64          `bson:"generation"`
	Name        string         `bson:"name"`
	Description string         `bson:"description,omitempty"`
	Status      groupStatusDoc `bson:"status"`
	CreatedAt   time.Time      `bson:"createdAt"`
	UpdatedAt   time.Time      `bson:"updatedAt"`
}

// MongoPoolRepo is the durable PoolRepository.
type MongoPoolRepo struct {
	col *mongo.Collection
}

// NewMongoPoolRepo creates the Pool repository and its per-Site uniqueness index, mirroring
// NewMongoZoneRepo. The compound (siteId, name) unique index is what surfaces ErrPoolNameTaken.
func NewMongoPoolRepo(db *mongo.Database) (*MongoPoolRepo, error) {
	col := db.Collection("pools")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "siteId", Value: 1}, {Key: "name", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("pool_site_name"),
	})
	if err != nil {
		return nil, err
	}

	return &MongoPoolRepo{col: col}, nil
}

func (r *MongoPoolRepo) Create(ctx context.Context, pool *infradomain.Pool) error {
	_, err := r.col.InsertOne(ctx, poolDoc{
		ID:          pool.ID,
		SiteID:      pool.SiteID,
		Generation:  pool.Generation,
		Name:        pool.Name,
		Description: pool.Description,
		Status:      toGroupStatusDoc(pool.Status),
		CreatedAt:   pool.CreatedAt,
		UpdatedAt:   pool.UpdatedAt,
	})
	if mongo.IsDuplicateKeyError(err) {
		return infradomain.ErrPoolNameTaken
	}
	return err
}

func (r *MongoPoolRepo) FindByID(ctx context.Context, id string) (*infradomain.Pool, error) {
	return r.findOne(ctx, bson.M{"_id": id})
}

func (r *MongoPoolRepo) FindByName(ctx context.Context, siteID, name string) (*infradomain.Pool, error) {
	return r.findOne(ctx, bson.M{"siteId": siteID, "name": name})
}

func (r *MongoPoolRepo) findOne(ctx context.Context, filter bson.M) (*infradomain.Pool, error) {
	var doc poolDoc
	err := r.col.FindOne(ctx, filter).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, infradomain.ErrPoolNotFound
	}
	if err != nil {
		return nil, err
	}
	return toPool(&doc), nil
}

func (r *MongoPoolRepo) List(ctx context.Context, siteID string) ([]*infradomain.Pool, error) {
	filter := bson.M{}
	if siteID != "" {
		filter["siteId"] = siteID
	}
	cursor, err := r.col.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
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

func (r *MongoPoolRepo) UpdateSpec(ctx context.Context, pool *infradomain.Pool) error {
	var doc poolDoc
	err := r.col.FindOneAndUpdate(ctx,
		bson.M{"_id": pool.ID},
		bson.M{
			"$set": bson.M{"name": pool.Name, "description": pool.Description, "updatedAt": pool.UpdatedAt},
			"$inc": bson.M{"generation": 1},
		},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&doc)
	if mongo.IsDuplicateKeyError(err) {
		return infradomain.ErrPoolNameTaken
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return infradomain.ErrPoolNotFound
	}
	if err != nil {
		return err
	}
	pool.Generation = doc.Generation
	return nil
}

func (r *MongoPoolRepo) AdoptSpec(ctx context.Context, id, description string, at time.Time) error {
	_, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id, "generation": bson.M{"$in": bson.A{nil, int64(0)}}},
		bson.M{"$set": bson.M{"description": description, "generation": int64(1), "updatedAt": at}},
	)
	return err
}

func (r *MongoPoolRepo) SetStatus(ctx context.Context, id string, status infradomain.GroupStatus) error {
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"status": toGroupStatusDoc(status)}})
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
		SiteID:      doc.SiteID,
		Generation:  doc.Generation,
		Name:        doc.Name,
		Description: doc.Description,
		Status:      toGroupStatus(doc.Status),
		CreatedAt:   doc.CreatedAt,
		UpdatedAt:   doc.UpdatedAt,
	}
}
