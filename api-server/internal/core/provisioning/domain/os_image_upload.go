package domain

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// OSImageUploadLifetime is how long a recorded OS Image upload waits for its content. It only
// bounds the start of the content request: a long transfer that began in time is not cut off when
// the lifetime passes, because the upload is claimed when the transfer starts.
const OSImageUploadLifetime = time.Hour

// osImageUploadFieldMaxLen bounds each text field of an upload intent, so a malformed request
// cannot carry an unbounded string into the store or the provider request.
const osImageUploadFieldMaxLen = 200

// sha256HexPattern is the form the upload contract requires for a content digest: 64 lowercase
// hex characters, the same form providers compare against what they received.
var sha256HexPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// OSImageUploadState is where one OS Image upload is in its single-use lifecycle.
type OSImageUploadState string

const (
	// OSImageUploadPending means the intent is recorded and no content request has claimed it.
	OSImageUploadPending OSImageUploadState = "pending"
	// OSImageUploadSending means a content request claimed the upload and is streaming it to the
	// provider. A sending upload is never claimed again; it is removed when that request ends.
	OSImageUploadSending OSImageUploadState = "sending"
)

// OSImageUpload is swallow's short-lived record of one intent to upload a new provider-owned OS
// Image (decision 027): which provisioner receives it, the image's name and architecture, and the
// exact size and sha256 of the content the caller will stream.
//
// The record holds no artifact bytes and never becomes the image: the content is forwarded to the
// provider as it arrives, and the provider owns the resulting artifact. It exists so the intent is
// validated before any content is sent, and so the content request carries only the upload's id.
// It is single-use: the first content request claims it (pending → sending) and removes it when
// it ends, whether it succeeded or failed; an unclaimed upload expires at ExpiresAt.
type OSImageUpload struct {
	// ID is the swallow-issued opaque identifier the content request addresses.
	ID string
	// IntegrationID is the provisioner Integration that receives the image.
	IntegrationID string
	// Name is the user-chosen image name, before any provider namespacing.
	Name string
	// Architecture is the CPU architecture, e.g. "amd64"; the adapter expands it.
	Architecture string
	// Title is an optional human-readable label.
	Title string
	// FileType is the optional artifact format; empty means the provider's default.
	FileType string
	// SizeBytes is the exact length of the content the caller declared. The content request must
	// carry exactly this many bytes, and providers that reserve space before the bytes arrive
	// need it up front.
	SizeBytes int64
	// SHA256 is the lowercase hex digest of the content the caller declared. The provider verifies
	// the received content against it, so a wrong digest fails the upload at the provider.
	SHA256 string
	// DefaultUser is the optional default login user stored as the image's metadata once the
	// upload succeeds (decision 039).
	DefaultUser string
	// State is the upload's single-use lifecycle position.
	State OSImageUploadState
	// CreatedAt is when swallow recorded the intent.
	CreatedAt time.Time
	// ExpiresAt is when an upload that no content request claimed stops being usable.
	ExpiresAt time.Time
}

// Validate checks the swallow-owned rules of an upload intent: the required fields, the field
// lengths, a positive size, the digest's form, and the default user's form. It does not check
// the size against the installation's limit or anything the provider decides (whether the
// filetype or name is acceptable); those belong to the caller and the provider adapter.
// Failures wrap ErrInvalidOSImageUpload with a message fit for the API client.
func (u *OSImageUpload) Validate() error {
	switch {
	case u.IntegrationID == "":
		return invalidOSImageUpload("integrationId is required")
	case u.Name == "":
		return invalidOSImageUpload("name is required")
	case u.Architecture == "":
		return invalidOSImageUpload("architecture is required")
	case len(u.Name) > osImageUploadFieldMaxLen, len(u.Architecture) > osImageUploadFieldMaxLen,
		len(u.Title) > osImageUploadFieldMaxLen, len(u.FileType) > osImageUploadFieldMaxLen:
		return invalidOSImageUpload(fmt.Sprintf("name, architecture, title, and filetype must be at most %d characters", osImageUploadFieldMaxLen))
	case u.SizeBytes <= 0:
		return invalidOSImageUpload("sizeBytes must be a positive integer")
	case !sha256HexPattern.MatchString(u.SHA256):
		return invalidOSImageUpload("sha256 must be the content's SHA-256 as 64 lowercase hex characters")
	case u.DefaultUser != "" && !ValidDefaultUser(u.DefaultUser):
		return invalidOSImageUpload("defaultUser must be a POSIX login name (lowercase letters, digits, '_' or '-', up to 32 characters)")
	}
	return nil
}

// invalidOSImageUpload wraps ErrInvalidOSImageUpload with the client-facing reason, so delivery
// can branch on the sentinel and still show why the request was refused.
func invalidOSImageUpload(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidOSImageUpload, reason)
}

var (
	// ErrInvalidOSImageUpload means an upload intent or its content broke a swallow rule — content
	// whose length differs from the declared size, or that ended early, included — and the
	// wrapped message says which. It is a client error, mapped onto 400, even when it surfaces
	// through a provider adapter's read of the content stream.
	ErrInvalidOSImageUpload = errors.New("invalid os image upload")

	// ErrOSImageUploadTooLarge means the declared content is larger than the installation accepts
	// (api.imageUploadMaxBytes). It is mapped onto 413 before anything is recorded.
	ErrOSImageUploadTooLarge = errors.New("the os image is larger than this installation's upload limit")

	// ErrOSImageUploadNotFound means no usable upload has the id: it never existed, it expired, or
	// a content request already finished with it. It is a domain miss, mapped onto 404.
	ErrOSImageUploadNotFound = errors.New("os image upload not found or expired")

	// ErrOSImageUploadInProgress means another content request already claimed the upload and is
	// streaming it. It is mapped onto 409; the other request keeps the upload.
	ErrOSImageUploadInProgress = errors.New("the os image upload's content is already being sent")
)

// OSImageUploadRepository persists OS Image uploads between their two requests.
//
// Implementations must be safe for concurrent use: two content requests for one upload race on
// Claim, and exactly one may win. Production implementations must use durable storage shared by
// every API process, because the two requests may reach different processes; they should also
// discard expired uploads on their own (for example a store TTL), since an upload nobody claims is
// never deleted by the application. In-memory implementations are limited to tests.
type OSImageUploadRepository interface {
	// Create stores a new pending upload. The id is swallow-issued and unique.
	Create(ctx context.Context, upload *OSImageUpload) error
	// Claim atomically moves the pending upload with the id to sending, provided it has not
	// expired at now, and returns it. It returns ErrOSImageUploadNotFound when no such upload
	// exists or it has expired, and ErrOSImageUploadInProgress when it is already sending.
	Claim(ctx context.Context, id string, now time.Time) (*OSImageUpload, error)
	// Delete removes the upload. Deleting an absent upload is not an error.
	Delete(ctx context.Context, id string) error
}
