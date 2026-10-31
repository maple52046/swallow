package workflow

import (
	"context"
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/bson"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
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

	if err := repo.ReplaceCredential(ctx, "site-1", workflowdomain.AutomationCredential{
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

// DF-13: clearing the become password (`{}`) removes the stored credential, so the Site reads as
// having none; before, an empty password was sealed and hasCredential stayed true.
func TestMongoAutomationConfigurationRepo_EmptyBecomePasswordClearsTheCredential(t *testing.T) {
	db := workflowTestDatabase(t)
	ctx := context.Background()
	sealer, err := secret.NewSealer("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	if _, err := db.Collection("automation_configurations").InsertOne(ctx, bson.M{"_id": "site-1", "enabled": true}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	repo := NewMongoAutomationConfigurationRepo(db, sealer)
	if err := repo.ReplaceCredential(ctx, "site-1", workflowdomain.AutomationCredential{BecomePassword: "pw"}); err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := repo.ReplaceCredential(ctx, "site-1", workflowdomain.AutomationCredential{}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	configuration, err := repo.FindBySiteID(ctx, "site-1")
	if err != nil {
		t.Fatalf("FindBySiteID: %v", err)
	}
	if configuration.HasCredential {
		t.Error("HasCredential = true after the become password was cleared")
	}
	if _, err := repo.Credential(ctx, "site-1"); !errors.Is(err, workflowdomain.ErrAutomationCredentialMissing) {
		t.Errorf("Credential() error = %v, want ErrAutomationCredentialMissing", err)
	}
	if err := repo.ReplaceCredential(ctx, "missing", workflowdomain.AutomationCredential{}); !errors.Is(err, workflowdomain.ErrAutomationConfigNotFound) {
		t.Errorf("clearing an unconfigured Site err = %v, want ErrAutomationConfigNotFound", err)
	}
}
