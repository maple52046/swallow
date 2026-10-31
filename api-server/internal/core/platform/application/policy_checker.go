package application

import (
	"context"
	"errors"
	"fmt"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// PolicyChecker refuses operations that contradict a platform's gpuStackOwner policy.
//
// It lives in the platform context because the policy does: the operation context must
// not need to know what a GPU operator is in order to refuse installing drivers behind
// one. It satisfies the operation context's PolicyChecker port.
type PolicyChecker struct {
	platforms platformdomain.PlatformRepository
	servers   serverdomain.ServerRepository
}

func NewPolicyChecker(
	platforms platformdomain.PlatformRepository,
	servers serverdomain.ServerRepository,
) *PolicyChecker {
	return &PolicyChecker{platforms: platforms, servers: servers}
}

// CheckOperation returns ErrPolicyConflict when the operation would fight a platform over
// something that platform owns.
func (c *PolicyChecker) CheckOperation(
	ctx context.Context,
	kind workflowdomain.WorkflowKind,
	platformID string,
	serverIDs []string,
) error {
	if (kind == workflowdomain.WorkflowKindUninstallKubernetes ||
		kind == workflowdomain.WorkflowKindUninstallSlurm) && platformID == "" {
		return fmt.Errorf("%w: %s requires a platformId",
			workflowdomain.ErrPolicyConflict, kind)
	}
	if (kind == workflowdomain.WorkflowKindDeployKubernetes ||
		kind == workflowdomain.WorkflowKindUninstallKubernetes ||
		kind == workflowdomain.WorkflowKindUninstallSlurm) && platformID != "" {
		if _, err := c.platforms.FindByID(ctx, platformID); err != nil {
			if errors.Is(err, platformdomain.ErrPlatformNotFound) {
				return fmt.Errorf("%w: platform %s no longer exists",
					workflowdomain.ErrPolicyConflict, platformID)
			}
			return err
		}
	}

	// Only driver installation is contested today. Deploying a platform or configuring
	// Slurm does not touch anything a GPU operator manages.
	if kind != workflowdomain.WorkflowKindInstallGPUDriver {
		return nil
	}

	for _, serverID := range serverIDs {
		server, err := c.servers.FindByID(ctx, serverID)
		if err != nil {
			return err
		}
		if server.Membership == nil || server.Membership.PlatformID == "" {
			// Not in a platform, so no platform policy applies. Installing a driver on
			// a spare is exactly what the provisioning-owned mode is for.
			continue
		}

		platform, err := c.platforms.FindByID(ctx, server.Membership.PlatformID)
		if errors.Is(err, platformdomain.ErrPlatformNotFound) {
			// The membership axis outlived its platform registration. Nothing to
			// enforce, and the membership sync will clear it.
			continue
		}
		if err != nil {
			return err
		}

		if platform.GPUStackOwner == platformdomain.GPUStackOwnerGPUOperator {
			return fmt.Errorf(
				"%w: %s belongs to platform %q, where the GPU operator owns drivers and DCGM; "+
					"installing them from provisioning as well would leave two owners on the same host",
				workflowdomain.ErrPolicyConflict, server.DisplayName(), platform.Name)
		}
	}

	return nil
}
