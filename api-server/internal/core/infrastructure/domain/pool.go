package domain

import (
	"context"
	"time"
)

// Pool is a swallow-owned, installation-wide grouping of Servers used to partition resources for
// allocation and access (glossary: Pool; decision 082).
//
// A Pool belongs to no Site, so its Servers may be in several Sites, and it exists only in swallow:
// no provisioner holds it, and a provisioner's own resource pools are only observed on each Server.
// It needs no realization, so it has no generation or status. Name is unique within the
// installation. Its memberships and Sudo Policy belong to the access context, keyed by ID.
type Pool struct {
	// ID is swallow-issued and stable; every swallow reference uses it.
	ID string
	// Name is user-facing and unique within the installation.
	Name string
	// Description is an optional user note.
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// PoolRepository persists swallow-owned Pools.
//
// Implementations must be safe for concurrent use and must enforce name uniqueness across the
// installation: Create and Update return ErrPoolNameTaken when a different Pool already holds the
// name. FindByID and Update return ErrPoolNotFound for an unknown Pool. Delete joins the caller's
// store transaction through ctx, so a Pool and its access records go together. Production
// implementations must use durable storage; in-memory implementations are limited to tests.
type PoolRepository interface {
	Create(ctx context.Context, pool *Pool) error
	FindByID(ctx context.Context, id string) (*Pool, error)
	// List returns every Pool sorted by name.
	List(ctx context.Context) ([]*Pool, error)
	// Update stores pool's name, description, and updatedAt.
	Update(ctx context.Context, pool *Pool) error
	Delete(ctx context.Context, id string) error
}
