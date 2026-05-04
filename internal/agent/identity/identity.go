// Package identity resolves the agent's node ID using a defined priority chain.
// The chain ensures deterministic identity selection and generates a UUID only
// as a last resort, persisting it to the state file to survive restarts.
package identity

import (
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/AFDEAPAC/swallow/config"
	"github.com/AFDEAPAC/swallow/internal/agent/state"
)

// Source describes where the resolved node ID came from.
type Source string

const (
	// SourceConfig means the node ID came from config, env var, or CLI flag.
	SourceConfig Source = "config"
	// SourceStateFile means the node ID was read from the local state file.
	SourceStateFile Source = "state-file"
	// SourceGenerated means a UUID was auto-generated and written to the state file.
	SourceGenerated Source = "generated"
)

// ResolvedIdentity carries the resolved node ID and its origin.
type ResolvedIdentity struct {
	NodeID string
	Source Source
}

// Resolve determines the agent's node ID using the following priority chain:
//  1. cfg.NodeID (set from env var, CLI flag, or config file)
//  2. State file node_id field
//  3. Auto-generated UUID (written to state file for future restarts)
//
// A generated UUID is only an agent-side identity candidate. The API server
// must independently validate that the node ID exists before accepting the agent.
func Resolve(cfg config.AgentConfig) (ResolvedIdentity, error) {
	// Priority 1: explicit configuration (env / flag / config file)
	if cfg.NodeID != "" {
		log.Printf("[identity] node_id resolved from config: %s", cfg.NodeID)
		return ResolvedIdentity{NodeID: cfg.NodeID, Source: SourceConfig}, nil
	}

	// Priority 2: local state file
	s, err := state.Read(cfg.StateFile)
	if err != nil {
		// Unreadable state file is recoverable; log and fall through to generate.
		log.Printf("[identity] warning: could not read state file %q: %v", cfg.StateFile, err)
	}
	if s != nil && s.NodeID != "" {
		log.Printf("[identity] node_id resolved from state file %q: %s", cfg.StateFile, s.NodeID)
		return ResolvedIdentity{NodeID: s.NodeID, Source: SourceStateFile}, nil
	}

	// Priority 3: generate a UUID and persist it
	generated := uuid.NewString()
	log.Printf("[identity] no node_id found; generated UUID: %s", generated)

	newState := &state.State{
		NodeID:        generated,
		GeneratedAt:   time.Now().UTC(),
		ServerAddress: cfg.ServerAddress,
	}
	if err := state.Write(cfg.StateFile, newState); err != nil {
		// Failure to write is non-fatal for this run, but the ID will not survive a restart.
		log.Printf("[identity] warning: could not persist generated node_id to %q: %v", cfg.StateFile, err)
	}

	// A generated UUID is unlikely to exist in the API server database, so we
	// log a clear guidance message to help the operator understand what to do.
	fmt.Printf("[identity] node_id %s was auto-generated. "+
		"Register this node in the API server before the agent can connect.\n", generated)

	return ResolvedIdentity{NodeID: generated, Source: SourceGenerated}, nil
}
