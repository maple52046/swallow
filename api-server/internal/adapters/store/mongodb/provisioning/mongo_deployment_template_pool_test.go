package provisioning

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/maple52046/swallow/internal/adapters/store/mongodb/mongotest"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	"github.com/maple52046/swallow/internal/shared/secret"
)

// A template name is unique within an Integration and a Pool; the index that made it unique within
// an Integration alone is dropped when the repository opens a database that still has it.
func TestDeploymentTemplateNamesAreUniquePerPool(t *testing.T) {
	_, db := mongotest.Database(t, "deployment_template_pool")
	ctx := context.Background()
	_, err := db.Collection("deployment_templates").Indexes().CreateOne(ctx, mongoIndex("integration_normalized_name"))
	if err != nil {
		t.Fatal(err)
	}
	repo := templatePoolRepo(t, db)

	if err := repo.Create(ctx, poolTemplate("t1", "pool-a", "Compute"), ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, poolTemplate("t2", "pool-b", "compute"), ""); err != nil {
		t.Errorf("the same name in another Pool: %v", err)
	}
	if err := repo.Create(ctx, poolTemplate("t3", "pool-a", " COMPUTE"), ""); !errors.Is(err, provisioningdomain.ErrDeploymentTemplateNameTaken) {
		t.Errorf("the same name in the same Pool: error = %v, want ErrDeploymentTemplateNameTaken", err)
	}
}

// The list keeps only the Pools named (none when the list is empty), CountByPool counts a Pool's
// templates, and AssignPool moves only the templates not in the Pool yet.
func TestDeploymentTemplatesAreListedAndMovedByPool(t *testing.T) {
	_, db := mongotest.Database(t, "deployment_template_pool")
	ctx := context.Background()
	repo := templatePoolRepo(t, db)
	for _, template := range []*provisioningdomain.DeploymentTemplate{
		poolTemplate("t1", "pool-a", "a"), poolTemplate("t2", "pool-b", "b"), poolTemplate("t3", "", "legacy"),
	} {
		if err := repo.Create(ctx, template, ""); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(filter provisioningdomain.DeploymentTemplateFilter) []string {
		t.Helper()
		items, err := repo.List(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, item := range items {
			out = append(out, item.ID)
		}
		return out
	}
	if got := ids(provisioningdomain.DeploymentTemplateFilter{}); len(got) != 3 {
		t.Errorf("unrestricted list = %v, want every template", got)
	}
	if got := ids(provisioningdomain.DeploymentTemplateFilter{PoolIDs: []string{"pool-a"}}); len(got) != 1 || got[0] != "t1" {
		t.Errorf("pool-a list = %v, want [t1]", got)
	}
	if got := ids(provisioningdomain.DeploymentTemplateFilter{PoolIDs: []string{}}); len(got) != 0 {
		t.Errorf("list over no Pool = %v, want none", got)
	}

	moved, err := repo.AssignPool(ctx, []string{"t1", "t3"}, "pool-a")
	if err != nil || moved != 1 {
		t.Fatalf("AssignPool = %d, %v; want 1 (t1 is already in pool-a)", moved, err)
	}
	if count, err := repo.CountByPool(ctx, "pool-a"); err != nil || count != 2 {
		t.Errorf("CountByPool(pool-a) = %d, %v; want 2", count, err)
	}
	adopted, err := repo.FindByID(ctx, "t3")
	if err != nil || adopted.PoolID != "pool-a" || adopted.CreatedBy != "alice" {
		t.Errorf("adopted template = %+v, %v; want pool-a created by alice", adopted, err)
	}
}

// templatePoolRepo opens the template repository over db.
func templatePoolRepo(t *testing.T, db *mongo.Database) *MongoDeploymentTemplateRepo {
	t.Helper()
	sealer, err := secret.NewSealer(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := NewMongoDeploymentTemplateRepo(db, sealer)
	if err != nil {
		t.Fatalf("NewMongoDeploymentTemplateRepo() error = %v", err)
	}
	return repo
}

// poolTemplate is a template of integration-1 owned by pool and created by alice.
func poolTemplate(id, pool, name string) *provisioningdomain.DeploymentTemplate {
	now := time.Now().UTC()
	return &provisioningdomain.DeploymentTemplate{
		ID: id, IntegrationID: "integration-1", PoolID: pool, CreatedBy: "alice", Name: name, ImageID: "ubuntu/noble",
		NetworkMode: provisioningdomain.DeploymentNetworkAutomatic, CreatedAt: now, UpdatedAt: now,
	}
}

// mongoIndex is the unique index on Integration and name an earlier release created.
func mongoIndex(name string) mongo.IndexModel {
	return mongo.IndexModel{
		Keys:    bson.D{{Key: "integrationId", Value: 1}, {Key: "normalizedName", Value: 1}},
		Options: options.Index().SetUnique(true).SetName(name),
	}
}
