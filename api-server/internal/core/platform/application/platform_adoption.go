package application

import (
	"fmt"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// AdoptedSpec builds the spec of a Platform deployed before Platforms had specs from its latest
// deployment record and the lifecycle of its Workflows, and returns what was applied with it. A Platform that is active, uninstalling, or whose uninstall failed has its spec applied;
// one whose deployment is running or failed has nothing applied yet; an uninstalled one is adopted
// absent with nothing applied. A Platform without a deployment record (one registered rather than
// deployed) is adopted with no members and as applied. It performs no I/O.
func AdoptedSpec(
	module platformdomain.PlatformModule,
	platform *platformdomain.Platform,
	snapshot platformdomain.LifecycleSnapshot,
	record *platformdomain.DeploymentRecord,
) (platformdomain.PlatformSpec, *platformdomain.PlatformSpec, error) {
	if record == nil {
		spec := platformdomain.PlatformSpec{Desired: platformdomain.PlatformPresent, Settings: map[string]any{}}
		applied := spec.Clone()
		return spec, &applied, nil
	}
	spec, err := module.SpecFromDeployRequest(record.Request)
	if err != nil {
		return platformdomain.PlatformSpec{}, nil, fmt.Errorf("adopt platform %s: %w", platform.ID, err)
	}
	spec.MachinePreparation = record.MachinePreparation
	applied := spec.Clone()
	switch snapshot.State {
	case platformdomain.PlatformLifecycleActive:
		return spec, &applied, nil
	case platformdomain.PlatformLifecycleUninstalling, platformdomain.PlatformLifecycleUninstallFailed:
		spec.Desired = platformdomain.PlatformAbsent
		return spec, &applied, nil
	case platformdomain.PlatformLifecycleUninstalled:
		spec.Desired = platformdomain.PlatformAbsent
		return spec, nil, nil
	default:
		return spec, nil, nil
	}
}
