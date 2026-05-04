package delivery

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"log"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	agentv1 "github.com/AFDEAPAC/swallow/gen/agent/v1"
	"github.com/AFDEAPAC/swallow/internal/server/application"
	serverdomain "github.com/AFDEAPAC/swallow/internal/server/domain"
)

// AgentGRPCHandler implements the AgentService gRPC server.
// It handles the bidirectional stream lifecycle:
//
//	Hello → InventoryUpdate → Heartbeat loop → disconnect cleanup
type AgentGRPCHandler struct {
	agentv1.UnimplementedAgentServiceServer
	updateInventory *application.UpdateInventoryUseCase
	servers         serverdomain.ServerRepository
	// nodeAuthToken is the shared secret agents must present; compared with
	// constant-time equality to avoid timing side-channels.
	nodeAuthToken string
}

// NewAgentGRPCHandler creates an AgentGRPCHandler.
func NewAgentGRPCHandler(
	updateInventory *application.UpdateInventoryUseCase,
	servers serverdomain.ServerRepository,
	nodeAuthToken string,
) *AgentGRPCHandler {
	return &AgentGRPCHandler{
		updateInventory: updateInventory,
		servers:         servers,
		nodeAuthToken:   nodeAuthToken,
	}
}

// Connect is the gRPC bidirectional stream entry point.
// Protocol order:
//  1. Validate auth token from gRPC metadata.
//  2. Receive HelloRequest → validate node exists → send HelloResponse.
//  3. Receive InventoryUpdate → apply → send InventoryAck.
//  4. Loop: receive HeartbeatRequest → update last_seen_at.
//  5. On EOF or error → mark agent as disconnected.
func (h *AgentGRPCHandler) Connect(stream agentv1.AgentService_ConnectServer) error {
	ctx := stream.Context()

	// Step 1: authenticate via bearer token in gRPC metadata
	if err := h.authenticate(ctx); err != nil {
		return err
	}

	// Step 2: expect Hello as the first message
	msg, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.Internal, "recv hello: %v", err)
	}
	hello, ok := msg.Payload.(*agentv1.AgentMessage_Hello)
	if !ok || hello.Hello == nil {
		return status.Error(codes.InvalidArgument, "first message must be HelloRequest")
	}

	nodeID := hello.Hello.NodeId
	agentVersion := hello.Hello.AgentVersion
	log.Printf("[grpc] agent hello: node_id=%s agent_version=%q", nodeID, agentVersion)

	// Validate node exists; reject unknown node IDs to prevent accidental node creation.
	if _, err := h.servers.FindByID(ctx, nodeID); err != nil {
		if errors.Is(err, serverdomain.ErrServerNotFound) {
			_ = stream.Send(&agentv1.ServerMessage{
				Payload: &agentv1.ServerMessage_Error{
					Error: &agentv1.ErrorMessage{
						Code:    "node_not_found",
						Message: "node_id is not registered in the API server; create the node first or configure the correct node_id",
					},
				},
			})
			return status.Errorf(codes.NotFound, "node %q not found", nodeID)
		}
		return status.Errorf(codes.Internal, "lookup node: %v", err)
	}

	// Mark agent as connected
	_ = h.servers.UpdateAgentInfo(ctx, nodeID, serverdomain.AgentInfo{
		Status:       serverdomain.AgentStatusConnected,
		LastSeenAt:   time.Now().UTC(),
		AgentVersion: agentVersion,
	})

	if err := stream.Send(&agentv1.ServerMessage{
		Payload: &agentv1.ServerMessage_HelloResponse{
			HelloResponse: &agentv1.HelloResponse{Accepted: true, Message: "welcome"},
		},
	}); err != nil {
		return status.Errorf(codes.Internal, "send hello response: %v", err)
	}

	log.Printf("[grpc] node %s connected", nodeID)

	// Step 3: expect InventoryUpdate as second message
	msg, err = stream.Recv()
	if err != nil {
		h.markDisconnected(nodeID, agentVersion)
		if err == io.EOF {
			return nil
		}
		return status.Errorf(codes.Internal, "recv inventory: %v", err)
	}
	if invMsg, ok := msg.Payload.(*agentv1.AgentMessage_Inventory); ok && invMsg.Inventory != nil {
		if err := h.applyInventory(ctx, invMsg.Inventory); err != nil {
			log.Printf("[grpc] inventory update failed for node %s: %v", nodeID, err)
			_ = stream.Send(&agentv1.ServerMessage{
				Payload: &agentv1.ServerMessage_Error{
					Error: &agentv1.ErrorMessage{Code: "inventory_error", Message: err.Error()},
				},
			})
		} else {
			log.Printf("[grpc] inventory updated for node %s", nodeID)
			_ = stream.Send(&agentv1.ServerMessage{
				Payload: &agentv1.ServerMessage_InventoryAck{
					InventoryAck: &agentv1.InventoryAck{Ok: true},
				},
			})
		}
	}

	// Step 4: heartbeat loop
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("[grpc] stream error for node %s: %v", nodeID, err)
			break
		}
		hb, ok := msg.Payload.(*agentv1.AgentMessage_Heartbeat)
		if !ok || hb.Heartbeat == nil {
			continue
		}
		if err := h.servers.UpdateAgentInfo(ctx, nodeID, serverdomain.AgentInfo{
			Status:       serverdomain.AgentStatusConnected,
			LastSeenAt:   time.Now().UTC(),
			AgentVersion: hb.Heartbeat.AgentVersion,
		}); err != nil {
			log.Printf("[grpc] heartbeat update failed for node %s: %v", nodeID, err)
		}
	}

	// Step 5: stream ended — mark disconnected
	h.markDisconnected(nodeID, agentVersion)
	return nil
}

