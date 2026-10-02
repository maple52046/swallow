// Package infra provides the MongoDB adapters of the software feature: Software Assignments, keyed
// by (serverId, kind) and the single source of truth for what software swallow installed where, and
// Registry Credentials, whose passwords are sealed. Both are swallow-owned data (not provider
// mirrors), so there is no staleness. The Docker Engine client lives in the dockerengine subpackage.
package infra

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// assignmentDoc is the BSON shape of a Software Assignment. The compound _id keeps one record per
// (serverId, kind) and makes Upsert naturally idempotent.
type assignmentDoc struct {
	ServerID       string         `bson:"serverId"`
	SiteID         string         `bson:"siteId"`
	Kind           string         `bson:"kind"`
	Roles          []string       `bson:"roles"`
	Spec           map[string]any `bson:"spec,omitempty"`
	State          string         `bson:"state"`
	LastWorkflowID string         `bson:"lastWorkflowId,omitempty"`
	LastAppliedAt  *time.Time     `bson:"lastAppliedAt,omitempty"`
	CreatedAt      time.Time      `bson:"createdAt"`
	UpdatedAt      time.Time      `bson:"updatedAt"`
}

// MongoAssignmentRepo persists Software Assignments in the software_assignments collection.
type MongoAssignmentRepo struct {
	col *mongo.Collection
}

// NewMongoAssignmentRepo creates the repository and its unique (serverId, kind) index so two
// concurrent installs cannot create duplicate records for one software on one Server.
func NewMongoAssignmentRepo(db *mongo.Database) (*MongoAssignmentRepo, error) {
	col := db.Collection("software_assignments")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "serverId", Value: 1}, {Key: "kind", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("server_kind"),
	})
	if err != nil {
		return nil, err
	}
	return &MongoAssignmentRepo{col: col}, nil
}

func assignmentKey(serverID string, kind softwaredomain.Kind) bson.M {
	return bson.M{"serverId": serverID, "kind": string(kind)}
}

// Upsert creates or replaces the assignment for (serverId, kind), preserving createdAt on replace.
func (r *MongoAssignmentRepo) Upsert(ctx context.Context, assignment *softwaredomain.Assignment) error {
	now := time.Now().UTC()
	if assignment.CreatedAt.IsZero() {
		assignment.CreatedAt = now
	}
	assignment.UpdatedAt = now
	doc := toDoc(assignment)
	_, err := r.col.UpdateOne(ctx, assignmentKey(assignment.ServerID, assignment.Kind), bson.M{
		"$set": bson.M{
			"siteId": doc.SiteID, "roles": doc.Roles, "spec": doc.Spec,
			"state": doc.State, "lastWorkflowId": doc.LastWorkflowID,
			"lastAppliedAt": doc.LastAppliedAt, "updatedAt": doc.UpdatedAt,
		},
		"$setOnInsert": bson.M{
			"serverId": doc.ServerID, "kind": doc.Kind, "createdAt": doc.CreatedAt,
		},
	}, options.Update().SetUpsert(true))
	return err
}

// FindByServerAndKind returns the assignment or ErrAssignmentNotFound.
func (r *MongoAssignmentRepo) FindByServerAndKind(ctx context.Context, serverID string, kind softwaredomain.Kind) (*softwaredomain.Assignment, error) {
	var doc assignmentDoc
	err := r.col.FindOne(ctx, assignmentKey(serverID, kind)).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, softwaredomain.ErrAssignmentNotFound
	}
	if err != nil {
		return nil, err
	}
	return toAssignment(&doc), nil
}

// List returns assignments matching the filter, ordered by serverId then kind. Absent records are
// excluded unless the filter opts in.
func (r *MongoAssignmentRepo) List(ctx context.Context, filter softwaredomain.AssignmentFilter) ([]*softwaredomain.Assignment, error) {
	query := bson.M{}
	if filter.ServerID != "" {
		query["serverId"] = filter.ServerID
	}
	if filter.Kind != "" {
		query["kind"] = string(filter.Kind)
	}
	if !filter.IncludeAbsent {
		query["state"] = bson.M{"$ne": string(softwaredomain.StateAbsent)}
	}
	cursor, err := r.col.Find(ctx, query, options.Find().SetSort(bson.D{{Key: "serverId", Value: 1}, {Key: "kind", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []assignmentDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]*softwaredomain.Assignment, len(docs))
	for i := range docs {
		out[i] = toAssignment(&docs[i])
	}
	return out, nil
}

// SetState updates only the lifecycle fields of an existing assignment. appliedAt updates the
// last-applied field only when non-nil (a successful install/uninstall).
func (r *MongoAssignmentRepo) SetState(ctx context.Context, serverID string, kind softwaredomain.Kind, state softwaredomain.AssignmentState, workflowID string, appliedAt *time.Time) error {
	set := bson.M{"state": string(state), "updatedAt": time.Now().UTC()}
	if workflowID != "" {
		set["lastWorkflowId"] = workflowID
	}
	if appliedAt != nil {
		set["lastAppliedAt"] = appliedAt
	}
	result, err := r.col.UpdateOne(ctx, assignmentKey(serverID, kind), bson.M{"$set": set})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return softwaredomain.ErrAssignmentNotFound
	}
	return nil
}

// ListAll returns every non-absent assignment for the OS-lifecycle sweep.
func (r *MongoAssignmentRepo) ListAll(ctx context.Context) ([]*softwaredomain.Assignment, error) {
	return r.List(ctx, softwaredomain.AssignmentFilter{IncludeAbsent: false})
}

func toDoc(assignment *softwaredomain.Assignment) *assignmentDoc {
	roles := make([]string, 0, len(assignment.Roles))
	for _, role := range assignment.Roles {
		roles = append(roles, string(role))
	}
	return &assignmentDoc{
		ServerID: assignment.ServerID, SiteID: assignment.SiteID, Kind: string(assignment.Kind),
		Roles: roles, Spec: assignment.Spec, State: string(assignment.State),
		LastWorkflowID: assignment.LastWorkflowID, LastAppliedAt: assignment.LastAppliedAt,
		CreatedAt: assignment.CreatedAt, UpdatedAt: assignment.UpdatedAt,
	}
}

func toAssignment(doc *assignmentDoc) *softwaredomain.Assignment {
	roles := make([]softwaredomain.Role, 0, len(doc.Roles))
	for _, role := range doc.Roles {
		roles = append(roles, softwaredomain.Role(role))
	}
	return &softwaredomain.Assignment{
		ServerID: doc.ServerID, SiteID: doc.SiteID, Kind: softwaredomain.Kind(doc.Kind),
		Roles: roles, Spec: doc.Spec, State: softwaredomain.AssignmentState(doc.State),
		LastWorkflowID: doc.LastWorkflowID, LastAppliedAt: doc.LastAppliedAt,
		CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt,
	}
}
