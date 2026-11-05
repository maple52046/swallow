package application

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// testUploadSHA256 is a well-formed digest; the service only checks its form, the provider
// compares it with the content.
const testUploadSHA256 = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

// osImageUploadableProvider adds the optional OSImageUploader capability. It records the request
// and the content it read, so a test can confirm the service forwards the declared intent and
// exactly the declared bytes; validateErr and uploadErr inject the adapter's refusals.
type osImageUploadableProvider struct {
	osImageBaseProvider
	validateErr error
	uploadErr   error
	validated   []provisioningdomain.UploadOSImageRequest
	received    *provisioningdomain.UploadOSImageRequest
	content     string
	result      *provisioningdomain.OSImage
}

func (p *osImageUploadableProvider) ValidateOSImageUpload(req provisioningdomain.UploadOSImageRequest) error {
	p.validated = append(p.validated, req)
	return p.validateErr
}

func (p *osImageUploadableProvider) UploadOSImage(
	_ context.Context, req provisioningdomain.UploadOSImageRequest,
) (*provisioningdomain.OSImage, error) {
	p.received = &req
	// Read the content like a real adapter streams it once; a broken read is wrapped the way
	// the MAAS adapter wraps it, so the test sees what the service sees in production.
	raw, err := io.ReadAll(req.Content)
	p.content = string(raw)
	if err != nil {
		return nil, &provisioningdomain.ProviderError{Kind: provisioningdomain.ProviderErrorUnavailable,
			Detail: "Reading the upload stream failed before the image was fully sent.", Err: err}
	}
	if p.uploadErr != nil {
		return nil, p.uploadErr
	}
	return p.result, nil
}

// osImageUploadRepoFake is an in-memory OSImageUploadRepository with the store's claim semantics.
type osImageUploadRepoFake struct {
	mu      sync.Mutex
	uploads map[string]*provisioningdomain.OSImageUpload
}

func newOSImageUploadRepoFake() *osImageUploadRepoFake {
	return &osImageUploadRepoFake{uploads: map[string]*provisioningdomain.OSImageUpload{}}
}

func (r *osImageUploadRepoFake) Create(_ context.Context, upload *provisioningdomain.OSImageUpload) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := *upload
	r.uploads[upload.ID] = &stored
	return nil
}

func (r *osImageUploadRepoFake) Claim(_ context.Context, id string, now time.Time) (*provisioningdomain.OSImageUpload, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	upload, ok := r.uploads[id]
	switch {
	case !ok:
		return nil, provisioningdomain.ErrOSImageUploadNotFound
	case upload.State == provisioningdomain.OSImageUploadSending:
		return nil, provisioningdomain.ErrOSImageUploadInProgress
	case !upload.ExpiresAt.After(now):
		return nil, provisioningdomain.ErrOSImageUploadNotFound
	}
	upload.State = provisioningdomain.OSImageUploadSending
	claimed := *upload
	return &claimed, nil
}

func (r *osImageUploadRepoFake) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.uploads, id)
	return nil
}

func (r *osImageUploadRepoFake) get(id string) (*provisioningdomain.OSImageUpload, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	upload, ok := r.uploads[id]
	return upload, ok
}

// uploadServiceFixture builds the service over fakes with a fixed clock and id.
type uploadServiceFixture struct {
	service  *OSImageUploadService
	provider *osImageUploadableProvider
	uploads  *osImageUploadRepoFake
	metadata *osImageMetadataRepoFake
	now      time.Time
}

func newUploadServiceFixture(provider provisioningdomain.OSProvisioningProvider) *uploadServiceFixture {
	f := &uploadServiceFixture{
		uploads:  newOSImageUploadRepoFake(),
		metadata: newOSImageMetadataRepoFake(),
		now:      time.Date(2026, 11, 5, 7, 0, 0, 0, time.UTC),
	}
	if uploadable, ok := provider.(*osImageUploadableProvider); ok {
		f.provider = uploadable
	}
	f.service = NewOSImageUploadService(osImageTestFactory{provider: provider}, f.uploads, f.metadata, 1024)
	f.service.now = func() time.Time { return f.now }
	f.service.newID = func() string { return "upload-1" }
	return f
}

func validUploadInput() CreateOSImageUploadInput {
	return CreateOSImageUploadInput{
		IntegrationID: "integration-1",
		Name:          " rocky-10 ",
		Architecture:  "amd64",
		FileType:      "tgz",
		SizeBytes:     4,
		SHA256:        testUploadSHA256,
	}
}

