package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// osImageMetadataStoreKey mirrors the natural metadata key inside the in-memory fake so a test
// can seed and assert metadata records by the same identity the repository stores them under.
func osImageMetadataStoreKey(integrationID, imageID, architecture string) string {
	return integrationID + "\x00" + imageID + "\x00" + architecture
}

// osImageMetadataRepoFake is an in-memory OSImageMetadataRepository for use-case tests. listErr
// lets a test force a store failure to check that the catalog read surfaces it.
type osImageMetadataRepoFake struct {
	mu              sync.Mutex
	metadataRecords map[string]*provisioningdomain.OSImageMetadata
	listErr         error
}

func newOSImageMetadataRepoFake() *osImageMetadataRepoFake {
	return &osImageMetadataRepoFake{metadataRecords: map[string]*provisioningdomain.OSImageMetadata{}}
}

func (r *osImageMetadataRepoFake) ListByIntegration(
	_ context.Context, integrationID string,
) ([]*provisioningdomain.OSImageMetadata, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []*provisioningdomain.OSImageMetadata
	for _, metadata := range r.metadataRecords {
		if metadata.IntegrationID == integrationID {
			clone := *metadata
			out = append(out, &clone)
		}
	}
	return out, nil
}

func (r *osImageMetadataRepoFake) Upsert(_ context.Context, metadata *provisioningdomain.OSImageMetadata) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	clone := *metadata
	r.metadataRecords[osImageMetadataStoreKey(metadata.IntegrationID, metadata.ImageID, metadata.Architecture)] = &clone
	return nil
}

func (r *osImageMetadataRepoFake) Delete(_ context.Context, integrationID, imageID, architecture string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.metadataRecords, osImageMetadataStoreKey(integrationID, imageID, architecture))
	return nil
}

func (r *osImageMetadataRepoFake) get(integrationID, imageID, architecture string) (*provisioningdomain.OSImageMetadata, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	metadata, ok := r.metadataRecords[osImageMetadataStoreKey(integrationID, imageID, architecture)]
	return metadata, ok
}

func (r *osImageMetadataRepoFake) seed(metadata *provisioningdomain.OSImageMetadata) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metadataRecords[osImageMetadataStoreKey(metadata.IntegrationID, metadata.ImageID, metadata.Architecture)] = metadata
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
	return r.rows[osImageMetadataStoreKey(integrationID, imageID, architecture)], nil
}

func (r *osImageVerificationRepoFake) RecordTarget(
	_ context.Context, integrationID, imageID, architecture string,
	target provisioningdomain.DeployTarget, evidence provisioningdomain.OSImageVerificationEvidence,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := osImageMetadataStoreKey(integrationID, imageID, architecture)
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
	key := osImageMetadataStoreKey(integrationID, imageID, architecture)
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
	delete(r.rows, osImageMetadataStoreKey(integrationID, imageID, architecture))
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
	r.rows[osImageMetadataStoreKey(verification.IntegrationID, verification.ImageID, verification.Architecture)] = verification
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
// so a test can confirm the provider was asked to delete before the metadata is pruned.
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

	uc := NewListOSImagesUseCase(osImageTestFactory{provider: provider}, newOSImageMetadataRepoFake(), verifications)
	items, err := uc.Execute(context.Background(), "integration-1")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	rocky, noble := items[0], items[1]
	if len(rocky.Status.VerifiedDeployTargets) != 2 || rocky.Status.VerifiedDeployTargets[0] != "disk" || rocky.Status.VerifiedDeployTargets[1] != "ram" {
		t.Errorf("rocky verifiedDeployTargets = %v, want [disk ram] (sorted)", rocky.Status.VerifiedDeployTargets)
	}
	if noble.Status.VerifiedDeployTargets == nil || len(noble.Status.VerifiedDeployTargets) != 0 {
		t.Errorf("noble verifiedDeployTargets = %v, want an empty (non-nil) array for an unverified image", noble.Status.VerifiedDeployTargets)
	}
}

