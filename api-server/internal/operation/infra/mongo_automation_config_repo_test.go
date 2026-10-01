package infra

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/bson"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/shared/secret"
)

// TestMongoAutomationConfigurationRepo_IgnoresLegacySiteKey proves decision 041 against a
// document an earlier release wrote: a sealed Site private key never reaches a run, and the next
// credential write drops both the key and the unsealed override marker.
func TestMongoAutomationConfigurationRepo_IgnoresLegacySiteKey(t *testing.T) {
	db := workflowTestDatabase(t)
	ctx := context.Background()
	sealer, err := secret.NewSealer("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	legacy, err := sealer.Seal(`{"sshPrivateKey":"LEGACY-SITE-KEY","becomePassword":"pw"}`)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	col := db.Collection("automation_configurations")
	if _, err := col.InsertOne(ctx, bson.M{
		"_id": "site-1", "enabled": true, "sshPort": 22, "knownHosts": "# scanned",
		"sealedCredential": legacy, credentialHasPrivateKeyField: true,
	}); err != nil {
		t.Fatalf("insert legacy document: %v", err)
	}
	repo := NewMongoAutomationConfigurationRepo(db, sealer)

	configuration, err := repo.FindBySiteID(ctx, "site-1")
	if err != nil {
		t.Fatalf("FindBySiteID: %v", err)
	}
	if !configuration.HasCredential {
		t.Errorf("HasCredential = false, want true for the stored become password")
	}
	credential, err := repo.Credential(ctx, "site-1")
	if err != nil {
		t.Fatalf("Credential: %v", err)
	}
	if credential.SSHPrivateKey != "" || credential.BecomePassword != "pw" {
		t.Errorf("Credential() = key %q, become password %q; want no key and %q",
			credential.SSHPrivateKey, credential.BecomePassword, "pw")
	}

	if err := repo.ReplaceCredential(ctx, "site-1", operationdomain.AutomationCredential{
		SSHPrivateKey: "MUST-NOT-BE-STORED", BecomePassword: "pw2",
	}); err != nil {
		t.Fatalf("ReplaceCredential: %v", err)
	}
	var raw bson.M
	if err := col.FindOne(ctx, bson.M{"_id": "site-1"}).Decode(&raw); err != nil {
		t.Fatalf("read document: %v", err)
	}
	if _, ok := raw[credentialHasPrivateKeyField]; ok {
		t.Errorf("%s survived ReplaceCredential", credentialHasPrivateKeyField)
	}
	sealed, _ := raw["sealedCredential"].(string)
	opened, err := sealer.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened != `{"becomePassword":"pw2"}` {
		t.Errorf("sealed credential = %s, want only the become password", opened)
	}
}
