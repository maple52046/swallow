package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// AttachPlatformClaims wires the Platform Claim store (decision 067). With it, Delete and a
// successful uninstall release the Platform's claims, and ReconcilePlatformClaims can repair
// them. Every process that deletes Platforms or completes uninstalls must wire it, or released
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
// concurrent deployment) is reported in the same words as the validation refusal, so the operator
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

// ReconcilePlatformClaims backfills the stored Platform Claims from the Platforms' durable
// lifecycle (decision 067). It is run once when the API starts. It is what turns the claims of
// Platforms deployed before claims existed into stored ones, and what drops a claim left behind by
// an installation from before acceptance, Delete, and uninstall became transactions (decision
// 069). It is not a repair loop: since then a claim is written and released only together with
// the Platform change it belongs to, so a new stale claim does not arise.
//
// It works in both directions and never fights a live decision:
//   - every target of a Swallow-deployed Platform that is not uninstalled gets that Platform's
//     claim when it has none (a Server claimed by another Platform is logged and left alone, and
//     a Server that no longer exists is skipped);
//   - a claim naming a Platform whose record no longer exists, or whose lifecycle is
//     uninstalled, is released. A Platform whose deployment was accepted a moment ago has claims
//     but may not have its Operation yet, so any other existing Platform keeps its claims.
//
// Errors reading Platforms or lifecycle abort the pass, leaving claims as they were; a failed
// write to one Platform's claims is logged and the pass continues.
func (s *PlatformService) ReconcilePlatformClaims(ctx context.Context) error {
	if s.claims == nil || s.lifecycle == nil {
		return nil
	}
	platforms, err := s.platforms.List(ctx, "")
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(platforms))
	for _, platform := range platforms {
		ids = append(ids, platform.ID)
	}
	snapshots, err := s.lifecycle.Read(ctx, ids)
	if err != nil {
		return err
	}
	// keepsClaims is every existing Platform that may hold claims: all but the uninstalled ones.
	keepsClaims := map[string]bool{}
	for _, platform := range platforms {
		snapshot, ok := snapshots[platform.ID]
		if ok && snapshot.State == platformdomain.PlatformLifecycleUninstalled {
			continue
		}
		keepsClaims[platform.ID] = true
		if !ok || snapshot.Origin != platformdomain.PlatformOriginDeployed || snapshot.Deployment == nil {
			continue
		}
		for _, serverID := range snapshot.Deployment.TargetServerIDs {
			// One Server at a time, so a deleted or contested Server does not stop the others.
			err := s.claims.ClaimForPlatform(ctx, platform.ID, []string{serverID}, snapshot.Deployment.RequestedAt)
			switch {
			case err == nil, errors.Is(err, serverdomain.ErrServerNotFound):
			case errors.Is(err, serverdomain.ErrServerClaimed):
				slog.Warn("platform claim reconcile left a server claimed by another platform",
					"platformId", platform.ID, "serverId", serverID, "error", err)
			default:
				slog.Error("platform claim reconcile could not claim a server",
					"platformId", platform.ID, "serverId", serverID, "error", err)
			}
		}
	}
	held, err := s.claims.ClaimingPlatformIDs(ctx)
	if err != nil {
		return err
	}
	for _, platformID := range held {
		if keepsClaims[platformID] {
			continue
		}
		if err := s.claims.ReleasePlatformClaims(ctx, platformID); err != nil {
			slog.Error("platform claim reconcile could not release a stale claim", "platformId", platformID, "error", err)
		}
	}
	return nil
}
