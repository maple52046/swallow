package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// CreateOSImageUploadInput is the swallow-neutral upload intent the delivery layer decodes from
// the create request. SizeBytes and SHA256 describe the content the caller will stream; the caller
// computes them, because the provider needs both before the first byte (decision 027).
type CreateOSImageUploadInput struct {
	IntegrationID string
	Name          string
	Architecture  string
	Title         string
	FileType      string
	DefaultUser   string
	SizeBytes     int64
	SHA256        string
}

// OSImageUploadService drives one provisioner to create a new provider-owned OS Image from
// user-supplied content, in two steps: Create records the validated intent as an OS Image upload,
// and SendContent streams the content to the provider (decision 027).
//
// Upload is an optional provider capability, so a provisioner without it is refused at Create,
// before the caller sends any content. swallow keeps no copy of the artifact: SendContent hands
// the caller's stream to the provider adapter, which forwards it as it arrives, and the provider
// owns the result and decides whether it is a custom image. The only metadata an upload may set
// is the default user, because the user uploading a custom image is the one who knows its login
// account. The service holds no per-request state; the upload record carries the intent between
// the two requests, so they may reach different API processes.
type OSImageUploadService struct {
	providers       provisioningdomain.ProviderFactory
	uploads         provisioningdomain.OSImageUploadRepository
	metadataRecords provisioningdomain.OSImageMetadataRepository
	maxSizeBytes    int64
	now             func() time.Time
	newID           func() string
}

// NewOSImageUploadService wires the provider factory uploads resolve their provisioner from, the
// store of upload records, and the metadata store a default user is written to. maxSizeBytes is
// the installation's upload limit (api.imageUploadMaxBytes); an intent declaring more is refused.
func NewOSImageUploadService(
	providers provisioningdomain.ProviderFactory,
	uploads provisioningdomain.OSImageUploadRepository,
	metadataRecords provisioningdomain.OSImageMetadataRepository,
	maxSizeBytes int64,
) *OSImageUploadService {
	return &OSImageUploadService{
		providers:       providers,
		uploads:         uploads,
		metadataRecords: metadataRecords,
		maxSizeBytes:    maxSizeBytes,
		now:             func() time.Time { return time.Now().UTC() },
		newID:           uuid.NewString,
	}
}

// Create validates an upload intent and records it as a pending OS Image upload that expires
// after provisioningdomain.OSImageUploadLifetime. It has no effect on the provider.
//
// Errors: a broken swallow rule is ErrInvalidOSImageUpload; a size over the installation's limit
// is ErrOSImageUploadTooLarge; the provider factory's errors (unknown Integration, not a
// provisioner, no credential) pass through; a provisioner without the upload capability, or whose
// adapter refuses the intent, is a rejected *ProviderError.
func (s *OSImageUploadService) Create(
	ctx context.Context,
	input CreateOSImageUploadInput,
) (*provisioningdomain.OSImageUpload, error) {
	now := s.now()
	upload := &provisioningdomain.OSImageUpload{
		ID:            s.newID(),
		IntegrationID: strings.TrimSpace(input.IntegrationID),
		Name:          strings.TrimSpace(input.Name),
		Architecture:  strings.TrimSpace(input.Architecture),
		Title:         strings.TrimSpace(input.Title),
		FileType:      strings.TrimSpace(input.FileType),
		SizeBytes:     input.SizeBytes,
		SHA256:        strings.TrimSpace(input.SHA256),
		DefaultUser:   strings.TrimSpace(input.DefaultUser),
		State:         provisioningdomain.OSImageUploadPending,
		CreatedAt:     now,
		ExpiresAt:     now.Add(provisioningdomain.OSImageUploadLifetime),
	}
	if err := upload.Validate(); err != nil {
		return nil, err
	}
	if upload.SizeBytes > s.maxSizeBytes {
		return nil, provisioningdomain.ErrOSImageUploadTooLarge
	}

	uploader, err := s.uploader(ctx, upload.IntegrationID)
	if err != nil {
		return nil, err
	}
	if err := uploader.ValidateOSImageUpload(uploadRequest(upload, nil)); err != nil {
		return nil, err
	}

	if err := s.uploads.Create(ctx, upload); err != nil {
		return nil, fmt.Errorf("record os image upload: %w", err)
	}
	return upload, nil
}

