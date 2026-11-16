package domain

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DeploymentTemplate is reusable OS deployment intent owned by swallow.
//
// User data is deliberately absent. Its presence is exposed as a boolean while the
// plaintext can only be requested explicitly by the deployment use case.
type DeploymentTemplate struct {
	ID            string
	IntegrationID string
	// PoolID is the Pool that owns the template: its members read and use it, its owners change it
	// (identity-and-access.md §4). Empty for a template created before templates had a Pool, which only
	// an admin reads until it is given one.
	PoolID string
	// CreatedBy is the username of whoever created the template, kept as a record: it never decides
	// access, and it stays as written when that User leaves the Pool or is deleted.
	CreatedBy      string
	Name           string
	Description    string
	ImageID        string
	Ephemeral      bool
	NetworkMode    DeploymentNetworkMode
	SubnetID       string
	DefaultGateway bool
	HasUserData    bool
	// MinimumHardware is the optional hardware a Server must have to be installed from the template;
	// nil sets no floor. It never describes exact hardware: a template installs any Server that meets it.
	MinimumHardware *MinimumHardware
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// MinimumHardware is a Deployment Template's optional hardware floor (glossary Deployment Template).
// Each zero field sets no floor for its dimension. GPUModel is matched against the Server's GPU
// Inventory model names (case-insensitively, as a substring), and GPUCount counts compute GPUs.
type MinimumHardware struct {
	CPUCores  int
	MemoryMiB int64
	StorageGB int
	GPUModel  string
	GPUCount  int
}

// Empty reports whether the floor constrains nothing.
func (m MinimumHardware) Empty() bool {
	return m.CPUCores == 0 && m.MemoryMiB == 0 && m.StorageGB == 0 && m.GPUModel == "" && m.GPUCount == 0
}

// Validate refuses a negative floor.
func (m MinimumHardware) Validate() error {
	if m.CPUCores < 0 || m.MemoryMiB < 0 || m.StorageGB < 0 || m.GPUCount < 0 {
		return fmt.Errorf("%w: minimumHardware values cannot be negative", ErrInvalidDeploymentTemplate)
	}
	return nil
}

// DeploymentTemplateReference is one use of a template by a Platform: a node group whose members
// are installed from it.
type DeploymentTemplateReference struct {
	PlatformID   string
	PlatformName string
	Group        string
}

// DeploymentTemplateReferenceReader lists the Platform node groups that install from templates, so a
// referenced template is not deleted and its readers see where it is used. It is a port: the Platform
// context owns the references, and the composition root supplies the adapter.
type DeploymentTemplateReferenceReader interface {
	TemplateReferences(ctx context.Context) (map[string][]DeploymentTemplateReference, error)
}

// DeploymentTemplateFilter narrows a template listing. Zero values mean no constraint, except that a
// non-nil PoolIDs restricts the listing to those Pools (an empty one lists nothing).
type DeploymentTemplateFilter struct {
	IntegrationID  string
	IntegrationIDs []string
	PoolIDs        []string
}

// DeploymentTemplateRepository persists deployment intent and seals its user data.
//
// The secret-specific methods make every plaintext read and write visible in review.
// Implementations must never persist the userData arguments as plaintext.
type DeploymentTemplateRepository interface {
	Create(ctx context.Context, template *DeploymentTemplate, userData string) error
	FindByID(ctx context.Context, id string) (*DeploymentTemplate, error)
	List(ctx context.Context, filter DeploymentTemplateFilter) ([]*DeploymentTemplate, error)
	Update(ctx context.Context, template *DeploymentTemplate) error
	Delete(ctx context.Context, id string) error
	ReplaceUserData(ctx context.Context, id, userData string) error
	ClearUserData(ctx context.Context, id string) error
	UserData(ctx context.Context, id string) (string, error)
	CountByIntegration(ctx context.Context, integrationID string) (int, error)
	// CountByPool counts the templates a Pool owns, which keep it from being deleted.
	CountByPool(ctx context.Context, poolID string) (int, error)
	// AssignPool gives the templates ids that do not already belong to poolID that Pool, and reports
	// how many changed.
	AssignPool(ctx context.Context, ids []string, poolID string) (int, error)
}

// PoolDirectory answers whether a Pool exists, so a template is never given a Pool that does not. It is
// a port: the infrastructure context owns Pools, and the composition root supplies the adapter.
type PoolDirectory interface {
	PoolExists(ctx context.Context, poolID string) (bool, error)
}

// ProvisionerIntegration is the narrow integration view provisioning needs.
type ProvisionerIntegration struct {
	ID     string
	SiteID string
	// ProviderKind selects the provisioner's capabilities that are not reached through its API,
	// such as the Boot ISO chain renderer.
	ProviderKind string
}

// ProvisionerIntegrationReader validates template ownership without importing the
// site context's internal model into application use cases.
type ProvisionerIntegrationReader interface {
	Find(ctx context.Context, id string) (*ProvisionerIntegration, error)
	ListBySite(ctx context.Context, siteID string) ([]ProvisionerIntegration, error)
}

var (
	// ErrDeploymentTemplateNotFound means the template ID is unknown.
	ErrDeploymentTemplateNotFound = errors.New("deployment template not found")
	// ErrDeploymentTemplateInUse means a Platform node group still installs from the template.
	ErrDeploymentTemplateInUse = errors.New("deployment template is used by a Platform")
	// ErrDeploymentTemplateNameTaken preserves case-insensitive uniqueness per Integration and Pool.
	ErrDeploymentTemplateNameTaken = errors.New("deployment template name already exists")
	// ErrDeploymentTemplateUserDataMissing means no cloud-init content is stored.
	ErrDeploymentTemplateUserDataMissing = errors.New("deployment template has no user data")
	// ErrInvalidDeploymentTemplate marks input validation failures.
	ErrInvalidDeploymentTemplate = errors.New("invalid deployment template")
	// ErrInvalidDeploymentBatch marks malformed or incompatible deployment input.
	ErrInvalidDeploymentBatch = errors.New("invalid deployment batch")
	// ErrDeploymentBatchConflict marks targets whose current state makes deployment unsafe.
	ErrDeploymentBatchConflict = errors.New("deployment batch conflict")
)
