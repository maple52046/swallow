package domain

import (
	"context"
	"time"
)

// Zone is a swallow-owned, Site-scoped grouping of Servers for availability, fault, or
// organizational separation (glossary: Zone).
//
// swallow owns the Zone; its spec is Name and Description, and the catalog controller makes every
// grouping-capable provisioner of the Site carry it (decision 071). Name is unique within SiteID.
// A Server's placement in a Zone is part of the Server's own spec, referencing the Zone by ID.
type Zone struct {
	// ID is swallow-issued and stable; every swallow reference uses it.
	ID string
	// SiteID is the owning Site. A Zone never spans Sites.
	SiteID string
	// Generation counts spec changes: 1 when created, +1 on every rename or description change.
	Generation int64
	// Name is operator-facing and unique within SiteID.
	Name string
	// Description is an optional operator note.
	Description string
	// Status is written only by the catalog controller.
	Status    GroupStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ZoneRepository persists swallow-owned Zones.
//
// Implementations must be safe for concurrent use and must enforce name uniqueness per Site:
// Create and UpdateSpec return ErrZoneNameTaken when a different Zone in the same Site already
// holds the name. FindByID, FindByName, UpdateSpec, and SetStatus return ErrZoneNotFound for an
// unknown Zone. Production implementations must use durable storage; in-memory implementations
// are limited to tests.
type ZoneRepository interface {
	Create(ctx context.Context, zone *Zone) error
	FindByID(ctx context.Context, id string) (*Zone, error)
	FindByName(ctx context.Context, siteID, name string) (*Zone, error)
	// List returns Zones sorted by name. An empty siteID returns every Zone across Sites.
	List(ctx context.Context, siteID string) ([]*Zone, error)
	// UpdateSpec stores zone's name and description and increments its generation, writing the
	// new generation and updatedAt back onto zone.
	UpdateSpec(ctx context.Context, zone *Zone) error
	// SetStatus replaces the controller-written status; nothing else changes.
	SetStatus(ctx context.Context, id string, status GroupStatus) error
	Delete(ctx context.Context, id string) error
}
