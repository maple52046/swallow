package infra

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

type staticNetworkLinkSnapshotDoc struct {
	InterfaceID string `bson:"interfaceId"`
	LinkID      string `bson:"linkId"`
	SubnetID    string `bson:"subnetId"`
	IPAddress   string `bson:"ipAddress"`
}

type provisioningTaskDoc struct {
	ID                string                         `bson:"_id"`
	Kind              string                         `bson:"kind"`
	ServerID          string                         `bson:"serverId"`
	IntegrationID     string                         `bson:"integrationId"`
	ProviderMachineID string                         `bson:"providerMachineId"`
	Status            string                         `bson:"status"`
	Phase             string                         `bson:"phase"`
	Attempt           int                            `bson:"attempt"`
	Snapshot          []staticNetworkLinkSnapshotDoc `bson:"snapshot"`
	Error             string                         `bson:"error,omitempty"`
	RequestID         string                         `bson:"requestId,omitempty"`
	NextRunAt         time.Time                      `bson:"nextRunAt"`
	LeaseOwner        string                         `bson:"leaseOwner,omitempty"`
	LeaseUntil        time.Time                      `bson:"leaseUntil,omitempty"`
	CreatedAt         time.Time                      `bson:"createdAt"`
	UpdatedAt         time.Time                      `bson:"updatedAt"`
}

// MongoProvisioningTaskRepo persists release cleanup history and atomically leases
// due work. Snapshots contain provider IDs and IPs but never credentials.
type MongoProvisioningTaskRepo struct {
	col *mongo.Collection
}

// NewMongoProvisioningTaskRepo creates history and dispatcher indexes.
func NewMongoProvisioningTaskRepo(db *mongo.Database) (*MongoProvisioningTaskRepo, error) {
	col := db.Collection("provisioning_tasks")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "serverId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("server_created_at"),
		},
		{
			Keys: bson.D{
				{Key: "status", Value: 1},
				{Key: "nextRunAt", Value: 1},
				{Key: "leaseUntil", Value: 1},
			},
			Options: options.Index().SetName("task_dispatch"),
		},
	})
	if err != nil {
		return nil, err
	}
	return &MongoProvisioningTaskRepo{col: col}, nil
}

// Create persists a task before the provider action it coordinates.
func (r *MongoProvisioningTaskRepo) Create(
	ctx context.Context,
	task *provisioningdomain.ProvisioningTask,
) error {
	_, err := r.col.InsertOne(ctx, provisioningTaskToDoc(task))
	return err
}

// FindByID reads one durable task.
func (r *MongoProvisioningTaskRepo) FindByID(
	ctx context.Context,
	id string,
) (*provisioningdomain.ProvisioningTask, error) {
	var doc provisioningTaskDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, provisioningdomain.ErrProvisioningTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	return provisioningTaskFromDoc(&doc), nil
}

// ListByServer returns newest tasks first for the Server Activity timeline.
func (r *MongoProvisioningTaskRepo) ListByServer(
	ctx context.Context,
	serverID string,
) ([]*provisioningdomain.ProvisioningTask, error) {
	cursor, err := r.col.Find(
		ctx,
		bson.M{"serverId": serverID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []provisioningTaskDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	tasks := make([]*provisioningdomain.ProvisioningTask, len(docs))
	for index := range docs {
		tasks[index] = provisioningTaskFromDoc(&docs[index])
	}
	return tasks, nil
}

// ClaimNext atomically leases one due task. Expired running leases are eligible so
// process termination cannot strand cleanup forever.
func (r *MongoProvisioningTaskRepo) ClaimNext(
	ctx context.Context,
	owner string,
	now time.Time,
	leaseDuration time.Duration,
) (*provisioningdomain.ProvisioningTask, error) {
	filter := bson.M{
		"nextRunAt": bson.M{"$lte": now},
		"$or": []bson.M{
			{"status": string(provisioningdomain.ProvisioningTaskPending)},
			{
				"status":     string(provisioningdomain.ProvisioningTaskRunning),
				"leaseUntil": bson.M{"$lte": now},
			},
		},
	}
	update := bson.M{
		"$set": bson.M{
			"status":     string(provisioningdomain.ProvisioningTaskRunning),
			"leaseOwner": owner,
			"leaseUntil": now.Add(leaseDuration),
			"updatedAt":  now,
		},
		"$inc": bson.M{"attempt": 1},
	}
	opts := options.FindOneAndUpdate().
		SetSort(bson.D{{Key: "nextRunAt", Value: 1}, {Key: "createdAt", Value: 1}}).
		SetReturnDocument(options.After)
	var doc provisioningTaskDoc
	err := r.col.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, provisioningdomain.ErrNoProvisioningTask
	}
	if err != nil {
		return nil, err
	}
	return provisioningTaskFromDoc(&doc), nil
}

// Save replaces current task state and clears any lease omitted by the application.
func (r *MongoProvisioningTaskRepo) Save(
	ctx context.Context,
	task *provisioningdomain.ProvisioningTask,
) error {
	result, err := r.col.ReplaceOne(ctx, bson.M{"_id": task.ID}, provisioningTaskToDoc(task))
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return provisioningdomain.ErrProvisioningTaskNotFound
	}
	return nil
}

