package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// AttachPlatformClaims wires the Platform Claim store (decision 067). With it, Delete and a
// successful uninstall release the Platform's claims. Every process that deletes Platforms or completes uninstalls must wire it, or released
// Platforms keep blocking their Servers.
func (s *PlatformService) AttachPlatformClaims(store serverdomain.PlatformClaimStore) {
	s.claims = store
}

// AttachPlatformClaims wires the Platform Claim store so an accepted deployment claims its
// targets inside the transaction that accepts it. Without it (tests) no claim is written.
func (s *DeployService) AttachPlatformClaims(store serverdomain.PlatformClaimStore) {
	s.claims = store
}

// releaseClaims clears every claim platformID holds; without a store there is nothing to clear.
func (s *PlatformService) releaseClaims(ctx context.Context, platformID string) error {
	if s.claims == nil {
		return nil
	}
	return s.claims.ReleasePlatformClaims(ctx, platformID)
}

// claimTargets claims targetIDs (exactly the launch's target set) for the Platform just created;
// targets supplies display names. A target claimed by another Platform since validation (a
// concurrent deployment) is reported in the same words as the validation refusal, so the user
// sees one explanation for both orderings.
func (s *DeployService) claimTargets(ctx context.Context, platform *platformdomain.Platform, targetIDs []string, targets []*serverdomain.Server) error {
	if s.claims == nil {
		return nil
	}
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
