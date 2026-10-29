package domain

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// PlatformClaim is the Swallow-owned record that a Platform deployment Swallow accepted targets
// this Server (glossary Platform Claim, decision 067). It is written when the deployment is
// accepted and cleared when that Platform's uninstall succeeds or its record is deleted, so it
// survives a failed deployment or a failed uninstall: the hosts may still carry platform state.
//
// It is intent, not observation. Membership is the Platform runtime's own answer and can be
// absent (a deployment that never joined, a Slurm cluster without slurmrestd, a failing sync),
// which is exactly when a claim must still stop the Server from being released or claimed again.
// The Platform's name is deliberately not stored, because a Platform can be renamed.
type PlatformClaim struct {
	PlatformID string
	ClaimedAt  time.Time
}

// ErrServerClaimed means a Server is already claimed by a different Platform.
var ErrServerClaimed = errors.New("server is claimed by another platform")

// ServerClaimedError names the Server and the Platform that already holds its claim. It
// unwraps to ErrServerClaimed.
type ServerClaimedError struct {
	ServerID   string
	PlatformID string
}

func (e *ServerClaimedError) Error() string {
	return fmt.Sprintf("server %s is claimed by platform %s", e.ServerID, e.PlatformID)
}

func (e *ServerClaimedError) Unwrap() error { return ErrServerClaimed }

// PlatformClaimStore persists Platform Claims. It is separate from ServerRepository so the many
// repository fakes need not implement it; the production Mongo repository implements both on the
// same document, and provider reconciliation never writes the claim.
//
// Implementations must make each Server's claim a conditional write, so two deployments accepted
// at the same time cannot both claim one Server.
type PlatformClaimStore interface {
	// ClaimForPlatform claims every Server in serverIDs for platformID. A Server that is
	// unclaimed is claimed at `at`; one already claimed by platformID is left as it is. A Server
	// claimed by another Platform fails the call with *ServerClaimedError, and an unknown id with
	// ErrServerNotFound; either way the claims this call wrote are released again, so a refused
	// deployment leaves no partial claim behind.
	ClaimForPlatform(ctx context.Context, platformID string, serverIDs []string, at time.Time) error
	// ReleasePlatformClaims clears the claim of every Server claimed by platformID. It is
	// idempotent: a Platform that claims nothing is a success.
	ReleasePlatformClaims(ctx context.Context, platformID string) error
	// ClaimingPlatformIDs lists every Platform id that claims at least one Server, so the startup
	// reconciliation can drop claims whose Platform is gone or already uninstalled.
	ClaimingPlatformIDs(ctx context.Context) ([]string, error)
	// ClaimedServerIDs lists the Servers platformID claims (none is an empty result, not an
	// error). A successful uninstall reads them before releasing the claims, to clean up what the
	// uninstall removed from those Servers.
	ClaimedServerIDs(ctx context.Context, platformID string) ([]string, error)
}
