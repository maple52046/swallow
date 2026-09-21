package application

import (
	"context"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// DeleteOSImageUseCase removes a provider-owned OS image from one provisioner and prunes any
// swallow name overlay for it.
//
// Scoped to an integration, like the catalog it deletes from: an image ID and architecture
// only identify an image within the provisioner that reported it. Deletion is an optional
// provider capability, so a provisioner without it is refused rather than silently no-op'd.
type DeleteOSImageUseCase struct {
	providers     provisioningdomain.ProviderFactory
	overlays      provisioningdomain.OSImageOverlayRepository
	verifications provisioningdomain.OSImageVerificationRepository
}

// NewDeleteOSImageUseCase wires the provider factory plus the swallow overlay and verification
// stores so a deleted image does not leave an orphan name or verification behind.
func NewDeleteOSImageUseCase(
	providers provisioningdomain.ProviderFactory,
	overlays provisioningdomain.OSImageOverlayRepository,
	verifications provisioningdomain.OSImageVerificationRepository,
) *DeleteOSImageUseCase {
	return &DeleteOSImageUseCase{providers: providers, overlays: overlays, verifications: verifications}
}

// Execute deletes the image identified by imageID and architecture from the named provisioner,
// then removes its swallow overlay.
//
// The provider deletion is authoritative and runs first; only after it succeeds is the overlay
// pruned. The image is then gone from the catalog, so its overlay could never be merged again,
// and Delete treats an already-absent overlay as success, so pruning is safe even for an image
// that was never renamed. An overlay-prune failure is returned rather than swallowed so the
// orphan is not hidden.
func (uc *DeleteOSImageUseCase) Execute(ctx context.Context, integrationID, imageID, architecture string) error {
	provider, err := uc.providers.For(ctx, integrationID)
	if err != nil {
		return err
	}

	remover, ok := provider.(provisioningdomain.OSImageRemover)
	if !ok {
		return unsupported("os image deletion")
	}
	if err := remover.DeleteOSImage(ctx, imageID, architecture); err != nil {
		return err
	}
	if err := uc.overlays.Delete(ctx, integrationID, imageID, architecture); err != nil {
		return err
	}
	// The image is gone from the catalog, so its verification could never be merged again; prune it
	// too. Delete treats an already-absent record as success, so this is safe for an image that was
	// never verified.
	return uc.verifications.Delete(ctx, integrationID, imageID, architecture)
}
