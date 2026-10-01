package infra

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/shared/secret"
)

type automationConfigurationDoc struct {
	SiteID           string            `bson:"_id"`
	Enabled          bool              `bson:"enabled"`
	SSHUser          string            `bson:"sshUser"`
	SSHPort          int               `bson:"sshPort"`
	KnownHosts       string            `bson:"knownHosts"`
	PlaybookMappings map[string]string `bson:"playbookMappings"`
	SealedCredential string            `bson:"sealedCredential,omitempty"`
	// CredentialHasPrivateKey records, unsealed, whether the sealed credential carries an SSH
	// private key, so reads can report the override without decrypting. It is absent on
	// credentials written before decision 039, which always carried a private key.
	CredentialHasPrivateKey *bool     `bson:"credentialHasPrivateKey,omitempty"`
	CreatedAt               time.Time `bson:"createdAt"`
	UpdatedAt               time.Time `bson:"updatedAt"`
}

// MongoAutomationConfigurationRepo seals site SSH credentials with AES-GCM.
type MongoAutomationConfigurationRepo struct {
	col    *mongo.Collection
	sealer *secret.Sealer
}

// NewMongoAutomationConfigurationRepo constructs the repository.
func NewMongoAutomationConfigurationRepo(db *mongo.Database, sealer *secret.Sealer) *MongoAutomationConfigurationRepo {
	return &MongoAutomationConfigurationRepo{col: db.Collection("automation_configurations"), sealer: sealer}
}

// FindBySiteID never returns credential material.
func (r *MongoAutomationConfigurationRepo) FindBySiteID(ctx context.Context, siteID string) (*operationdomain.AutomationConfiguration, error) {
	var doc automationConfigurationDoc
	err := r.col.FindOne(ctx, bson.M{"_id": siteID}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, operationdomain.ErrAutomationConfigNotFound
	}
	if err != nil {
		return nil, err
	}
	mappings := make(map[operationdomain.WorkflowKind]string, len(doc.PlaybookMappings))
	for kind, playbook := range doc.PlaybookMappings {
		mappings[operationdomain.WorkflowKind(kind)] = playbook
	}
	hasCredential := doc.SealedCredential != ""
	return &operationdomain.AutomationConfiguration{
		SiteID: doc.SiteID, Enabled: doc.Enabled, SSHUser: doc.SSHUser, SSHPort: doc.SSHPort,
		KnownHosts: doc.KnownHosts, PlaybookMappings: mappings,
		HasCredential:         hasCredential,
		HasPrivateKeyOverride: hasCredential && (doc.CredentialHasPrivateKey == nil || *doc.CredentialHasPrivateKey),
		CreatedAt:             doc.CreatedAt, UpdatedAt: doc.UpdatedAt,
	}, nil
}

// Upsert changes non-secret settings without touching the credential.
func (r *MongoAutomationConfigurationRepo) Upsert(ctx context.Context, configuration *operationdomain.AutomationConfiguration) error {
	now := time.Now().UTC()
	mappings := make(map[string]string, len(configuration.PlaybookMappings))
	for kind, playbook := range configuration.PlaybookMappings {
		mappings[string(kind)] = playbook
	}
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": configuration.SiteID}, bson.M{
		"$set": bson.M{
			"enabled": configuration.Enabled, "sshUser": configuration.SSHUser,
			"sshPort": configuration.SSHPort, "knownHosts": configuration.KnownHosts,
			"playbookMappings": mappings, "updatedAt": now,
		},
		"$setOnInsert": bson.M{"createdAt": now},
	}, options.Update().SetUpsert(true))
	return err
}

// ReplaceCredential encrypts the whole credential document as one authenticated value.
func (r *MongoAutomationConfigurationRepo) ReplaceCredential(ctx context.Context, siteID string, credential operationdomain.AutomationCredential) error {
	raw, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	sealed, err := r.sealer.Seal(string(raw))
	if err != nil {
		return err
	}
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": siteID}, bson.M{"$set": bson.M{
		"sealedCredential":        sealed,
		"credentialHasPrivateKey": strings.TrimSpace(credential.SSHPrivateKey) != "",
		"updatedAt":               time.Now().UTC(),
	}})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return operationdomain.ErrAutomationConfigNotFound
	}
	return nil
}

// Credential is used only by the dispatcher immediately before a run. It returns the site
// credential as stored; resolving the Deployment Key fallback is the job of
// operationapp.EffectiveAutomationConfigurations, which wraps this repository for automation.
func (r *MongoAutomationConfigurationRepo) Credential(ctx context.Context, siteID string) (operationdomain.AutomationCredential, error) {
	var doc automationConfigurationDoc
	err := r.col.FindOne(ctx, bson.M{"_id": siteID},
		options.FindOne().SetProjection(bson.M{"sealedCredential": 1})).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return operationdomain.AutomationCredential{}, operationdomain.ErrAutomationConfigNotFound
	}
	if err != nil {
		return operationdomain.AutomationCredential{}, err
	}
	if doc.SealedCredential == "" {
		return operationdomain.AutomationCredential{}, operationdomain.ErrAutomationCredentialMissing
	}
	raw, err := r.sealer.Open(doc.SealedCredential)
	if err != nil {
		return operationdomain.AutomationCredential{}, err
	}
	var credential operationdomain.AutomationCredential
	if err := json.Unmarshal([]byte(raw), &credential); err != nil {
		return operationdomain.AutomationCredential{}, err
	}
	return credential, nil
}
