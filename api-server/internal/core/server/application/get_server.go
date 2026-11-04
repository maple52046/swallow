package application

import (
	"context"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// GetServerUseCase reads one Server with its health classified, the same representation as a list
// item (servers-list.md).
type GetServerUseCase struct {
	servers serverdomain.ServerRepository
	health  HealthResolver
}

// NewGetServerUseCase builds the detail read over the repository. health is required, as for the
// list.
func NewGetServerUseCase(servers serverdomain.ServerRepository, health HealthResolver) *GetServerUseCase {
	return &GetServerUseCase{servers: servers, health: health}
}

// Execute returns the Server, serverdomain.ErrServerNotFound for an unknown id, or the
// classification's error; a metrics outage only leaves its health unknown (HealthResolver).
func (uc *GetServerUseCase) Execute(ctx context.Context, id string) (*ServerItem, error) {
	server, err := uc.servers.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := attachHealth(ctx, uc.health, []*serverdomain.Server{server}); err != nil {
		return nil, err
	}

	item := ToServerItem(server)
	return &item, nil
}
