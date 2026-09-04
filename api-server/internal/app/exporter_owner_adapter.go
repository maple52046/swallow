package app

import (
	"context"
	"errors"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// exporterOwnerResolver resolves a server's effective exporter owner from its lock state
// and platform policy, satisfying the operation context's ExporterOwnerResolver port.
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
