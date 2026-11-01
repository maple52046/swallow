package convergence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/maple52046/swallow/internal/core/convergence"
)

// ChangePlanCollection holds the Change Plans (decision 076).
const ChangePlanCollection = "change_plans"

// MongoChangePlanRepo implements convergence.ChangePlanRepository over `change_plans`. The open
// field is true exactly while a plan is unfinished, and a unique partial index on the resource over
// open plans keeps one unfinished plan per resource.
type MongoChangePlanRepo struct {
	col *mongo.Collection
}

var _ convergence.ChangePlanRepository = (*MongoChangePlanRepo)(nil)

type plannedActionDoc struct {
	TargetKind  string `bson:"targetKind"`
	TargetID    string `bson:"targetId"`
	Action      string `bson:"action"`
	From        string `bson:"from,omitempty"`
	To          string `bson:"to,omitempty"`
	Destructive bool   `bson:"destructive"`
	Reason      string `bson:"reason,omitempty"`
}

type changePlanDoc struct {
	ID           string             `bson:"_id"`
	ResourceKind string             `bson:"resourceKind"`
	ResourceID   string             `bson:"resourceId"`
	Generation   int64              `bson:"generation"`
	ObservedAt   time.Time          `bson:"observedAt"`
	Actions      []plannedActionDoc `bson:"actions"`
	Effects      []string           `bson:"effects"`
	Blockers     []string           `bson:"blockers"`
	State        string             `bson:"state"`
	Open         bool               `bson:"open"`
	StateReason  string             `bson:"stateReason,omitempty"`
	DecidedBy    string             `bson:"decidedBy,omitempty"`
	DecidedAt    *time.Time         `bson:"decidedAt,omitempty"`
	WorkflowID   string             `bson:"workflowId,omitempty"`
	ExpiresAt    time.Time          `bson:"expiresAt"`
	CreatedAt    time.Time          `bson:"createdAt"`
	UpdatedAt    time.Time          `bson:"updatedAt"`
}

// NewMongoChangePlanRepo creates the indexes and returns the repository.
func NewMongoChangePlanRepo(ctx context.Context, db *mongo.Database) (*MongoChangePlanRepo, error) {
	col := db.Collection(ChangePlanCollection)
	_, err := col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "resourceKind", Value: 1}, {Key: "resourceId", Value: 1}},
			Options: options.Index().SetName("change_plans_one_open_per_resource").SetUnique(true).
				SetPartialFilterExpression(bson.M{"open": true}),
		},
		{Keys: bson.D{{Key: "resourceKind", Value: 1}, {Key: "resourceId", Value: 1}, {Key: "createdAt", Value: -1}}, Options: options.Index().SetName("change_plans_resource")},
		{Keys: bson.D{{Key: "open", Value: 1}, {Key: "expiresAt", Value: 1}}, Options: options.Index().SetName("change_plans_expiry")},
	})
	if err != nil {
		return nil, fmt.Errorf("change_plans indexes: %w", err)
	}
	return &MongoChangePlanRepo{col: col}, nil
}

// Create implements convergence.ChangePlanRepository.
func (r *MongoChangePlanRepo) Create(ctx context.Context, plan *convergence.ChangePlan) error {
	_, err := r.col.InsertOne(ctx, toChangePlanDoc(plan))
	if mongo.IsDuplicateKeyError(err) {
		return convergence.ErrChangePlanConflict
	}
	return err
}

// FindByID implements convergence.ChangePlanRepository.
func (r *MongoChangePlanRepo) FindByID(ctx context.Context, id string) (*convergence.ChangePlan, error) {
	return r.findOne(ctx, bson.M{"_id": id})
}

// FindUnfinished implements convergence.ChangePlanRepository.
func (r *MongoChangePlanRepo) FindUnfinished(ctx context.Context, resource convergence.ResourceRef) (*convergence.ChangePlan, error) {
	return r.findOne(ctx, bson.M{"resourceKind": resource.Kind, "resourceId": resource.ID, "open": true})
}

func (r *MongoChangePlanRepo) findOne(ctx context.Context, filter bson.M) (*convergence.ChangePlan, error) {
	var doc changePlanDoc
	err := r.col.FindOne(ctx, filter).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, convergence.ErrChangePlanNotFound
	}
	if err != nil {
		return nil, err
	}
	return doc.toDomain(), nil
}

