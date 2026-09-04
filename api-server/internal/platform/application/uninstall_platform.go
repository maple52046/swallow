package application

import (
	"context"
	"errors"
	"fmt"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// UninstallService validates a platform and starts its idempotent k0s removal operation.
type UninstallService struct {
	platforms  platformdomain.PlatformRepository
	servers    serverdomain.ServerRepository
	lifecycle  platformdomain.LifecycleReader
	launcher   platformdomain.UninstallLauncher
	protection serverdomain.MutationGuard
}

// NewUninstallService constructs the platform uninstall use case.
func NewUninstallService(
	platforms platformdomain.PlatformRepository,
	servers serverdomain.ServerRepository,
	lifecycle platformdomain.LifecycleReader,
	launcher platformdomain.UninstallLauncher,
	protection ...serverdomain.MutationGuard,
) *UninstallService {
	service := &UninstallService{
		platforms: platforms, servers: servers, lifecycle: lifecycle, launcher: launcher,
	}
	if len(protection) > 0 {
		service.protection = protection[0]
	}
	return service
}

// UninstallPlatformInput identifies the platform and requesting operator.
//
// When ReleaseServers is true the uninstall also releases every member server back to the
// provider; ReleaseOptions then controls disk erasure and static-IP handling per server.
type UninstallPlatformInput struct {
	PlatformID     string
	RequestedBy    string
	ReleaseServers bool
	ReleaseOptions platformdomain.ServerReleaseOptions
}

// UninstallPlatformResult is the accepted uninstall operation.
type UninstallPlatformResult struct {
	PlatformID  string `json:"platformId"`
	OperationID string `json:"operationId"`
}

// Uninstall validates durable provenance and launches against the original target snapshot.
func (s *UninstallService) Uninstall(
	ctx context.Context,
	input UninstallPlatformInput,
) (*UninstallPlatformResult, error) {
	platform, err := s.platforms.FindByID(ctx, input.PlatformID)
	if err != nil {
		return nil, err
	}
	if platform.Type != platformdomain.PlatformTypeKubernetes {
		return nil, fmt.Errorf("%w: only Kubernetes platforms deployed by Swallow can be uninstalled",
			platformdomain.ErrPlatformNotDeployManaged)
	}
	if s.lifecycle == nil {
		return nil, platformdomain.ErrPlatformNotDeployManaged
	}
	snapshots, err := s.lifecycle.Read(ctx, []string{platform.ID})
	if err != nil {
		return nil, err
	}
	snapshot, ok := snapshots[platform.ID]
	if !ok || snapshot.Origin != platformdomain.PlatformOriginDeployed || snapshot.Deployment == nil {
		return nil, platformdomain.ErrPlatformNotDeployManaged
	}

	retryOf := ""
	switch snapshot.State {
	case platformdomain.PlatformLifecycleDeploying, platformdomain.PlatformLifecycleUninstalling:
		return nil, fmt.Errorf("%w: platform lifecycle is %s",
			platformdomain.ErrPlatformUninstallConflict, snapshot.State)
	case platformdomain.PlatformLifecycleUninstalled:
		return nil, platformdomain.ErrPlatformAlreadyUninstalled
	case platformdomain.PlatformLifecycleUninstallFailed:
		if snapshot.Uninstall != nil {
			retryOf = snapshot.Uninstall.ID
		}
	}

	targetIDs := append([]string(nil), snapshot.Deployment.TargetServerIDs...)
	if len(targetIDs) == 0 {
		return nil, fmt.Errorf("%w: deployment operation has no target snapshot",
			platformdomain.ErrPlatformUninstallConflict)
	}
	for _, targetID := range targetIDs {
		server, targetErr := s.servers.FindByID(ctx, targetID)
		if errors.Is(targetErr, serverdomain.ErrServerNotFound) {
			return nil, fmt.Errorf("%w: deployment target %s no longer exists",
				platformdomain.ErrPlatformUninstallConflict, targetID)
		}
		if targetErr != nil {
			return nil, targetErr
		}
		if server.Absent {
			return nil, fmt.Errorf("%w: deployment target %s is absent",
				platformdomain.ErrPlatformUninstallConflict, server.DisplayName())
		}
		if s.protection == nil && server.Provisioning != nil && server.Provisioning.Locked {
			return nil, &serverdomain.ServerLockedError{Name: server.DisplayName()}
		}
	}

	if s.protection != nil {
		if err := s.protection.RequireUnlocked(ctx, targetIDs); err != nil {
			return nil, err
		}
	}

	// Releasing a host wipes it, so exporter restoration is meaningless there; the two are
	// mutually exclusive. Only restore exporters when the hosts are kept.
	restoreExporters := platform.ExporterOwner == platformdomain.ExporterOwnerK8s && !input.ReleaseServers
	operationID, err := s.launcher.LaunchUninstall(ctx, platformdomain.UninstallLaunch{
		Platform: platform, TargetServerIDs: targetIDs,
		RestoreExporters:   restoreExporters,
		ReleaseServers:     input.ReleaseServers,
		ReleaseOptions:     input.ReleaseOptions,
		RetryOfOperationID: retryOf, RequestedBy: input.RequestedBy,
	})
	if err != nil {
		return nil, err
	}
	return &UninstallPlatformResult{PlatformID: platform.ID, OperationID: operationID}, nil
}
