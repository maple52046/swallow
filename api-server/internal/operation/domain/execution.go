package domain

import (
	"context"
	"errors"
	"time"
)

// Execution describes the locally owned run for an operation.
type Execution struct {
	RunID          string
	Playbook       string
	Status         Status
	StatusReason   string
	StartedAt      *time.Time
	FinishedAt     *time.Time
	LeaseOwner     string
	LeaseExpiresAt *time.Time
}

// ExecutionOperation is durable intent plus the embedded Ansible run that executes it.
type ExecutionOperation struct {
	ID              string
	Kind            OperationKind
	Intent          string
	SiteID          string
	ClusterID       string
	TargetServerIDs []string
	ExtraVars       map[string]any
	Execution       Execution
	RequestedBy     string
	RequestedAt     time.Time
	UpdatedAt       time.Time
}

// AutomationConfiguration is the single embedded-runner configuration for a site.
type AutomationConfiguration struct {
	SiteID           string
	Enabled          bool
	SSHUser          string
	SSHPort          int
	KnownHosts       string
	PlaybookMappings map[OperationKind]string
	HasCredential    bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// AutomationCredential is write-only API input and encrypted repository output.
type AutomationCredential struct {
	SSHPrivateKey  string `json:"sshPrivateKey"`
	BecomePassword string `json:"becomePassword,omitempty"`
}

var (
	ErrAutomationConfigNotFound    = errors.New("automation configuration not found")
	ErrAutomationDisabled          = errors.New("automation is disabled for this site")
	ErrAutomationCredentialMissing = errors.New("automation credential is not configured")
	ErrPlaybookNotAllowed          = errors.New("playbook is not registered in the release manifest")
	ErrExecutionLeaseLost          = errors.New("execution lease is no longer owned by this dispatcher")
)

// ExecutionListFilter narrows execution-operation listings.
type ExecutionListFilter struct {
	SiteID     string
	ClusterID  string
	ServerID   string
	Kind       OperationKind
	Status     Status
	ActiveOnly bool
	Offset     int
	Limit      int
}

// ExecutionListResult is a page of execution operations.
type ExecutionListResult struct {
	Operations []*ExecutionOperation
	Total      int
}

// ExecutionRepository persists locally owned operations and their leases.
type ExecutionRepository interface {
	Create(ctx context.Context, operation *ExecutionOperation) error
	FindByID(ctx context.Context, id string) (*ExecutionOperation, error)
	List(ctx context.Context, filter ExecutionListFilter) (ExecutionListResult, error)
	FindActiveByServerIDs(ctx context.Context, serverIDs []string) ([]*ExecutionOperation, error)
	Claim(ctx context.Context, id, owner string, leaseExpiresAt time.Time) (bool, error)
	UpdateExecution(ctx context.Context, id, owner string, execution Execution) error
	MarkExpiredIndeterminate(ctx context.Context, now time.Time) (int64, error)
}

// SiteLeaseRepository makes the one-active-run-per-site rule atomic.
type SiteLeaseRepository interface {
	Acquire(ctx context.Context, siteID, owner string, expiresAt time.Time) (bool, error)
	Renew(ctx context.Context, siteID, owner string, expiresAt time.Time) error
	Release(ctx context.Context, siteID, owner string) error
	ReleaseExpired(ctx context.Context, now time.Time) error
}

// AutomationConfigurationRepository stores site settings and sealed credentials.
type AutomationConfigurationRepository interface {
	FindBySiteID(ctx context.Context, siteID string) (*AutomationConfiguration, error)
	Upsert(ctx context.Context, configuration *AutomationConfiguration) error
	ReplaceCredential(ctx context.Context, siteID string, credential AutomationCredential) error
	Credential(ctx context.Context, siteID string) (AutomationCredential, error)
}

// PlaybookCatalog resolves only release-manifest entries.
type PlaybookCatalog interface {
	Resolve(name string) (string, error)
}

// RunnerInput contains all ephemeral material required for one local run.
type RunnerInput struct {
	Operation     *ExecutionOperation
	Configuration *AutomationConfiguration
	Credential    AutomationCredential
	Inventory     map[string]any
	PlaybookPath  string
}

// Runner owns local ansible-runner process execution and persistent artifacts.
type Runner interface {
	Run(ctx context.Context, input RunnerInput) error
	Logs(ctx context.Context, runID string) (string, error)
}
