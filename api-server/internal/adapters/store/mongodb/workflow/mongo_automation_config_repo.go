package workflow

import (
	"context"
	"encoding/json"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
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
	CreatedAt        time.Time         `bson:"createdAt"`
	UpdatedAt        time.Time         `bson:"updatedAt"`
}

// sealedAutomationCredential is the JSON document sealed into sealedCredential. It holds only
// the become password (decision 041). Credentials written by earlier releases may also carry
// an "sshPrivateKey" member; decoding into this type drops it, so a stored Site key can never
// reach a run, and the next ReplaceCredential overwrites it.
type sealedAutomationCredential struct {
	BecomePassword string `json:"becomePassword,omitempty"`
}

// credentialHasPrivateKeyField is the unsealed marker earlier releases wrote beside the sealed
// credential. ReplaceCredential removes it so no document keeps claiming a Site key exists.
const credentialHasPrivateKeyField = "credentialHasPrivateKey"

// MongoAutomationConfigurationRepo stores Site automation settings and seals the Site's
// become password with AES-GCM. It never stores SSH key material.
type MongoAutomationConfigurationRepo struct {
	col    *mongo.Collection
	sealer *secret.Sealer
}

// NewMongoAutomationConfigurationRepo constructs the repository.
func NewMongoAutomationConfigurationRepo(db *mongo.Database, sealer *secret.Sealer) *MongoAutomationConfigurationRepo {
	return &MongoAutomationConfigurationRepo{col: db.Collection("automation_configurations"), sealer: sealer}
}

// FindBySiteID never returns credential material.
func (r *MongoAutomationConfigurationRepo) FindBySiteID(ctx context.Context, siteID string) (*workflowdomain.AutomationConfiguration, error) {
	var doc automationConfigurationDoc
	err := r.col.FindOne(ctx, bson.M{"_id": siteID}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, workflowdomain.ErrAutomationConfigNotFound
	}
	if err != nil {
		return nil, err
	}
	mappings := make(map[workflowdomain.WorkflowKind]string, len(doc.PlaybookMappings))
	for kind, playbook := range doc.PlaybookMappings {
		mappings[workflowdomain.WorkflowKind(kind)] = playbook
	}
	return &workflowdomain.AutomationConfiguration{
		SiteID: doc.SiteID, Enabled: doc.Enabled, SSHUser: doc.SSHUser, SSHPort: doc.SSHPort,
		KnownHosts: doc.KnownHosts, PlaybookMappings: mappings,
		HasCredential: doc.SealedCredential != "",
		CreatedAt:     doc.CreatedAt, UpdatedAt: doc.UpdatedAt,
	}, nil
}

// Upsert changes non-secret settings without touching the credential.
func (r *MongoAutomationConfigurationRepo) Upsert(ctx context.Context, configuration *workflowdomain.AutomationConfiguration) error {
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

// ReplaceCredential seals the Site's become password as one authenticated value, replacing
// whatever credential the Site stored before. credential.SSHPrivateKey is never stored: the
// Deployment Key is the only automation key (decision 041), and callers reject a Site key
// before reaching this port.
//
// An empty become password clears the credential: the sealed value is removed rather than
// sealing an empty one, so hasCredential reads false again (DF-13) and automation runs with the
// Deployment Key and no become password.
func (r *MongoAutomationConfigurationRepo) ReplaceCredential(ctx context.Context, siteID string, credential workflowdomain.AutomationCredential) error {
	update := bson.M{
		"$set":   bson.M{"updatedAt": time.Now().UTC()},
		"$unset": bson.M{credentialHasPrivateKeyField: "", "sealedCredential": ""},
	}
	if credential.BecomePassword != "" {
		raw, err := json.Marshal(sealedAutomationCredential{BecomePassword: credential.BecomePassword})
		if err != nil {
			return err
		}
		sealed, err := r.sealer.Seal(string(raw))
		if err != nil {
			return err
		}
		update = bson.M{
			"$set":   bson.M{"sealedCredential": sealed, "updatedAt": time.Now().UTC()},
			"$unset": bson.M{credentialHasPrivateKeyField: ""},
		}
	}
	result, err := r.col.UpdateOne(ctx, bson.M{"_id": siteID}, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return workflowdomain.ErrAutomationConfigNotFound
	}
	return nil
}

// Credential is used only by automation immediately before a run. It returns the Site's
// become password and never an SSH private key, even when an earlier release sealed one;
// workflowapp.EffectiveAutomationConfigurations, which wraps this repository for automation,
// adds the Deployment Key. A configured Site without a stored credential is
// ErrAutomationCredentialMissing; an unconfigured Site is ErrAutomationConfigNotFound.
func (r *MongoAutomationConfigurationRepo) Credential(ctx context.Context, siteID string) (workflowdomain.AutomationCredential, error) {
	var doc automationConfigurationDoc
	err := r.col.FindOne(ctx, bson.M{"_id": siteID},
		options.FindOne().SetProjection(bson.M{"sealedCredential": 1})).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return workflowdomain.AutomationCredential{}, workflowdomain.ErrAutomationConfigNotFound
	}
	if err != nil {
		return workflowdomain.AutomationCredential{}, err
	}
	if doc.SealedCredential == "" {
		return workflowdomain.AutomationCredential{}, workflowdomain.ErrAutomationCredentialMissing
	}
	raw, err := r.sealer.Open(doc.SealedCredential)
	if err != nil {
		return workflowdomain.AutomationCredential{}, err
	}
	var stored sealedAutomationCredential
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return workflowdomain.AutomationCredential{}, err
	}
	return workflowdomain.AutomationCredential{BecomePassword: stored.BecomePassword}, nil
}
