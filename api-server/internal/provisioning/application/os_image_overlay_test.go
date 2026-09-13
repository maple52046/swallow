package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// osImageOverlayStoreKey mirrors the natural overlay key inside the in-memory fake so a test
// can seed and assert overlays by the same identity the repository stores them under.
func osImageOverlayStoreKey(integrationID, imageID, architecture string) string {
	return integrationID + "\x00" + imageID + "\x00" + architecture
}

// osImageOverlayRepoFake is an in-memory OSImageOverlayRepository for use-case tests. listErr
// lets a test force a store failure to check that the catalog read surfaces it.
type osImageOverlayRepoFake struct {
	mu       sync.Mutex
	overlays map[string]*provisioningdomain.OSImageOverlay
	listErr  error
}

func newOSImageOverlayRepoFake() *osImageOverlayRepoFake {
	return &osImageOverlayRepoFake{overlays: map[string]*provisioningdomain.OSImageOverlay{}}
}

func (r *osImageOverlayRepoFake) ListByIntegration(
	_ context.Context, integrationID string,
) ([]*provisioningdomain.OSImageOverlay, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []*provisioningdomain.OSImageOverlay
	for _, overlay := range r.overlays {
		if overlay.IntegrationID == integrationID {
			clone := *overlay
			out = append(out, &clone)
		}
	}
	return out, nil
}

func (r *osImageOverlayRepoFake) Upsert(_ context.Context, overlay *provisioningdomain.OSImageOverlay) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	clone := *overlay
	r.overlays[osImageOverlayStoreKey(overlay.IntegrationID, overlay.ImageID, overlay.Architecture)] = &clone
	return nil
}

func (r *osImageOverlayRepoFake) Delete(_ context.Context, integrationID, imageID, architecture string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.overlays, osImageOverlayStoreKey(integrationID, imageID, architecture))
	return nil
}

func (r *osImageOverlayRepoFake) get(integrationID, imageID, architecture string) (*provisioningdomain.OSImageOverlay, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	overlay, ok := r.overlays[osImageOverlayStoreKey(integrationID, imageID, architecture)]
	return overlay, ok
}

func (r *osImageOverlayRepoFake) seed(overlay *provisioningdomain.OSImageOverlay) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.overlays[osImageOverlayStoreKey(overlay.IntegrationID, overlay.ImageID, overlay.Architecture)] = overlay
}

// osImageBaseProvider implements the minimum OSProvisioningProvider with no optional
// capability, so a type assertion for OSImageRemover fails against it.
type osImageBaseProvider struct {
	images []*provisioningdomain.OSImage
}

func (p *osImageBaseProvider) Name() string { return "test" }

func (p *osImageBaseProvider) Probe(context.Context) (provisioningdomain.ProviderInfo, error) {
	return provisioningdomain.ProviderInfo{Name: "test"}, nil
}

func (p *osImageBaseProvider) Capabilities() provisioningdomain.ProviderCapabilities {
	return provisioningdomain.ProviderCapabilities{}
}

func (p *osImageBaseProvider) ListMachines(
	context.Context, provisioningdomain.MachineFilter,
) ([]*provisioningdomain.Machine, error) {
	return nil, nil
}

func (p *osImageBaseProvider) GetMachine(context.Context, string) (*provisioningdomain.Machine, error) {
	return nil, provisioningdomain.ErrMachineNotFound
}

func (p *osImageBaseProvider) ListOSImages(context.Context) ([]*provisioningdomain.OSImage, error) {
	return p.images, nil
}

func (p *osImageBaseProvider) Deploy(
	context.Context, provisioningdomain.DeployRequest,
) (*provisioningdomain.Machine, error) {
	return nil, nil
}

func (p *osImageBaseProvider) Release(context.Context, string) (*provisioningdomain.Machine, error) {
	return nil, nil
}

// osImageRemovableProvider adds the optional OSImageRemover capability and records deletions,
// so a test can confirm the provider was asked to delete before the overlay is pruned.
type osImageRemovableProvider struct {
	osImageBaseProvider
	deleted [][2]string
}

func (p *osImageRemovableProvider) DeleteOSImage(_ context.Context, imageID, architecture string) error {
	p.deleted = append(p.deleted, [2]string{imageID, architecture})
	return nil
}

// osImageTestFactory returns a fixed provider, standing in for the integration-resolving
// ProviderFactory.
type osImageTestFactory struct {
	provider provisioningdomain.OSProvisioningProvider
}

func (f osImageTestFactory) For(context.Context, string) (provisioningdomain.OSProvisioningProvider, error) {
	return f.provider, nil
}

