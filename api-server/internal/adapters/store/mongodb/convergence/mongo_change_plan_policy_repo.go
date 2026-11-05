package convergence

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/maple52046/swallow/internal/core/convergence"
)

// ChangePlanPolicyCollection holds the installation's Change Plan policy (decision 076).
const ChangePlanPolicyCollection = "change_plan_policy"

// changePlanPolicyID is the _id of the only policy document: one policy per installation, so the
// fixed id gives the uniqueness without an index or a migration.
const changePlanPolicyID = "installation"

type changePlanPolicyDoc struct {
	ID              string    `bson:"_id"`
	ApproveOnSubmit bool      `bson:"approveOnSubmit"`
	UpdatedAt       time.Time `bson:"updatedAt"`
	UpdatedBy       string    `bson:"updatedBy"`
}

// MongoChangePlanPolicyRepo implements convergence.ChangePlanPolicyStore over
// `change_plan_policy`. The api reads it inside spec-write transactions; a collection that does not
// exist yet reads as no stored policy, so the first write creates it outside any transaction.
type MongoChangePlanPolicyRepo struct {
	col *mongo.Collection
}

var _ convergence.ChangePlanPolicyStore = (*MongoChangePlanPolicyRepo)(nil)

// NewMongoChangePlanPolicyRepo returns the repository over db.
func NewMongoChangePlanPolicyRepo(db *mongo.Database) *MongoChangePlanPolicyRepo {
	return &MongoChangePlanPolicyRepo{col: db.Collection(ChangePlanPolicyCollection)}
}

// Find implements convergence.ChangePlanPolicyStore.
func (r *MongoChangePlanPolicyRepo) Find(ctx context.Context) (convergence.ChangePlanPolicy, error) {
	var doc changePlanPolicyDoc
	err := r.col.FindOne(ctx, bson.M{"_id": changePlanPolicyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return convergence.ChangePlanPolicy{}, convergence.ErrChangePlanPolicyNotFound
	}
	if err != nil {
		return convergence.ChangePlanPolicy{}, err
	}
	return convergence.ChangePlanPolicy{ApproveOnSubmit: doc.ApproveOnSubmit, UpdatedAt: doc.UpdatedAt, UpdatedBy: doc.UpdatedBy}, nil
}

// Save implements convergence.ChangePlanPolicyStore with one upsert of the single document.
func (r *MongoChangePlanPolicyRepo) Save(ctx context.Context, policy convergence.ChangePlanPolicy) error {
	doc := changePlanPolicyDoc{
		ID: changePlanPolicyID, ApproveOnSubmit: policy.ApproveOnSubmit, UpdatedAt: policy.UpdatedAt.UTC(), UpdatedBy: policy.UpdatedBy,
	}
	_, err := r.col.ReplaceOne(ctx, bson.M{"_id": changePlanPolicyID}, doc, options.Replace().SetUpsert(true))
	return err
}
