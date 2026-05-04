package app

import (
	"context"
	"errors"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/AFDEAPAC/swallow/config"
	agentv1 "github.com/AFDEAPAC/swallow/gen/agent/v1"
	"github.com/AFDEAPAC/swallow/internal/agent/detect"
	"github.com/AFDEAPAC/swallow/internal/agent/identity"
)

// agentVersion is embedded at build time when needed; empty string is acceptable.
const agentVersion = ""

// RunAgent is the top-level agent runtime. It:
//  1. Resolves the node ID (config → state file → generated UUID).
//  2. Collects local environment via modular detectors.
//  3. Enters a connect-and-stream loop with exponential backoff on failure.
//
// Fatal errors (unknown node ID, invalid config, auth failure) cause an immediate
// non-zero exit; transient network/stream errors trigger reconnect with backoff.
func RunAgent(cfg config.AgentConfig) error {
	// Step 1: resolve node identity
	id, err := identity.Resolve(cfg)
	if err != nil {
		return err
	}
	log.Printf("[agent] node_id=%s source=%s", id.NodeID, id.Source)

	// Step 2: detect local environment
	log.Println("[agent] starting local environment detection")
	inv, detectionErrs := detect.Collect()
	for _, e := range detectionErrs {
		log.Printf("[agent] detection warning: %v", e)
	}
	log.Printf("[agent] detection complete: os=%s/%s cpu=%q gpus=%d",
		inv.OS.Type, inv.OS.Distribution, inv.CPU.Model, len(inv.GPUs))

	// Step 3: connect and stream with backoff
	backoff := time.Second
	const maxBackoff = 60 * time.Second

	for {
		err := connectAndStream(cfg, id.NodeID, inv)
		if err == nil {
			// Clean disconnect (e.g. server sent EOF) — reconnect
			log.Println("[agent] stream ended cleanly; reconnecting")
		} else if isFatal(err) {
			return err
		} else {
			log.Printf("[agent] stream error: %v; reconnecting in %s", err, backoff)
		}

		time.Sleep(backoff)
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// connectAndStream dials the gRPC server, performs the hello/inventory/heartbeat
// protocol, and returns when the stream terminates.
// Returns a sentinel fatalError for identity or auth failures that must not be retried.
func connectAndStream(cfg config.AgentConfig, nodeID string, inv detect.Inventory) error {
	// Auth token is sent as gRPC metadata on every connection.
	// It is not stored in the local state file.
	md := metadata.Pairs("authorization", "Bearer "+cfg.AuthToken)
	ctx := metadata.NewOutgoingContext(context.Background(), md)

	conn, err := grpc.NewClient(cfg.ServerAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := agentv1.NewAgentServiceClient(conn)
	stream, err := client.Connect(ctx)
	if err != nil {
		return err
	}

	// Send Hello
	if err := stream.Send(&agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_Hello{
			Hello: &agentv1.HelloRequest{NodeId: nodeID, AgentVersion: agentVersion},
		},
	}); err != nil {
		return err
	}

	// Wait for HelloResponse
	resp, err := stream.Recv()
	if err != nil {
		return err
	}
	switch p := resp.Payload.(type) {
	case *agentv1.ServerMessage_HelloResponse:
		if !p.HelloResponse.Accepted {
			return fatalError("server rejected hello: " + p.HelloResponse.Message)
		}
		log.Printf("[agent] server accepted connection: %s", p.HelloResponse.Message)
	case *agentv1.ServerMessage_Error:
		// node_not_found is always fatal — do not retry
		if p.Error.Code == "node_not_found" {
			return fatalError(p.Error.Message)
		}
		return errors.New(p.Error.Message)
	default:
		return errors.New("unexpected response to hello")
	}

	// Check for auth rejection via gRPC status code
	if s, ok := status.FromError(err); ok {
		if s.Code() == codes.Unauthenticated || s.Code() == codes.NotFound {
			return fatalError(s.Message())
		}
	}

	// Send InventoryUpdate
	if err := stream.Send(buildInventoryUpdate(nodeID, inv)); err != nil {
		return err
	}

	// Wait for InventoryAck (best-effort; stream continues regardless)
	resp, err = stream.Recv()
	if err == nil {
		if ack, ok := resp.Payload.(*agentv1.ServerMessage_InventoryAck); ok {
			log.Printf("[agent] inventory ack: ok=%v", ack.InventoryAck.Ok)
		}
	}

	// Heartbeat loop
	ticker := time.NewTicker(cfg.HeartbeatInterval)
	defer ticker.Stop()

	for range ticker.C {
		err := stream.Send(&agentv1.AgentMessage{
			Payload: &agentv1.AgentMessage_Heartbeat{
				Heartbeat: &agentv1.HeartbeatRequest{
					NodeId:       nodeID,
					TimestampUnix: time.Now().Unix(),
					AgentVersion:  agentVersion,
				},
			},
		})
		if err != nil {
			log.Printf("[agent] heartbeat send failed: %v", err)
			return err
		}
	}
	return nil
}

// buildInventoryUpdate maps detect.Inventory to the proto message.
func buildInventoryUpdate(nodeID string, inv detect.Inventory) *agentv1.AgentMessage {
	gpus := make([]*agentv1.GPUInfo, len(inv.GPUs))
	for i, g := range inv.GPUs {
		gpus[i] = &agentv1.GPUInfo{Vendor: g.Vendor, Model: g.Model, Index: g.Index}
	}
	return &agentv1.AgentMessage{
		Payload: &agentv1.AgentMessage_Inventory{
			Inventory: &agentv1.InventoryUpdate{
				NodeId: nodeID,
				Os: &agentv1.OSInfo{
					Type:          inv.OS.Type,
					Distribution:  inv.OS.Distribution,
					Version:       inv.OS.Version,
					KernelVersion: inv.OS.KernelVersion,
					Architecture:  inv.OS.Architecture,
				},
				Cpu: &agentv1.CPUInfo{
					Model:   inv.CPU.Model,
					Cores:   inv.CPU.Cores,
					Threads: inv.CPU.Threads,
				},
				Memory: &agentv1.MemoryInfo{TotalKb: inv.Memory.TotalKB},
				Gpus:   gpus,
				Network: &agentv1.NetworkInfo{
					Hostname:  inv.Network.Hostname,
					PrimaryIp: inv.Network.PrimaryIP,
					AllIps:    inv.Network.AllIPs,
				},
				Ipmi: &agentv1.IPMIInfo{
					Available: inv.IPMI.Available,
					BmcIp:     inv.IPMI.BMCIP,
					Source:    inv.IPMI.Source,
					Status:    inv.IPMI.Status,
				},
			},
		},
	}
}

// fatalAgentError is returned for identity/auth failures that should not be retried.
type fatalAgentError struct{ msg string }

func (e fatalAgentError) Error() string { return "[agent] fatal: " + e.msg }

func fatalError(msg string) error { return fatalAgentError{msg: msg} }

// isFatal returns true when the error should terminate the agent without reconnect.
func isFatal(err error) bool {
	var fe fatalAgentError
	if errors.As(err, &fe) {
		return true
	}
	if s, ok := status.FromError(err); ok {
		return s.Code() == codes.Unauthenticated || s.Code() == codes.NotFound
	}
	return false
}
