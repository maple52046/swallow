package application

import (
	"context"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// OSImageItem is the API representation of a deployable OS image.
//
// Name, OSSystem, and Release are the effective values a user sees: the swallow overlay value
// when one is set, otherwise the provider's own value. The provider* fields always carry the
// provider's original values, and the custom* fields carry the swallow overrides (empty when
// none), so a client can prefill an edit form with the provider defaults and offer to reset an
// override. See docs/decisions/025 (Provider Data Overlay). The overlay never changes the
// deployable image identity (ID); only display labels are overridable.
type OSImageItem struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ProviderName     string `json:"providerName"`
	CustomName       string `json:"customName,omitempty"`
	OSSystem         string `json:"osSystem"`
	ProviderOSSystem string `json:"providerOsSystem"`
	CustomOSSystem   string `json:"customOsSystem,omitempty"`
	Release          string `json:"release"`
	ProviderRelease  string `json:"providerRelease"`
	CustomRelease    string `json:"customRelease,omitempty"`
	// Tags are swallow-owned labels (no provider counterpart). Always a non-nil array so
	// clients can iterate without a null check; empty when the image has no tags.
	Tags         []string `json:"tags"`
	Architecture string   `json:"architecture"`
}

// ListOSImagesUseCase reads the images one provisioner can currently deploy and merges the
// swallow-owned overlay onto them.
//
// Scoped to an integration rather than fleet-wide: two sites can have different images synced,
// so a merged list would offer an operator images their target site cannot actually deploy —
// and the overlay is likewise integration-scoped. The provider stays authoritative for the
// catalog; the overlay only supplies swallow-chosen display values where they exist
// (docs/decisions/025).
type ListOSImagesUseCase struct {
	providers provisioningdomain.ProviderFactory
	overlays  provisioningdomain.OSImageOverlayRepository
}

// NewListOSImagesUseCase wires the provider factory and the swallow overlay store the catalog
// read merges together.
func NewListOSImagesUseCase(
	providers provisioningdomain.ProviderFactory,
	overlays provisioningdomain.OSImageOverlayRepository,
) *ListOSImagesUseCase {
	return &ListOSImagesUseCase{providers: providers, overlays: overlays}
}

// Execute lists the provider catalog and overlays swallow display values on it.
//
// The provider read is the source of truth for which images exist and their provider values; an
// overlay whose image is no longer in the catalog is simply not emitted, so a stale override
// never invents an image. A failure reading the provider is returned as-is; a failure reading
// overlays is also returned rather than silently dropping overrides, because a partial merge
// would misrepresent what the operator configured.
func (uc *ListOSImagesUseCase) Execute(ctx context.Context, integrationID string) ([]OSImageItem, error) {
	provider, err := uc.providers.For(ctx, integrationID)
	if err != nil {
		return nil, err
	}

	images, err := provider.ListOSImages(ctx)
	if err != nil {
		return nil, err
	}

	overlays, err := uc.overlays.ListByIntegration(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	// Index overlays by the provider image identity so the merge is a single pass rather than
	// a scan per image. One image name can back several architectures, so the key includes both.
	byKey := make(map[string]*provisioningdomain.OSImageOverlay, len(overlays))
	for _, overlay := range overlays {
		byKey[osImageOverlayKey(overlay.ImageID, overlay.Architecture)] = overlay
	}

	items := make([]OSImageItem, 0, len(images))
	for _, image := range images {
		item := OSImageItem{
			ID:               image.ID,
			Name:             image.Name,
			ProviderName:     image.Name,
			OSSystem:         image.OSSystem,
			ProviderOSSystem: image.OSSystem,
			Release:          image.Release,
			ProviderRelease:  image.Release,
			Tags:             []string{},
			Architecture:     image.Architecture,
		}
		// Overlay precedence is one-directional and per field: a non-empty swallow value becomes
		// the effective value while the provider value stays visible as provider*. Tags have no
		// provider counterpart, so they are taken from the overlay as-is when present.
		if overlay := byKey[osImageOverlayKey(image.ID, image.Architecture)]; overlay != nil {
			if overlay.DisplayName != "" {
				item.Name = overlay.DisplayName
				item.CustomName = overlay.DisplayName
			}
			if overlay.OSSystem != "" {
				item.OSSystem = overlay.OSSystem
				item.CustomOSSystem = overlay.OSSystem
			}
			if overlay.Release != "" {
				item.Release = overlay.Release
				item.CustomRelease = overlay.Release
			}
			if len(overlay.Tags) > 0 {
				item.Tags = overlay.Tags
			}
		}
		items = append(items, item)
	}
	return items, nil
}

// osImageOverlayKey joins the two halves of an image's overlay identity with a separator that
// cannot appear in an image ID or architecture, so distinct pairs never collide in the lookup.
func osImageOverlayKey(imageID, architecture string) string {
	return imageID + "\x00" + architecture
}