func TestListOSImagesMergesOverlay(t *testing.T) {
	provider := &osImageBaseProvider{images: []*provisioningdomain.OSImage{
		{ID: "ubuntu/jammy", Name: "Ubuntu 22.04 LTS", OSSystem: "ubuntu", Release: "jammy", Architecture: "amd64", SizeBytes: 5 * 1024 * 1024 * 1024},
		{ID: "ubuntu/noble", Name: "Ubuntu 24.04 LTS", OSSystem: "ubuntu", Release: "noble", Architecture: "amd64"},
	}}
	overlays := newOSImageOverlayRepoFake()
	// Override every field on jammy, but only the release on noble, to prove the merge is
	// per field and leaves un-overridden fields on their provider value.
	overlays.seed(&provisioningdomain.OSImageOverlay{
		IntegrationID: "integration-1", ImageID: "ubuntu/jammy", Architecture: "amd64",
		DisplayName: "Golden Ubuntu", OSSystem: "Ubuntu LTS", Release: "22.04", Tags: []string{"golden", "baseline"},
	})
	overlays.seed(&provisioningdomain.OSImageOverlay{
		IntegrationID: "integration-1", ImageID: "ubuntu/noble", Architecture: "amd64", Release: "24.04 LTS",
	})
	// An overlay whose image the provider no longer lists must not resurrect the image.
	overlays.seed(&provisioningdomain.OSImageOverlay{
		IntegrationID: "integration-1", ImageID: "ubuntu/focal", Architecture: "amd64", DisplayName: "Ghost",
	})

	uc := NewListOSImagesUseCase(osImageTestFactory{provider: provider}, overlays)
	items, err := uc.Execute(context.Background(), "integration-1")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("Execute() returned %d items, want 2 (an overlay for a missing image must not add one)", len(items))
	}

	full := items[0]
	if full.Name != "Golden Ubuntu" || full.CustomName != "Golden Ubuntu" || full.ProviderName != "Ubuntu 22.04 LTS" {
		t.Errorf("name merge = {effective:%q custom:%q provider:%q}, want %q over %q",
			full.Name, full.CustomName, full.ProviderName, "Golden Ubuntu", "Ubuntu 22.04 LTS")
	}
	if full.OSSystem != "Ubuntu LTS" || full.CustomOSSystem != "Ubuntu LTS" || full.ProviderOSSystem != "ubuntu" {
		t.Errorf("os merge = {effective:%q custom:%q provider:%q}, want %q over %q",
			full.OSSystem, full.CustomOSSystem, full.ProviderOSSystem, "Ubuntu LTS", "ubuntu")
	}
	if full.Release != "22.04" || full.CustomRelease != "22.04" || full.ProviderRelease != "jammy" {
		t.Errorf("release merge = {effective:%q custom:%q provider:%q}, want %q over %q",
			full.Release, full.CustomRelease, full.ProviderRelease, "22.04", "jammy")
	}
	if len(full.Tags) != 2 || full.Tags[0] != "golden" || full.Tags[1] != "baseline" {
		t.Errorf("tags merge = %v, want [golden baseline]", full.Tags)
	}
	if full.SizeBytes != 5*1024*1024*1024 {
		t.Errorf("sizeBytes = %d, want provider size preserved through overlay merge", full.SizeBytes)
	}

	partial := items[1]
	if len(partial.Tags) != 0 {
		t.Errorf("partial tags = %v, want empty array when no overlay tags", partial.Tags)
	}
	// Only the release was overridden: name and OS must stay on their provider values.
	if partial.Name != "Ubuntu 24.04 LTS" || partial.CustomName != "" {
		t.Errorf("partial name = {effective:%q custom:%q}, want provider label and empty custom", partial.Name, partial.CustomName)
	}
	if partial.OSSystem != "ubuntu" || partial.CustomOSSystem != "" {
		t.Errorf("partial os = {effective:%q custom:%q}, want provider value and empty custom", partial.OSSystem, partial.CustomOSSystem)
	}
	if partial.Release != "24.04 LTS" || partial.CustomRelease != "24.04 LTS" || partial.ProviderRelease != "noble" {
		t.Errorf("partial release = {effective:%q custom:%q provider:%q}, want %q over %q",
			partial.Release, partial.CustomRelease, partial.ProviderRelease, "24.04 LTS", "noble")
	}
}

func TestListOSImagesReturnsOverlayError(t *testing.T) {
	overlays := newOSImageOverlayRepoFake()
	overlays.listErr = errors.New("overlay store unavailable")
	uc := NewListOSImagesUseCase(osImageTestFactory{provider: &osImageBaseProvider{}}, overlays)
	if _, err := uc.Execute(context.Background(), "integration-1"); err == nil {
		t.Fatal("Execute() expected an error when the overlay store fails, got nil")
	}
}

func TestSetOSImageOverlayStoresTrimmedOverrides(t *testing.T) {
	overlays := newOSImageOverlayRepoFake()
	uc := NewSetOSImageOverlayUseCase(overlays)
	if err := uc.Set(context.Background(), "integration-1", "ubuntu/jammy", "amd64", "  Golden Ubuntu  ", "", "  22.04  ", nil); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	stored, ok := overlays.get("integration-1", "ubuntu/jammy", "amd64")
	if !ok {
		t.Fatal("Set() did not persist an overlay")
	}
	if stored.DisplayName != "Golden Ubuntu" || stored.Release != "22.04" {
		t.Errorf("stored overrides = {name:%q release:%q}, want trimmed %q / %q", stored.DisplayName, stored.Release, "Golden Ubuntu", "22.04")
	}
	if stored.OSSystem != "" {
		t.Errorf("stored os = %q, want empty (no override supplied)", stored.OSSystem)
	}
	if stored.UpdatedAt.IsZero() {
		t.Error("Set() left UpdatedAt zero; a write time must be recorded")
	}
}

