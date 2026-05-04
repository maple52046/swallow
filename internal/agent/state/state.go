// Package state manages the agent's local state file.
// The state file persists a node ID that was auto-generated at first startup,
// preventing a new UUID from being produced on every restart.
// It must never store secrets such as auth tokens or IPMI credentials.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// State represents the contents of the agent local state file.
type State struct {
	NodeID        string    `json:"node_id"`
	GeneratedAt   time.Time `json:"generated_at"`
	ServerAddress string    `json:"server_address"`
	AgentVersion  string    `json:"agent_version,omitempty"`
}

// Read reads and parses the state file at path.
// Returns (nil, nil) when the file does not exist so callers can distinguish
// missing-file from a parse error.
func Read(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("state file %q: %w", path, err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("state file %q: parse error: %w", path, err)
	}
	return &s, nil
}

// Write serialises s and writes it to path, creating any missing parent directories.
func Write(path string, s *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("state file dir %q: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("state file marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("state file write %q: %w", path, err)
	}
	return nil
}
