package application

import (
	"context"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// OSImageItem is one provider image as the catalog API reports it (provisioning.md, decision 071):
// its identity, the metadata swallow owns for it (Spec), and what the provider and swallow's own
// verification report (Status). The metadata needs no provider realization, so the item has no
// generation or conditions.
type OSImageItem struct {
	ID           string              `json:"id"`
	Architecture string              `json:"architecture"`
	Spec         OSImageMetadataItem `json:"spec"`
	Status       OSImageStatusItem   `json:"status"`
}

// OSImageMetadataItem is the swallow-owned metadata of one image. An empty string means "use the
// provider's value"; Tags and DefaultUser have no provider counterpart. Tags is never nil.
type OSImageMetadataItem struct {
	Name        string   `json:"name"`
	OSSystem    string   `json:"osSystem"`
	Release     string   `json:"release"`
	Tags        []string `json:"tags"`
	DefaultUser string   `json:"defaultUser"`
}

// OSImageStatusItem is what is effectively shown and used for the image, and the facts behind it.
// Name, OSSystem, and Release are the metadata value when set, else the provider's; DefaultUser is
// the metadata value, else swallow's built-in for the provider OS family
// (provisioningdomain.EffectiveDefaultUser), and omitted when none applies. The provider* fields
// are the provider's own values. SizeBytes is omitted when the provider reports no complete size.
// VerifiedDeployTargets and FailedDeployTargets are swallow's verification attestations ("disk",
// "ram"), never nil; a target is in at most one of them.
type OSImageStatusItem struct {
	Name                  string   `json:"name"`
	OSSystem              string   `json:"osSystem"`
	Release               string   `json:"release"`
	DefaultUser           string   `json:"defaultUser,omitempty"`
	ProviderName          string   `json:"providerName"`
	ProviderOSSystem      string   `json:"providerOsSystem"`
	ProviderRelease       string   `json:"providerRelease"`
	SizeBytes             int64    `json:"sizeBytes,omitempty"`
	VerifiedDeployTargets []string `json:"verifiedDeployTargets"`
	FailedDeployTargets   []string `json:"failedDeployTargets"`
}

// ListOSImagesUseCase reads the images one provisioner can currently deploy and merges the
// swallow-owned metadata onto them.
//
// Scoped to an integration rather than fleet-wide: two sites can have different images synced,
// so a merged list would offer an operator images their target site cannot actually deploy —
// and the metadata is likewise integration-scoped. The provider stays authoritative for the
// catalog; the metadata only supplies swallow-chosen values where they exist (decision 071).
type ListOSImagesUseCase struct {
	providers       provisioningdomain.ProviderFactory
	metadataRecords provisioningdomain.OSImageMetadataRepository
	verifications   provisioningdomain.OSImageVerificationRepository
}

// NewListOSImagesUseCase wires the provider factory, the swallow display metadata, and the swallow
// verification store the catalog read merges together.
func NewListOSImagesUseCase(
	providers provisioningdomain.ProviderFactory,
	metadataRecords provisioningdomain.OSImageMetadataRepository,
	verifications provisioningdomain.OSImageVerificationRepository,
) *ListOSImagesUseCase {
	return &ListOSImagesUseCase{providers: providers, metadataRecords: metadataRecords, verifications: verifications}
}

// Execute lists the provider catalog and merges swallow's metadata onto it.
//
// The provider read is the source of truth for which images exist and their provider values; an
// metadata whose image is no longer in the catalog is simply not emitted, so a stale override
// never invents an image. A failure reading the provider is returned as-is; a failure reading
// metadata records is also returned rather than silently dropping overrides, because a partial merge
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

	metadataRecords, err := uc.metadataRecords.ListByIntegration(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	// Index metadata records by the provider image identity so the merge is a single pass rather than
	// a scan per image. One image name can back several architectures, so the key includes both.
	byKey := make(map[string]*provisioningdomain.OSImageMetadata, len(metadataRecords))
	for _, metadata := range metadataRecords {
		byKey[osImageMetadataKey(metadata.ImageID, metadata.Architecture)] = metadata
	}

	// Verification is a separate swallow-owned store keyed by the same identity; a read failure is
	// returned rather than silently dropping attestations, which would misreport a verified image
	// as unverified.
	verifications, err := uc.verifications.ListByIntegration(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	verificationByKey := make(map[string]*provisioningdomain.OSImageVerification, len(verifications))
	for _, verification := range verifications {
		verificationByKey[osImageMetadataKey(verification.ImageID, verification.Architecture)] = verification
	}

	items := make([]OSImageItem, 0, len(images))
	for _, image := range images {
		item := newOSImageItem(image)
		if verification := verificationByKey[osImageMetadataKey(image.ID, image.Architecture)]; verification != nil {
			for _, target := range verification.VerifiedTargets() {
				item.Status.VerifiedDeployTargets = append(item.Status.VerifiedDeployTargets, string(target))
			}
			for _, target := range verification.FailedTargetsList() {
				item.Status.FailedDeployTargets = append(item.Status.FailedDeployTargets, string(target))
			}
		}
		// Metadata precedence is one-directional and per field: a non-empty swallow value becomes
		// the effective value while the provider value stays visible as provider*.
		if metadata := byKey[osImageMetadataKey(image.ID, image.Architecture)]; metadata != nil {
			applyOSImageMetadata(&item, metadata)
		}
		items = append(items, item)
	}
	return items, nil
}

// osImageMetadataKey joins the two halves of an image's metadata identity with a separator that
// cannot appear in an image ID or architecture, so distinct pairs never collide in the lookup.
func osImageMetadataKey(imageID, architecture string) string {
	return imageID + "\x00" + architecture
}

// newOSImageItem projects a provider image onto the API item with the provider's own values as
// the effective values and no swallow metadata. ListOSImagesUseCase applies metadata on top of
// this base; a freshly uploaded image has no metadata, so this bare projection is already its full
// representation.
func newOSImageItem(image *provisioningdomain.OSImage) OSImageItem {
	return OSImageItem{
		ID:           image.ID,
		Architecture: image.Architecture,
		Spec:         OSImageMetadataItem{Tags: []string{}},
		Status: OSImageStatusItem{
			Name:                  image.Name,
			OSSystem:              image.OSSystem,
			Release:               image.Release,
			DefaultUser:           provisioningdomain.BuiltinDefaultUser(image.OSSystem),
			ProviderName:          image.Name,
			ProviderOSSystem:      image.OSSystem,
			ProviderRelease:       image.Release,
			SizeBytes:             image.SizeBytes,
			VerifiedDeployTargets: []string{},
			FailedDeployTargets:   []string{},
		},
	}
}

// applyOSImageMetadata sets the item's spec from swallow's metadata and makes each set value the
// effective one.
func applyOSImageMetadata(item *OSImageItem, metadata *provisioningdomain.OSImageMetadata) {
	item.Spec = OSImageMetadataItem{
		Name: metadata.DisplayName, OSSystem: metadata.OSSystem, Release: metadata.Release,
		Tags: append([]string{}, metadata.Tags...), DefaultUser: metadata.DefaultUser,
	}
	if metadata.DisplayName != "" {
		item.Status.Name = metadata.DisplayName
	}
	if metadata.OSSystem != "" {
		item.Status.OSSystem = metadata.OSSystem
	}
	if metadata.Release != "" {
		item.Status.Release = metadata.Release
	}
	if metadata.DefaultUser != "" {
		item.Status.DefaultUser = metadata.DefaultUser
	}
}
