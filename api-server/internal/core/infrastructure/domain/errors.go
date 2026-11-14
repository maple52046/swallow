// Package domain defines swallow's Infrastructure grouping model: Zone and Pool.
//
// Both are swallow-owned groupings of Servers. A Zone is Site-scoped desired state that the catalog
// controller realizes on every grouping-capable provisioner of its Site (decision 071), and the
// concept exists in swallow regardless of provider support. A Pool is installation-wide and exists
// only in swallow (decision 082). This package holds the entities and the Zone status, their
// persistence ports, the ports onto Site provisioners and Server placements, and the domain errors —
// no MongoDB, HTTP, or provider vocabulary.
//
// This is not a Clean Architecture infrastructure layer. "Infrastructure" here is the
// user-facing area whose domain is Zones and Pools; the provisioner realizes Zones through the
// composition adapters in internal/app.
package domain

import "errors"

var (
	// ErrZoneNotFound means no Zone has the requested id.
	ErrZoneNotFound = errors.New("zone not found")
	// ErrZoneNameTaken means a Zone with this name already exists in the Site. Names are
	// unique per Site, not globally, because two Sites may both use "rack-a".
	ErrZoneNameTaken = errors.New("zone name already exists in site")

	// ErrPoolNotFound means no Pool has the requested id.
	ErrPoolNotFound = errors.New("pool not found")
	// ErrPoolNameTaken means a Pool with this name already exists. Pool names are unique across the
	// installation because a Pool belongs to no Site.
	ErrPoolNameTaken = errors.New("pool name already exists")

	// ErrInvalidGroup marks a Zone/Pool whose name, or a Zone whose Site reference, is not
	// acceptable, such as an empty name or a name that only differs by surrounding whitespace.
	ErrInvalidGroup = errors.New("invalid zone or pool")

	// ErrSiteNotFound means the Site a Zone would belong to does not exist. A Zone must hang off an
	// existing Site, per the Site glossary term.
	ErrSiteNotFound = errors.New("site not found")

	// ErrServerNotFound means the Server addressed by a placement request does not exist.
	ErrServerNotFound = errors.New("server not found")

	// ErrGroupingSiteMismatch means a placement referenced a Zone that belongs to a different Site
	// than the Server. Assigning across Sites is refused rather than realized; a Pool may hold
	// Servers of any Site.
	ErrGroupingSiteMismatch = errors.New("zone belongs to a different site than the server")

	// ErrNothingToAssign means a placement request named neither a Zone nor a Pool, so there
	// is no state change to make.
	ErrNothingToAssign = errors.New("placement request specifies neither a zone nor a pool")

	// ErrGroupInUse means a Zone or Pool cannot be deleted because a Server's placement spec
	// still references it.
	ErrGroupInUse = errors.New("zone or pool is still the placement of a server")

	// ErrPlacementConflict means the Server's spec changed while a placement was being written.
	ErrPlacementConflict = errors.New("server spec changed during the placement")

	// ErrClaimedServerPool means a placement would change the Pool of a Server a Platform claims. Its
	// Pool changes only with the whole Platform (MovePlatformToPool), so a Platform never ends up
	// across Pools by moving one of its Servers.
	ErrClaimedServerPool = errors.New("a server claimed by a platform changes pool only with its platform")

	// ErrPlatformNotFound means the Platform a Pool move names does not exist.
	ErrPlatformNotFound = errors.New("platform not found")
	// ErrPlatformHasNoServers means a Platform has no Server to move to a Pool, so moving it would
	// change nothing and could not give it a Pool.
	ErrPlatformHasNoServers = errors.New("platform has no servers")

	// ErrServerNotAdopted means the Server has no spec yet; its controller adopts it from the
	// first observation shortly, and a placement is accepted after that.
	ErrServerNotAdopted = errors.New("server is not adopted yet")
)