func TestSetOSImageOverlayNormalizesTags(t *testing.T) {
	overlays := newOSImageOverlayRepoFake()
	uc := NewSetOSImageOverlayUseCase(overlays)
	// Tags are trimmed, blanks dropped, and duplicates removed while preserving first order.
	if err := uc.Set(context.Background(), "integration-1", "ubuntu/jammy", "amd64", "", "", "", []string{" gpu ", "gpu", "", "ml"}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	stored, ok := overlays.get("integration-1", "ubuntu/jammy", "amd64")
	if !ok {
		t.Fatal("Set() did not persist an overlay carrying only tags")
	}
	if len(stored.Tags) != 2 || stored.Tags[0] != "gpu" || stored.Tags[1] != "ml" {
		t.Errorf("stored tags = %v, want [gpu ml]", stored.Tags)
	}
}

func TestSetOSImageOverlayRejectsOverLongField(t *testing.T) {
	overlays := newOSImageOverlayRepoFake()
	uc := NewSetOSImageOverlayUseCase(overlays)
	err := uc.Set(context.Background(), "integration-1", "ubuntu/jammy", "amd64", strings.Repeat("x", maxOSImageOverlayFieldLength+1), "", "", nil)
	if !errors.Is(err, provisioningdomain.ErrOSImageOverlayInvalid) {
		t.Fatalf("Set(over-long) error = %v, want %v", err, provisioningdomain.ErrOSImageOverlayInvalid)
	}
	if _, ok := overlays.get("integration-1", "ubuntu/jammy", "amd64"); ok {
		t.Error("Set() must not persist an overlay when validation fails")
	}
}

func TestSetOSImageOverlayClearsWhenAllBlank(t *testing.T) {
	overlays := newOSImageOverlayRepoFake()
	overlays.seed(&provisioningdomain.OSImageOverlay{
		IntegrationID: "integration-1", ImageID: "ubuntu/jammy", Architecture: "amd64", DisplayName: "Golden Ubuntu",
	})
	uc := NewSetOSImageOverlayUseCase(overlays)
	// An all-blank edit means "use the provider values everywhere", so the overlay is removed
	// rather than persisted as an empty record.
	if err := uc.Set(context.Background(), "integration-1", "ubuntu/jammy", "amd64", "  ", "", "", []string{"  "}); err != nil {
		t.Fatalf("Set(all-blank) error = %v", err)
	}
	if _, ok := overlays.get("integration-1", "ubuntu/jammy", "amd64"); ok {
		t.Error("Set(all-blank) left an empty overlay in place")
	}
}

func TestClearOSImageOverlayRemovesOverlay(t *testing.T) {
	overlays := newOSImageOverlayRepoFake()
	overlays.seed(&provisioningdomain.OSImageOverlay{
		IntegrationID: "integration-1", ImageID: "ubuntu/jammy", Architecture: "amd64", DisplayName: "Golden Ubuntu",
	})
	uc := NewSetOSImageOverlayUseCase(overlays)
	if err := uc.Clear(context.Background(), "integration-1", "ubuntu/jammy", "amd64"); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if _, ok := overlays.get("integration-1", "ubuntu/jammy", "amd64"); ok {
		t.Error("Clear() left the overlay in place")
	}
}

func TestDeleteOSImagePrunesOverlay(t *testing.T) {
	provider := &osImageRemovableProvider{}
	overlays := newOSImageOverlayRepoFake()
	overlays.seed(&provisioningdomain.OSImageOverlay{
		IntegrationID: "integration-1", ImageID: "ubuntu/jammy", Architecture: "amd64", DisplayName: "Golden Ubuntu",
	})
	uc := NewDeleteOSImageUseCase(osImageTestFactory{provider: provider}, overlays)
	if err := uc.Execute(context.Background(), "integration-1", "ubuntu/jammy", "amd64"); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(provider.deleted) != 1 {
		t.Fatalf("provider deletions = %d, want 1", len(provider.deleted))
	}
	if _, ok := overlays.get("integration-1", "ubuntu/jammy", "amd64"); ok {
		t.Error("Execute() did not prune the overlay after deleting the image")
	}
}

func TestDeleteOSImageUnsupportedProviderIsRejected(t *testing.T) {
	overlays := newOSImageOverlayRepoFake()
	uc := NewDeleteOSImageUseCase(osImageTestFactory{provider: &osImageBaseProvider{}}, overlays)
	err := uc.Execute(context.Background(), "integration-1", "ubuntu/jammy", "amd64")
	var provErr *provisioningdomain.ProviderError
	if !errors.As(err, &provErr) || provErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Fatalf("Execute() error = %v, want a rejected ProviderError", err)
	}
}
