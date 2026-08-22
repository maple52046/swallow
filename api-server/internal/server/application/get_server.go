package application

import (
	"context"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

type GetServerUseCase struct {
	servers serverdomain.ServerRepository
	health  HealthResolver
}

func NewGetServerUseCase(servers serverdomain.ServerRepository, health HealthResolver) *GetServerUseCase {
	return &GetServerUseCase{servers: servers, health: health}
}

func (uc *GetServerUseCase) Execute(ctx context.Context, id string) (*ServerItem, error) {
	server, err := uc.servers.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if uc.health != nil {
		if health, err := uc.health.ResolveHealth(ctx, []string{id}); err == nil {
			if h, ok := health[id]; ok {
				server.Health = h
			}
		}
	}

	item := ToServerItem(server)
	return &item, nil
}
