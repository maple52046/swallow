package domain

import "slices"

// Scope is what one caller may reach, computed once per request from the caller's current User
// Role and Pool Memberships (identity-and-access.md §4). An admin reaches everything. Any other
// User reads the Pools it is an owner or member of, and operates the Pools it owns. A resource that
// belongs to no Pool ("" pool id) is reachable by admins only.
//
// A Scope is a request-scoped value: it must not be cached across requests, because a demotion or a
// removed membership takes effect on the caller's next request.
type Scope struct {
	UserID   string
	Username string
	Admin    bool
	// Pools maps each Pool id the caller belongs to onto its Pool Role there.
	Pools map[string]PoolRole
}

// ReadsPool reports whether the caller may read the resources of poolID.
func (s Scope) ReadsPool(poolID string) bool {
	if s.Admin {
		return true
	}
	_, ok := s.Pools[poolID]
	return poolID != "" && ok
}

// OperatesPool reports whether the caller may operate the resources of poolID.
func (s Scope) OperatesPool(poolID string) bool {
	if s.Admin {
		return true
	}
	return poolID != "" && s.Pools[poolID] == RoleOwner
}

// ReadablePools returns the Pool ids the caller may read, sorted, for a list filter; it is
// meaningful only when Admin is false (an admin is not filtered at all).
func (s Scope) ReadablePools() []string {
	ids := make([]string, 0, len(s.Pools))
	for id := range s.Pools {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// Access is what a route needs of the caller for one resource: to read it or to operate it.
type Access int

const (
	// Read is satisfied by any Pool Role (and by admins).
	Read Access = iota
	// Operate is satisfied by the owner role (and by admins).
	Operate
)

// Allows reports whether the caller has access of the given kind to the resources of poolID.
func (s Scope) Allows(access Access, poolID string) bool {
	if access == Operate {
		return s.OperatesPool(poolID)
	}
	return s.ReadsPool(poolID)
}