// Create records a validated, trimmed intent as a pending upload expiring one lifetime later, and
// asks the adapter to validate it without sending anything.
func TestCreateOSImageUploadRecordsPendingIntent(t *testing.T) {
	f := newUploadServiceFixture(&osImageUploadableProvider{})

	upload, err := f.service.Create(context.Background(), validUploadInput())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if upload.ID != "upload-1" || upload.Name != "rocky-10" || upload.State != provisioningdomain.OSImageUploadPending {
		t.Errorf("Create() = %+v, want pending upload-1 named rocky-10", upload)
	}
	if want := f.now.Add(provisioningdomain.OSImageUploadLifetime); !upload.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %s, want %s", upload.ExpiresAt, want)
	}
	if _, ok := f.uploads.get("upload-1"); !ok {
		t.Error("Create() did not store the upload")
	}
	if len(f.provider.validated) != 1 || f.provider.validated[0].FileType != "tgz" || f.provider.received != nil {
		t.Errorf("adapter validated %+v and received %+v, want one validation and no upload", f.provider.validated, f.provider.received)
	}
}

// Create refuses an intent before anything is stored: a broken swallow rule, a size over the
// installation's limit, a provisioner without the capability, and an adapter refusal.
func TestCreateOSImageUploadRefusesIntent(t *testing.T) {
	adapterRefusal := &provisioningdomain.ProviderError{Kind: provisioningdomain.ProviderErrorRejected, Detail: "unsupported file type"}
	tests := []struct {
		name     string
		provider provisioningdomain.OSProvisioningProvider
		edit     func(*CreateOSImageUploadInput)
		wantErr  error
	}{
		{name: "missing name", provider: &osImageUploadableProvider{},
			edit: func(in *CreateOSImageUploadInput) { in.Name = "  " }, wantErr: provisioningdomain.ErrInvalidOSImageUpload},
		{name: "malformed sha256", provider: &osImageUploadableProvider{},
			edit: func(in *CreateOSImageUploadInput) { in.SHA256 = "deadbeef" }, wantErr: provisioningdomain.ErrInvalidOSImageUpload},
		{name: "non-positive size", provider: &osImageUploadableProvider{},
			edit: func(in *CreateOSImageUploadInput) { in.SizeBytes = 0 }, wantErr: provisioningdomain.ErrInvalidOSImageUpload},
		{name: "invalid default user", provider: &osImageUploadableProvider{},
			edit: func(in *CreateOSImageUploadInput) { in.DefaultUser = "Cloud User" }, wantErr: provisioningdomain.ErrInvalidOSImageUpload},
		{name: "over the upload limit", provider: &osImageUploadableProvider{},
			edit: func(in *CreateOSImageUploadInput) { in.SizeBytes = 1025 }, wantErr: provisioningdomain.ErrOSImageUploadTooLarge},
		{name: "adapter refusal", provider: &osImageUploadableProvider{validateErr: adapterRefusal},
			edit: func(*CreateOSImageUploadInput) {}, wantErr: adapterRefusal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newUploadServiceFixture(tc.provider)
			input := validUploadInput()
			tc.edit(&input)

			if _, err := f.service.Create(context.Background(), input); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Create() error = %v, want %v", err, tc.wantErr)
			}
			if _, ok := f.uploads.get("upload-1"); ok {
				t.Error("a refused intent must not be stored")
			}
		})
	}

	t.Run("provisioner without upload", func(t *testing.T) {
		f := newUploadServiceFixture(&osImageBaseProvider{})
		_, err := f.service.Create(context.Background(), validUploadInput())
		var provErr *provisioningdomain.ProviderError
		if !errors.As(err, &provErr) || provErr.Kind != provisioningdomain.ProviderErrorRejected {
			t.Fatalf("Create() error = %v, want a rejected ProviderError", err)
		}
	})
}

// SendContent forwards the declared intent and exactly the declared bytes to the provider,
// stores the default user, returns the provider's image, and removes the upload.
func TestSendOSImageUploadContentForwardsAndRemovesUpload(t *testing.T) {
	provider := &osImageUploadableProvider{
		result: &provisioningdomain.OSImage{ID: "rocky-10", Name: "rocky-10", OSSystem: "custom", Release: "rocky-10", Architecture: "amd64", Complete: true},
	}
	f := newUploadServiceFixture(provider)
	input := validUploadInput()
	input.DefaultUser = "cloud-user"
	if _, err := f.service.Create(context.Background(), input); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	item, err := f.service.SendContent(context.Background(), "upload-1", 4, strings.NewReader("data"))
	if err != nil {
		t.Fatalf("SendContent() error = %v", err)
	}
	if provider.received.Name != "rocky-10" || provider.received.Size != 4 || provider.received.SHA256 != testUploadSHA256 {
		t.Errorf("provider request = %+v, want the declared name, size, and sha256", provider.received)
	}
	if provider.content != "data" {
		t.Errorf("provider content = %q, want %q", provider.content, "data")
	}
	if item.ID != "rocky-10" || item.Status.ProviderOSSystem != "custom" || item.Status.DefaultUser != "cloud-user" {
		t.Errorf("SendContent() item = %+v, want rocky-10, custom, cloud-user", item)
	}
	stored, _ := f.metadata.ListByIntegration(context.Background(), "integration-1")
	if len(stored) != 1 || stored[0].ImageID != "rocky-10" || stored[0].DefaultUser != "cloud-user" {
		t.Errorf("stored metadata = %+v, want one cloud-user metadata for rocky-10", stored)
	}
	if _, ok := f.uploads.get("upload-1"); ok {
		t.Error("a finished content request must remove its upload")
	}
}

