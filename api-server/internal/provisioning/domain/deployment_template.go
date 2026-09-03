package domain

import (
	"context"
	"errors"
	"time"
)

// DeploymentTemplate is reusable OS deployment intent owned by swallow.
//
// User data is deliberately absent. Its presence is exposed as a boolean while the
// plaintext can only be requested explicitly by the deployment use case.
type DeploymentTemplate struct {
	ID             string
	IntegrationID  string
	Name           string
	Description    string
	ImageID        string
	Ephemeral      bool
	NetworkMode    DeploymentNetworkMode
	SubnetID       string
	DefaultGateway bool
	HasUserData    bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// DeploymentTemplateFilter narrows a template listing. Zero values mean no constraint.
type DeploymentTemplateFilter struct {
	IntegrationID  string
	IntegrationIDs []string
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
}

// ProvisionerIntegration is the narrow integration view provisioning needs.
type ProvisionerIntegration struct {
	ID     string
	SiteID string
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
	// ErrDeploymentTemplateNameTaken preserves case-insensitive uniqueness per integration.
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
