package domain

import (
	"context"
	"errors"
	"time"
)

// ServerTagOverlay is the swallow-owned set of tags for one Server, used only when that Server's
// provisioner cannot own tags itself.
//
// It is the fallback half of decision 031's capability-first rule for tags: a provisioner that
// advertises Tagging owns the tags (swallow drives it and mirrors the result into Observed.Tags),
// so no overlay is written for such a Server; a provisioner that does not leaves the tags unowned,
// and swallow owns them here. The overlay is owned data (decision 025) — there is no external
// owner, so it carries no observedAt and no staleness — and it is never written back to a
// provider. Reconcile unions it into the Server's Observed.Tags at projection time, so list,
// detail, discovery, and Server Type all see the same effective tags regardless of which half of
// the rule produced them.
type ServerTagOverlay struct {
	// ServerID is the swallow Server this overlay belongs to and the overlay's whole key: a
	// Server has exactly one owned tag set.
	ServerID string
	// IntegrationID is the Server's provisioner integration, stored so reconcile can list every
	// overlay for one integration in a single pass rather than reading them one Server at a time.
	IntegrationID string
	// Tags is the swallow-owned tag set. Empty means the Server has no owned tags; an overlay
	// with no tags is meaningless and callers delete it rather than storing it.
	Tags []string
	// UpdatedAt records when swallow last wrote this overlay. It is swallow's own write time, not
	// a provider observation, so it is not a staleness signal.
	UpdatedAt time.Time
}

// ServerTagOverlayRepository persists swallow-owned Server tag overlays.
//
// Implementations must be safe for concurrent use and keyed by ServerID (one overlay per Server).
// Overlays are owned data, so a missing overlay is a normal absence — the Server simply has no
// swallow-owned tags — surfaced as ErrServerTagOverlayNotFound by Get and as an empty result by
// ListByIntegration; Delete treats an already-absent overlay as success. Production
// implementations must use durable storage; in-memory implementations are limited to tests.
type ServerTagOverlayRepository interface {
	// Get returns the overlay for one Server, or ErrServerTagOverlayNotFound when none exists.
	Get(ctx context.Context, serverID string) (*ServerTagOverlay, error)
	// ListByIntegration returns every overlay stored for one integration. It returns an empty
	// slice, not an error, when none exist.
	ListByIntegration(ctx context.Context, integrationID string) ([]*ServerTagOverlay, error)
	// Upsert stores or replaces the overlay for its ServerID, replacing the whole tag set.
	Upsert(ctx context.Context, overlay *ServerTagOverlay) error
	// Delete removes the overlay for the given Server. Deleting an absent overlay is not an error:
	// the requested end state (no swallow-owned tags) is already true.
	Delete(ctx context.Context, serverID string) error
}

// ErrServerTagOverlayNotFound means no swallow-owned tag overlay exists for a Server. It is a
// normal absence, not a failure: the Server simply has no owned tags.
var ErrServerTagOverlayNotFound = errors.New("server tag overlay not found")
