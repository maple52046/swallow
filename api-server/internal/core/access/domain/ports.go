package domain

import "context"

// The ports below are the access context's read-only views of facts other contexts own. The
// composition root adapts each one onto its owner's repository, so this context depends on none of
// those contexts' stores.

// ServerPools reports which Pool each Server is placed in. PoolsOf returns a map from each known
// Server id to its Pool id ("" for a Server in no Pool); an id it does not know is absent from the
// map, which callers treat as a Server the caller may not see.
type ServerPools interface {
	PoolsOf(ctx context.Context, serverIDs []string) (map[string]string, error)
	// ServersIn returns the ids of every Server placed in one of poolIDs.
	ServersIn(ctx context.Context, poolIDs []string) ([]string, error)
}

// PlatformPools reports which Pool a Platform belongs to (identity-and-access.md §5): the Pool every
// one of its nodes is placed in, "" when its nodes are in different Pools or in none. found is false
// for an unknown Platform.
type PlatformPools interface {
	PoolOf(ctx context.Context, platformID string) (poolID string, found bool, err error)
}

// WorkflowTargets reports the Servers and Platforms a Workflow targets, the resources whose access
// decides who may read or control it. found is false for an unknown Workflow.
type WorkflowTargets interface {
	TargetsOf(ctx context.Context, workflowID string) (serverIDs, platformIDs []string, found bool, err error)
}

// PoolRef is what the access context needs of one Pool to manage its memberships: its id, Site,
// and name.
type PoolRef struct {
	ID     string
	SiteID string
	Name   string
}

// PoolLookup finds a Pool; found is false for an unknown one.
type PoolLookup interface {
	Pool(ctx context.Context, poolID string) (pool PoolRef, found bool, err error)
}

// UserCleanup removes what other contexts keep for a User that is being deleted: its Sessions, API
// Keys, and Access Keys. It joins the caller's transaction through ctx, so a refused deletion
// leaves everything in place.
type UserCleanup interface {
	RemoveUserData(ctx context.Context, userID string) error
}

// DirectoryServer is what the Directory projection needs of one Server: its id, its Pool, the names
// it answers to (observed hostname and FQDN), and its addresses (for sudo rules).
type DirectoryServer struct {
	ID        string
	PoolID    string
	Names     []string
	Addresses []string
}

// DirectoryPool is what the Directory projection needs of one Pool.
type DirectoryPool struct {
	ID   string
	Name string
}

// DirectoryInputs reads the facts the Directory projection combines that other contexts own: every
// present Server, every Pool, every Access Key by owner (authorized_keys lines), and the Host Access
// limits Platforms put on their nodes (a Server without a limit is absent from the map).
type DirectoryInputs interface {
	Servers(ctx context.Context) ([]DirectoryServer, error)
	Pools(ctx context.Context) ([]DirectoryPool, error)
	AccessKeys(ctx context.Context) (map[string][]string, error)
	HostAccessLimits(ctx context.Context) (map[string]HostAccessLimit, error)
}
