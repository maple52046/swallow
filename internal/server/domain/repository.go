package domain

import (
	"context"
	"time"
)

// ListFilter narrows a server listing. Zero values mean no constraint.
type ListFilter struct {
	SiteID        string
	IntegrationID string
	// ProvisioningState filters on the provisioning axis.
	ProvisioningState string
	// Keyword is a case-insensitive substring match on hostname, FQDN, or address.
	Keyword string
	// IncludeAbsent includes servers whose provisioner stopped reporting them.
	// Excluded by default: an absent machine is usually not what a caller wants to
	// act on, but it must stay findable.
	IncludeAbsent bool
	// ClusterID filters on the membership axis.
	ClusterID string

	Offset int
	// Limit of 0 means no limit, which is what the discovery endpoints need: a
	// scrape target list or an automation inventory must be complete, not paginated.
	Limit int
}

type ListResult struct {
	Servers []*Server
	Total   int
}

// ServerRepository persists server projections.
//
// There is no Create: servers are produced by reconciliation, so Upsert is the only
// way one comes into being. That is deliberate — a second, manual creation path would
// produce servers with no provisioner behind them, which nothing else could act on.
type ServerRepository interface {
	FindByID(ctx context.Context, id string) (*Server, error)

	// FindBySource looks a server up by its external key. Returns ErrServerNotFound
	// when the provisioner is reporting a machine gdcm has not seen before.
	FindBySource(ctx context.Context, source Source) (*Server, error)

	// FindByHardware returns every server matching any non-empty hardware
	// identifier. More than one result is ErrAmbiguousHardware's territory and is
	// resolved by the caller, not here. Empty hardware matches nothing.
	FindByHardware(ctx context.Context, hardware Hardware) ([]*Server, error)

	List(ctx context.Context, filter ListFilter) (ListResult, error)

	// Upsert creates or replaces the projection for a server, keyed by ID.
	Upsert(ctx context.Context, server *Server) error

	// MarkAbsent flags every server of one integration not touched by the reconcile
	// pass that started at seenBefore.
	//
	// A sweep by timestamp rather than a list of present IDs: a fleet's inventory can
	// be thousands of machines, and passing them all back as an exclusion set on
	// every pass scales badly for no benefit.
	MarkAbsent(ctx context.Context, integrationID string, seenBefore time.Time) (int, error)

	// SetMembership replaces the membership axis, or clears it when membership is nil.
	SetMembership(ctx context.Context, id string, membership *MembershipStatus) error

	// CountByIntegration reports how many servers were projected from an
	// integration, so that deleting one that still has servers can be refused.
	CountByIntegration(ctx context.Context, integrationID string) (int, error)

	Delete(ctx context.Context, id string) error
}