// authenticate extracts and validates the Bearer token from gRPC metadata.
// Token is compared against the shared nodeAuthToken using constant-time
// equality to avoid timing side-channels.
func (h *AgentGRPCHandler) authenticate(ctx context.Context) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return status.Error(codes.Unauthenticated, "missing authorization metadata")
	}
	tokenStr := strings.TrimPrefix(vals[0], "Bearer ")
	if subtle.ConstantTimeCompare([]byte(tokenStr), []byte(h.nodeAuthToken)) != 1 {
		return status.Error(codes.Unauthenticated, "invalid token")
	}
	return nil
}

// applyInventory maps the proto InventoryUpdate to the domain model and persists it.
func (h *AgentGRPCHandler) applyInventory(ctx context.Context, inv *agentv1.InventoryUpdate) error {
	gpus := make([]serverdomain.GPUInfo, len(inv.Gpus))
	for i, g := range inv.Gpus {
		gpus[i] = serverdomain.GPUInfo{Vendor: g.Vendor, Model: g.Model, Index: g.Index}
	}

	input := application.UpdateInventoryInput{
		NodeID: inv.NodeId,
		CPU: serverdomain.CPUInfo{
			Model:   inv.Cpu.GetModel(),
			Cores:   inv.Cpu.GetCores(),
			Threads: inv.Cpu.GetThreads(),
		},
		Memory: serverdomain.MemoryInfo{TotalKB: inv.Memory.GetTotalKb()},
		GPUs:   gpus,
		Network: serverdomain.NetworkInfo{
			Hostname:  inv.Network.GetHostname(),
			PrimaryIP: inv.Network.GetPrimaryIp(),
			AllIPs:    inv.Network.GetAllIps(),
		},
		IPMI: serverdomain.IPMIInfo{
			Available: inv.Ipmi.GetAvailable(),
			BMCIP:     inv.Ipmi.GetBmcIp(),
			Source:    inv.Ipmi.GetSource(),
			Status:    inv.Ipmi.GetStatus(),
		},
	}
	if inv.Os != nil {
		input.OS = serverdomain.OSInfo{
			Type:          inv.Os.Type,
			Distribution:  inv.Os.Distribution,
			Version:       inv.Os.Version,
			KernelVersion: inv.Os.KernelVersion,
			Architecture:  inv.Os.Architecture,
		}
	}

	return h.updateInventory.Execute(ctx, input)
}

// markDisconnected records the agent's disconnected state.
// Uses a background context because the stream context may already be cancelled.
func (h *AgentGRPCHandler) markDisconnected(nodeID, agentVersion string) {
	_ = h.servers.UpdateAgentInfo(context.Background(), nodeID, serverdomain.AgentInfo{
		Status:       serverdomain.AgentStatusDisconnected,
		LastSeenAt:   time.Now().UTC(),
		AgentVersion: agentVersion,
	})
	log.Printf("[grpc] node %s disconnected", nodeID)
}