// Retry requeues failed cleanup without repeating Release.
func (r *MongoProvisioningTaskRepo) Retry(
	ctx context.Context,
	id string,
	now time.Time,
) error {
	result, err := r.col.UpdateOne(
		ctx,
		bson.M{
			"_id":    id,
			"status": string(provisioningdomain.ProvisioningTaskFailed),
		},
		bson.M{
			"$set": bson.M{
				"status":    string(provisioningdomain.ProvisioningTaskPending),
				"error":     "",
				"nextRunAt": now,
				"updatedAt": now,
			},
			"$unset": bson.M{"leaseOwner": "", "leaseUntil": ""},
		},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		if _, findErr := r.FindByID(ctx, id); findErr != nil {
			return findErr
		}
		return provisioningdomain.ErrProvisioningTaskConflict
	}
	return nil
}

func provisioningTaskToDoc(task *provisioningdomain.ProvisioningTask) provisioningTaskDoc {
	snapshot := make([]staticNetworkLinkSnapshotDoc, len(task.Snapshot))
	for index, link := range task.Snapshot {
		snapshot[index] = staticNetworkLinkSnapshotDoc{
			InterfaceID: link.InterfaceID,
			LinkID:      link.LinkID,
			SubnetID:    link.SubnetID,
			IPAddress:   link.IPAddress,
		}
	}
	return provisioningTaskDoc{
		ID:                task.ID,
		Kind:              string(task.Kind),
		ServerID:          task.ServerID,
		IntegrationID:     task.IntegrationID,
		ProviderMachineID: task.ProviderMachineID,
		Status:            string(task.Status),
		Phase:             string(task.Phase),
		Attempt:           task.Attempt,
		Snapshot:          snapshot,
		Error:             task.Error,
		RequestID:         task.RequestID,
		NextRunAt:         task.NextRunAt,
		LeaseOwner:        task.LeaseOwner,
		LeaseUntil:        task.LeaseUntil,
		CreatedAt:         task.CreatedAt,
		UpdatedAt:         task.UpdatedAt,
	}
}

func provisioningTaskFromDoc(doc *provisioningTaskDoc) *provisioningdomain.ProvisioningTask {
	snapshot := make([]provisioningdomain.StaticNetworkLinkSnapshot, len(doc.Snapshot))
	for index, link := range doc.Snapshot {
		snapshot[index] = provisioningdomain.StaticNetworkLinkSnapshot{
			InterfaceID: link.InterfaceID,
			LinkID:      link.LinkID,
			SubnetID:    link.SubnetID,
			IPAddress:   link.IPAddress,
		}
	}
	return &provisioningdomain.ProvisioningTask{
		ID:                doc.ID,
		Kind:              provisioningdomain.ProvisioningTaskKind(doc.Kind),
		ServerID:          doc.ServerID,
		IntegrationID:     doc.IntegrationID,
		ProviderMachineID: doc.ProviderMachineID,
		Status:            provisioningdomain.ProvisioningTaskStatus(doc.Status),
		Phase:             provisioningdomain.ProvisioningTaskPhase(doc.Phase),
		Attempt:           doc.Attempt,
		Snapshot:          snapshot,
		Error:             doc.Error,
		RequestID:         doc.RequestID,
		NextRunAt:         doc.NextRunAt,
		LeaseOwner:        doc.LeaseOwner,
		LeaseUntil:        doc.LeaseUntil,
		CreatedAt:         doc.CreatedAt,
		UpdatedAt:         doc.UpdatedAt,
	}
}
