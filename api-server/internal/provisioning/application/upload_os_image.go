package application

import (
	"context"
	"io"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// UploadOSImageInput is the swallow-neutral upload intent the delivery layer assembles from the
// multipart request before any provider is resolved.
//
// Size and SHA256 are computed by the delivery layer while it spools the streamed content, so
// they describe exactly the bytes Content will yield. Content is a forward-only stream read once
// by the provider adapter; swallow keeps no copy after the provider accepts it.
type UploadOSImageInput struct {
	IntegrationID string
	Name          string
	Architecture  string
	Title         string
	FileType      string
	Size          int64
	SHA256        string
	Content       io.Reader
}

// UploadOSImageUseCase drives one provisioner to create a new provider-owned OS image from
// operator-supplied content.
//
// Symmetric to DeleteOSImageUseCase and scoped to an integration: an image only exists within the
// provisioner that stores it. Upload is an optional provider capability, so a provisioner without
// it is refused rather than silently accepting bytes it cannot keep (see ADR 027). A freshly
// uploaded image has no swallow overlay, so the result is the provider's own values with empty
// overrides; whether the provider classifies it as a custom image is provider-determined.
type UploadOSImageUseCase struct {
	providers provisioningdomain.ProviderFactory
}

// NewUploadOSImageUseCase wires the provider factory the upload resolves its provider from. No
// overlay store is needed: a new image carries no swallow override.
func NewUploadOSImageUseCase(providers provisioningdomain.ProviderFactory) *UploadOSImageUseCase {
	return &UploadOSImageUseCase{providers: providers}
}

// Execute resolves the provider for the integration, refuses a provisioner that cannot upload,
// and returns the created image as the provider reports it.
//
// The refusal for an unsupported provisioner is a provider rejection, not an internal error: the
// request was understood and declined. The provider owns the resulting artifact and its
// classification; swallow neither keeps a copy nor labels it custom.
func (uc *UploadOSImageUseCase) Execute(ctx context.Context, input UploadOSImageInput) (*OSImageItem, error) {
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
	return &item, nil
}
