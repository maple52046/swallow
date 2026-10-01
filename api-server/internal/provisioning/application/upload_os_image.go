package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// UploadOSImageInput is the swallow-neutral upload intent the delivery layer assembles from the
// multipart request before any provider is resolved.
//
// Size and SHA256 are computed by the delivery layer while it spools the streamed content, so
// they describe exactly the bytes Content will yield. Content is a forward-only stream read once
// by the provider adapter; swallow keeps no copy after the provider accepts it. DefaultUser is the
// optional swallow-owned default login user stored on the new image's overlay (decision 039).
type UploadOSImageInput struct {
	IntegrationID string
	Name          string
	Architecture  string
	Title         string
	FileType      string
	Size          int64
	SHA256        string
	Content       io.Reader
	DefaultUser   string
}

// UploadOSImageUseCase drives one provisioner to create a new provider-owned OS image from
// operator-supplied content.
//
// Symmetric to DeleteOSImageUseCase and scoped to an integration: an image only exists within the
// provisioner that stores it. Upload is an optional provider capability, so a provisioner without
// it is refused rather than silently accepting bytes it cannot keep (see ADR 027). A freshly
// uploaded image carries no display override; whether the provider classifies it as a custom
// image is provider-determined. The only overlay value an upload may set is the default user,
// because an operator uploading a custom image is the one who knows its login account.
type UploadOSImageUseCase struct {
	providers provisioningdomain.ProviderFactory
	overlays  provisioningdomain.OSImageOverlayRepository
	now       func() time.Time
}

// NewUploadOSImageUseCase wires the provider factory the upload resolves its provider from and the
// overlay store an optional default user is written to.
func NewUploadOSImageUseCase(
	providers provisioningdomain.ProviderFactory,
	overlays provisioningdomain.OSImageOverlayRepository,
) *UploadOSImageUseCase {
	return &UploadOSImageUseCase{
		providers: providers,
		overlays:  overlays,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

// Execute resolves the provider for the integration, refuses a provisioner that cannot upload,
// and returns the created image as the provider reports it.
//
// The refusal for an unsupported provisioner is a provider rejection, not an internal error: the
// request was understood and declined. The provider owns the resulting artifact and its
// classification; swallow neither keeps a copy nor labels it custom. An invalid DefaultUser is
// ErrOSImageOverlayInvalid before any provider call. When the provider accepted the upload but
// storing the default user failed, the error is returned although the image exists; the operator
// sets the default user afterwards through the overlay.
func (uc *UploadOSImageUseCase) Execute(ctx context.Context, input UploadOSImageInput) (*OSImageItem, error) {
	defaultUser := strings.TrimSpace(input.DefaultUser)
	if defaultUser != "" && !provisioningdomain.ValidDefaultUser(defaultUser) {
		return nil, provisioningdomain.ErrOSImageOverlayInvalid
	}

	provider, err := uc.providers.For(ctx, input.IntegrationID)
	if err != nil {
		return nil, err
	}

	uploader, ok := provider.(provisioningdomain.OSImageUploader)
	if !ok {
		return nil, unsupported("os image upload")
	}

	image, err := uploader.UploadOSImage(ctx, provisioningdomain.UploadOSImageRequest{
		Name:         input.Name,
		Architecture: input.Architecture,
		Title:        input.Title,
		FileType:     input.FileType,
		Size:         input.Size,
		SHA256:       input.SHA256,
		Content:      input.Content,
	})
	if err != nil {
		return nil, err
	}

	item := newOSImageItem(image)
	if defaultUser == "" {
		return &item, nil
	}
	if err := uc.overlays.Upsert(ctx, &provisioningdomain.OSImageOverlay{
		IntegrationID: input.IntegrationID,
		ImageID:       image.ID,
		Architecture:  image.Architecture,
		DefaultUser:   defaultUser,
		UpdatedAt:     uc.now(),
	}); err != nil {
		return nil, fmt.Errorf("the image was uploaded but its default user was not saved: %w", err)
	}
	item.DefaultUser = defaultUser
	item.CustomDefaultUser = defaultUser
	return &item, nil
}
