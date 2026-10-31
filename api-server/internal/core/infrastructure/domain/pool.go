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
// SiteID. It is realized as a provider resource pool when the Site's provisioner is
// grouping-capable, and exists in swallow otherwise.
type Pool struct {
	// ID is swallow-issued and stable; every swallow reference uses it.
	ID string
	// SiteID is the owning Site. A Pool never spans Sites.
	SiteID string
	// Name is operator-facing and unique within SiteID.
	Name string
	// Description is an optional operator note.
	Description string
	// ProviderRealized records whether the most recent swallow write to this Pool was
	// propagated to a grouping-capable provisioner. Swallow-owned metadata reflecting the last
	// write only; false means the Site had no grouping-capable provisioner when it was written.
	ProviderRealized bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// PoolRepository persists swallow-owned Pools.
//
// Implementations must be safe for concurrent use and must enforce name uniqueness per Site:
// Create and Update return ErrPoolNameTaken when a different Pool in the same Site already holds
// the name. FindByID and Update return ErrPoolNotFound for an unknown id. Production
// implementations must use durable storage; in-memory implementations are limited to tests.
type PoolRepository interface {
	Create(ctx context.Context, pool *Pool) error
	FindByID(ctx context.Context, id string) (*Pool, error)
	// List returns Pools sorted by name. An empty siteID returns every Pool across Sites.
	List(ctx context.Context, siteID string) ([]*Pool, error)
	// Update replaces the mutable fields (name, description, updatedAt) of an existing Pool.
	Update(ctx context.Context, pool *Pool) error
	Delete(ctx context.Context, id string) error
}
