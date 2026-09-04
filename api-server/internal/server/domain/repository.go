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
	// PlatformID filters on the membership axis.
	PlatformID string
	// Tag restricts the result to servers whose provisioner tags include this exact
	// value. It exists so a Prometheus scrape job can target one server type — the RDC
	// exporter job asks for "amd-gpu" — without the discovery endpoint hard-coding what
	// the tag means.
	Tag string

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
// way one comes into being. Delete exists only for an explicit provider-backed removal;
// its application use case must remove the external Machine first so reconciliation
// cannot recreate a locally deleted projection.
type ServerRepository interface {
	FindByID(ctx context.Context, id string) (*Server, error)

	// FindBySource looks a server up by its external key. Returns ErrServerNotFound
	// when the provisioner is reporting a machine swallow has not seen before.
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

	// SetGPUs replaces the mirrored GPU inventory, or clears it when gpus is empty.
	//
	// Separate from Upsert because the inventory sweep writes it on its own cadence: a
	// reconcile pass must be able to rewrite the rest of the projection without wiping
	// the GPUs the sweep found, and the sweep must be able to update GPUs without a full
	// projection in hand.
	SetGPUs(ctx context.Context, id string, gpus []GPU) error

	// SetDeployment replaces the Swallow-owned deployment result, or clears it after
	// release. Provider inventory Upsert must preserve this field.
	SetDeployment(ctx context.Context, id string, deployment *DeploymentStatus) error

	// CountByIntegration reports how many servers were projected from an
	// integration, so that deleting one that still has servers can be refused.
	CountByIntegration(ctx context.Context, integrationID string) (int, error)

	// Delete removes only the projection. Callers must establish the provider Machine is
	// already absent before invoking it; repositories cannot enforce that ordering.
	Delete(ctx context.Context, id string) error
}
