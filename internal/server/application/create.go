package application

import (
	"context"
	"time"

	"github.com/google/uuid"

	serverdomain "github.com/AFDEAPAC/swallow/internal/server/domain"
)

type CreateServerInput struct {
	Hostname string
	IP       string
}

type CreateServerOutput struct {
	ID string `json:"id"`
}

type CreateServerUseCase struct {
	servers serverdomain.ServerRepository
}

func NewCreateServerUseCase(servers serverdomain.ServerRepository) *CreateServerUseCase {
	return &CreateServerUseCase{servers: servers}
}

func (uc *CreateServerUseCase) Execute(ctx context.Context, input CreateServerInput) (*CreateServerOutput, error) {
	if input.Hostname == "" || input.IP == "" {
		return nil, serverdomain.ErrHostnameTaken // will be replaced by validation error in handler
	}

	hostnameExists, err := uc.servers.ExistsByHostname(ctx, input.Hostname)
	if err != nil {
		return nil, err
	}
	if hostnameExists {
		return nil, serverdomain.ErrHostnameTaken
	}

	ipExists, err := uc.servers.ExistsByIP(ctx, input.IP)
	if err != nil {
		return nil, err
	}
	if ipExists {
		return nil, serverdomain.ErrIPTaken
	}

	now := time.Now().UTC()
	server := &serverdomain.Server{
		ID:        uuid.NewString(),
		Hostname:  input.Hostname,
		IP:        input.IP,
		Status:    serverdomain.StatusUnknown,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := uc.servers.Create(ctx, server); err != nil {
		return nil, err
	}

	return &CreateServerOutput{ID: server.ID}, nil
}
