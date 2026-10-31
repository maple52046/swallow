package application

import (
	"context"
	"strings"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// maxOSImageMetadataFieldLength bounds each swallow OS Image metadata field (and each tag) so one
// label cannot grow without limit. These are swallow presentation strings, so the bound is
// generous but finite; it counts runes, not bytes, so multi-byte labels are not penalised.
const maxOSImageMetadataFieldLength = 200

// maxOSImageMetadataTags bounds how many tags one image may carry, so a single metadata cannot
// grow unbounded. It is generous for organizing labels while keeping the document small.
const maxOSImageMetadataTags = 50

// DeployedImageNameRefresher re-mirrors the effective OS image display name onto one
// integration's Server projections after metadata write, so a rename shows on the fleet list
// and detail immediately instead of waiting for the next reconcile pass.
//
// Implementations must be best-effort from the caller's point of view: a returned error means the
// mirror could not be refreshed right now, and the metadata write is still authoritative and must
// not be rolled back — the periodic reconcile eventually re-mirrors the name. ReconcileUseCase is
// the production implementation (one catalog + metadata read, no provider machine poll).
type DeployedImageNameRefresher interface {
	RefreshDeployedImageNames(ctx context.Context, integrationID string) error
}

// SetOSImageMetadataUseCase writes or clears swallow's metadata for one OS Image.
//
// The metadata is desired state that needs no provider realization (decision 071): setting it
// never touches the provider, and clearing it reverts the image to its provider values. Each
// field (name, OS, release) is an independent override; an empty field means "use the provider
// value". The use case deliberately does not verify the image still exists in the provider
// catalog — metadata for an image that later disappears is simply not merged by
// ListOSImagesUseCase — so it stays a cheap swallow-local write with no provider round trip.
//
// After a successful write it eagerly re-mirrors the effective display name onto the affected
// integration's deployed Server projections so a rename is visible on the
// fleet list at once; that step is best-effort and never fails the metadata write.
type SetOSImageMetadataUseCase struct {
	metadataRecords provisioningdomain.OSImageMetadataRepository
	// refresher propagates the new effective name to Server projections after a write. It is
	// optional: nil means skip eager propagation and rely on the next reconcile pass, which the
	// application-layer unit tests use since they only exercise metadata normalization.
	refresher DeployedImageNameRefresher
	// now supplies the metadata write time and is injectable so tests can assert it without
	// depending on the wall clock.
	now func() time.Time
}

// NewSetOSImageMetadataUseCase wires the swallow metadata store the edit writes to and the optional
// refresher that propagates the effective name to Server projections after a write. Pass a nil
// refresher to skip eager propagation (the next reconcile pass still re-mirrors the name).
func NewSetOSImageMetadataUseCase(
	metadataRecords provisioningdomain.OSImageMetadataRepository,
	refresher DeployedImageNameRefresher,
) *SetOSImageMetadataUseCase {
	return &SetOSImageMetadataUseCase{
		metadataRecords: metadataRecords,
		refresher:       refresher,
		now:             func() time.Time { return time.Now().UTC() },
	}
}

// OSImageMetadataInput is the full set of swallow-owned values for one image's metadata. A Set
// replaces the whole metadata, so an omitted (empty) field clears that value.
type OSImageMetadataInput struct {
	Name        string
	OSSystem    string
	Release     string
	Tags        []string
	DefaultUser string
}

// Set stores the given display overrides, tags, and default user for the image identified by
// integrationID, imageID, and architecture. Each value is trimmed; an empty value clears that
// field. Tags are trimmed, blanks dropped, and duplicates removed while preserving order. When
// nothing remains after normalization, the whole metadata is deleted rather than storing an empty
// record, so an all-blank edit reverts the image to its provider values. A field or tag longer
// than the field limit, more than the tag limit, or a default user that is not a POSIX login name
// is rejected with ErrOSImageMetadataInvalid.
func (uc *SetOSImageMetadataUseCase) Set(
	ctx context.Context,
	integrationID, imageID, architecture string,
	input OSImageMetadataInput,
) error {
	normalizedTags, err := normalizeOSImageTags(input.Tags)
	if err != nil {
		return err
	}
	metadata := &provisioningdomain.OSImageMetadata{
		IntegrationID: integrationID,
		ImageID:       imageID,
		Architecture:  architecture,
		DisplayName:   strings.TrimSpace(input.Name),
		OSSystem:      strings.TrimSpace(input.OSSystem),
		Release:       strings.TrimSpace(input.Release),
		Tags:          normalizedTags,
		DefaultUser:   strings.TrimSpace(input.DefaultUser),
		UpdatedAt:     uc.now(),
	}
	for _, field := range []string{metadata.DisplayName, metadata.OSSystem, metadata.Release} {
		if len([]rune(field)) > maxOSImageMetadataFieldLength {
			return provisioningdomain.ErrOSImageMetadataInvalid
		}
	}
	if metadata.DefaultUser != "" && !provisioningdomain.ValidDefaultUser(metadata.DefaultUser) {
		return provisioningdomain.ErrOSImageMetadataInvalid
	}
	// Metadata with nothing set is meaningless: clear it instead of persisting an empty
	// record, so read-back and bulk "reset" behave identically to never having set one.
	if !metadata.HasOverride() {
		if err := uc.metadataRecords.Delete(ctx, integrationID, imageID, architecture); err != nil {
			return err
		}
		uc.propagateName(ctx, integrationID)
		return nil
	}
	if err := uc.metadataRecords.Upsert(ctx, metadata); err != nil {
		return err
	}
	uc.propagateName(ctx, integrationID)
	return nil
}

// propagateName eagerly re-mirrors the effective display name and default user onto the
// integration's deployed Server projections so an edit is visible on the fleet list, and used by
// automation, at once. It is best-effort: the
// metadata write has already succeeded and is authoritative, so a refresh failure is intentionally
// not returned — the periodic reconcile re-mirrors the name — and a nil refresher (unit tests)
// simply skips propagation.
func (uc *SetOSImageMetadataUseCase) propagateName(ctx context.Context, integrationID string) {
	if uc.refresher == nil {
		return
	}
	// The error is deliberately dropped: surfacing it would fail metadata write that already
	// committed, and the next reconcile pass corrects any name this refresh could not update.
	_ = uc.refresher.RefreshDeployedImageNames(ctx, integrationID)
}

// normalizeOSImageTags trims each tag, drops blanks, removes duplicates while preserving first
// occurrence order, and enforces the per-tag length and total-count limits. It returns a nil
// slice when no tags remain, so an all-blank tag list contributes nothing to HasOverride.
func normalizeOSImageTags(tags []string) ([]string, error) {
	if len(tags) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(tags))
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed == "" {
			continue
		}
		if len([]rune(trimmed)) > maxOSImageMetadataFieldLength {
			return nil, provisioningdomain.ErrOSImageMetadataInvalid
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) > maxOSImageMetadataTags {
		return nil, provisioningdomain.ErrOSImageMetadataInvalid
	}
	if len(normalized) == 0 {
		return nil, nil
	}
	return normalized, nil
}

// Clear removes any swallow metadata for the image, reverting every field to its provider value.
// Clearing an image that has no metadata is a success: the requested end state already holds. On
// success it eagerly re-mirrors the reverted (provider) name onto the affected Server projections.
func (uc *SetOSImageMetadataUseCase) Clear(ctx context.Context, integrationID, imageID, architecture string) error {
	if err := uc.metadataRecords.Delete(ctx, integrationID, imageID, architecture); err != nil {
		return err
	}
	uc.propagateName(ctx, integrationID)
	return nil
}
