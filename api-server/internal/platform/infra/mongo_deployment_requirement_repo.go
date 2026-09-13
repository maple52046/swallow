package infra

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
)

type minimumResourcesDoc struct {
	CPUCores  int     `bson:"cpuCores"`
	MemoryMiB int64   `bson:"memoryMiB"`
	StorageGB float64 `bson:"storageGB"`
}

type deploymentRequirementDoc struct {
	PlatformType     string               `bson:"_id"`
	MinimumResources *minimumResourcesDoc `bson:"minimumResources"`
	UpdatedAt        time.Time            `bson:"updatedAt"`
}

// MongoDeploymentRequirementRepo stores exactly one current requirement per Platform type;
// using the Platform type as Mongo's _id gives the uniqueness guarantee without a migration.
type MongoDeploymentRequirementRepo struct {
	col *mongo.Collection
}

func NewMongoDeploymentRequirementRepo(db *mongo.Database) *MongoDeploymentRequirementRepo {
	return &MongoDeploymentRequirementRepo{col: db.Collection("platform_deployment_requirements")}
}

func (r *MongoDeploymentRequirementRepo) FindByPlatformType(
	ctx context.Context,
	platformType platformdomain.PlatformType,
) (*platformdomain.DeploymentRequirement, error) {
	var doc deploymentRequirementDoc
	if err := r.col.FindOne(ctx, bson.M{"_id": string(platformType)}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, platformdomain.ErrDeploymentRequirementNotFound
		}
		return nil, err
	}
	return deploymentRequirementFromDoc(&doc), nil
}

func (r *MongoDeploymentRequirementRepo) Upsert(
	ctx context.Context,
	requirement *platformdomain.DeploymentRequirement,
) error {
	doc := deploymentRequirementToDoc(requirement)
	_, err := r.col.ReplaceOne(
		ctx,
		bson.M{"_id": doc.PlatformType},
		doc,
		options.Replace().SetUpsert(true),
	)
	return err
}

func deploymentRequirementToDoc(requirement *platformdomain.DeploymentRequirement) deploymentRequirementDoc {
	doc := deploymentRequirementDoc{
		PlatformType: string(requirement.PlatformType), UpdatedAt: requirement.UpdatedAt,
	}
	if requirement.MinimumResources != nil {
		doc.MinimumResources = &minimumResourcesDoc{
			CPUCores:  requirement.MinimumResources.CPUCores,
			MemoryMiB: requirement.MinimumResources.MemoryMiB,
			StorageGB: requirement.MinimumResources.StorageGB,
		}
	}
	return doc
}

func deploymentRequirementFromDoc(doc *deploymentRequirementDoc) *platformdomain.DeploymentRequirement {
	requirement := &platformdomain.DeploymentRequirement{
		PlatformType: platformdomain.PlatformType(doc.PlatformType), UpdatedAt: doc.UpdatedAt,
	}
	if doc.MinimumResources != nil {
		requirement.MinimumResources = &platformdomain.MinimumResources{
			CPUCores:  doc.MinimumResources.CPUCores,
			MemoryMiB: doc.MinimumResources.MemoryMiB,
			StorageGB: doc.MinimumResources.StorageGB,
		}
	}
	return requirement
}
