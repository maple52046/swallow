package domain

import (
	"context"
	"strings"

	"github.com/maple52046/swallow/internal/core/convergence"
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
// the site feature; the composition adapter maps the site repository's not-found onto ErrSiteNotFound.
type SiteReader interface {
	// Exists returns true when the Site is present. A transport failure is returned as an error
	// rather than as a false, so a create does not proceed on a failed lookup.
	Exists(ctx context.Context, siteID string) (bool, error)
}

// GroupStatus is what the catalog controller reports for a Zone or Pool. ObservedGeneration is the
// generation realized on every target; Conditions summarize the targets (realized only when every
// target is), and Provisioners holds one realization per provisioner Integration of the Site.
type GroupStatus struct {
	ObservedGeneration int64
	Conditions         []convergence.Condition
	Provisioners       []GroupRealization
}

// GroupRealization is a Zone's or Pool's realization on one provisioner (TargetID is the
// provisioner Integration id). ProviderName is the name the group carries there, "" until swallow
// has created or recognized it, so a rename knows which provider group to rename.
type GroupRealization struct {
	convergence.TargetStatus
	ProviderName string
}

// Realization returns the realization for one provisioner, or a fresh one when there is none.
func (s GroupStatus) Realization(integrationID string) GroupRealization {
	for _, r := range s.Provisioners {
		if r.TargetID == integrationID {
			return r
		}
	}
	return GroupRealization{TargetStatus: convergence.TargetStatus{TargetID: integrationID}}
}

// GroupingTargets resolves the provisioners Zones and Pools are realized on: every enabled
// provisioner Integration of a Site.
type GroupingTargets interface {
	// ForSite returns the enabled provisioner Integration ids of one Site.
	ForSite(ctx context.Context, siteID string) ([]string, error)
	// Sites returns every Site that has an enabled provisioner Integration.
	Sites(ctx context.Context) ([]string, error)
}

// PlacementReferences counts the Servers whose spec places them in a Zone or Pool, so a group a
// Server still needs is not deleted.
type PlacementReferences interface {
	CountZone(ctx context.Context, zoneID string) (int, error)
	CountPool(ctx context.Context, poolID string) (int, error)
}

// ServerPlacement is a Server's placement spec: the Zone and Pool it must be in, "" meaning the
// provisioner's default group.
type ServerPlacement struct {
	ServerID   string
	SiteID     string
	Generation int64
	ZoneID     string
	PoolID     string
}

// ServerPlacements reads and writes the placement part of a Server's spec. It is a narrow port
// over the server feature: the composition adapter maps that feature's not-found, conflict, and
// not-adopted errors onto this package's ErrServerNotFound, ErrPlacementConflict, and
// ErrServerNotAdopted.
type ServerPlacements interface {
	Read(ctx context.Context, serverID string) (*ServerPlacement, error)
	// Write replaces the placement, conditional on the Server still being at expectedGeneration,
	// and returns the new generation. The rest of the Server's spec is kept.
	Write(ctx context.Context, serverID string, expectedGeneration int64, zoneID, poolID string) (int64, error)
}
