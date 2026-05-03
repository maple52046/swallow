package application

import (
	"context"

	serverdomain "github.com/AFDEAPAC/swallow/internal/server/domain"
	"github.com/AFDEAPAC/swallow/internal/shared/pagination"
)

type ListServersInput struct {
	Status  string
	Keyword string
	Page    pagination.Page
}

type ServerItem struct {
	ID        string `json:"id"`
	Hostname  string `json:"hostname"`
	IP        string `json:"ip"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type ListServersUseCase struct {
	servers serverdomain.ServerRepository
}

func NewListServersUseCase(servers serverdomain.ServerRepository) *ListServersUseCase {
	return &ListServersUseCase{servers: servers}
}

func (uc *ListServersUseCase) Execute(ctx context.Context, input ListServersInput) (pagination.Result[ServerItem], error) {
	filter := serverdomain.ListFilter{
		Status:  serverdomain.ServerStatus(input.Status),
		Keyword: input.Keyword,
		Offset:  input.Page.Offset(),
		Limit:   input.Page.PageSize,
	}

	result, err := uc.servers.List(ctx, filter)
	if err != nil {
		return pagination.Result[ServerItem]{}, err
	}

	items := make([]ServerItem, len(result.Servers))
	for i, s := range result.Servers {
		items[i] = ServerItem{
			ID:        s.ID,
			Hostname:  s.Hostname,
			IP:        s.IP,
			Status:    string(s.Status),
			CreatedAt: s.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt: s.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}

	return pagination.NewResult(items, result.Total, input.Page), nil
}
