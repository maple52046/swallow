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

// DeployedImageNameRefresher re-mirrors the effective OS image display name onto one
// integration's Server projections after an overlay write, so a rename shows on the fleet list
// and detail immediately instead of waiting for the next reconcile pass.
//
// Implementations must be best-effort from the caller's point of view: a returned error means the
// mirror could not be refreshed right now, and the overlay write is still authoritative and must
// not be rolled back — the periodic reconcile eventually re-mirrors the name. ReconcileUseCase is
// the production implementation (one catalog + overlay read, no provider machine poll).
type DeployedImageNameRefresher interface {
	RefreshDeployedImageNames(ctx context.Context, integrationID string) error
}

// SetOSImageOverlayUseCase writes or clears the swallow-owned display overlay for one OS Image.
//
// The overlay is owned data layered over a provider-owned image (docs/decisions/025): setting it
// never touches the provider, and clearing it reverts the image to its provider values. Each
// field (name, OS, release) is an independent override; an empty field means "use the provider
// value". The use case deliberately does not verify the image still exists in the provider
// catalog — an overlay for an image that later disappears is simply not merged by
// ListOSImagesUseCase — so it stays a cheap swallow-local write with no provider round trip.
//
// After a successful write it eagerly re-mirrors the effective display name onto the affected
// integration's deployed Server projections (docs/decisions/025) so a rename is visible on the
// fleet list at once; that step is best-effort and never fails the overlay write.
type SetOSImageOverlayUseCase struct {
	overlays provisioningdomain.OSImageOverlayRepository
	// refresher propagates the new effective name to Server projections after a write. It is
	// optional: nil means skip eager propagation and rely on the next reconcile pass, which the
	// application-layer unit tests use since they only exercise overlay normalization.
	refresher DeployedImageNameRefresher
	// now supplies the overlay write time and is injectable so tests can assert it without
	// depending on the wall clock.
	now func() time.Time
}

// NewSetOSImageOverlayUseCase wires the swallow overlay store the edit writes to and the optional
// refresher that propagates the effective name to Server projections after a write. Pass a nil
// refresher to skip eager propagation (the next reconcile pass still re-mirrors the name).
func NewSetOSImageOverlayUseCase(
	overlays provisioningdomain.OSImageOverlayRepository,
	refresher DeployedImageNameRefresher,
) *SetOSImageOverlayUseCase {
	return &SetOSImageOverlayUseCase{
		overlays:  overlays,
		refresher: refresher,
		now:       func() time.Time { return time.Now().UTC() },
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
		if err := uc.overlays.Delete(ctx, integrationID, imageID, architecture); err != nil {
			return err
		}
		uc.propagateName(ctx, integrationID)
		return nil
	}
	if err := uc.overlays.Upsert(ctx, overlay); err != nil {
		return err
	}
	uc.propagateName(ctx, integrationID)
	return nil
}

// propagateName eagerly re-mirrors the effective display name onto the integration's deployed
// Server projections so a rename is visible on the fleet list at once. It is best-effort: the
// overlay write has already succeeded and is authoritative, so a refresh failure is intentionally
// not returned — the periodic reconcile re-mirrors the name — and a nil refresher (unit tests)
// simply skips propagation.
func (uc *SetOSImageOverlayUseCase) propagateName(ctx context.Context, integrationID string) {
	if uc.refresher == nil {
		return
	}
	// The error is deliberately dropped: surfacing it would fail an overlay write that already
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
// Clearing an image that has no overlay is a success: the requested end state already holds. On
// success it eagerly re-mirrors the reverted (provider) name onto the affected Server projections.
func (uc *SetOSImageOverlayUseCase) Clear(ctx context.Context, integrationID, imageID, architecture string) error {
	if err := uc.overlays.Delete(ctx, integrationID, imageID, architecture); err != nil {
		return err
	}
	uc.propagateName(ctx, integrationID)
	return nil
}
