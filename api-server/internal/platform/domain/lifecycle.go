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

// ServerReleaseOptions carries the provider-neutral release choices applied to each
// member server when an uninstall also releases them. They mirror the standalone Release
// action so an operator gets the same disk-erase and static-IP handling.
type ServerReleaseOptions struct {
	Erase           bool
	SecureErase     bool
	QuickErase      bool
	UnbindStaticIPs bool
}

// UninstallLaunch is the validated intent handed to the operation context.
//
// When ReleaseServers is true the operation additionally releases every target server
// back to the provider after k0s removal; ReleaseOptions then applies to each release.
// RestoreExporters and ReleaseServers are mutually exclusive: a released host is wiped, so
// there is nothing to restore exporters onto.
type UninstallLaunch struct {
	Platform           *Platform
	TargetServerIDs    []string
	RestoreExporters   bool
	ReleaseServers     bool
	ReleaseOptions     ServerReleaseOptions
	RetryOfOperationID string
	RequestedBy        string
}

// UninstallLauncher starts an uninstall operation without coupling contexts.
type UninstallLauncher interface {
	LaunchUninstall(ctx context.Context, launch UninstallLaunch) (string, error)
}

// PlatformOperationCanceler cancels a platform's in-flight durable operations so that
// deleting the platform frees its member servers instead of leaving orphaned work that
// keeps holding their resource leases. It is a port: the platform context requests the
// cancellation, and the operation context implements it. Cancellation is best understood
// as "make these operations terminal"; a run that has already finished is not an error.
type PlatformOperationCanceler interface {
	CancelActiveForPlatform(ctx context.Context, platformID string) error
}

var (
	ErrPlatformNotDeployManaged   = errors.New("platform was not deployed by Swallow")
	ErrPlatformAlreadyUninstalled = errors.New("platform is already uninstalled")
	ErrPlatformUninstallConflict  = errors.New("platform cannot be uninstalled in its current state")
)
