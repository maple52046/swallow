package application

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// osImageUploadableProvider adds the optional OSImageUploader capability and records the request,
// so a test can confirm the use case forwards the operator intent unchanged and returns the
// provider's created image.
type osImageUploadableProvider struct {
	osImageBaseProvider
	received provisioningdomain.UploadOSImageRequest
	result   *provisioningdomain.OSImage
}

func (p *osImageUploadableProvider) UploadOSImage(
	_ context.Context, req provisioningdomain.UploadOSImageRequest,
) (*provisioningdomain.OSImage, error) {
	// Drain the content like a real adapter reads the stream once, then keep the request so the
	// test can assert on the metadata that was forwarded.
	if req.Content != nil {
		_, _ = io.Copy(io.Discard, req.Content)
	}
	p.received = req
	return p.result, nil
}

// A provisioner without the upload capability is refused as a provider rejection rather than
// silently accepting bytes it cannot keep.
func TestUploadOSImageUnsupportedProviderIsRejected(t *testing.T) {
	uc := NewUploadOSImageUseCase(osImageTestFactory{provider: &osImageBaseProvider{}}, newOSImageOverlayRepoFake())

	_, err := uc.Execute(context.Background(), UploadOSImageInput{
		IntegrationID: "integration-1",
		Name:          "img",
		Architecture:  "amd64",
		Size:          4,
		SHA256:        "deadbeef",
		Content:       strings.NewReader("data"),
	})
	var provErr *provisioningdomain.ProviderError
	if !errors.As(err, &provErr) || provErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Fatalf("Execute() error = %v, want a rejected ProviderError", err)
	}
}

// The use case forwards the upload to a capable provider and projects the created image onto the
// API item; a freshly uploaded image has no overlay, so provider values are the effective values.
func TestUploadOSImageForwardsToProvider(t *testing.T) {
	provider := &osImageUploadableProvider{
		result: &provisioningdomain.OSImage{
			ID:           "ubuntu-24.04-rocm",
			Name:         "Ubuntu 24.04 ROCm",
			OSSystem:     "custom",
			Release:      "ubuntu-24.04-rocm",
			Architecture: "amd64",
			SizeBytes:    10485760,
		},
	}
	uc := NewUploadOSImageUseCase(osImageTestFactory{provider: provider}, newOSImageOverlayRepoFake())

	item, err := uc.Execute(context.Background(), UploadOSImageInput{
		IntegrationID: "integration-1",
		Name:          "ubuntu-24.04-rocm",
		Architecture:  "amd64",
		Title:         "Ubuntu 24.04 ROCm",
		FileType:      "tgz",
		Size:          4,
		SHA256:        "deadbeef",
		Content:       strings.NewReader("data"),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if provider.received.Name != "ubuntu-24.04-rocm" ||
		provider.received.SHA256 != "deadbeef" ||
		provider.received.Size != 4 ||
		provider.received.FileType != "tgz" {
		t.Errorf("provider received unexpected request: %+v", provider.received)
	}

	if item.ID != "ubuntu-24.04-rocm" || item.Name != "Ubuntu 24.04 ROCm" || item.ProviderName != "Ubuntu 24.04 ROCm" {
		t.Errorf("item identity/name: %+v", item)
	}
	if item.OSSystem != "custom" || item.ProviderOSSystem != "custom" {
		t.Errorf("item OSSystem: got %q/%q, want custom", item.OSSystem, item.ProviderOSSystem)
	}
	if item.CustomName != "" || item.CustomOSSystem != "" || item.CustomRelease != "" {
		t.Errorf("a new image must carry no custom override, got %+v", item)
	}
	if item.Tags == nil || len(item.Tags) != 0 {
		t.Errorf("tags must be a non-nil empty slice, got %#v", item.Tags)
	}
	if item.SizeBytes != 10485760 {
		t.Errorf("SizeBytes: got %d, want 10485760", item.SizeBytes)
	}
}

// An upload may carry the image's default user; it is stored as the image's swallow overlay once
// the provider accepts the artifact, and an invalid value is refused before any bytes are sent.
func TestUploadOSImageStoresDefaultUser(t *testing.T) {
	provider := &osImageUploadableProvider{
		result: &provisioningdomain.OSImage{ID: "custom/rocky-10", Name: "rocky-10", OSSystem: "custom", Architecture: "amd64"},
	}
	overlays := newOSImageOverlayRepoFake()
	uc := NewUploadOSImageUseCase(osImageTestFactory{provider: provider}, overlays)
	input := UploadOSImageInput{
		IntegrationID: "integration-1", Name: "rocky-10", Architecture: "amd64",
		Size: 4, SHA256: "deadbeef", Content: strings.NewReader("data"),
	}

	input.DefaultUser = "Cloud User"
	if _, err := uc.Execute(context.Background(), input); !errors.Is(err, provisioningdomain.ErrOSImageOverlayInvalid) {
		t.Fatalf("invalid default user error = %v, want ErrOSImageOverlayInvalid", err)
	}
	if provider.received.Name != "" {
		t.Errorf("an invalid default user must be refused before the provider upload")
	}

	input.DefaultUser = " cloud-user "
	item, err := uc.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if item.DefaultUser != "cloud-user" || item.CustomDefaultUser != "cloud-user" {
		t.Errorf("item default user = %q/%q, want cloud-user", item.DefaultUser, item.CustomDefaultUser)
	}
	stored, _ := overlays.ListByIntegration(context.Background(), "integration-1")
	if len(stored) != 1 || stored[0].ImageID != "custom/rocky-10" || stored[0].DefaultUser != "cloud-user" {
		t.Errorf("stored overlays = %+v, want one cloud-user overlay for custom/rocky-10", stored)
	}
}
