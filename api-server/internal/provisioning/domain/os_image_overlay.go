package domain

import (
	"context"
	"errors"
	"regexp"
	"strings"
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
	// DefaultUser is the image's default login user — the account its cloud-init creates and
	// authorizes provisioner SSH keys for — which swallow automation logs in as on Servers
	// deployed with this image (decision 039). Like Tags it has no provider counterpart; it is
	// merged over a swallow built-in (BuiltinDefaultUser), not over a provider value, and never
	// changes what the image deploys. Empty means "use the built-in, if any".
	DefaultUser string
	// UpdatedAt records when swallow last wrote this overlay. It is swallow's own write time,
	// not a provider observation, so it is not a staleness signal.
	UpdatedAt time.Time
}

// HasOverride reports whether the overlay carries at least one override, tag, or default user.
// An overlay with nothing set is meaningless and callers delete it rather than storing it.
func (o *OSImageOverlay) HasOverride() bool {
	return o.DisplayName != "" || o.OSSystem != "" || o.Release != "" || len(o.Tags) > 0 || o.DefaultUser != ""
}

// defaultUserPattern is the portable POSIX login-name shape (the useradd default): it keeps the
// value safe to pass as an SSH and Ansible user without quoting.
var defaultUserPattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// ValidDefaultUser reports whether user is an acceptable OS Image default user.
func ValidDefaultUser(user string) bool {
	return defaultUserPattern.MatchString(user)
}

// builtinDefaultUsers maps a provider OS family to the default user its official cloud images
// create. It is swallow's convention for synced images; a custom image (OS family "custom") has
// no reliable default, so an operator sets one on the overlay.
var builtinDefaultUsers = map[string]string{
	"ubuntu": "ubuntu",
	"centos": "centos",
	"rhel":   "cloud-user",
}

// BuiltinDefaultUser returns swallow's built-in default user for a provider OS family, or "".
// providerOSSystem must be the provider's own value, never an overlay label: relabeling an image's
// OS for display must not change which account automation logs in as.
func BuiltinDefaultUser(providerOSSystem string) string {
	return builtinDefaultUsers[strings.ToLower(strings.TrimSpace(providerOSSystem))]
}

// EffectiveDefaultUser is the single precedence rule for an image's default user: the overlay
// value, else the built-in for the provider OS family, else "" (automation then falls back to the
// Site SSH user and built-in candidates).
func EffectiveDefaultUser(overlayDefaultUser, providerOSSystem string) string {
	if overlayDefaultUser != "" {
		return overlayDefaultUser
	}
	return BuiltinDefaultUser(providerOSSystem)
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
