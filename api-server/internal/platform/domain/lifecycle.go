package domain

import (
	"context"
	"errors"
	"time"
)

// PlatformOrigin identifies whether Swallow registered a platform or deployed it.
type PlatformOrigin string

const (
	PlatformOriginRegistered PlatformOrigin = "registered"
	PlatformOriginDeployed   PlatformOrigin = "deployed"
)

// PlatformLifecycleState is the durable operation-derived lifecycle projection.
type PlatformLifecycleState string

const (
	PlatformLifecycleRegistered      PlatformLifecycleState = "registered"
	PlatformLifecycleDeploying       PlatformLifecycleState = "deploying"
	PlatformLifecycleDeployFailed    PlatformLifecycleState = "deploy_failed"
	PlatformLifecycleActive          PlatformLifecycleState = "active"
	PlatformLifecycleUninstalling    PlatformLifecycleState = "uninstalling"
	PlatformLifecycleUninstallFailed PlatformLifecycleState = "uninstall_failed"
	PlatformLifecycleUninstalled     PlatformLifecycleState = "uninstalled"
)

// LifecycleOperation is the narrow operation projection the platform context needs.
type LifecycleOperation struct {
	ID              string
	Status          string
	TargetServerIDs []string
	RequestedAt     time.Time
	Intent          *LifecycleDeployment
}

// LifecycleDeployment is the non-secret deployment intent needed by Platform read models.
// It deliberately excludes the Operation extra-vars map and automation implementation.
type LifecycleDeployment struct {
	Topology        KubernetesTopology
	RoleAssignments []RoleAssignment
}

// LifecycleSnapshot describes a platform using durable deployment and uninstall history.
type LifecycleSnapshot struct {
	Origin      PlatformOrigin
	State       PlatformLifecycleState
	OperationID string
	Deployment  *LifecycleOperation
	Uninstall   *LifecycleOperation
}

// LifecycleReader resolves operation history in one batch without exposing operation models.
type LifecycleReader interface {
	Read(ctx context.Context, platformIDs []string) (map[string]LifecycleSnapshot, error)
}

// ManagedIntegrationCleaner deletes only a credential integration Swallow owns.
type ManagedIntegrationCleaner interface {
	DeleteForPlatform(ctx context.Context, platform *Platform, allowLegacySignature bool) error
}

// UninstallLaunch is the validated intent handed to the operation context.
type UninstallLaunch struct {
	Platform           *Platform
	TargetServerIDs    []string
	RestoreExporters   bool
	RetryOfOperationID string
	RequestedBy        string
}

// UninstallLauncher starts an uninstall operation without coupling contexts.
type UninstallLauncher interface {
	LaunchUninstall(ctx context.Context, launch UninstallLaunch) (string, error)
}

var (
	ErrPlatformNotDeployManaged   = errors.New("platform was not deployed by Swallow")
	ErrPlatformAlreadyUninstalled = errors.New("platform is already uninstalled")
	ErrPlatformUninstallConflict  = errors.New("platform cannot be uninstalled in its current state")
)
