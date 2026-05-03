package application

import (
	"context"

	serverdomain "github.com/AFDEAPAC/swallow/internal/server/domain"
)

type DeleteServerUseCase struct {
	servers serverdomain.ServerRepository
}

func NewDeleteServerUseCase(servers serverdomain.ServerRepository) *DeleteServerUseCase {
	return &DeleteServerUseCase{servers: servers}
}

func (uc *DeleteServerUseCase) Execute(ctx context.Context, id string) error {
	return uc.servers.Delete(ctx, id)
}
