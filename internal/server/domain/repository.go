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

type ServerRepository interface {
	Create(ctx context.Context, server *Server) error
	List(ctx context.Context, filter ListFilter) (ListResult, error)
	Delete(ctx context.Context, id string) error
	ExistsByHostname(ctx context.Context, hostname string) (bool, error)
	ExistsByIP(ctx context.Context, ip string) (bool, error)
}