// A claimed upload is removed whatever the outcome; a content error is reported as the client's
// error even when the provider adapter's read of the stream is what surfaced it.
func TestSendOSImageUploadContentFailures(t *testing.T) {
	providerRefusal := &provisioningdomain.ProviderError{Kind: provisioningdomain.ProviderErrorRejected, Detail: "Saved content does not match given SHA256 value."}
	tests := []struct {
		name          string
		provider      *osImageUploadableProvider
		contentLength int64
		content       string
		wantErr       error
		wantForwarded bool
	}{
		{name: "length differs from the declared size", provider: &osImageUploadableProvider{},
			contentLength: 5, content: "data!", wantErr: provisioningdomain.ErrInvalidOSImageUpload},
		{name: "content ends early", provider: &osImageUploadableProvider{},
			contentLength: 4, content: "da", wantErr: provisioningdomain.ErrInvalidOSImageUpload, wantForwarded: true},
		{name: "provider refuses the content", provider: &osImageUploadableProvider{uploadErr: providerRefusal},
			contentLength: 4, content: "data", wantErr: providerRefusal, wantForwarded: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newUploadServiceFixture(tc.provider)
			if _, err := f.service.Create(context.Background(), validUploadInput()); err != nil {
				t.Fatalf("Create() error = %v", err)
			}

			_, err := f.service.SendContent(context.Background(), "upload-1", tc.contentLength, strings.NewReader(tc.content))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("SendContent() error = %v, want %v", err, tc.wantErr)
			}
			var provErr *provisioningdomain.ProviderError
			if errors.Is(tc.wantErr, provisioningdomain.ErrInvalidOSImageUpload) && errors.As(err, &provErr) {
				t.Errorf("SendContent() error = %v, want the content error itself, not the adapter's", err)
			}
			if forwarded := tc.provider.received != nil; forwarded != tc.wantForwarded {
				t.Errorf("forwarded to the provider = %t, want %t", forwarded, tc.wantForwarded)
			}
			if _, ok := f.uploads.get("upload-1"); ok {
				t.Error("a failed content request must remove the upload it claimed")
			}
		})
	}
}

// An unknown or expired upload is not found, and an upload another request is sending is in
// progress; neither touches the provider nor removes the other request's upload.
func TestSendOSImageUploadContentClaimRefusals(t *testing.T) {
	provider := &osImageUploadableProvider{}
	f := newUploadServiceFixture(provider)
	if _, err := f.service.Create(context.Background(), validUploadInput()); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if _, err := f.service.SendContent(context.Background(), "unknown", 4, strings.NewReader("data")); !errors.Is(err, provisioningdomain.ErrOSImageUploadNotFound) {
		t.Errorf("unknown upload error = %v, want ErrOSImageUploadNotFound", err)
	}

	f.uploads.uploads["upload-1"].State = provisioningdomain.OSImageUploadSending
	if _, err := f.service.SendContent(context.Background(), "upload-1", 4, strings.NewReader("data")); !errors.Is(err, provisioningdomain.ErrOSImageUploadInProgress) {
		t.Errorf("sending upload error = %v, want ErrOSImageUploadInProgress", err)
	}
	if _, ok := f.uploads.get("upload-1"); !ok {
		t.Error("a refused claim must leave the other request's upload in place")
	}

	f.uploads.uploads["upload-1"].State = provisioningdomain.OSImageUploadPending
	f.now = f.now.Add(provisioningdomain.OSImageUploadLifetime)
	if _, err := f.service.SendContent(context.Background(), "upload-1", 4, strings.NewReader("data")); !errors.Is(err, provisioningdomain.ErrOSImageUploadNotFound) {
		t.Errorf("expired upload error = %v, want ErrOSImageUploadNotFound", err)
	}
	if provider.received != nil {
		t.Error("a refused claim must not reach the provider")
	}
}

// The exact-length reader never feeds the provider more than the declared size.
func TestExactLengthReaderStopsAtDeclaredSize(t *testing.T) {
	raw, err := io.ReadAll(&exactLengthReader{r: strings.NewReader("data and more"), remaining: 4})
	if err != nil || string(raw) != "data" {
		t.Errorf("ReadAll() = %q, %v; want %q, nil", raw, err, "data")
	}
}
