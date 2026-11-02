package app

import (
	"context"
	"errors"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// exporterOwnerResolver resolves a server's effective exporter owner (Exporter Ownership) from its
// lock state and platform policy. The software context asks it through the ExporterOwnership port
// before a standalone host-exporter install, and the OS deployment launchers before composing the
// Node Exporter.
//
// It lives in the app wiring layer because it bridges two contexts — the server's
// provisioning lock and a platform's exporterOwner policy — without either context
// depending on the other. The resolution order matters: a locked machine is off-limits
// regardless of any platform it belongs to.
type exporterOwnerResolver struct {
	platforms platformdomain.PlatformRepository
}

// EffectiveExporterOwner returns "unmanaged" for a locked machine, the platform's policy
// for a platform member, and "ansible" otherwise. A membership pointing at a platform that
// no longer exists falls back to ansible rather than erroring, matching how the policy
// checker treats a stale membership.
func (r exporterOwnerResolver) EffectiveExporterOwner(ctx context.Context, server *serverdomain.Server) (string, error) {
	if server.Provisioning != nil && server.Provisioning.Locked {
		return string(platformdomain.ExporterOwnerUnmanaged), nil
	}
	if server.Membership != nil && server.Membership.PlatformID != "" {
		platform, err := r.platforms.FindByID(ctx, server.Membership.PlatformID)
		if errors.Is(err, platformdomain.ErrPlatformNotFound) {
			return string(platformdomain.ExporterOwnerAnsible), nil
		}
		if err != nil {
			return "", err
		}
		return string(platform.ExporterOwner), nil
	}
	return string(platformdomain.ExporterOwnerAnsible), nil
}

// HostAutomationOwnsExporters implements softwareapp.ExporterOwnership: swallow's host automation
// owns the exporters of a Server whose effective owner is ansible.
func (r exporterOwnerResolver) HostAutomationOwnsExporters(ctx context.Context, server *serverdomain.Server) (bool, error) {
	owner, err := r.EffectiveExporterOwner(ctx, server)
	if err != nil {
		return false, err
	}
	return owner == string(platformdomain.ExporterOwnerAnsible), nil
}
