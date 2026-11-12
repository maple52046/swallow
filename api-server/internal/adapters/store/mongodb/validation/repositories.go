package validation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
)

// MongoSuiteRepo is the durable store of user-defined Validation Suites (collection
// validation_suites).
type MongoSuiteRepo struct {
	col *mongo.Collection
}

// NewMongoSuiteRepo returns the suite store.
func NewMongoSuiteRepo(db *mongo.Database) *MongoSuiteRepo {
	return &MongoSuiteRepo{col: db.Collection("validation_suites")}
}

// Create implements validationdomain.SuiteRepository.
func (r *MongoSuiteRepo) Create(ctx context.Context, suite validationdomain.Suite) error {
	_, err := r.col.InsertOne(ctx, toSuiteDoc(suite))
	return err
}

// Replace implements validationdomain.SuiteRepository.
func (r *MongoSuiteRepo) Replace(ctx context.Context, suite validationdomain.Suite) error {
	result, err := r.col.ReplaceOne(ctx, bson.M{"_id": suite.ID}, toSuiteDoc(suite))
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return validationdomain.ErrSuiteNotFound
	}
	return nil
}

// Get implements validationdomain.SuiteRepository.
func (r *MongoSuiteRepo) Get(ctx context.Context, id string) (validationdomain.Suite, error) {
	var doc suiteDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return validationdomain.Suite{}, fmt.Errorf("%w: %s", validationdomain.ErrSuiteNotFound, id)
	}
	if err != nil {
		return validationdomain.Suite{}, err
	}
	return toSuite(doc), nil
}

// List implements validationdomain.SuiteRepository.
func (r *MongoSuiteRepo) List(ctx context.Context, includeRetired bool) ([]validationdomain.Suite, error) {
	filter := bson.M{}
	if !includeRetired {
		filter["retired"] = false
	}
	cursor, err := r.col.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var docs []suiteDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	suites := make([]validationdomain.Suite, 0, len(docs))
	for _, doc := range docs {
		suites = append(suites, toSuite(doc))
	}
	return suites, nil
}

// MongoRunRepo is the durable store of Validation Runs (collection validation_runs). A TTL index on
// expiresAt deletes a run after its retention, so the store, not a sweep, ends its life.
type MongoRunRepo struct {
	col *mongo.Collection
}

// NewMongoRunRepo returns the run store and ensures its indexes: the list order, each filter, and
// the retention TTL.
func NewMongoRunRepo(db *mongo.Database) (*MongoRunRepo, error) {
	col := db.Collection("validation_runs", options.Collection().SetRegistry(registry))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "requestedAt", Value: -1}}, Options: options.Index().SetName("validation_run_requested")},
		{Keys: bson.D{{Key: "siteId", Value: 1}, {Key: "requestedAt", Value: -1}}, Options: options.Index().SetName("validation_run_site_requested")},
		{Keys: bson.D{{Key: "target.serverIds", Value: 1}}, Options: options.Index().SetName("validation_run_servers")},
		{Keys: bson.D{{Key: "target.memberIds", Value: 1}}, Options: options.Index().SetName("validation_run_members")},
		{Keys: bson.D{{Key: "target.platformId", Value: 1}}, Options: options.Index().SetName("validation_run_platform")},
		{Keys: bson.D{{Key: "trigger.scheduleId", Value: 1}}, Options: options.Index().SetName("validation_run_schedule")},
		{Keys: bson.D{{Key: "status", Value: 1}}, Options: options.Index().SetName("validation_run_status")},
		{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetName("validation_run_retention").SetExpireAfterSeconds(0)},
	})
	if err != nil {
		return nil, fmt.Errorf("validation run indexes: %w", err)
	}
	return &MongoRunRepo{col: col}, nil
}

// Create implements validationdomain.RunRepository.
func (r *MongoRunRepo) Create(ctx context.Context, run validationdomain.Run) error {
	_, err := r.col.InsertOne(ctx, toRunDoc(run))
	return err
}

// Save implements validationdomain.RunRepository.
func (r *MongoRunRepo) Save(ctx context.Context, run validationdomain.Run) error {
	result, err := r.col.ReplaceOne(ctx, bson.M{"_id": run.ID}, toRunDoc(run))
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return validationdomain.ErrRunNotFound
	}
	return nil
}

// Get implements validationdomain.RunRepository.
func (r *MongoRunRepo) Get(ctx context.Context, id string) (validationdomain.Run, error) {
	var doc runDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return validationdomain.Run{}, fmt.Errorf("%w: %s", validationdomain.ErrRunNotFound, id)
	}
	if err != nil {
		return validationdomain.Run{}, err
	}
	return toRun(doc), nil
}

