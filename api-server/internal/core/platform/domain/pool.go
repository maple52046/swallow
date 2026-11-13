package domain

// PoolOfMembers derives the Pool a Platform belongs to (decision 082; identity-and-access.md §5)
// from its Servers and where each is placed: the one Pool every Server in serverIDs is placed in
// (pools maps a Server id onto its placement, "" for none), or "" when they are in different Pools,
// any is in none or unknown, or there is no Server. Only the owners of that Pool, and admins, may
// manage the Platform, so a Platform spread over Pools belongs to admins alone.
func PoolOfMembers(serverIDs []string, pools map[string]string) string {
	pool := ""
	for _, id := range serverIDs {
		placed, ok := pools[id]
		if !ok || placed == "" {
			return ""
		}
		if pool == "" {
			pool = placed
		} else if placed != pool {
			return ""
		}
	}
	return pool
}
