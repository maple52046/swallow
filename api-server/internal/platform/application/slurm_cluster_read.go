package application

import (
	"context"
	"fmt"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
)

// GetSlurmClusterUseCase reads a Slurm platform's live cluster state on demand.
//
// It is deliberately separate from membership sync: it does not write the Server membership
// axis or the platform sync counters, and it never runs on the reconcile interval. It powers
// the Slurm-specific management view, which needs controllers, partitions, and per-node Slurm
// state that the generic membership Member cannot carry. Callers get a typed domain error when
// the platform is not Slurm or has no integration, so the UI can degrade gracefully instead of
// showing a live view that cannot exist yet.
type GetSlurmClusterUseCase struct {
	platforms platformdomain.PlatformRepository
	readers   platformdomain.ReaderFactory
}

// NewGetSlurmClusterUseCase wires the platform repository and the same reader factory used by
// membership sync; the factory resolves the platform's slurmrestd integration into a reader.
func NewGetSlurmClusterUseCase(
	platforms platformdomain.PlatformRepository,
	readers platformdomain.ReaderFactory,
) *GetSlurmClusterUseCase {
	return &GetSlurmClusterUseCase{platforms: platforms, readers: readers}
}

// Execute reads the live Slurm cluster state for a platform.
//
// Errors: ErrPlatformNotFound when the id is unknown; ErrUnsupportedPlatformType when the
// platform is not Slurm (or its reader cannot read cluster state); ErrNoPlatformIntegration
// when slurmrestd is not recorded yet; and a platformdomain.ReaderError when slurmrestd is
// unreachable or rejects the credential. It performs no writes.
func (uc *GetSlurmClusterUseCase) Execute(ctx context.Context, platformID string) (*platformdomain.SlurmClusterState, error) {
	platform, err := uc.platforms.FindByID(ctx, platformID)
	if err != nil {
		return nil, err
	}
	if platform.Type != platformdomain.PlatformTypeSlurm {
		return nil, fmt.Errorf("%w: cluster state is only available for a Slurm platform", platformdomain.ErrUnsupportedPlatformType)
	}
	reader, err := uc.readers.For(ctx, platform)
	if err != nil {
		return nil, err
	}
	// The concrete Slurm reader implements the superset SlurmClusterReader; a reader that does
	// not (a misconfigured or non-Slurm integration) is a validation error, not a crash.
	clusterReader, ok := reader.(platformdomain.SlurmClusterReader)
	if !ok {
		return nil, fmt.Errorf("%w: the platform integration cannot read Slurm cluster state", platformdomain.ErrUnsupportedPlatformType)
	}
	return clusterReader.GetClusterState(ctx)
}
