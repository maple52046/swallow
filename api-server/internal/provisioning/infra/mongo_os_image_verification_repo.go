package infra

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// osImageVerificationDoc is the MongoDB shape of a swallow-owned OS Image verification.
//
// Like the overlay document it carries no synthetic _id: the identity is the provider image
// identity (integrationId, imageId, architecture), enforced by a unique compound index. Per-target
// evidence is stored as a nested map keyed by deploy target ("disk"/"ram"). bson tags live only here.
type osImageVerificationDoc struct {
	IntegrationID string                                    `bson:"integrationId"`
	ImageID       string                                    `bson:"imageId"`
	Architecture  string                                    `bson:"architecture"`
	Targets       map[string]osImageVerificationEvidenceDoc `bson:"targets"`
	FailedTargets map[string]osImageVerificationFailureDoc  `bson:"failedTargets,omitempty"`
	UpdatedAt     time.Time                                 `bson:"updatedAt"`
}

type osImageVerificationEvidenceDoc struct {
	VerifiedAt  time.Time `bson:"verifiedAt"`
	OperationID string    `bson:"operationId,omitempty"`
	ServerID    string    `bson:"serverId,omitempty"`
}

type osImageVerificationFailureDoc struct {
	FailedAt    time.Time `bson:"failedAt"`
	OperationID string    `bson:"operationId,omitempty"`
	ServerID    string    `bson:"serverId,omitempty"`
	Reason      string    `bson:"reason,omitempty"`
}

// MongoOSImageVerificationRepo stores swallow-owned OS Image verification attestations in the
// os_image_verifications collection.
type MongoOSImageVerificationRepo struct {
	col *mongo.Collection
}

// NewMongoOSImageVerificationRepo creates the unique key index and returns the repository. The
// index on (integrationId, imageId, architecture) is the persistence guarantee behind the domain
// key: one image within one Integration never carries two conflicting verification records.
func NewMongoOSImageVerificationRepo(db *mongo.Database) (*MongoOSImageVerificationRepo, error) {
	col := db.Collection("os_image_verifications")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "integrationId", Value: 1},
			{Key: "imageId", Value: 1},
			{Key: "architecture", Value: 1},
		},
		Options: options.Index().SetUnique(true).SetName("integration_image_architecture"),
	})
	if err != nil {
		return nil, err
	}
	return &MongoOSImageVerificationRepo{col: col}, nil
}

