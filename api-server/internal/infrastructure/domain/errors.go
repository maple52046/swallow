// Package domain defines swallow's Infrastructure grouping model: Zone and Pool.
//
// Zone and Pool are swallow-owned, Site-scoped groupings of Servers (decision 029). swallow
// owns their catalog and lifecycle; when a Site's provisioner can express the same grouping the
// intent is realized there through the GroupingRealizer port, but the concept exists in swallow
// regardless of provider support. This package holds the entities, their persistence ports, the
// provider-realization port, and the domain errors — no MongoDB, HTTP, or provider vocabulary.
//
// This is not the Clean Architecture infrastructure (`infra/`) layer. "Infrastructure" here is
// the operator-facing area (the dashboard's Infrastructure section) whose domain is Zones and
// Pools; the provider adapters that realize them live under this feature's own `infra/`.
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
	// ErrPoolNameTaken means a Pool with this name already exists in the Site.
	ErrPoolNameTaken = errors.New("pool name already exists in site")

	// ErrInvalidGroup marks a Zone/Pool whose name or Site reference is not acceptable, such
	// as an empty name or a name that only differs by surrounding whitespace.
	ErrInvalidGroup = errors.New("invalid zone or pool")

	// ErrSiteNotFound means the Site a Zone/Pool would belong to does not exist. A grouping
	// must hang off an existing Site, per the Site glossary term.
	ErrSiteNotFound = errors.New("site not found")

	// ErrServerNotFound means the Server addressed by a placement request does not exist.
	ErrServerNotFound = errors.New("server not found")

	// ErrGroupingSiteMismatch means a placement referenced a Zone or Pool that belongs to a
	// different Site than the Server. Assigning across Sites is refused rather than realized.
	ErrGroupingSiteMismatch = errors.New("zone or pool belongs to a different site than the server")

	// ErrNothingToAssign means a placement request named neither a Zone nor a Pool, so there
	// is no state change to make.
	ErrNothingToAssign = errors.New("placement request specifies neither a zone nor a pool")

	// ErrProviderGroupingUnsupported means a Server's provisioner cannot express zones/pools,
	// so a placement cannot be realized. Unlike Zone/Pool catalog writes — which a Site with no
	// capable provisioner simply keeps swallow-local — a placement always targets a provisioner,
	// so its inability to group is surfaced rather than silently skipped.
	ErrProviderGroupingUnsupported = errors.New("provisioner does not support zone or pool grouping")
)
