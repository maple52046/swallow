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

// osImageVerificationRepoFake is an in-memory OSImageVerificationRepository for use-case tests.
type osImageVerificationRepoFake struct {
	mu      sync.Mutex
	rows    map[string]*provisioningdomain.OSImageVerification
	listErr error
}

func newOSImageVerificationRepoFake() *osImageVerificationRepoFake {
	return &osImageVerificationRepoFake{rows: map[string]*provisioningdomain.OSImageVerification{}}
}

func (r *osImageVerificationRepoFake) ListByIntegration(
	_ context.Context, integrationID string,
) ([]*provisioningdomain.OSImageVerification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []*provisioningdomain.OSImageVerification
	for _, verification := range r.rows {
		if verification.IntegrationID == integrationID {
			out = append(out, verification)
		}
	}
	return out, nil
}

func (r *osImageVerificationRepoFake) Find(
	_ context.Context, integrationID, imageID, architecture string,
) (*provisioningdomain.OSImageVerification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rows[osImageOverlayStoreKey(integrationID, imageID, architecture)], nil
}

func (r *osImageVerificationRepoFake) RecordTarget(
	_ context.Context, integrationID, imageID, architecture string,
	target provisioningdomain.DeployTarget, evidence provisioningdomain.OSImageVerificationEvidence,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := osImageOverlayStoreKey(integrationID, imageID, architecture)
	verification, ok := r.rows[key]
	if !ok {
		verification = &provisioningdomain.OSImageVerification{
			IntegrationID: integrationID, ImageID: imageID, Architecture: architecture,
			Targets: map[provisioningdomain.DeployTarget]provisioningdomain.OSImageVerificationEvidence{},
		}
		r.rows[key] = verification
	}
	verification.Targets[target] = evidence
	delete(verification.FailedTargets, target) // success clears any prior failure for the target
	return nil
}

func (r *osImageVerificationRepoFake) RecordFailedTarget(
	_ context.Context, integrationID, imageID, architecture string,
	target provisioningdomain.DeployTarget, failure provisioningdomain.OSImageVerificationFailure,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := osImageOverlayStoreKey(integrationID, imageID, architecture)
	verification, ok := r.rows[key]
	if !ok {
		verification = &provisioningdomain.OSImageVerification{
			IntegrationID: integrationID, ImageID: imageID, Architecture: architecture,
			Targets: map[provisioningdomain.DeployTarget]provisioningdomain.OSImageVerificationEvidence{},
		}
		r.rows[key] = verification
	}
	if verification.FailedTargets == nil {
		verification.FailedTargets = map[provisioningdomain.DeployTarget]provisioningdomain.OSImageVerificationFailure{}
	}
	verification.FailedTargets[target] = failure
	delete(verification.Targets, target) // failure clears any prior success for the target
	return nil
}

func (r *osImageVerificationRepoFake) Delete(_ context.Context, integrationID, imageID, architecture string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rows, osImageOverlayStoreKey(integrationID, imageID, architecture))
	return nil
}

func (r *osImageVerificationRepoFake) DeleteByIntegration(_ context.Context, integrationID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, verification := range r.rows {
		if verification.IntegrationID == integrationID {
			delete(r.rows, key)
		}
	}
	return nil
}

func (r *osImageVerificationRepoFake) seed(verification *provisioningdomain.OSImageVerification) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[osImageOverlayStoreKey(verification.IntegrationID, verification.ImageID, verification.Architecture)] = verification
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

func TestListOSImagesProjectsVerifiedDeployTargets(t *testing.T) {
	provider := &osImageBaseProvider{images: []*provisioningdomain.OSImage{
		{ID: "custom/rocky", Name: "Rocky", OSSystem: "custom", Release: "rocky", Architecture: "amd64"},
		{ID: "ubuntu/noble", Name: "Ubuntu 24.04", OSSystem: "ubuntu", Release: "noble", Architecture: "amd64"},
	}}
	verifications := newOSImageVerificationRepoFake()
	verifications.seed(&provisioningdomain.OSImageVerification{
		IntegrationID: "integration-1", ImageID: "custom/rocky", Architecture: "amd64",
		Targets: map[provisioningdomain.DeployTarget]provisioningdomain.OSImageVerificationEvidence{
			provisioningdomain.DeployTargetRAM:  {},
			provisioningdomain.DeployTargetDisk: {},
		},
	})

	uc := NewListOSImagesUseCase(osImageTestFactory{provider: provider}, newOSImageOverlayRepoFake(), verifications)
	items, err := uc.Execute(context.Background(), "integration-1")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	rocky, noble := items[0], items[1]
	if len(rocky.VerifiedDeployTargets) != 2 || rocky.VerifiedDeployTargets[0] != "disk" || rocky.VerifiedDeployTargets[1] != "ram" {
		t.Errorf("rocky verifiedDeployTargets = %v, want [disk ram] (sorted)", rocky.VerifiedDeployTargets)
	}
	if noble.VerifiedDeployTargets == nil || len(noble.VerifiedDeployTargets) != 0 {
		t.Errorf("noble verifiedDeployTargets = %v, want an empty (non-nil) array for an unverified image", noble.VerifiedDeployTargets)
	}
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

	uc := NewListOSImagesUseCase(osImageTestFactory{provider: provider}, overlays, newOSImageVerificationRepoFake())
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
	uc := NewListOSImagesUseCase(osImageTestFactory{provider: &osImageBaseProvider{}}, overlays, newOSImageVerificationRepoFake())
	if _, err := uc.Execute(context.Background(), "integration-1"); err == nil {
		t.Fatal("Execute() expected an error when the overlay store fails, got nil")
	}
}

