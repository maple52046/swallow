package access

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	convergencestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/convergence"
	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
)

const (
	policyCollection   = "pool_access_policies"
	settingsCollection = "access_settings"
	auditCollection    = "access_audit"
	statusCollection   = "directory_status"
	singletonID        = "installation"
	directoryStatusID  = "directory"
	auditRetention     = 365 * 24 * time.Hour
)

// policyDoc is a Pool's stored Sudo Policy defaults, keyed by Pool id.
type policyDoc struct {
	PoolID           string    `bson:"_id"`
	MemberSudo       bool      `bson:"memberSudo"`
	SudoPasswordless bool      `bson:"sudoPasswordless"`
	UpdatedAt        time.Time `bson:"updatedAt"`
	UpdatedBy        string    `bson:"updatedBy,omitempty"`
}

// MongoPolicies is the durable accessdomain.PolicyRepository.
type MongoPolicies struct {
	col *mongo.Collection
}

// NewMongoPolicies returns the repository.
func NewMongoPolicies(db *mongo.Database) *MongoPolicies {
	return &MongoPolicies{col: db.Collection(policyCollection)}
}

var _ accessdomain.PolicyRepository = (*MongoPolicies)(nil)

// Find implements accessdomain.PolicyRepository: a Pool without a stored policy has the defaults.
func (r *MongoPolicies) Find(ctx context.Context, poolID string) (accessdomain.PoolPolicy, error) {
	var doc policyDoc
	err := r.col.FindOne(ctx, bson.M{"_id": poolID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return accessdomain.PoolPolicy{PoolID: poolID}, nil
	}
	if err != nil {
		return accessdomain.PoolPolicy{}, err
	}
	return toPolicy(doc), nil
}

// ListAll implements accessdomain.PolicyRepository.
func (r *MongoPolicies) ListAll(ctx context.Context) ([]accessdomain.PoolPolicy, error) {
	cursor, err := r.col.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []policyDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]accessdomain.PoolPolicy, 0, len(docs))
	for _, doc := range docs {
		out = append(out, toPolicy(doc))
	}
	return out, nil
}

// Save implements accessdomain.PolicyRepository.
func (r *MongoPolicies) Save(ctx context.Context, policy accessdomain.PoolPolicy) error {
	_, err := r.col.ReplaceOne(ctx, bson.M{"_id": policy.PoolID}, policyDoc{
		PoolID: policy.PoolID, MemberSudo: policy.MemberSudo, SudoPasswordless: policy.SudoPasswordless,
		UpdatedAt: policy.UpdatedAt, UpdatedBy: policy.UpdatedBy,
	}, options.Replace().SetUpsert(true))
	return err
}

// Delete implements accessdomain.PolicyRepository.
func (r *MongoPolicies) Delete(ctx context.Context, poolID string) error {
	_, err := r.col.DeleteOne(ctx, bson.M{"_id": poolID})
	return err
}

func toPolicy(doc policyDoc) accessdomain.PoolPolicy {
	return accessdomain.PoolPolicy{
		PoolID: doc.PoolID, MemberSudo: doc.MemberSudo, SudoPasswordless: doc.SudoPasswordless,
		UpdatedAt: doc.UpdatedAt, UpdatedBy: doc.UpdatedBy,
	}
}

// settingsDoc is the installation's stored access settings, one document.
type settingsDoc struct {
	ID                    string    `bson:"_id"`
	AdminSudoPasswordless bool      `bson:"adminSudoPasswordless"`
	UpdatedAt             time.Time `bson:"updatedAt"`
	UpdatedBy             string    `bson:"updatedBy,omitempty"`
}

// MongoSettings is the durable accessdomain.SettingsRepository.
type MongoSettings struct {
	col *mongo.Collection
}

// NewMongoSettings returns the repository.
func NewMongoSettings(db *mongo.Database) *MongoSettings {
	return &MongoSettings{col: db.Collection(settingsCollection)}
}

var _ accessdomain.SettingsRepository = (*MongoSettings)(nil)

