package application

import (
	"context"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// RefusePlatformClaimed returns *ServerClaimedByPlatformError when server carries a Platform
// Claim, and nil otherwise (decision 067). It is the single rule every operator path that would
// take a node out from under its Platform applies: direct release, the release Operation, Recover
// of a deployed Server, and Server Delete; action selects the refusal's next step. names may be
// nil, in which case the Platform is named by id.
func RefusePlatformClaimed(
	ctx context.Context,
	names provisioningdomain.PlatformNames,
	server *serverdomain.Server,
	action provisioningdomain.ClaimRefusedAction,
) error {
	if server.PlatformClaim == nil {
		return nil
	}
	refusal := &provisioningdomain.ServerClaimedByPlatformError{
		Server: server.DisplayName(), PlatformID: server.PlatformClaim.PlatformID, Action: action,
	}
	if names != nil {
		if name, platformType, err := names.PlatformName(ctx, server.PlatformClaim.PlatformID); err == nil {
			refusal.PlatformName, refusal.PlatformType = name, platformType
		}
	}
	return refusal
}