// SendContent streams the content of one upload to its provisioner and returns the created image
// once the provider reports it fully staged.
//
// contentLength is the request's declared length and content the request body; the service reads
// content once, sequentially, and never more than the upload's size. The upload is claimed first,
// so a concurrent request for the same upload is ErrOSImageUploadInProgress and an unknown or
// expired one ErrOSImageUploadNotFound; both leave the upload as it was. Every request that claims
// the upload removes it when it returns, whether it succeeded or failed, so a failed upload is
// retried with a new one. A contentLength other than the declared size, or content that ends
// early, is ErrInvalidOSImageUpload. The provider adapter's errors pass through (a refusal is a
// rejected *ProviderError, a transport failure or an image not staged in time an unavailable one).
// When the provider accepted the content but storing the default user failed, the error is
// returned although the image exists; the user sets the default user afterwards.
func (s *OSImageUploadService) SendContent(
	ctx context.Context,
	id string,
	contentLength int64,
	content io.Reader,
) (*OSImageItem, error) {
	upload, err := s.uploads.Claim(ctx, id, s.now())
	if err != nil {
		return nil, err
	}
	defer s.removeUpload(ctx, upload.ID)

	if contentLength != upload.SizeBytes {
		return nil, fmt.Errorf("%w: Content-Length must equal the upload's sizeBytes (%d bytes)",
			provisioningdomain.ErrInvalidOSImageUpload, upload.SizeBytes)
	}

	uploader, err := s.uploader(ctx, upload.IntegrationID)
	if err != nil {
		return nil, err
	}
	body := &exactLengthReader{r: content, remaining: upload.SizeBytes}
	image, err := uploader.UploadOSImage(ctx, uploadRequest(upload, body))
	if err != nil {
		// Content that ended early fails the provider transfer too; the caller's mistake, not
		// the adapter's wording of the broken transfer, is what the client needs to see.
		if body.shortErr != nil {
			return nil, body.shortErr
		}
		return nil, err
	}

	item := newOSImageItem(image)
	if upload.DefaultUser == "" {
		return &item, nil
	}
	metadata := &provisioningdomain.OSImageMetadata{
		IntegrationID: upload.IntegrationID,
		ImageID:       image.ID,
		Architecture:  image.Architecture,
		DefaultUser:   upload.DefaultUser,
		UpdatedAt:     s.now(),
	}
	if err := s.metadataRecords.Upsert(ctx, metadata); err != nil {
		return nil, fmt.Errorf("the image was uploaded but its default user was not saved: %w", err)
	}
	applyOSImageMetadata(&item, metadata)
	return &item, nil
}

// uploader resolves the Integration's provisioner and its optional upload capability. The
// capability is checked again by SendContent because the Integration may have changed since
// Create; a provisioner without it is a rejected *ProviderError, not an internal error.
func (s *OSImageUploadService) uploader(
	ctx context.Context,
	integrationID string,
) (provisioningdomain.OSImageUploader, error) {
	provider, err := s.providers.For(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	uploader, ok := provider.(provisioningdomain.OSImageUploader)
	if !ok {
		return nil, unsupported("os image upload")
	}
	return uploader, nil
}

// removeUpload deletes a claimed upload when its content request returns. It runs detached from
// the request's cancellation so a client that went away still releases the record. A failed
// delete is only logged: the upload is already claimed, so it can never be used again, and the
// store's expiry discards it.
func (s *OSImageUploadService) removeUpload(ctx context.Context, id string) {
	deleteCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := s.uploads.Delete(deleteCtx, id); err != nil {
		slog.Warn("os image upload record was not removed; it expires on its own", "uploadId", id, "error", err)
	}
}

// uploadRequest builds the provider request for an upload record; content is nil when the
// request is only validated.
func uploadRequest(upload *provisioningdomain.OSImageUpload, content io.Reader) provisioningdomain.UploadOSImageRequest {
	return provisioningdomain.UploadOSImageRequest{
		Name:         upload.Name,
		Architecture: upload.Architecture,
		Title:        upload.Title,
		FileType:     upload.FileType,
		Size:         upload.SizeBytes,
		SHA256:       upload.SHA256,
		Content:      content,
	}
}

// exactLengthReader yields exactly remaining bytes of r and then io.EOF, and reports a stream
// that ends sooner as ErrInvalidOSImageUpload instead of a plain EOF.
//
// The distinction matters to the provider adapter: an adapter reading in chunks treats EOF (and
// io.ErrUnexpectedEOF from a short final chunk) as the end of the content, so a client that
// disconnected mid-transfer would otherwise look like a complete upload and leave the provider
// waiting for bytes that never come. It never reads past remaining, so a body cannot feed the
// provider more than the declared size.
type exactLengthReader struct {
	r         io.Reader
	remaining int64
	// shortErr is the ErrInvalidOSImageUpload returned once the stream ended early, kept so the
	// service can report it in place of the adapter's error for the same broken transfer.
	shortErr error
}

func (e *exactLengthReader) Read(p []byte) (int, error) {
	if e.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > e.remaining {
		p = p[:e.remaining]
	}
	n, err := e.r.Read(p)
	e.remaining -= int64(n)
	if e.remaining == 0 {
		return n, io.EOF
	}
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			e.shortErr = fmt.Errorf("%w: the content ended %d bytes before the upload's sizeBytes",
				provisioningdomain.ErrInvalidOSImageUpload, e.remaining)
			return n, e.shortErr
		}
		return n, fmt.Errorf("read os image upload content: %w", err)
	}
	return n, nil
}