// List implements validationdomain.RunRepository. A non-positive limit returns every match.
func (r *MongoRunRepo) List(ctx context.Context, filter validationdomain.RunFilter) ([]validationdomain.Run, int, error) {
	query := bson.M{}
	if filter.SiteID != "" {
		query["siteId"] = filter.SiteID
	}
	if filter.SuiteID != "" {
		query["suite.id"] = filter.SuiteID
	}
	if filter.ServerID != "" {
		query["$or"] = bson.A{bson.M{"target.serverIds": filter.ServerID}, bson.M{"target.memberIds": filter.ServerID}}
	}
	if filter.PlatformID != "" {
		query["target.platformId"] = filter.PlatformID
	}
	if filter.ScheduleID != "" {
		query["trigger.scheduleId"] = filter.ScheduleID
	}
	if filter.Trigger != "" {
		query["trigger.kind"] = string(filter.Trigger)
	}
	if filter.Verdict != "" {
		query["verdict"] = string(filter.Verdict)
	}
	switch {
	case filter.Status != "":
		query["status"] = string(filter.Status)
	case filter.ActiveOnly:
		query["status"] = bson.M{"$in": bson.A{string(validationdomain.StatusPending), string(validationdomain.StatusRunning)}}
	}
	total, err := r.col.CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, err
	}
	find := options.Find().SetSort(bson.D{{Key: "requestedAt", Value: -1}, {Key: "_id", Value: 1}}).SetSkip(int64(filter.Offset))
	if filter.Limit > 0 {
		find.SetLimit(int64(filter.Limit))
	}
	cursor, err := r.col.Find(ctx, query, find)
	if err != nil {
		return nil, 0, err
	}
	var docs []runDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, 0, err
	}
	runs := make([]validationdomain.Run, 0, len(docs))
	for _, doc := range docs {
		runs = append(runs, toRun(doc))
	}
	return runs, int(total), nil
}

// MongoScheduleRepo is the durable store of Validation Schedules (collection validation_schedules).
// The API writes spec and generation; the controller writes status; neither write touches the
// other's fields.
type MongoScheduleRepo struct {
	col *mongo.Collection
}

// NewMongoScheduleRepo returns the schedule store and ensures the Site/name list index.
func NewMongoScheduleRepo(db *mongo.Database) (*MongoScheduleRepo, error) {
	col := db.Collection("validation_schedules", options.Collection().SetRegistry(registry))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "siteId", Value: 1}, {Key: "spec.name", Value: 1}},
		Options: options.Index().SetName("validation_schedule_site_name"),
	})
	if err != nil {
		return nil, fmt.Errorf("validation schedule indexes: %w", err)
	}
	return &MongoScheduleRepo{col: col}, nil
}

// Create implements validationdomain.ScheduleRepository.
func (r *MongoScheduleRepo) Create(ctx context.Context, schedule validationdomain.Schedule) error {
	_, err := r.col.InsertOne(ctx, scheduleDoc{
		ID: schedule.ID, SiteID: schedule.SiteID, Generation: schedule.Generation, Spec: toScheduleSpecDoc(schedule.Spec),
		Status: toScheduleStatusDoc(schedule.Status), CreatedAt: schedule.CreatedAt, UpdatedAt: schedule.UpdatedAt,
	})
	return err
}

// WriteSpec implements validationdomain.ScheduleRepository: one conditional update writes the spec
// and increments the generation, so a stale writer is told rather than overwriting.
func (r *MongoScheduleRepo) WriteSpec(ctx context.Context, id string, expectedGeneration int64, spec validationdomain.ScheduleSpec, siteID string, now time.Time) (int64, error) {
	filter := bson.M{"_id": id}
	if expectedGeneration > 0 {
		filter["generation"] = expectedGeneration
	}
	var doc scheduleDoc
	err := r.col.FindOneAndUpdate(ctx, filter,
		bson.M{"$set": bson.M{"spec": toScheduleSpecDoc(spec), "siteId": siteID, "updatedAt": now}, "$inc": bson.M{"generation": 1}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		if _, getErr := r.Get(ctx, id); getErr != nil {
			return 0, getErr
		}
		return 0, validationdomain.ErrGenerationConflict
	}
	if err != nil {
		return 0, err
	}
	return doc.Generation, nil
}

// WriteStatus implements validationdomain.ScheduleRepository; a schedule deleted meanwhile is
// ErrScheduleNotFound.
func (r *MongoScheduleRepo) WriteStatus(ctx context.Context, id string, status validationdomain.ScheduleStatus) error {
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"status": toScheduleStatusDoc(status)}})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return validationdomain.ErrScheduleNotFound
	}
	return nil
}

// Get implements validationdomain.ScheduleRepository.
func (r *MongoScheduleRepo) Get(ctx context.Context, id string) (validationdomain.Schedule, error) {
	var doc scheduleDoc
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return validationdomain.Schedule{}, fmt.Errorf("%w: %s", validationdomain.ErrScheduleNotFound, id)
	}
	if err != nil {
		return validationdomain.Schedule{}, err
	}
	return toSchedule(doc), nil
}

// List implements validationdomain.ScheduleRepository.
func (r *MongoScheduleRepo) List(ctx context.Context, filter validationdomain.ScheduleFilter) ([]validationdomain.Schedule, error) {
	query := bson.M{}
	if filter.SiteID != "" {
		query["siteId"] = filter.SiteID
	}
	cursor, err := r.col.Find(ctx, query, options.Find().SetSort(bson.D{{Key: "spec.name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var docs []scheduleDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	schedules := make([]validationdomain.Schedule, 0, len(docs))
	for _, doc := range docs {
		schedules = append(schedules, toSchedule(doc))
	}
	return schedules, nil
}

// Delete implements validationdomain.ScheduleRepository.
func (r *MongoScheduleRepo) Delete(ctx context.Context, id string) error {
	result, err := r.col.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("%w: %s", validationdomain.ErrScheduleNotFound, id)
	}
	return nil
}

// UsesSuite implements validationdomain.ScheduleRepository.
func (r *MongoScheduleRepo) UsesSuite(ctx context.Context, suiteID string) (bool, error) {
	count, err := r.col.CountDocuments(ctx, bson.M{"spec.suiteId": suiteID}, options.Count().SetLimit(1))
	return count > 0, err
}
