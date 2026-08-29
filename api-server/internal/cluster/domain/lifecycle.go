package domain

import (
	"context"
	"errors"
	"time"
)

// ClusterOrigin identifies whether Swallow registered a cluster or deployed it.
type ClusterOrigin string

const (
	ClusterOriginRegistered ClusterOrigin = "registered"
	ClusterOriginDeployed   ClusterOrigin = "deployed"
)

// ClusterLifecycleState is the durable operation-derived lifecycle projection.
type ClusterLifecycleState string

const (
	ClusterLifecycleRegistered      ClusterLifecycleState = "registered"
	ClusterLifecycleDeploying       ClusterLifecycleState = "deploying"
	ClusterLifecycleDeployFailed    ClusterLifecycleState = "deploy_failed"
	ClusterLifecycleActive          ClusterLifecycleState = "active"
	ClusterLifecycleUninstalling    ClusterLifecycleState = "uninstalling"
	ClusterLifecycleUninstallFailed ClusterLifecycleState = "uninstall_failed"
	ClusterLifecycleUninstalled     ClusterLifecycleState = "uninstalled"
)

// LifecycleOperation is the narrow operation projection the cluster context needs.
type LifecycleOperation struct {
	ID              string
	Status          string
	TargetServerIDs []string
	RequestedAt     time.Time
}

// LifecycleSnapshot describes a cluster using durable deployment and uninstall history.
type LifecycleSnapshot struct {
	Origin      ClusterOrigin
	State       ClusterLifecycleState
	OperationID string
	Deployment  *LifecycleOperation
	Uninstall   *LifecycleOperation
}

// LifecycleReader resolves operation history in one batch without exposing operation models.
type LifecycleReader interface {
	Read(ctx context.Context, clusterIDs []string) (map[string]LifecycleSnapshot, error)
}

// ManagedIntegrationCleaner deletes only a credential integration Swallow owns.
type ManagedIntegrationCleaner interface {
	DeleteForCluster(ctx context.Context, cluster *Cluster, allowLegacySignature bool) error
}

// UninstallLaunch is the validated intent handed to the operation context.
type UninstallLaunch struct {
	Cluster            *Cluster
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
	ErrClusterNotDeployManaged   = errors.New("cluster was not deployed by Swallow")
	ErrClusterAlreadyUninstalled = errors.New("cluster is already uninstalled")
	ErrClusterUninstallConflict  = errors.New("cluster cannot be uninstalled in its current state")
)
