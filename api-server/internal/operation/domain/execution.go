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
	Kind            WorkflowKind
	Intent          string
	SiteID          string
	PlatformID      string
	TargetServerIDs []string
	ExtraVars       map[string]any
	// SecretVars are extra vars that must not persist in plain text, such as a VRRP
	// password. They are sealed at rest and materialised only for the run, so they are
	// populated at creation and by the dispatcher's dedicated read, and are nil on every
	// ordinary read. Never expose them in an API response.
	SecretVars map[string]any
	// RetryOfOperationID links this operation to the one it was created to retry, or is
	// empty when it was requested directly. The original is never modified.
	RetryOfOperationID string
	Execution          Execution
	RequestedBy        string
	RequestedAt        time.Time
	UpdatedAt          time.Time
}

// AutomationConfiguration is the single embedded-runner configuration for a site.
type AutomationConfiguration struct {
	SiteID           string
	Enabled          bool
	SSHUser          string
	SSHPort          int
	KnownHosts       string
	PlaybookMappings map[WorkflowKind]string
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
	SiteID      string
	PlatformID  string
	PlatformIDs []string
	ServerID    string
	Kind        WorkflowKind
	Kinds       []WorkflowKind
	Status      Status
	ActiveOnly  bool
	Offset      int
	Limit       int
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
	// SecretVars decrypts the sealed run-time secret vars for one operation. It is called
	// by the dispatcher immediately before a run and nowhere else, so that secret material
	// has exactly one read path rather than riding along on every list and get.
	SecretVars(ctx context.Context, id string) (map[string]any, error)
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
	// SecretVars are merged into the run's extra vars only inside the private runtime
	// directory, so a value like a VRRP password reaches the playbook without ever being
	// persisted in the operation record or an artifact.
	SecretVars map[string]any
}

// RunnerResult is what a run produced beyond success or failure.
//
// Data is whatever the playbook deliberately wrote to the run's result file — a platform
// deployment returns its platform credential this way. It is read before the run's private
// directory is removed, so a secret in it never reaches stdout or a retained artifact. It
// is nil when the playbook wrote nothing.
type RunnerResult struct {
	Data map[string]any
}

// TaskEvent is one task result from a run, projected from the runner's own event stream.
//
// It is the unit of progress detail for a long multi-phase run. Host is a serverId,
// matching the inventory. No task output is carried, because a task result can contain a
// secret and the events are a public projection.
type TaskEvent struct {
	Play      string
	Task      string
	Host      string
	Status    string
	Changed   bool
	StartedAt *time.Time
	EndedAt   *time.Time
}

// Runner owns local ansible-runner process execution and persistent artifacts.
type Runner interface {
	// Run executes the playbook and returns whatever result the playbook captured. A
	// non-nil error means the run did not succeed; the result may still be empty on
	// success when the playbook captured nothing.
	Run(ctx context.Context, input RunnerInput) (RunnerResult, error)
	Logs(ctx context.Context, runID string) (string, error)
	// Events returns the task-level progress of a run from its retained artifacts, in the
	// order the runner emitted them. A run with no events yet returns an empty slice.
	Events(ctx context.Context, runID string) ([]TaskEvent, error)
	// Stderr returns an error-only report of a run: one block per failed or unreachable task
	// with its message, return code, and captured stderr/stdout. It is the focused counterpart
	// to Logs (the full runner output) and returns an empty string when the run recorded no
	// failure or its artifacts are absent. It reads retained artifacts, so it is available
	// after the run ends.
	Stderr(ctx context.Context, runID string) (string, error)
}
