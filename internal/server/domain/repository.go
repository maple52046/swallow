package domain

import "context"

type ListFilter struct {
	Status  ServerStatus
	Keyword string
	Offset  int
	Limit   int
}

type ListResult struct {
	Servers []*Server
	Total   int
}

// ServerRepository defines the persistence contract for Server entities.
// Implementations must not create nodes implicitly; inventory and agent updates
// must validate that the node already exists before writing.
type ServerRepository interface {
	Create(ctx context.Context, server *Server) error
	FindByID(ctx context.Context, id string) (*Server, error)
	List(ctx context.Context, filter ListFilter) (ListResult, error)
	Delete(ctx context.Context, id string) error
	ExistsByHostname(ctx context.Context, hostname string) (bool, error)
	ExistsByIP(ctx context.Context, ip string) (bool, error)

	// UpdateInventory replaces only the inventory sub-document for the given node.
	// Returns ErrServerNotFound if the node does not exist.
	UpdateInventory(ctx context.Context, id string, inv Inventory) error

	// UpdateAgentInfo replaces only the agent sub-document for the given node.
	// Used by the gRPC stream handler to record connectivity state and last_seen_at.
	// Returns ErrServerNotFound if the node does not exist.
	UpdateAgentInfo(ctx context.Context, id string, info AgentInfo) error
}
