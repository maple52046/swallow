package domain

import (
	"context"
	"errors"
	"time"
)

// AnsibleExecutionStatus is the private executor lifecycle behind an Operation Step.
type AnsibleExecutionStatus string

const (
	AnsibleQueued            AnsibleExecutionStatus = "queued"
	AnsibleRunning           AnsibleExecutionStatus = "running"
	AnsibleSucceeded         AnsibleExecutionStatus = "succeeded"
	AnsibleFailed            AnsibleExecutionStatus = "failed"
	AnsibleCanceled          AnsibleExecutionStatus = "canceled"
	AnsibleRequiresAttention AnsibleExecutionStatus = "requires_attention"
)

func (s AnsibleExecutionStatus) Terminal() bool {
	switch s {
	case AnsibleSucceeded, AnsibleFailed, AnsibleCanceled, AnsibleRequiresAttention:
		return true
	default:
		return false
	}
}

// AutomationInputSnapshot freezes the non-secret connection policy used by one Ansible attempt.
type AutomationInputSnapshot struct {
	SSHUser    string `bson:"sshUser" json:"sshUser"`
	SSHPort    int    `bson:"sshPort" json:"sshPort"`
	KnownHosts string `bson:"knownHosts" json:"knownHosts"`
}

// AnsibleExecution is an idempotent command consumed by exactly one executor process.
type AnsibleExecution struct {
	ID              string                   `bson:"_id" json:"id"`
	IdempotencyKey  string                   `bson:"idempotencyKey" json:"idempotencyKey"`
	OperationID     string                   `bson:"operationId" json:"operationId"`
	Kind            OperationKind            `bson:"kind" json:"kind"`
	PlatformID      string                   `bson:"platformId,omitempty" json:"platformId,omitempty"`
	StepID          string                   `bson:"stepId" json:"stepId"`
	Attempt         int                      `bson:"attempt" json:"attempt"`
	SiteID          string                   `bson:"siteId" json:"siteId"`
	TargetServerIDs []string                 `bson:"targetServerIds" json:"targetServerIds"`
	Playbook        string                   `bson:"playbook" json:"playbook"`
	ExtraVars       map[string]any           `bson:"extraVars,omitempty" json:"-"`
	Inventory       map[string]any           `bson:"inventory,omitempty" json:"-"`
	Configuration   *AutomationInputSnapshot `bson:"configuration,omitempty" json:"-"`
	SecretRefs      map[string]string        `bson:"secretRefs,omitempty" json:"-"`
	RunID           string                   `bson:"runId" json:"runId"`
	Status          AnsibleExecutionStatus   `bson:"status" json:"status"`
	StatusReason    string                   `bson:"statusReason,omitempty" json:"statusReason,omitempty"`
	CancelRequested bool                     `bson:"cancelRequested" json:"cancelRequested"`
	LeaseOwner      string                   `bson:"leaseOwner,omitempty" json:"-"`
	LeaseExpiresAt  *time.Time               `bson:"leaseExpiresAt,omitempty" json:"-"`
	CreatedAt       time.Time                `bson:"createdAt" json:"createdAt"`
	StartedAt       *time.Time               `bson:"startedAt,omitempty" json:"startedAt,omitempty"`
	FinishedAt      *time.Time               `bson:"finishedAt,omitempty" json:"finishedAt,omitempty"`
	UpdatedAt       time.Time                `bson:"updatedAt" json:"updatedAt"`
}

var (
	ErrAnsibleExecutionNotFound  = errors.New("ansible execution not found")
	ErrAnsibleExecutionLeaseLost = errors.New("ansible execution lease lost")
)

// AnsibleExecutionRepository is the durable queue shared by Temporal activities and the
// standalone executor process.
type AnsibleExecutionRepository interface {
	CreateOrGet(ctx context.Context, execution *AnsibleExecution) (*AnsibleExecution, error)
	FindByID(ctx context.Context, id string) (*AnsibleExecution, error)
	ClaimNext(ctx context.Context, owner string, expiresAt time.Time) (*AnsibleExecution, error)
	Renew(ctx context.Context, id, owner string, expiresAt time.Time) error
	Finish(ctx context.Context, id, owner string, status AnsibleExecutionStatus, reason string) error
	RequestCancel(ctx context.Context, id string) error
	MarkExpiredRequiresAttention(ctx context.Context, now time.Time) (int64, error)
}
