package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// claimTargets claims targetIDs (exactly the launch's target set) for the Platform just created;
// targets supplies display names. A target claimed by another Platform since validation (a
// concurrent deployment) is reported in the same words as the validation refusal, so the user
// sees one explanation for both orderings.
func (s *DeployService) claimTargets(ctx context.Context, platform *platformdomain.Platform, targetIDs []string, targets []*serverdomain.Server) error {
	err := s.claims.ClaimForPlatform(ctx, platform.ID, targetIDs, time.Now().UTC())
	var claimed *serverdomain.ServerClaimedError
	if errors.As(err, &claimed) {
		name := claimed.ServerID
		for _, target := range targets {
			if target.ID == claimed.ServerID {
				name = target.DisplayName()
			}
		}
		holder := claimed.PlatformID
		if existing, findErr := s.platforms.FindByID(ctx, claimed.PlatformID); findErr == nil {
			holder = fmt.Sprintf("%s Platform %s", existing.Type, existing.Name)
		}
		return fmt.Errorf("%w: server %s is already claimed by %s", platformdomain.ErrInvalidDeployment, name, holder)
	}
	return err
}

// claimedBy names the Platform that claims server for a refusal message: its type and name when
// the record is known, its id otherwise.
func claimedBy(server *serverdomain.Server, platformsByID map[string]*platformdomain.Platform) string {
	if existing := platformsByID[server.PlatformClaim.PlatformID]; existing != nil {
		return fmt.Sprintf("%s Platform %s", existing.Type, existing.Name)
	}
	return "Platform " + server.PlatformClaim.PlatformID
}