func TestListOSImagesMergesOverlay(t *testing.T) {
	provider := &osImageBaseProvider{images: []*provisioningdomain.OSImage{
		{ID: "ubuntu/jammy", Name: "Ubuntu 22.04 LTS", OSSystem: "ubuntu", Release: "jammy", Architecture: "amd64", SizeBytes: 5 * 1024 * 1024 * 1024},
		{ID: "ubuntu/noble", Name: "Ubuntu 24.04 LTS", OSSystem: "ubuntu", Release: "noble", Architecture: "amd64"},
	}}
	metadataRecords := newOSImageMetadataRepoFake()
	// Override every field on jammy, but only the release on noble, to prove the merge is
	// per field and leaves un-overridden fields on their provider value.
	metadataRecords.seed(&provisioningdomain.OSImageMetadata{
		IntegrationID: "integration-1", ImageID: "ubuntu/jammy", Architecture: "amd64",
		DisplayName: "Golden Ubuntu", OSSystem: "Ubuntu LTS", Release: "22.04", Tags: []string{"golden", "baseline"},
	})
	metadataRecords.seed(&provisioningdomain.OSImageMetadata{
		IntegrationID: "integration-1", ImageID: "ubuntu/noble", Architecture: "amd64", Release: "24.04 LTS",
	})
	// Metadata whose image the provider no longer lists must not resurrect the image.
	metadataRecords.seed(&provisioningdomain.OSImageMetadata{
		IntegrationID: "integration-1", ImageID: "ubuntu/focal", Architecture: "amd64", DisplayName: "Ghost",
	})

	uc := NewListOSImagesUseCase(osImageTestFactory{provider: provider}, metadataRecords, newOSImageVerificationRepoFake())
	items, err := uc.Execute(context.Background(), "integration-1")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("Execute() returned %d items, want 2 (metadata for a missing image must not add one)", len(items))
	}

	full := items[0]
	if full.Status.Name != "Golden Ubuntu" || full.Spec.Name != "Golden Ubuntu" || full.Status.ProviderName != "Ubuntu 22.04 LTS" {
		t.Errorf("name merge = {effective:%q custom:%q provider:%q}, want %q over %q",
			full.Status.Name, full.Spec.Name, full.Status.ProviderName, "Golden Ubuntu", "Ubuntu 22.04 LTS")
	}
	if full.Status.OSSystem != "Ubuntu LTS" || full.Spec.OSSystem != "Ubuntu LTS" || full.Status.ProviderOSSystem != "ubuntu" {
		t.Errorf("os merge = {effective:%q custom:%q provider:%q}, want %q over %q",
			full.Status.OSSystem, full.Spec.OSSystem, full.Status.ProviderOSSystem, "Ubuntu LTS", "ubuntu")
	}
	if full.Status.Release != "22.04" || full.Spec.Release != "22.04" || full.Status.ProviderRelease != "jammy" {
		t.Errorf("release merge = {effective:%q custom:%q provider:%q}, want %q over %q",
			full.Status.Release, full.Spec.Release, full.Status.ProviderRelease, "22.04", "jammy")
	}
	if len(full.Spec.Tags) != 2 || full.Spec.Tags[0] != "golden" || full.Spec.Tags[1] != "baseline" {
		t.Errorf("tags merge = %v, want [golden baseline]", full.Spec.Tags)
	}
	if full.Status.SizeBytes != 5*1024*1024*1024 {
		t.Errorf("sizeBytes = %d, want provider size preserved through metadata merge", full.Status.SizeBytes)
	}

	partial := items[1]
	if len(partial.Spec.Tags) != 0 {
		t.Errorf("partial tags = %v, want empty array when no metadata tags", partial.Spec.Tags)
	}
	// Only the release was overridden: name and OS must stay on their provider values.
	if partial.Status.Name != "Ubuntu 24.04 LTS" || partial.Spec.Name != "" {
		t.Errorf("partial name = {effective:%q custom:%q}, want provider label and empty custom", partial.Status.Name, partial.Spec.Name)
	}
	if partial.Status.OSSystem != "ubuntu" || partial.Spec.OSSystem != "" {
		t.Errorf("partial os = {effective:%q custom:%q}, want provider value and empty custom", partial.Status.OSSystem, partial.Spec.OSSystem)
	}
	if partial.Status.Release != "24.04 LTS" || partial.Spec.Release != "24.04 LTS" || partial.Status.ProviderRelease != "noble" {
		t.Errorf("partial release = {effective:%q custom:%q provider:%q}, want %q over %q",
			partial.Status.Release, partial.Spec.Release, partial.Status.ProviderRelease, "24.04 LTS", "noble")
	}
}

