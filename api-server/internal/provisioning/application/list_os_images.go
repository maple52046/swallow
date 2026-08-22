package application

import (
	"context"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// OSImageItem is the API representation of a deployable OS image.
type OSImageItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	OSSystem     string `json:"osSystem"`
	Release      string `json:"release"`
	Architecture string `json:"architecture"`
}

// ListOSImagesUseCase reads the images one provisioner can currently deploy.
//
// Scoped to an integration rather than fleet-wide: two sites can have different images
// synced, so a merged list would offer an operator images their target site cannot
// actually deploy.
type ListOSImagesUseCase struct {
	providers provisioningdomain.ProviderFactory
}

func NewListOSImagesUseCase(providers provisioningdomain.ProviderFactory) *ListOSImagesUseCase {
	return &ListOSImagesUseCase{providers: providers}
}

func (uc *ListOSImagesUseCase) Execute(ctx context.Context, integrationID string) ([]OSImageItem, error) {
	provider, err := uc.providers.For(ctx, integrationID)
	if err != nil {
		return nil, err
	}

	images, err := provider.ListOSImages(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]OSImageItem, 0, len(images))
	for _, image := range images {
		items = append(items, OSImageItem{
			ID:           image.ID,
			Name:         image.Name,
			OSSystem:     image.OSSystem,
			Release:      image.Release,
			Architecture: image.Architecture,
		})
	}
	return items, nil
}
