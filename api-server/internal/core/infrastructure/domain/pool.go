package domain

import (
	"context"
	"time"
)

// Pool is a swallow-owned, Site-scoped resource pool: a grouping of Servers used to partition
// resources for allocation and organization (glossary: Pool).
//
// It mirrors Zone's shape and ownership rules but is a distinct concept: a Zone groups for
// availability/fault/organization, a Pool partitions for allocation. Name is unique within
// SiteID. The catalog controller realizes it as a resource pool on every grouping-capable
// provisioner of the Site.
type Pool struct {
	// ID is swallow-issued and stable; every swallow reference uses it.
	ID string
	// SiteID is the owning Site. A Pool never spans Sites.
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

// PoolRepository persists swallow-owned Pools, with the same contract as ZoneRepository and
// ErrPoolNameTaken / ErrPoolNotFound in place of the Zone errors.
type PoolRepository interface {
	Create(ctx context.Context, pool *Pool) error
	FindByID(ctx context.Context, id string) (*Pool, error)
	FindByName(ctx context.Context, siteID, name string) (*Pool, error)
	// List returns Pools sorted by name. An empty siteID returns every Pool across Sites.
	List(ctx context.Context, siteID string) ([]*Pool, error)
	// UpdateSpec stores pool's name and description and increments its generation, writing the
	// new generation and updatedAt back onto pool.
	UpdateSpec(ctx context.Context, pool *Pool) error
	// AdoptSpec makes a Pool that has no generation yet (one migrated from before desired state)
	// desired state at its first observation: it stores description and sets generation 1. It
	// does nothing when the Pool already has a generation or no longer exists.
	AdoptSpec(ctx context.Context, id, description string, at time.Time) error
	// SetStatus replaces the controller-written status; nothing else changes.
	SetStatus(ctx context.Context, id string, status GroupStatus) error
	Delete(ctx context.Context, id string) error
}
