package convergence

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/maple52046/swallow/internal/core/convergence"
)

// eventRetention is how long convergence events are kept; a TTL index deletes older ones, so the
// timeline of a resource that drifts often stays bounded.
const eventRetention = 90 * 24 * time.Hour

// MongoLeaseStore implements convergence.LeaseStore over `controller_leases`, one document per
// lease keyed by its name.
type MongoLeaseStore struct {
	col *mongo.Collection
}

var _ convergence.LeaseStore = (*MongoLeaseStore)(nil)

// NewMongoLeaseStore returns the lease store. The lease name is the document id, which is the only
// uniqueness it needs.
func NewMongoLeaseStore(db *mongo.Database) *MongoLeaseStore {
	return &MongoLeaseStore{col: db.Collection("controller_leases")}
}

type leaseDoc struct {
	ID        string    `bson:"_id"`
	Holder    string    `bson:"holder"`
	ExpiresAt time.Time `bson:"expiresAt"`
	RenewedAt time.Time `bson:"renewedAt"`
}

// TryAcquire implements convergence.LeaseStore with one conditional upsert: it matches the lease
// only when holder already has it or it has expired. When another holder has an unexpired lease the
// filter matches nothing, the upsert tries to insert the same _id, and the duplicate-key error is
// the "not acquired" answer.
func (s *MongoLeaseStore) TryAcquire(ctx context.Context, name, holder string, ttl time.Duration) (bool, error) {
	now := time.Now().UTC()
	filter := bson.M{"_id": name, "$or": bson.A{
		bson.M{"holder": holder},
		bson.M{"expiresAt": bson.M{"$lte": now}},
	}}
	update := bson.M{"$set": bson.M{"holder": holder, "expiresAt": now.Add(ttl), "renewedAt": now}}
	_, err := s.col.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	if mongo.IsDuplicateKeyError(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("acquire lease %q: %w", name, err)
	}
	return true, nil
}

// Release implements convergence.LeaseStore.
func (s *MongoLeaseStore) Release(ctx context.Context, name, holder string) error {
	if _, err := s.col.DeleteOne(ctx, bson.M{"_id": name, "holder": holder}); err != nil {
		return fmt.Errorf("release lease %q: %w", name, err)
	}
	return nil
}

// MongoEventRecorder implements convergence.EventRecorder over `convergence_events`.
type MongoEventRecorder struct {
	col *mongo.Collection
}

var _ convergence.EventRecorder = (*MongoEventRecorder)(nil)

type eventDoc struct {
	ID           string    `bson:"_id"`
	ResourceKind string    `bson:"resourceKind"`
	ResourceID   string    `bson:"resourceId"`
	Type         string    `bson:"type"`
	Fields       []string  `bson:"fields,omitempty"`
	Message      string    `bson:"message,omitempty"`
	Generation   int64     `bson:"generation"`
	At           time.Time `bson:"at"`
}

// NewMongoEventRecorder creates the timeline index and the retention TTL index and returns the
// recorder.
func NewMongoEventRecorder(ctx context.Context, db *mongo.Database) (*MongoEventRecorder, error) {
	col := db.Collection("convergence_events")
	_, err := col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "resourceKind", Value: 1}, {Key: "resourceId", Value: 1}, {Key: "at", Value: -1}}, Options: options.Index().SetName("convergence_events_resource")},
		{Keys: bson.D{{Key: "at", Value: 1}}, Options: options.Index().SetName("convergence_events_ttl").SetExpireAfterSeconds(int32(eventRetention / time.Second))},
	})
	if err != nil {
		return nil, fmt.Errorf("convergence_events indexes: %w", err)
	}
	return &MongoEventRecorder{col: col}, nil
}

// Record implements convergence.EventRecorder; an event without an id or time gets one.
func (r *MongoEventRecorder) Record(ctx context.Context, event convergence.Event) error {
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	_, err := r.col.InsertOne(ctx, eventDoc{
		ID: event.ID, ResourceKind: event.ResourceKind, ResourceID: event.ResourceID, Type: string(event.Type),
		Fields: event.Fields, Message: event.Message, Generation: event.Generation, At: event.At.UTC(),
	})
	return err
}

// List implements convergence.EventRecorder.
func (r *MongoEventRecorder) List(ctx context.Context, resourceKind, resourceID string, limit int) ([]convergence.Event, error) {
	cursor, err := r.col.Find(ctx, bson.M{"resourceKind": resourceKind, "resourceId": resourceID},
		options.Find().SetSort(bson.D{{Key: "at", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []eventDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	events := make([]convergence.Event, len(docs))
	for i, doc := range docs {
		events[i] = convergence.Event{
			ID: doc.ID, ResourceKind: doc.ResourceKind, ResourceID: doc.ResourceID, Type: convergence.EventType(doc.Type),
			Fields: doc.Fields, Message: doc.Message, Generation: doc.Generation, At: doc.At,
		}
	}
	return events, nil
}
