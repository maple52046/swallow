package application

import (
	"context"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// DeleteOSImageUseCase removes a provider-owned OS image from one provisioner.
//
// Scoped to an integration, like the catalog it deletes from: an image ID and architecture
// only identify an image within the provisioner that reported it. Deletion is an optional
// provider capability, so a provisioner without it is refused rather than silently
// no-op'd.
type DeleteOSImageUseCase struct {
	providers provisioningdomain.ProviderFactory
}

func NewDeleteOSImageUseCase(providers provisioningdomain.ProviderFactory) *DeleteOSImageUseCase {
	return &DeleteOSImageUseCase{providers: providers}
}

// Execute deletes the image identified by imageID and architecture from the named
// provisioner.
func (uc *DeleteOSImageUseCase) Execute(ctx context.Context, integrationID, imageID, architecture string) error {
	provider, err := uc.providers.For(ctx, integrationID)
	if err != nil {
		return err
	}

	remover, ok := provider.(provisioningdomain.OSImageRemover)
	if !ok {
		return unsupported("os image deletion")
	}
	return remover.DeleteOSImage(ctx, imageID, architecture)
}
