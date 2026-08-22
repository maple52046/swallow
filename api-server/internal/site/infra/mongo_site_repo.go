package infra

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

type siteDoc struct {
	ID          string    `bson:"_id"`
	Name        string    `bson:"name"`
	Description string    `bson:"description,omitempty"`
	CreatedAt   time.Time `bson:"createdAt"`
	UpdatedAt   time.Time `bson:"updatedAt"`
}

type MongoSiteRepo struct {
	col *mongo.Collection
}

func NewMongoSiteRepo(db *mongo.Database) (*MongoSiteRepo, error) {
	col := db.Collection("sites")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "name", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("site_name"),
	})
	if err != nil {
		return nil, err
	}

	return &MongoSiteRepo{col: col}, nil
}

func (r *MongoSiteRepo) Create(ctx context.Context, site *sitedomain.Site) error {
	_, err := r.col.InsertOne(ctx, siteDoc{
		ID:          site.ID,
		Name:        site.Name,
		Description: site.Description,
		CreatedAt:   site.CreatedAt,
		UpdatedAt:   site.UpdatedAt,
	})
	if mongo.IsDuplicateKeyError(err) {
		return sitedomain.ErrSiteNameTaken
	}
	return err
}

func (r *MongoSiteRepo) FindByID(ctx context.Context, id string) (*sitedomain.Site, error) {
	var doc siteDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, sitedomain.ErrSiteNotFound
	}
	if err != nil {
		return nil, err
	}
	return toSite(&doc), nil
}

func (r *MongoSiteRepo) List(ctx context.Context) ([]*sitedomain.Site, error) {
	cursor, err := r.col.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []siteDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	sites := make([]*sitedomain.Site, len(docs))
	for i := range docs {
		sites[i] = toSite(&docs[i])
	}
	return sites, nil
}

func (r *MongoSiteRepo) Update(ctx context.Context, site *sitedomain.Site) error {
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": site.ID},
		bson.M{"$set": bson.M{
			"name":        site.Name,
			"description": site.Description,
			"updatedAt":   site.UpdatedAt,
		}},
	)
	if mongo.IsDuplicateKeyError(err) {
		return sitedomain.ErrSiteNameTaken
	}
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return sitedomain.ErrSiteNotFound
	}
	return nil
}

func (r *MongoSiteRepo) Delete(ctx context.Context, id string) error {
	result, err := r.col.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return sitedomain.ErrSiteNotFound
	}
	return nil
}

func toSite(doc *siteDoc) *sitedomain.Site {
	return &sitedomain.Site{
		ID:          doc.ID,
		Name:        doc.Name,
		Description: doc.Description,
		CreatedAt:   doc.CreatedAt,
		UpdatedAt:   doc.UpdatedAt,
	}
}