// List implements convergence.ChangePlanRepository.
func (r *MongoChangePlanRepo) List(ctx context.Context, filter convergence.ChangePlanFilter) ([]*convergence.ChangePlan, error) {
	query := bson.M{}
	if filter.Resource.Kind != "" {
		query["resourceKind"] = filter.Resource.Kind
	}
	if filter.Resource.ID != "" {
		query["resourceId"] = filter.Resource.ID
	}
	if len(filter.States) > 0 {
		states := make(bson.A, len(filter.States))
		for i, state := range filter.States {
			states[i] = string(state)
		}
		query["state"] = bson.M{"$in": states}
	}
	return r.find(ctx, query, options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}))
}

// ListExpired implements convergence.ChangePlanRepository.
func (r *MongoChangePlanRepo) ListExpired(ctx context.Context, now time.Time) ([]*convergence.ChangePlan, error) {
	return r.find(ctx, bson.M{"open": true, "expiresAt": bson.M{"$lte": now}}, options.Find())
}

func (r *MongoChangePlanRepo) find(ctx context.Context, query bson.M, opts *options.FindOptions) ([]*convergence.ChangePlan, error) {
	cursor, err := r.col.Find(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []changePlanDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	plans := make([]*convergence.ChangePlan, len(docs))
	for i := range docs {
		plans[i] = docs[i].toDomain()
	}
	return plans, nil
}

// Transition implements convergence.ChangePlanRepository with one conditional update on the plan's
// current state, so two deciders cannot both move it.
func (r *MongoChangePlanRepo) Transition(
	ctx context.Context,
	id string,
	from []convergence.ChangePlanState,
	to convergence.ChangePlanState,
	transition convergence.PlanTransition,
) error {
	states := make(bson.A, len(from))
	for i, state := range from {
		states[i] = string(state)
	}
	set := bson.M{"state": string(to), "open": to.Unfinished(), "updatedAt": transition.At.UTC()}
	if transition.Reason != "" {
		set["stateReason"] = transition.Reason
	}
	if transition.DecidedBy != "" {
		set["decidedBy"] = transition.DecidedBy
		set["decidedAt"] = transition.DecidedAt
	}
	if transition.WorkflowID != "" {
		set["workflowId"] = transition.WorkflowID
	}
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": id, "state": bson.M{"$in": states}}, bson.M{"$set": set})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		if _, err := r.FindByID(ctx, id); err != nil {
			return err
		}
		return convergence.ErrChangePlanState
	}
	return nil
}

func toChangePlanDoc(plan *convergence.ChangePlan) changePlanDoc {
	actions := make([]plannedActionDoc, len(plan.Actions))
	for i, action := range plan.Actions {
		actions[i] = plannedActionDoc{
			TargetKind: action.Target.Kind, TargetID: action.Target.ID, Action: action.Action,
			From: action.From, To: action.To, Destructive: action.Destructive, Reason: action.Reason,
		}
	}
	return changePlanDoc{
		ID: plan.ID, ResourceKind: plan.Resource.Kind, ResourceID: plan.Resource.ID, Generation: plan.Generation,
		ObservedAt: plan.ObservedAt.UTC(), Actions: actions, Effects: nonNil(plan.Effects), Blockers: nonNil(plan.Blockers),
		State: string(plan.State), Open: plan.State.Unfinished(), StateReason: plan.StateReason,
		DecidedBy: plan.DecidedBy, DecidedAt: plan.DecidedAt, WorkflowID: plan.WorkflowID,
		ExpiresAt: plan.ExpiresAt.UTC(), CreatedAt: plan.CreatedAt.UTC(), UpdatedAt: plan.UpdatedAt.UTC(),
	}
}

func (d changePlanDoc) toDomain() *convergence.ChangePlan {
	actions := make([]convergence.PlannedAction, len(d.Actions))
	for i, action := range d.Actions {
		actions[i] = convergence.PlannedAction{
			Target: convergence.ResourceRef{Kind: action.TargetKind, ID: action.TargetID}, Action: action.Action,
			From: action.From, To: action.To, Destructive: action.Destructive, Reason: action.Reason,
		}
	}
	return &convergence.ChangePlan{
		ID: d.ID, Resource: convergence.ResourceRef{Kind: d.ResourceKind, ID: d.ResourceID}, Generation: d.Generation,
		ObservedAt: d.ObservedAt, Actions: actions, Effects: nonNil(d.Effects), Blockers: nonNil(d.Blockers),
		State: convergence.ChangePlanState(d.State), StateReason: d.StateReason, DecidedBy: d.DecidedBy,
		DecidedAt: d.DecidedAt, WorkflowID: d.WorkflowID, ExpiresAt: d.ExpiresAt, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
