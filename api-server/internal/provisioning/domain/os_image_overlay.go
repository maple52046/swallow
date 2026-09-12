package domain

import (
	"context"
	"errors"
	"time"
)

// OSImageOverlay is swallow-owned display data layered over a provider-owned OS Image.
//
// It is a Provider Data Overlay (see docs/decisions/025): the provider stays authoritative for
// the image artifact and its own fields, while swallow owns only the display overrides it adds
// here. The overlay is owned data — there is no external owner, so it carries no observedAt and
// no staleness — and it is never written back to the provider. It is keyed by the same identity
// the provider catalog uses to name an image within one Integration; because one image name can
// back several architectures, the architecture is part of that key.
//
// Each override field is optional: an empty string means "no override, use the provider value".
// The provider offers no way to rename an image or relabel its OS/release, and none of these
// display strings has an external owner, so swallow may hold them without conflicting with the
// provider. The deployable image identity (its ID/distroSeries) is never overridden — only the
// human-facing labels are.
type OSImageOverlay struct {
	// IntegrationID scopes the overlay to one provisioner Integration; an image ID only
	// identifies an image within the provisioner that reported it.
	IntegrationID string
	// ImageID is the provider's opaque image identifier, exactly as ListOSImages reports it
	// (for example "ubuntu/jammy"). It is part of the overlay key.
	ImageID string
	// Architecture is the CPU architecture the overlaid image targets (for example "amd64").
	// One image name can back several architectures, so it completes the overlay key.
	Architecture string
	// DisplayName overrides the provider label as the effective image name. Empty means the
	// image keeps its provider label.
	DisplayName string
	// OSSystem overrides the provider's OS family label (for example "ubuntu"). Empty means the
	// image keeps its provider OS value. This is a display label only; it does not change what
	// the image deploys.
	OSSystem string
	// Release overrides the provider's release label (for example "jammy"). Empty means the
	// image keeps its provider release value. Display only, like OSSystem.
	Release string
	// Tags are swallow-owned labels attached to the image for organizing and searching. Unlike
	// the other fields they have no provider counterpart to override — a provider image carries
	// no tags — so they are purely additive owned data, not an override. Empty means no tags.
	Tags []string
	// UpdatedAt records when swallow last wrote this overlay. It is swallow's own write time,
	// not a provider observation, so it is not a staleness signal.
	UpdatedAt time.Time
}

// HasOverride reports whether the overlay carries at least one override or tag. An overlay with
// nothing set is meaningless and callers delete it rather than storing it.
func (o *OSImageOverlay) HasOverride() bool {
	return o.DisplayName != "" || o.OSSystem != "" || o.Release != "" || len(o.Tags) > 0
}

// OSImageOverlayRepository persists swallow-owned OS Image overlays.
//
// Implementations must be safe for concurrent use. The overlay key is
// (IntegrationID, ImageID, Architecture); implementations enforce it as a uniqueness
// constraint so one image never carries two conflicting overlays. Overlays are owned data, so a
// missing overlay is a normal absence — the image simply shows its provider values — not an
// error: ListByIntegration returns only the overlays that exist, and Delete treats an
// already-absent overlay as success. Production implementations must use durable storage;
// in-memory implementations are limited to tests.
type OSImageOverlayRepository interface {
	// ListByIntegration returns every overlay stored for one Integration. It returns an empty
	// slice, not an error, when none exist.
	ListByIntegration(ctx context.Context, integrationID string) ([]*OSImageOverlay, error)
	// Upsert stores or replaces the overlay for its (IntegrationID, ImageID, Architecture)
	// key, replacing every override field so cleared fields are persisted as empty.
	Upsert(ctx context.Context, overlay *OSImageOverlay) error
	// Delete removes the overlay for the given key, reverting the image to its provider values.
	// Deleting an overlay that does not exist is not an error: the requested end state (no
	// swallow overrides) is already true.
	Delete(ctx context.Context, integrationID, imageID, architecture string) error
}

// ErrOSImageOverlayInvalid means a requested OS Image overlay field failed validation — for
// example an override exceeded the allowed length. It is a domain validation miss, mapped by
// delivery onto a 400, not an infrastructure failure.
var ErrOSImageOverlayInvalid = errors.New("invalid os image overlay")
