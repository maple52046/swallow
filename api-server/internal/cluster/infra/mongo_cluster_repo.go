package infra

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	clusterdomain "github.com/maple52046/swallow/internal/cluster/domain"
)

type clusterDoc struct {
	ID                 string `bson:"_id"`
	SiteID             string `bson:"siteId"`
	Name               string `bson:"name"`
	Type               string `bson:"type"`
	IntegrationID      string `bson:"integrationId,omitempty"`
	OwnedIntegrationID string `bson:"ownedIntegrationId,omitempty"`
	GPUStackOwner      string `bson:"gpuStackOwner"`
	// ExporterOwner is omitempty so a document written before this field existed
	// decodes as empty and is defaulted to ansible on read.
	ExporterOwner string      `bson:"exporterOwner,omitempty"`
	Sync          clusterSync `bson:"sync"`
	CreatedAt     time.Time   `bson:"createdAt"`
	UpdatedAt     time.Time   `bson:"updatedAt"`
}

type clusterSync struct {
	LastStartedAt   *time.Time `bson:"lastStartedAt,omitempty"`
	LastSucceededAt *time.Time `bson:"lastSucceededAt,omitempty"`
	LastError       string     `bson:"lastError,omitempty"`
	MemberCount     int        `bson:"memberCount"`
	MatchedCount    int        `bson:"matchedCount"`
}

type MongoClusterRepo struct {
	col *mongo.Collection
}

func NewMongoClusterRepo(db *mongo.Database) (*MongoClusterRepo, error) {
	col := db.Collection("clusters")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "siteId", Value: 1}, {Key: "name", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("site_cluster_name"),
	})
	if err != nil {
		return nil, err
	}

	return &MongoClusterRepo{col: col}, nil
}

func (r *MongoClusterRepo) Create(ctx context.Context, cluster *clusterdomain.Cluster) error {
	_, err := r.col.InsertOne(ctx, toDoc(cluster))
	if mongo.IsDuplicateKeyError(err) {
		return clusterdomain.ErrClusterNameTaken
	}
	return err
}

func (r *MongoClusterRepo) FindByID(ctx context.Context, id string) (*clusterdomain.Cluster, error) {
	var doc clusterDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, clusterdomain.ErrClusterNotFound
	}
	if err != nil {
		return nil, err
	}
	return toCluster(&doc), nil
}

func (r *MongoClusterRepo) List(ctx context.Context, siteID string) ([]*clusterdomain.Cluster, error) {
	query := bson.M{}
	if siteID != "" {
		query["siteId"] = siteID
	}

	cursor, err := r.col.Find(ctx, query, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []clusterDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	clusters := make([]*clusterdomain.Cluster, len(docs))
	for i := range docs {
		clusters[i] = toCluster(&docs[i])
	}
	return clusters, nil
}

func (r *MongoClusterRepo) Update(ctx context.Context, cluster *clusterdomain.Cluster) error {
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": cluster.ID},
		bson.M{"$set": bson.M{
			"name":               cluster.Name,
			"integrationId":      cluster.IntegrationID,
			"ownedIntegrationId": cluster.OwnedIntegrationID,
			"gpuStackOwner":      string(cluster.GPUStackOwner),
			"exporterOwner":      string(cluster.ExporterOwner),
			"updatedAt":          cluster.UpdatedAt,
		}},
	)
	if mongo.IsDuplicateKeyError(err) {
		return clusterdomain.ErrClusterNameTaken
	}
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return clusterdomain.ErrClusterNotFound
	}
	return nil
}

func (r *MongoClusterRepo) UpdateSyncState(
	ctx context.Context,
	id, integrationID string,
	state clusterdomain.SyncState,
) error {
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id, "integrationId": integrationID},
		bson.M{"$set": bson.M{"sync": clusterSync{
			LastStartedAt:   state.LastStartedAt,
			LastSucceededAt: state.LastSucceededAt,
			LastError:       state.LastError,
			MemberCount:     state.MemberCount,
			MatchedCount:    state.MatchedCount,
		}}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return clusterdomain.ErrClusterNotFound
	}
	return nil
}

func (r *MongoClusterRepo) Delete(ctx context.Context, id string) error {
	result, err := r.col.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return clusterdomain.ErrClusterNotFound
	}
	return nil
}

func toDoc(cluster *clusterdomain.Cluster) *clusterDoc {
	return &clusterDoc{
		ID:                 cluster.ID,
		SiteID:             cluster.SiteID,
		Name:               cluster.Name,
		Type:               string(cluster.Type),
		IntegrationID:      cluster.IntegrationID,
		OwnedIntegrationID: cluster.OwnedIntegrationID,
		GPUStackOwner:      string(cluster.GPUStackOwner),
		ExporterOwner:      string(cluster.ExporterOwner),
		Sync: clusterSync{
			LastStartedAt:   cluster.Sync.LastStartedAt,
			LastSucceededAt: cluster.Sync.LastSucceededAt,
			LastError:       cluster.Sync.LastError,
			MemberCount:     cluster.Sync.MemberCount,
			MatchedCount:    cluster.Sync.MatchedCount,
		},
		CreatedAt: cluster.CreatedAt,
		UpdatedAt: cluster.UpdatedAt,
	}
}

func toCluster(doc *clusterDoc) *clusterdomain.Cluster {
	// A cluster stored before exporterOwner existed decodes as empty; treat that as the
	// default ansible owner rather than an invalid empty value.
	exporterOwner := clusterdomain.ExporterOwner(doc.ExporterOwner)
	if exporterOwner == "" {
		exporterOwner = clusterdomain.ExporterOwnerAnsible
	}
	return &clusterdomain.Cluster{
		ID:                 doc.ID,
		SiteID:             doc.SiteID,
		Name:               doc.Name,
		Type:               clusterdomain.ClusterType(doc.Type),
		IntegrationID:      doc.IntegrationID,
		OwnedIntegrationID: doc.OwnedIntegrationID,
		GPUStackOwner:      clusterdomain.GPUStackOwner(doc.GPUStackOwner),
		ExporterOwner:      exporterOwner,
		Sync: clusterdomain.SyncState{
			LastStartedAt:   doc.Sync.LastStartedAt,
			LastSucceededAt: doc.Sync.LastSucceededAt,
			LastError:       doc.Sync.LastError,
			MemberCount:     doc.Sync.MemberCount,
			MatchedCount:    doc.Sync.MatchedCount,
		},
		CreatedAt: doc.CreatedAt,
		UpdatedAt: doc.UpdatedAt,
	}
}
