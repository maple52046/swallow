package infra

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

type operationDoc struct {
	ID              string        `bson:"_id"`
	Kind            string        `bson:"kind"`
	Intent          string        `bson:"intent,omitempty"`
	SiteID          string        `bson:"siteId"`
	ClusterID       string        `bson:"clusterId,omitempty"`
	TargetServerIDs []string      `bson:"targetServerIds"`
	Automation      automationDoc `bson:"automation"`
	RequestedBy     string        `bson:"requestedBy"`
	RequestedAt     time.Time     `bson:"requestedAt"`
	UpdatedAt       time.Time     `bson:"updatedAt"`
	// Terminal is stored rather than derived so that "find unfinished operations"
	// is an index hit instead of a scan with a status set that grows over time.
	Terminal bool `bson:"terminal"`
}

type automationDoc struct {
	IntegrationID string     `bson:"integrationId"`
	JobTemplateID string     `bson:"jobTemplateId"`
	JobName       string     `bson:"jobName,omitempty"`
	JobID         string     `bson:"jobId,omitempty"`
	Status        string     `bson:"status"`
	StartedAt     *time.Time `bson:"startedAt,omitempty"`
	FinishedAt    *time.Time `bson:"finishedAt,omitempty"`
	ObservedAt    time.Time  `bson:"observedAt"`
}

type MongoOperationRepo struct {
	col *mongo.Collection
}

func NewMongoOperationRepo(db *mongo.Database) (*MongoOperationRepo, error) {
	col := db.Collection("operations")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "requestedAt", Value: -1}},
			Options: options.Index().SetName("requested_at"),
		},
		{
			Keys:    bson.D{{Key: "terminal", Value: 1}, {Key: "targetServerIds", Value: 1}},
			Options: options.Index().SetName("active_targets"),
		},
		{
			Keys: bson.D{
				{Key: "automation.integrationId", Value: 1},
				{Key: "automation.jobId", Value: 1},
			},
			Options: options.Index().SetSparse(true).SetName("automation_job"),
		},
		{
			Keys:    bson.D{{Key: "siteId", Value: 1}, {Key: "requestedAt", Value: -1}},
			Options: options.Index().SetName("site_requested_at"),
		},
	})
	if err != nil {
		return nil, err
	}

	return &MongoOperationRepo{col: col}, nil
}

func (r *MongoOperationRepo) Create(ctx context.Context, operation *operationdomain.Operation) error {
	_, err := r.col.InsertOne(ctx, toDoc(operation))
	return err
}

func (r *MongoOperationRepo) FindByID(ctx context.Context, id string) (*operationdomain.Operation, error) {
	var doc operationDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, operationdomain.ErrOperationNotFound
	}
	if err != nil {
		return nil, err
	}
	return toOperation(&doc), nil
}

func (r *MongoOperationRepo) FindByJobID(ctx context.Context, integrationID, jobID string) (*operationdomain.Operation, error) {
	var doc operationDoc
	err := r.col.FindOne(ctx, bson.M{
		"automation.integrationId": integrationID,
		"automation.jobId":         jobID,
	}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, operationdomain.ErrOperationNotFound
	}
	if err != nil {
		return nil, err
	}
	return toOperation(&doc), nil
}

func (r *MongoOperationRepo) List(ctx context.Context, filter operationdomain.ListFilter) (operationdomain.ListResult, error) {
	query := bson.M{}
	if filter.SiteID != "" {
		query["siteId"] = filter.SiteID
	}
	if filter.ClusterID != "" {
		query["clusterId"] = filter.ClusterID
	}
	if filter.ServerID != "" {
		query["targetServerIds"] = filter.ServerID
	}
	if filter.Kind != "" {
		query["kind"] = string(filter.Kind)
	}
	if filter.Status != "" {
		query["automation.status"] = string(filter.Status)
	}
	if filter.ActiveOnly {
		query["terminal"] = false
	}

	total, err := r.col.CountDocuments(ctx, query)
	if err != nil {
		return operationdomain.ListResult{}, err
	}

	opts := options.Find().
		SetSkip(int64(filter.Offset)).
		SetSort(bson.D{{Key: "requestedAt", Value: -1}})
	if filter.Limit > 0 {
		opts.SetLimit(int64(filter.Limit))
	}

	cursor, err := r.col.Find(ctx, query, opts)
	if err != nil {
		return operationdomain.ListResult{}, err
	}
	defer cursor.Close(ctx)

	var docs []operationDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return operationdomain.ListResult{}, err
	}

	operations := make([]*operationdomain.Operation, len(docs))
	for i := range docs {
		operations[i] = toOperation(&docs[i])
	}

	return operationdomain.ListResult{Operations: operations, Total: int(total)}, nil
}