// ListByIntegration returns every verification stored for one Integration, or an empty slice.
func (r *MongoOSImageVerificationRepo) ListByIntegration(
	ctx context.Context,
	integrationID string,
) ([]*provisioningdomain.OSImageVerification, error) {
	cursor, err := r.col.Find(ctx, bson.M{"integrationId": integrationID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []osImageVerificationDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	verifications := make([]*provisioningdomain.OSImageVerification, len(docs))
	for i := range docs {
		verifications[i] = toOSImageVerification(&docs[i])
	}
	return verifications, nil
}

// Find returns the verification for one image identity, or nil when the image is unverified.
func (r *MongoOSImageVerificationRepo) Find(
	ctx context.Context,
	integrationID, imageID, architecture string,
) (*provisioningdomain.OSImageVerification, error) {
	var doc osImageVerificationDoc
	err := r.col.FindOne(ctx, bson.M{
		"integrationId": integrationID,
		"imageId":       imageID,
		"architecture":  architecture,
	}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toOSImageVerification(&doc), nil
}

// RecordTarget marks one deploy target verified. It sets only that target's evidence field, so
// verifying RAM never clears a previously verified disk; SetUpsert makes the first verification an
// insert and later ones updates against the same unique key. Recording a success also unsets any
// prior failure for the same target, so verified and failed stay mutually exclusive and the record
// reflects the latest run.
func (r *MongoOSImageVerificationRepo) RecordTarget(
	ctx context.Context,
	integrationID, imageID, architecture string,
	target provisioningdomain.DeployTarget,
	evidence provisioningdomain.OSImageVerificationEvidence,
) error {
	now := time.Now().UTC()
	filter := bson.M{
		"integrationId": integrationID,
		"imageId":       imageID,
		"architecture":  architecture,
	}
	update := bson.M{
		"$set": bson.M{
			"targets." + string(target): osImageVerificationEvidenceDoc{
				VerifiedAt:  evidence.VerifiedAt,
				OperationID: evidence.OperationID,
				ServerID:    evidence.ServerID,
			},
			"updatedAt": now,
		},
		"$unset": bson.M{"failedTargets." + string(target): ""},
		"$setOnInsert": bson.M{
			"integrationId": integrationID,
			"imageId":       imageID,
			"architecture":  architecture,
		},
	}
	_, err := r.col.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// RecordFailedTarget marks one deploy target's latest verification as failed. It is the mirror of
// RecordTarget: it sets only that target's failure entry and unsets any prior success, so a target
// that stops deploying is no longer shown as verified, while other targets keep their outcome.
func (r *MongoOSImageVerificationRepo) RecordFailedTarget(
	ctx context.Context,
	integrationID, imageID, architecture string,
	target provisioningdomain.DeployTarget,
	failure provisioningdomain.OSImageVerificationFailure,
) error {
	now := time.Now().UTC()
	filter := bson.M{
		"integrationId": integrationID,
		"imageId":       imageID,
		"architecture":  architecture,
	}
	update := bson.M{
		"$set": bson.M{
			"failedTargets." + string(target): osImageVerificationFailureDoc{
				FailedAt:    failure.FailedAt,
				OperationID: failure.OperationID,
				ServerID:    failure.ServerID,
				Reason:      failure.Reason,
			},
			"updatedAt": now,
		},
		"$unset": bson.M{"targets." + string(target): ""},
		"$setOnInsert": bson.M{
			"integrationId": integrationID,
			"imageId":       imageID,
			"architecture":  architecture,
		},
	}
	_, err := r.col.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// Delete removes the verification for one image identity; an absent record is success.
func (r *MongoOSImageVerificationRepo) Delete(
	ctx context.Context,
	integrationID, imageID, architecture string,
) error {
	_, err := r.col.DeleteOne(ctx, bson.M{
		"integrationId": integrationID,
		"imageId":       imageID,
		"architecture":  architecture,
	})
	return err
}

// DeleteByIntegration removes every verification for one Integration.
func (r *MongoOSImageVerificationRepo) DeleteByIntegration(ctx context.Context, integrationID string) error {
	_, err := r.col.DeleteMany(ctx, bson.M{"integrationId": integrationID})
	return err
}

// toOSImageVerification maps a stored document onto the domain type, keeping bson concerns here.
func toOSImageVerification(doc *osImageVerificationDoc) *provisioningdomain.OSImageVerification {
	targets := make(map[provisioningdomain.DeployTarget]provisioningdomain.OSImageVerificationEvidence, len(doc.Targets))
	for key, evidence := range doc.Targets {
		targets[provisioningdomain.DeployTarget(key)] = provisioningdomain.OSImageVerificationEvidence{
			VerifiedAt:  evidence.VerifiedAt,
			OperationID: evidence.OperationID,
			ServerID:    evidence.ServerID,
		}
	}
	var failedTargets map[provisioningdomain.DeployTarget]provisioningdomain.OSImageVerificationFailure
	if len(doc.FailedTargets) > 0 {
		failedTargets = make(map[provisioningdomain.DeployTarget]provisioningdomain.OSImageVerificationFailure, len(doc.FailedTargets))
		for key, failure := range doc.FailedTargets {
			failedTargets[provisioningdomain.DeployTarget(key)] = provisioningdomain.OSImageVerificationFailure{
				FailedAt:    failure.FailedAt,
				OperationID: failure.OperationID,
				ServerID:    failure.ServerID,
				Reason:      failure.Reason,
			}
		}
	}
	return &provisioningdomain.OSImageVerification{
		IntegrationID: doc.IntegrationID,
		ImageID:       doc.ImageID,
		Architecture:  doc.Architecture,
		Targets:       targets,
		FailedTargets: failedTargets,
		UpdatedAt:     doc.UpdatedAt,
	}
}
