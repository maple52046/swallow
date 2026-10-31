package workflow

import (
	"context"
	"encoding/base64"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	storemongo "github.com/maple52046/swallow/internal/adapters/store/mongodb"
	"github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	"github.com/maple52046/swallow/internal/shared/secret"
)

// A Workflow whose record cannot be written leaves none of the secrets it sealed in the same
// transaction (decision 069): before transactions, the service deleted them by hand afterwards.
func TestWorkflowCreateRollsBackItsSecretsWhenTheRecordFails(t *testing.T) {
	db := workflowTestDatabase(t)
	ctx := context.Background()
	workflows, err := NewMongoWorkflowRepo(db)
	if err != nil {
		t.Fatalf("NewMongoWorkflowRepo() error = %v", err)
	}
	sealer, err := secret.NewSealer(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatalf("NewSealer() error = %v", err)
	}
	secrets, err := NewMongoOperationSecretRepo(db, sealer)
	if err != nil {
		t.Fatalf("NewMongoOperationSecretRepo() error = %v", err)
	}
	service := application.NewWorkflowService(storemongo.NewTransactor(db.Client()), workflows, nil, secrets)

	input := application.CreateWorkflowInput{
		ID:   "8b9f2f8e-5a4c-4c1e-9f0e-1f2a3b4c5d6e",
		Kind: workflowdomain.WorkflowKindDeployOS, Definition: "deploy-os", DefinitionVersion: 1,
		SiteID:       "site-1",
		Steps:        []workflowdomain.Task{{ID: "provision-os", Kind: "provision-os", Name: "Provision OS"}},
		SecretValues: map[string]any{"password": "first"},
	}
	if _, err := service.Create(ctx, input); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	countSecrets := func() int64 {
		t.Helper()
		n, err := db.Collection("workflow_secrets").CountDocuments(ctx, bson.M{})
		if err != nil {
			t.Fatalf("count workflow_secrets: %v", err)
		}
		return n
	}
	before := countSecrets()

	// The same id again: the record's insert fails on its unique _id after the secret was sealed.
	input.SecretValues = map[string]any{"password": "second"}
	if _, err := service.Create(ctx, input); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("second Create() error = %v, want a duplicate key error", err)
	}
	if after := countSecrets(); after != before {
		t.Errorf("workflow_secrets after the failed Create = %d, want %d (the secret rolled back)", after, before)
	}
}
