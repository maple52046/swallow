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
	ID        string               `json:"id"`
	Hostname  string               `json:"hostname"`
	IP        string               `json:"ip"`
	Status    string               `json:"status"`
	CreatedAt string               `json:"createdAt"`
	UpdatedAt string               `json:"updatedAt"`
	Inventory *ServerItemInventory `json:"inventory,omitempty"`
	Agent     *ServerItemAgent     `json:"agent,omitempty"`
}

type ServerItemInventory struct {
	CPU    ServerItemCPU    `json:"cpu"`
	Memory ServerItemMemory `json:"memory"`
	GPUs   []ServerItemGPU  `json:"gpus"`
	OS     ServerItemOS     `json:"os"`
}

type ServerItemCPU struct {
	Model   string `json:"model"`
	Cores   int32  `json:"cores"`
	Threads int32  `json:"threads"`
}

type ServerItemMemory struct {
	TotalKB int64 `json:"totalKB"`
}

type ServerItemGPU struct {
	Vendor string `json:"vendor"`
	Model  string `json:"model"`
	Index  int32  `json:"index"`
}

type ServerItemOS struct {
	Type          string `json:"type"`
	Distribution  string `json:"distribution"`
	Version       string `json:"version"`
	KernelVersion string `json:"kernelVersion"`
	Architecture  string `json:"architecture"`
}

type ServerItemAgent struct {
	Status       string `json:"status"`
	LastSeenAt   string `json:"lastSeenAt"`
	AgentVersion string `json:"agentVersion"`
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
		item := ServerItem{
			ID:        s.ID,
			Hostname:  s.Hostname,
			IP:        s.IP,
			Status:    string(s.Status),
			CreatedAt: s.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt: s.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		}

		if s.Inventory != nil {
			inv := s.Inventory
			gpus := make([]ServerItemGPU, len(inv.GPUs))
			for j, g := range inv.GPUs {
				gpus[j] = ServerItemGPU{Vendor: g.Vendor, Model: g.Model, Index: g.Index}
			}
			item.Inventory = &ServerItemInventory{
				CPU:    ServerItemCPU{Model: inv.CPU.Model, Cores: inv.CPU.Cores, Threads: inv.CPU.Threads},
				Memory: ServerItemMemory{TotalKB: inv.Memory.TotalKB},
				GPUs:   gpus,
				OS: ServerItemOS{
					Type:          inv.OS.Type,
					Distribution:  inv.OS.Distribution,
					Version:       inv.OS.Version,
					KernelVersion: inv.OS.KernelVersion,
					Architecture:  inv.OS.Architecture,
				},
			}
		}

		if s.Agent != nil {
			item.Agent = &ServerItemAgent{
				Status:       string(s.Agent.Status),
				LastSeenAt:   s.Agent.LastSeenAt.Format("2006-01-02T15:04:05Z"),
				AgentVersion: s.Agent.AgentVersion,
			}
		}

		items[i] = item
	}

	return pagination.NewResult(items, result.Total, input.Page), nil
}