func TestListOSImagesReturnsOverlayError(t *testing.T) {
	metadataRecords := newOSImageMetadataRepoFake()
	metadataRecords.listErr = errors.New("metadata store unavailable")
	uc := NewListOSImagesUseCase(osImageTestFactory{provider: &osImageBaseProvider{}}, metadataRecords, newOSImageVerificationRepoFake())
	if _, err := uc.Execute(context.Background(), "integration-1"); err == nil {
		t.Fatal("Execute() expected an error when the metadata store fails, got nil")
	}
}

func TestSetOSImageMetadataStoresTrimmedOverrides(t *testing.T) {
	metadataRecords := newOSImageMetadataRepoFake()
	uc := NewSetOSImageMetadataUseCase(metadataRecords, nil)
	if err := uc.Set(context.Background(), "integration-1", "ubuntu/jammy", "amd64", OSImageMetadataInput{Name: "  Golden Ubuntu  ", OSSystem: "", Release: "  22.04  ", Tags: nil}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	stored, ok := metadataRecords.get("integration-1", "ubuntu/jammy", "amd64")
	if !ok {
		t.Fatal("Set() did not persist metadata")
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

func TestSetOSImageMetadataNormalizesTags(t *testing.T) {
	metadataRecords := newOSImageMetadataRepoFake()
	uc := NewSetOSImageMetadataUseCase(metadataRecords, nil)
	// Tags are trimmed, blanks dropped, and duplicates removed while preserving first order.
	if err := uc.Set(context.Background(), "integration-1", "ubuntu/jammy", "amd64", OSImageMetadataInput{Name: "", OSSystem: "", Release: "", Tags: []string{" gpu ", "gpu", "", "ml"}}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	stored, ok := metadataRecords.get("integration-1", "ubuntu/jammy", "amd64")
	if !ok {
		t.Fatal("Set() did not persist metadata carrying only tags")
	}
	if len(stored.Tags) != 2 || stored.Tags[0] != "gpu" || stored.Tags[1] != "ml" {
		t.Errorf("stored tags = %v, want [gpu ml]", stored.Tags)
	}
}

func TestSetOSImageMetadataRejectsOverLongField(t *testing.T) {
	metadataRecords := newOSImageMetadataRepoFake()
	uc := NewSetOSImageMetadataUseCase(metadataRecords, nil)
	err := uc.Set(context.Background(), "integration-1", "ubuntu/jammy", "amd64", OSImageMetadataInput{Name: strings.Repeat("x", maxOSImageMetadataFieldLength+1)})
	if !errors.Is(err, provisioningdomain.ErrOSImageMetadataInvalid) {
		t.Fatalf("Set(over-long) error = %v, want %v", err, provisioningdomain.ErrOSImageMetadataInvalid)
	}
	if _, ok := metadataRecords.get("integration-1", "ubuntu/jammy", "amd64"); ok {
		t.Error("Set() must not persist metadata when validation fails")
	}
}

func TestSetOSImageMetadataClearsWhenAllBlank(t *testing.T) {
	metadataRecords := newOSImageMetadataRepoFake()
	metadataRecords.seed(&provisioningdomain.OSImageMetadata{
		IntegrationID: "integration-1", ImageID: "ubuntu/jammy", Architecture: "amd64", DisplayName: "Golden Ubuntu",
	})
	uc := NewSetOSImageMetadataUseCase(metadataRecords, nil)
	// An all-blank edit means "use the provider values everywhere", so the metadata is removed
	// rather than persisted as an empty record.
	if err := uc.Set(context.Background(), "integration-1", "ubuntu/jammy", "amd64", OSImageMetadataInput{Name: "  ", OSSystem: "", Release: "", Tags: []string{"  "}}); err != nil {
		t.Fatalf("Set(all-blank) error = %v", err)
	}
	if _, ok := metadataRecords.get("integration-1", "ubuntu/jammy", "amd64"); ok {
		t.Error("Set(all-blank) left an empty metadata in place")
	}
}

func TestClearOSImageMetadataRemovesOverlay(t *testing.T) {
	metadataRecords := newOSImageMetadataRepoFake()
	metadataRecords.seed(&provisioningdomain.OSImageMetadata{
		IntegrationID: "integration-1", ImageID: "ubuntu/jammy", Architecture: "amd64", DisplayName: "Golden Ubuntu",
	})
	uc := NewSetOSImageMetadataUseCase(metadataRecords, nil)
	if err := uc.Clear(context.Background(), "integration-1", "ubuntu/jammy", "amd64"); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if _, ok := metadataRecords.get("integration-1", "ubuntu/jammy", "amd64"); ok {
		t.Error("Clear() left the metadata in place")
	}
}

// recordingRefresher captures the integrations passed to RefreshDeployedImageNames and can be
// primed to fail, so a test can assert metadata write propagates the rename and that a
// propagation failure never fails the write.
type recordingRefresher struct {
	calls []string
	err   error
}

func (r *recordingRefresher) RefreshDeployedImageNames(_ context.Context, integrationID string) error {
	r.calls = append(r.calls, integrationID)
	return r.err
}

// A successful metadata Set and Clear each trigger the deployed-image-name refresh, so a rename is
// mirrored onto the affected integration's servers immediately instead of at the next reconcile.
func TestSetOSImageMetadataPropagatesNameToServers(t *testing.T) {
	metadataRecords := newOSImageMetadataRepoFake()
	refresher := &recordingRefresher{}
	uc := NewSetOSImageMetadataUseCase(metadataRecords, refresher)

	if err := uc.Set(context.Background(), "integration-1", "ubuntu/jammy", "amd64", OSImageMetadataInput{Name: "Golden Ubuntu", OSSystem: "", Release: "", Tags: nil}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := uc.Clear(context.Background(), "integration-1", "ubuntu/jammy", "amd64"); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if len(refresher.calls) != 2 || refresher.calls[0] != "integration-1" || refresher.calls[1] != "integration-1" {
		t.Errorf("refresh calls = %v, want [integration-1 integration-1] for one Set and one Clear", refresher.calls)
	}
}

// The refresh is best-effort: a failing refresher must not turn a committed metadata write into a
// failed request, because the periodic reconcile still re-mirrors the name.
func TestSetOSImageMetadataIgnoresRefresherFailure(t *testing.T) {
	metadataRecords := newOSImageMetadataRepoFake()
	refresher := &recordingRefresher{err: errors.New("catalog unavailable")}
	uc := NewSetOSImageMetadataUseCase(metadataRecords, refresher)

	if err := uc.Set(context.Background(), "integration-1", "ubuntu/jammy", "amd64", OSImageMetadataInput{Name: "Golden Ubuntu", OSSystem: "", Release: "", Tags: nil}); err != nil {
		t.Fatalf("Set() with a failing refresher must still succeed, got %v", err)
	}
	if _, ok := metadataRecords.get("integration-1", "ubuntu/jammy", "amd64"); !ok {
		t.Error("Set() must persist the metadata even when propagation fails")
	}
}

func TestDeleteOSImagePrunesOverlay(t *testing.T) {
	provider := &osImageRemovableProvider{}
	metadataRecords := newOSImageMetadataRepoFake()
	metadataRecords.seed(&provisioningdomain.OSImageMetadata{
		IntegrationID: "integration-1", ImageID: "ubuntu/jammy", Architecture: "amd64", DisplayName: "Golden Ubuntu",
	})
	uc := NewDeleteOSImageUseCase(osImageTestFactory{provider: provider}, metadataRecords, newOSImageVerificationRepoFake())
	if err := uc.Execute(context.Background(), "integration-1", "ubuntu/jammy", "amd64"); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(provider.deleted) != 1 {
		t.Fatalf("provider deletions = %d, want 1", len(provider.deleted))
	}
	if _, ok := metadataRecords.get("integration-1", "ubuntu/jammy", "amd64"); ok {
		t.Error("Execute() did not prune the metadata after deleting the image")
	}
}

func TestDeleteOSImageUnsupportedProviderIsRejected(t *testing.T) {
	metadataRecords := newOSImageMetadataRepoFake()
	uc := NewDeleteOSImageUseCase(osImageTestFactory{provider: &osImageBaseProvider{}}, metadataRecords, newOSImageVerificationRepoFake())
	err := uc.Execute(context.Background(), "integration-1", "ubuntu/jammy", "amd64")
	var provErr *provisioningdomain.ProviderError
	if !errors.As(err, &provErr) || provErr.Kind != provisioningdomain.ProviderErrorRejected {
		t.Fatalf("Execute() error = %v, want a rejected ProviderError", err)
	}
}

// The catalog reports an effective default user: the metadata value, else swallow's built-in for
// the provider OS family. Relabeling the OS for display must not change the built-in.
func TestListOSImagesProjectsDefaultUser(t *testing.T) {
	provider := &osImageBaseProvider{images: []*provisioningdomain.OSImage{
		{ID: "ubuntu/noble", Name: "Ubuntu 24.04", OSSystem: "ubuntu", Release: "noble", Architecture: "amd64"},
		{ID: "custom/rocky", Name: "Rocky", OSSystem: "custom", Release: "rocky", Architecture: "amd64"},
		{ID: "custom/plain", Name: "Plain", OSSystem: "custom", Release: "plain", Architecture: "amd64"},
	}}
	metadataRecords := newOSImageMetadataRepoFake()
	metadataRecords.seed(&provisioningdomain.OSImageMetadata{
		IntegrationID: "integration-1", ImageID: "ubuntu/noble", Architecture: "amd64", OSSystem: "Golden OS",
	})
	metadataRecords.seed(&provisioningdomain.OSImageMetadata{
		IntegrationID: "integration-1", ImageID: "custom/rocky", Architecture: "amd64", DefaultUser: "cloud-user",
	})

	uc := NewListOSImagesUseCase(osImageTestFactory{provider: provider}, metadataRecords, newOSImageVerificationRepoFake())
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
		if item.Status.DefaultUser != tc.wantEffective || item.Spec.DefaultUser != tc.want {
			t.Errorf("%s default user = %q/%q, want %q/%q", item.ID, item.Status.DefaultUser, item.Spec.DefaultUser, tc.wantEffective, tc.want)
		}
	}
}

func TestSetOSImageMetadataValidatesDefaultUser(t *testing.T) {
	metadataRecords := newOSImageMetadataRepoFake()
	uc := NewSetOSImageMetadataUseCase(metadataRecords, nil)

	err := uc.Set(context.Background(), "integration-1", "custom/rocky", "amd64", OSImageMetadataInput{DefaultUser: "Cloud User"})
	if !errors.Is(err, provisioningdomain.ErrOSImageMetadataInvalid) {
		t.Fatalf("Set() error = %v, want ErrOSImageMetadataInvalid for a non-POSIX user", err)
	}

	if err := uc.Set(context.Background(), "integration-1", "custom/rocky", "amd64", OSImageMetadataInput{DefaultUser: " cloud-user "}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	stored, _ := metadataRecords.ListByIntegration(context.Background(), "integration-1")
	if len(stored) != 1 || stored[0].DefaultUser != "cloud-user" {
		t.Errorf("stored metadataRecords = %+v, want a default-user-only metadata kept (not deleted as empty)", stored)
	}
}
