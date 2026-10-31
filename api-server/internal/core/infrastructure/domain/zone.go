package domain

import (
	"context"
	"time"
)

// Zone is a swallow-owned, Site-scoped grouping of Servers for availability, fault, or
// organizational separation (glossary: Zone).
//
// swallow owns the identity and lifecycle; a Zone exists whether or not the Site's provisioner
// can express one. Name is unique within SiteID. The Server-to-Zone relationship is not stored
// here: it is provider-observed on the Server projection and only driven through the provisioner.
type Zone struct {
	// ID is swallow-issued and stable; every swallow reference uses it.
	ID string
	// SiteID is the owning Site. A Zone never spans Sites.
	SiteID string
	// Name is operator-facing and unique within SiteID.
	Name string
	// Description is an optional operator note.
	Description string
	// ProviderRealized records whether the most recent swallow write to this Zone was
	// propagated to a grouping-capable provisioner. It is swallow-owned metadata reflecting the
	// last write only, not a live reconciliation against the provider's actual set: false means
	// the Site had no grouping-capable provisioner when the Zone was last written.
	ProviderRealized bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// ZoneRepository persists swallow-owned Zones.
//
// Implementations must be safe for concurrent use and must enforce name uniqueness per Site:
// Create and Update return ErrZoneNameTaken when a different Zone in the same Site already holds
// the name. FindByID and Update return ErrZoneNotFound for an unknown id. Production
// implementations must use durable storage; in-memory implementations are limited to tests.
type ZoneRepository interface {
	Create(ctx context.Context, zone *Zone) error
	FindByID(ctx context.Context, id string) (*Zone, error)
	// List returns Zones sorted by name. An empty siteID returns every Zone across Sites.
	List(ctx context.Context, siteID string) ([]*Zone, error)
	// Update replaces the mutable fields (name, description, updatedAt) of an existing Zone.
	Update(ctx context.Context, zone *Zone) error
	Delete(ctx context.Context, id string) error
}
