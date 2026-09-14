package infra

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// MongoAnsibleEventRepo is the durable, append-only store of per-task Ansible events streamed
// during a run. It holds only the compact, output-free TaskEvent projection (never task output),
// so it is the streaming counterpart to the on-disk stdout artifact and is safe to expose.
type MongoAnsibleEventRepo struct{ col *mongo.Collection }

// ansibleTaskEventDoc is the persisted shape of one streamed event. The _id is deterministic
// (runId#seq) so an idempotent upsert keyed on it collapses a re-tail after a worker restart onto
// the same document rather than duplicating the event.
type ansibleTaskEventDoc struct {
	ID        string     `bson:"_id"`
	RunID     string     `bson:"runId"`
	Seq       int        `bson:"seq"`
	Play      string     `bson:"play,omitempty"`
	Task      string     `bson:"task,omitempty"`
	Host      string     `bson:"host,omitempty"`
	Status    string     `bson:"status"`
	Changed   bool       `bson:"changed"`
	StartedAt *time.Time `bson:"startedAt,omitempty"`
	EndedAt   *time.Time `bson:"endedAt,omitempty"`
}

// NewMongoAnsibleEventRepo creates the streamed-event store and its ordering index. The (runId,
// seq) unique index enforces idempotent appends and backs the ordered ListEvents read.
func NewMongoAnsibleEventRepo(db *mongo.Database) (*MongoAnsibleEventRepo, error) {
	repo := &MongoAnsibleEventRepo{col: db.Collection("ansible_task_events")}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := repo.col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "runId", Value: 1}, {Key: "seq", Value: 1}},
		Options: options.Index().SetName("ansible_events_run_seq").SetUnique(true),
	})
	return repo, err
}

// AppendEvents idempotently upserts the given events by their deterministic (runId, seq) id, so
// re-streaming an overlapping window (after a worker restart or a retry) never duplicates a row.
// An empty slice is a no-op.
func (r *MongoAnsibleEventRepo) AppendEvents(ctx context.Context, events []operationdomain.StreamedTaskEvent) error {
	if len(events) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, 0, len(events))
	for _, event := range events {
		doc := ansibleTaskEventDoc{
			ID:        eventDocID(event.RunID, event.Seq),
			RunID:     event.RunID,
			Seq:       event.Seq,
			Play:      event.Play,
			Task:      event.Task,
			Host:      event.Host,
			Status:    event.Status,
			Changed:   event.Changed,
			StartedAt: event.StartedAt,
			EndedAt:   event.EndedAt,
		}
		models = append(models, mongo.NewReplaceOneModel().
			SetFilter(bson.M{"_id": doc.ID}).
			SetReplacement(doc).
			SetUpsert(true))
	}
	// Unordered so one duplicate-key race does not abort the rest of the batch.
	_, err := r.col.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false))
	if err != nil && !mongo.IsDuplicateKeyError(err) {
		return err
	}
	return nil
}

// ListEvents returns a run's streamed task events in ascending ordinal order. A run with no
// streamed events yet returns an empty slice, not an error.
func (r *MongoAnsibleEventRepo) ListEvents(ctx context.Context, runID string) ([]operationdomain.TaskEvent, error) {
	cursor, err := r.col.Find(ctx, bson.M{"runId": runID}, options.Find().SetSort(bson.D{{Key: "seq", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []ansibleTaskEventDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	events := make([]operationdomain.TaskEvent, 0, len(docs))
	for i := range docs {
		events = append(events, operationdomain.TaskEvent{
			Play:      docs[i].Play,
			Task:      docs[i].Task,
			Host:      docs[i].Host,
			Status:    docs[i].Status,
			Changed:   docs[i].Changed,
			StartedAt: docs[i].StartedAt,
			EndedAt:   docs[i].EndedAt,
		})
	}
	return events, nil
}

// eventDocID builds the deterministic per-event id used for idempotent upserts.
func eventDocID(runID string, seq int) string {
	return fmt.Sprintf("%s#%d", runID, seq)
}
