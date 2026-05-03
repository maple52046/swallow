package infra

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	serverdomain "github.com/AFDEAPAC/swallow/internal/server/domain"
)

type serverDoc struct {
	ID        string    `bson:"_id"`
	Hostname  string    `bson:"hostname"`
	IP        string    `bson:"ip"`
	Status    string    `bson:"status"`
	CreatedAt time.Time `bson:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt"`
}

type MongoServerRepo struct {
	col *mongo.Collection
}

func NewMongoServerRepo(db *mongo.Database) (*MongoServerRepo, error) {
	col := db.Collection("servers")

	indexModels := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "hostname", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "ip", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := col.Indexes().CreateMany(ctx, indexModels); err != nil {
		return nil, err
	}

	return &MongoServerRepo{col: col}, nil
}

func (r *MongoServerRepo) Create(ctx context.Context, server *serverdomain.Server) error {
	doc := serverDoc{
		ID:        server.ID,
		Hostname:  server.Hostname,
		IP:        server.IP,
		Status:    string(server.Status),
		CreatedAt: server.CreatedAt,
		UpdatedAt: server.UpdatedAt,
	}
	_, err := r.col.InsertOne(ctx, doc)
	if mongo.IsDuplicateKeyError(err) {
		return serverdomain.ErrHostnameTaken
	}
	return err
}

func (r *MongoServerRepo) List(ctx context.Context, filter serverdomain.ListFilter) (serverdomain.ListResult, error) {
	query := bson.M{}

	if filter.Status != "" {
		query["status"] = string(filter.Status)
	}
	if filter.Keyword != "" {
		regex := bson.M{"$regex": filter.Keyword, "$options": "i"}
		query["$or"] = bson.A{
			bson.M{"hostname": regex},
			bson.M{"ip": regex},
		}
	}

	total, err := r.col.CountDocuments(ctx, query)
	if err != nil {
		return serverdomain.ListResult{}, err
	}

	opts := options.Find().
		SetSkip(int64(filter.Offset)).
		SetLimit(int64(filter.Limit)).
		SetSort(bson.D{{Key: "createdAt", Value: -1}})

	cursor, err := r.col.Find(ctx, query, opts)
	if err != nil {
		return serverdomain.ListResult{}, err
	}
	defer cursor.Close(ctx)

	var docs []serverDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return serverdomain.ListResult{}, err
	}

	servers := make([]*serverdomain.Server, len(docs))
	for i, d := range docs {
		servers[i] = toServer(&d)
	}

	return serverdomain.ListResult{Servers: servers, Total: int(total)}, nil
}

func (r *MongoServerRepo) Delete(ctx context.Context, id string) error {
	result, err := r.col.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return serverdomain.ErrServerNotFound
	}
	return nil
}

func (r *MongoServerRepo) ExistsByHostname(ctx context.Context, hostname string) (bool, error) {
	count, err := r.col.CountDocuments(ctx, bson.M{"hostname": hostname})
	return count > 0, err
}

func (r *MongoServerRepo) ExistsByIP(ctx context.Context, ip string) (bool, error) {
	count, err := r.col.CountDocuments(ctx, bson.M{"ip": ip})
	return count > 0, err
}

func toServer(doc *serverDoc) *serverdomain.Server {
	return &serverdomain.Server{
		ID:        doc.ID,
		Hostname:  doc.Hostname,
		IP:        doc.IP,
		Status:    serverdomain.ServerStatus(doc.Status),
		CreatedAt: doc.CreatedAt,
		UpdatedAt: doc.UpdatedAt,
	}
}
