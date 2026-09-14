package domain

import (
	"context"
	"strings"
)

// maxGroupNameLen bounds a Zone/Pool name so a single field cannot carry an unbounded string
// into storage or a provider request. It is generous for an operator label.
const maxGroupNameLen = 100

// ValidateName trims and checks a Zone/Pool name, returning the cleaned value.
//
// It is shared by Zone and Pool because both use the same operator-facing naming rule: a
// non-empty, length-bounded label. An empty or whitespace-only name, or one past the length
// bound, is ErrInvalidGroup. The returned value is what callers must store and send to a
// provider, so surrounding whitespace never causes swallow and the provider to disagree.
func ValidateName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || len(trimmed) > maxGroupNameLen {
		return "", ErrInvalidGroup
	}
	return trimmed, nil
}

// SiteReader reports whether a Site exists, so a Zone/Pool can be refused before it is stored
// against a Site that does not exist. It is a read-only projection of the Site aggregate owned by
// the site feature; the infra adapter maps the site repository's not-found onto ErrSiteNotFound.
type SiteReader interface {
	// Exists returns true when the Site is present. A transport failure is returned as an error
	// rather than as a false, so a create does not proceed on a failed lookup.
	Exists(ctx context.Context, siteID string) (bool, error)
}

// ServerPlacementTarget is the minimum a placement needs about a Server: which Site and
// provisioner integration it belongs to, the provider's own machine id to address it by, and its
// currently observed group names so a response can show the effective state of a field that a
// request left unchanged.
type ServerPlacementTarget struct {
	ServerID          string
	SiteID            string
	IntegrationID     string
	ProviderMachineID string
	ObservedZone      string
	ObservedPool      string
}

// ServerLocator resolves a Server id into its placement target. It is a narrow read port over
// the server feature's projection; the infra adapter maps the server repository's not-found onto
// ErrServerNotFound so this feature does not import the server domain's error vocabulary.
type ServerLocator interface {
	Locate(ctx context.Context, serverID string) (*ServerPlacementTarget, error)
}

// GroupingRealizer propagates swallow-owned Zone/Pool intent to a provisioner when that
// provisioner can express the same grouping (decision 029).
//
// Catalog writes are keyed by Site: the implementation resolves the Site's provisioner
// integration itself, and a Site with no grouping-capable provisioner is not an error — the
// boolean result reports whether the intent was actually realized, so a swallow-only Zone/Pool
// returns (false, nil). A provider transport or refusal is returned as an error (a
// provisioningdomain.ProviderError, surfaced to the caller).
//
// Placement is keyed by the Server's own provisioner integration and provider machine id, which
// the caller already holds from the Server projection. Because a Server always has a provisioner,
// a provisioner that lacks the grouping capability is returned as ErrProviderGroupingUnsupported
// rather than as a silent no-op.
type GroupingRealizer interface {
	EnsureZone(ctx context.Context, siteID, name, description string) (realized bool, err error)
	RenameZone(ctx context.Context, siteID, currentName, newName, description string) (realized bool, err error)
	DeleteZone(ctx context.Context, siteID, name string) (realized bool, err error)

	EnsurePool(ctx context.Context, siteID, name, description string) (realized bool, err error)
	RenamePool(ctx context.Context, siteID, currentName, newName, description string) (realized bool, err error)
	DeletePool(ctx context.Context, siteID, name string) (realized bool, err error)

	AssignServerZone(ctx context.Context, integrationID, providerMachineID, zoneName string) error
	AssignServerPool(ctx context.Context, integrationID, providerMachineID, poolName string) error
}