func (r *MongoOperationRepo) UpdateAutomation(ctx context.Context, id string, ref operationdomain.AutomationRef) error {
	result, err := r.col.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{
			"automation": automationDoc{
				IntegrationID: ref.IntegrationID,
				JobTemplateID: ref.JobTemplateID,
				JobName:       ref.JobName,
				JobID:         ref.JobID,
				Status:        string(ref.Status),
				StartedAt:     ref.StartedAt,
				FinishedAt:    ref.FinishedAt,
				ObservedAt:    ref.ObservedAt,
			},
			"terminal":  ref.Status.Terminal(),
			"updatedAt": time.Now().UTC(),
		}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return operationdomain.ErrOperationNotFound
	}
	return nil
}

func (r *MongoOperationRepo) FindActiveByServerIDs(ctx context.Context, serverIDs []string) ([]*operationdomain.Operation, error) {
	if len(serverIDs) == 0 {
		return nil, nil
	}

	cursor, err := r.col.Find(ctx, bson.M{
		"terminal":        false,
		"targetServerIds": bson.M{"$in": serverIDs},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []operationDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	operations := make([]*operationdomain.Operation, len(docs))
	for i := range docs {
		operations[i] = toOperation(&docs[i])
	}
	return operations, nil
}

func toDoc(operation *operationdomain.Operation) *operationDoc {
	return &operationDoc{
		ID:              operation.ID,
		Kind:            string(operation.Kind),
		Intent:          operation.Intent,
		SiteID:          operation.SiteID,
		ClusterID:       operation.ClusterID,
		TargetServerIDs: operation.TargetServerIDs,
		Automation: automationDoc{
			IntegrationID: operation.Automation.IntegrationID,
			JobTemplateID: operation.Automation.JobTemplateID,
			JobName:       operation.Automation.JobName,
			JobID:         operation.Automation.JobID,
			Status:        string(operation.Automation.Status),
			StartedAt:     operation.Automation.StartedAt,
			FinishedAt:    operation.Automation.FinishedAt,
			ObservedAt:    operation.Automation.ObservedAt,
		},
		RequestedBy: operation.RequestedBy,
		RequestedAt: operation.RequestedAt,
		UpdatedAt:   operation.UpdatedAt,
		Terminal:    operation.Automation.Status.Terminal(),
	}
}

func toOperation(doc *operationDoc) *operationdomain.Operation {
	return &operationdomain.Operation{
		ID:              doc.ID,
		Kind:            operationdomain.OperationKind(doc.Kind),
		Intent:          doc.Intent,
		SiteID:          doc.SiteID,
		ClusterID:       doc.ClusterID,
		TargetServerIDs: doc.TargetServerIDs,
		Automation: operationdomain.AutomationRef{
			IntegrationID: doc.Automation.IntegrationID,
			JobTemplateID: doc.Automation.JobTemplateID,
			JobName:       doc.Automation.JobName,
			JobID:         doc.Automation.JobID,
			Status:        operationdomain.Status(doc.Automation.Status),
			StartedAt:     doc.Automation.StartedAt,
			FinishedAt:    doc.Automation.FinishedAt,
			ObservedAt:    doc.Automation.ObservedAt,
		},
		RequestedBy: doc.RequestedBy,
		RequestedAt: doc.RequestedAt,
		UpdatedAt:   doc.UpdatedAt,
	}
}