func TestSetOSImageOverlayStoresTrimmedOverrides(t *testing.T) {
	overlays := newOSImageOverlayRepoFake()
	uc := NewSetOSImageOverlayUseCase(overlays, nil)
	if err := uc.Set(context.Background(), "integration-1", "ubuntu/jammy", "amd64", OSImageOverlayInput{Name: "  Golden Ubuntu  ", OSSystem: "", Release: "  22.04  ", Tags: nil}); err != nil {
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
	uc := NewSetOSImageOverlayUseCase(overlays, nil)
	// Tags are trimmed, blanks dropped, and duplicates removed while preserving first order.
	if err := uc.Set(context.Background(), "integration-1", "ubuntu/jammy", "amd64", OSImageOverlayInput{Name: "", OSSystem: "", Release: "", Tags: []string{" gpu ", "gpu", "", "ml"}}); err != nil {
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
	uc := NewSetOSImageOverlayUseCase(overlays, nil)
	err := uc.Set(context.Background(), "integration-1", "ubuntu/jammy", "amd64", OSImageOverlayInput{Name: strings.Repeat("x", maxOSImageOverlayFieldLength+1)})
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
	uc := NewSetOSImageOverlayUseCase(overlays, nil)
	// An all-blank edit means "use the provider values everywhere", so the overlay is removed
	// rather than persisted as an empty record.
	if err := uc.Set(context.Background(), "integration-1", "ubuntu/jammy", "amd64", OSImageOverlayInput{Name: "  ", OSSystem: "", Release: "", Tags: []string{"  "}}); err != nil {
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
	uc := NewSetOSImageOverlayUseCase(overlays, nil)
	if err := uc.Clear(context.Background(), "integration-1", "ubuntu/jammy", "amd64"); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if _, ok := overlays.get("integration-1", "ubuntu/jammy", "amd64"); ok {
		t.Error("Clear() left the overlay in place")
	}
}

// recordingRefresher captures the integrations passed to RefreshDeployedImageNames and can be
// primed to fail, so a test can assert an overlay write propagates the rename and that a
// propagation failure never fails the write.
type recordingRefresher struct {
	calls []string
	err   error
}

func (r *recordingRefresher) RefreshDeployedImageNames(_ context.Context, integrationID string) error {
	r.calls = append(r.calls, integrationID)
	return r.err
}

// A successful overlay Set and Clear each trigger the deployed-image-name refresh, so a rename is
// mirrored onto the affected integration's servers immediately instead of at the next reconcile.
func TestSetOSImageOverlayPropagatesNameToServers(t *testing.T) {
	overlays := newOSImageOverlayRepoFake()
	refresher := &recordingRefresher{}
	uc := NewSetOSImageOverlayUseCase(overlays, refresher)

	if err := uc.Set(context.Background(), "integration-1", "ubuntu/jammy", "amd64", OSImageOverlayInput{Name: "Golden Ubuntu", OSSystem: "", Release: "", Tags: nil}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := uc.Clear(context.Background(), "integration-1", "ubuntu/jammy", "amd64"); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if len(refresher.calls) != 2 || refresher.calls[0] != "integration-1" || refresher.calls[1] != "integration-1" {
		t.Errorf("refresh calls = %v, want [integration-1 integration-1] for one Set and one Clear", refresher.calls)
	}
}

// The refresh is best-effort: a failing refresher must not turn a committed overlay write into a
// failed request, because the periodic reconcile still re-mirrors the name.
func TestSetOSImageOverlayIgnoresRefresherFailure(t *testing.T) {
	overlays := newOSImageOverlayRepoFake()
	refresher := &recordingRefresher{err: errors.New("catalog unavailable")}
	uc := NewSetOSImageOverlayUseCase(overlays, refresher)

	if err := uc.Set(context.Background(), "integration-1", "ubuntu/jammy", "amd64", OSImageOverlayInput{Name: "Golden Ubuntu", OSSystem: "", Release: "", Tags: nil}); err != nil {
		t.Fatalf("Set() with a failing refresher must still succeed, got %v", err)
	}
	if _, ok := overlays.get("integration-1", "ubuntu/jammy", "amd64"); !ok {
		t.Error("Set() must persist the overlay even when propagation fails")
	}
}

func TestDeleteOSImagePrunesOverlay(t *testing.T) {
	provider := &osImageRemovableProvider{}
	overlays := newOSImageOverlayRepoFake()
	overlays.seed(&provisioningdomain.OSImageOverlay{
		IntegrationID: "integration-1", ImageID: "ubuntu/jammy", Architecture: "amd64", DisplayName: "Golden Ubuntu",
	})
	uc := NewDeleteOSImageUseCase(osImageTestFactory{provider: provider}, overlays, newOSImageVerificationRepoFake())
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
	uc := NewDeleteOSImageUseCase(osImageTestFactory{provider: &osImageBaseProvider{}}, overlays, newOSImageVerificationRepoFake())
	err := uc.Execute(context.Background(), "integration-1", "ubuntu/jammy", "amd64")
	var provErr *provisioningdomain.ProviderError
	if !errors.As(err, &provErr) || provErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Fatalf("Execute() error = %v, want a rejected ProviderError", err)
	}
}

// The catalog reports an effective default user: the overlay value, else swallow's built-in for
// the provider OS family. Relabeling the OS for display must not change the built-in.
func TestListOSImagesProjectsDefaultUser(t *testing.T) {
	provider := &osImageBaseProvider{images: []*provisioningdomain.OSImage{
		{ID: "ubuntu/noble", Name: "Ubuntu 24.04", OSSystem: "ubuntu", Release: "noble", Architecture: "amd64"},
		{ID: "custom/rocky", Name: "Rocky", OSSystem: "custom", Release: "rocky", Architecture: "amd64"},
		{ID: "custom/plain", Name: "Plain", OSSystem: "custom", Release: "plain", Architecture: "amd64"},
	}}
	overlays := newOSImageOverlayRepoFake()
	overlays.seed(&provisioningdomain.OSImageOverlay{
		IntegrationID: "integration-1", ImageID: "ubuntu/noble", Architecture: "amd64", OSSystem: "Golden OS",
	})
	overlays.seed(&provisioningdomain.OSImageOverlay{
		IntegrationID: "integration-1", ImageID: "custom/rocky", Architecture: "amd64", DefaultUser: "cloud-user",
	})

	uc := NewListOSImagesUseCase(osImageTestFactory{provider: provider}, overlays, newOSImageVerificationRepoFake())
	items, err := uc.Execute(context.Background(), "integration-1")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, tc := range []struct {
		index               int
		wantEffective, want string
	}{
		{index: 0, wantEffective: "ubuntu"},
		{index: 1, wantEffective: "cloud-user", want: "cloud-user"},
		{index: 2},
	} {
		item := items[tc.index]
		if item.DefaultUser != tc.wantEffective || item.CustomDefaultUser != tc.want {
			t.Errorf("%s default user = %q/%q, want %q/%q", item.ID, item.DefaultUser, item.CustomDefaultUser, tc.wantEffective, tc.want)
		}
	}
}

func TestSetOSImageOverlayValidatesDefaultUser(t *testing.T) {
	overlays := newOSImageOverlayRepoFake()
	uc := NewSetOSImageOverlayUseCase(overlays, nil)

	err := uc.Set(context.Background(), "integration-1", "custom/rocky", "amd64", OSImageOverlayInput{DefaultUser: "Cloud User"})
	if !errors.Is(err, provisioningdomain.ErrOSImageOverlayInvalid) {
		t.Fatalf("Set() error = %v, want ErrOSImageOverlayInvalid for a non-POSIX user", err)
	}

	if err := uc.Set(context.Background(), "integration-1", "custom/rocky", "amd64", OSImageOverlayInput{DefaultUser: " cloud-user "}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	stored, _ := overlays.ListByIntegration(context.Background(), "integration-1")
	if len(stored) != 1 || stored[0].DefaultUser != "cloud-user" {
		t.Errorf("stored overlays = %+v, want a default-user-only overlay kept (not deleted as empty)", stored)
	}
}
