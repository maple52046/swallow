package application

import (
	"context"
	"fmt"
	"time"

	serverdomain "github.com/AFDEAPAC/swallow/internal/server/domain"
)

// UpdateInventoryInput carries the inventory payload sent by the agent.
type UpdateInventoryInput struct {
	NodeID  string
	OS      serverdomain.OSInfo
	CPU     serverdomain.CPUInfo
	Memory  serverdomain.MemoryInfo
	GPUs    []serverdomain.GPUInfo
	Network serverdomain.NetworkInfo
	IPMI    serverdomain.IPMIInfo
}

// UpdateInventoryUseCase updates the inventory sub-document of an existing node.
// It never creates a new node; an unknown node ID is treated as a fatal error.
type UpdateInventoryUseCase struct {
	servers serverdomain.ServerRepository
}

// NewUpdateInventoryUseCase returns an UpdateInventoryUseCase.
func NewUpdateInventoryUseCase(servers serverdomain.ServerRepository) *UpdateInventoryUseCase {
	return &UpdateInventoryUseCase{servers: servers}
}

// Execute updates inventory for the node identified by input.NodeID.
// Returns a wrapped ErrServerNotFound when the node does not exist, so the
// gRPC handler can map it to a clear rejection message for the agent.
func (uc *UpdateInventoryUseCase) Execute(ctx context.Context, input UpdateInventoryInput) error {
	if input.NodeID == "" {
		return fmt.Errorf("node_id is required")
	}

	inv := serverdomain.Inventory{
		OS:        input.OS,
		CPU:       input.CPU,
		Memory:    input.Memory,
		GPUs:      input.GPUs,
		Network:   input.Network,
		IPMI:      input.IPMI,
		UpdatedAt: time.Now().UTC(),
	}

	return uc.servers.UpdateInventory(ctx, input.NodeID, inv)
}
