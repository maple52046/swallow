package application

import (
	"context"
	"strings"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// maxOSImageOverlayFieldLength bounds each swallow OS Image overlay field (and each tag) so one
// label cannot grow without limit. These are swallow presentation strings, so the bound is
// generous but finite; it counts runes, not bytes, so multi-byte labels are not penalised.
const maxOSImageOverlayFieldLength = 200

// maxOSImageOverlayTags bounds how many tags one image may carry, so a single overlay cannot
// grow unbounded. It is generous for organizing labels while keeping the document small.
const maxOSImageOverlayTags = 50

// SetOSImageOverlayUseCase writes or clears the swallow-owned display overlay for one OS Image.
//
// The overlay is owned data layered over a provider-owned image (docs/decisions/025): setting it
// never touches the provider, and clearing it reverts the image to its provider values. Each
// field (name, OS, release) is an independent override; an empty field means "use the provider
// value". The use case deliberately does not verify the image still exists in the provider
// catalog — an overlay for an image that later disappears is simply not merged by
// ListOSImagesUseCase — so it stays a cheap swallow-local write with no provider round trip.
type SetOSImageOverlayUseCase struct {
	overlays provisioningdomain.OSImageOverlayRepository
	// now supplies the overlay write time and is injectable so tests can assert it without
	// depending on the wall clock.
	now func() time.Time
}

// NewSetOSImageOverlayUseCase wires the swallow overlay store the edit writes to.
func NewSetOSImageOverlayUseCase(overlays provisioningdomain.OSImageOverlayRepository) *SetOSImageOverlayUseCase {
	return &SetOSImageOverlayUseCase{
		overlays: overlays,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// Set stores the given display overrides and tags for the image identified by integrationID,
// imageID, and architecture. Each value is trimmed; an empty value clears that field's override.
// Tags are trimmed, blanks dropped, and duplicates removed while preserving order. When nothing
// carries an override or tag after normalization, the whole overlay is deleted rather than
// storing an empty record, so an all-blank edit reverts the image to its provider values. A
// field or tag longer than the field limit, or more than the tag limit, is rejected with
// ErrOSImageOverlayInvalid.
func (uc *SetOSImageOverlayUseCase) Set(
	ctx context.Context,
	integrationID, imageID, architecture, name, osSystem, release string,
	tags []string,
) error {
	normalizedTags, err := normalizeOSImageTags(tags)
	if err != nil {
		return err
	}
	overlay := &provisioningdomain.OSImageOverlay{
		IntegrationID: integrationID,
		ImageID:       imageID,
		Architecture:  architecture,
		DisplayName:   strings.TrimSpace(name),
		OSSystem:      strings.TrimSpace(osSystem),
		Release:       strings.TrimSpace(release),
		Tags:          normalizedTags,
		UpdatedAt:     uc.now(),
	}
	for _, field := range []string{overlay.DisplayName, overlay.OSSystem, overlay.Release} {
		if len([]rune(field)) > maxOSImageOverlayFieldLength {
			return provisioningdomain.ErrOSImageOverlayInvalid
		}
	}
	// An overlay with nothing set is meaningless: clear it instead of persisting an empty
	// record, so read-back and bulk "reset" behave identically to never having set one.
	if !overlay.HasOverride() {
		return uc.overlays.Delete(ctx, integrationID, imageID, architecture)
	}
	return uc.overlays.Upsert(ctx, overlay)
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
		if len([]rune(trimmed)) > maxOSImageOverlayFieldLength {
			return nil, provisioningdomain.ErrOSImageOverlayInvalid
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) > maxOSImageOverlayTags {
		return nil, provisioningdomain.ErrOSImageOverlayInvalid
	}
	if len(normalized) == 0 {
		return nil, nil
	}
	return normalized, nil
}

// Clear removes any swallow overlay for the image, reverting every field to its provider value.
// Clearing an image that has no overlay is a success: the requested end state already holds.
func (uc *SetOSImageOverlayUseCase) Clear(ctx context.Context, integrationID, imageID, architecture string) error {
	return uc.overlays.Delete(ctx, integrationID, imageID, architecture)
}