// Find implements accessdomain.SettingsRepository.
func (r *MongoSettings) Find(ctx context.Context) (accessdomain.Settings, error) {
	var doc settingsDoc
	err := r.col.FindOne(ctx, bson.M{"_id": singletonID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return accessdomain.Settings{}, nil
	}
	if err != nil {
		return accessdomain.Settings{}, err
	}
	return accessdomain.Settings{AdminSudoPasswordless: doc.AdminSudoPasswordless, UpdatedAt: doc.UpdatedAt, UpdatedBy: doc.UpdatedBy}, nil
}

// Save implements accessdomain.SettingsRepository.
func (r *MongoSettings) Save(ctx context.Context, settings accessdomain.Settings) error {
	_, err := r.col.ReplaceOne(ctx, bson.M{"_id": singletonID}, settingsDoc{
		ID: singletonID, AdminSudoPasswordless: settings.AdminSudoPasswordless, UpdatedAt: settings.UpdatedAt, UpdatedBy: settings.UpdatedBy,
	}, options.Replace().SetUpsert(true))
	return err
}

// auditDoc is one stored audit entry.
type auditDoc struct {
	ID      string    `bson:"_id"`
	At      time.Time `bson:"at"`
	ActorID string    `bson:"actorId,omitempty"`
	Actor   string    `bson:"actor,omitempty"`
	Action  string    `bson:"action"`
	Target  string    `bson:"target,omitempty"`
	PoolID  string    `bson:"poolId,omitempty"`
	Detail  string    `bson:"detail,omitempty"`
}

// MongoAudit is the durable accessdomain.AuditRepository. Entries expire after a year (TTL index),
// so the log cannot grow without bound.
type MongoAudit struct {
	col *mongo.Collection
}

// NewMongoAudit creates the repository and its time and retention indexes.
func NewMongoAudit(ctx context.Context, db *mongo.Database) (*MongoAudit, error) {
	col := db.Collection(auditCollection)
	_, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "at", Value: 1}},
		Options: options.Index().SetName("access_audit_ttl").SetExpireAfterSeconds(int32(auditRetention / time.Second)),
	})
	if err != nil {
		return nil, fmt.Errorf("access_audit indexes: %w", err)
	}
	return &MongoAudit{col: col}, nil
}

var _ accessdomain.AuditRepository = (*MongoAudit)(nil)

// Record implements accessdomain.AuditRepository.
func (r *MongoAudit) Record(ctx context.Context, entry accessdomain.AuditEntry) error {
	_, err := r.col.InsertOne(ctx, auditDoc{
		ID: entry.ID, At: entry.At.UTC(), ActorID: entry.ActorID, Actor: entry.Actor, Action: string(entry.Action),
		Target: entry.Target, PoolID: entry.PoolID, Detail: entry.Detail,
	})
	return err
}

// List implements accessdomain.AuditRepository.
func (r *MongoAudit) List(ctx context.Context, limit int) ([]accessdomain.AuditEntry, error) {
	cursor, err := r.col.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "at", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []auditDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]accessdomain.AuditEntry, 0, len(docs))
	for _, doc := range docs {
		out = append(out, accessdomain.AuditEntry{
			ID: doc.ID, At: doc.At, ActorID: doc.ActorID, Actor: doc.Actor, Action: accessdomain.AuditAction(doc.Action),
			Target: doc.Target, PoolID: doc.PoolID, Detail: doc.Detail,
		})
	}
	return out, nil
}

// statusDoc is the Directory controller's stored status, one document.
type statusDoc struct {
	ID            string                          `bson:"_id"`
	Conditions    []convergencestore.ConditionDoc `bson:"conditions,omitempty"`
	ContentHash   string                          `bson:"contentHash,omitempty"`
	LastAppliedAt *time.Time                      `bson:"lastAppliedAt,omitempty"`
	ObservedAt    *time.Time                      `bson:"observedAt,omitempty"`
	Users         int                             `bson:"users"`
	Groups        int                             `bson:"groups"`
	SudoRules     int                             `bson:"sudoRules"`
	Hosts         int                             `bson:"hosts"`
}

// MongoDirectoryStatus is the durable accessdomain.DirectoryStatusRepository.
type MongoDirectoryStatus struct {
	col *mongo.Collection
}

// NewMongoDirectoryStatus returns the repository.
func NewMongoDirectoryStatus(db *mongo.Database) *MongoDirectoryStatus {
	return &MongoDirectoryStatus{col: db.Collection(statusCollection)}
}

var _ accessdomain.DirectoryStatusRepository = (*MongoDirectoryStatus)(nil)

// Find implements accessdomain.DirectoryStatusRepository.
func (r *MongoDirectoryStatus) Find(ctx context.Context) (accessdomain.DirectoryStatus, error) {
	var doc statusDoc
	err := r.col.FindOne(ctx, bson.M{"_id": directoryStatusID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return accessdomain.DirectoryStatus{}, nil
	}
	if err != nil {
		return accessdomain.DirectoryStatus{}, err
	}
	return accessdomain.DirectoryStatus{
		Conditions: convergencestore.ToConditions(doc.Conditions), ContentHash: doc.ContentHash,
		LastAppliedAt: doc.LastAppliedAt, ObservedAt: doc.ObservedAt,
		Users: doc.Users, Groups: doc.Groups, SudoRules: doc.SudoRules, Hosts: doc.Hosts,
	}, nil
}

// Save implements accessdomain.DirectoryStatusRepository.
func (r *MongoDirectoryStatus) Save(ctx context.Context, status accessdomain.DirectoryStatus) error {
	_, err := r.col.ReplaceOne(ctx, bson.M{"_id": directoryStatusID}, statusDoc{
		ID: directoryStatusID, Conditions: convergencestore.ConditionDocs(status.Conditions), ContentHash: status.ContentHash,
		LastAppliedAt: status.LastAppliedAt, ObservedAt: status.ObservedAt,
		Users: status.Users, Groups: status.Groups, SudoRules: status.SudoRules, Hosts: status.Hosts,
	}, options.Replace().SetUpsert(true))
	return err
}
