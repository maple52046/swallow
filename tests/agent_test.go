package tests

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AFDEAPAC/swallow/config"
	"github.com/AFDEAPAC/swallow/internal/agent/identity"
	"github.com/AFDEAPAC/swallow/internal/agent/state"
	serverdomain "github.com/AFDEAPAC/swallow/internal/server/domain"
)

// --- State file tests ---

func TestStateFile_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := &state.State{
		NodeID:        "test-node-123",
		GeneratedAt:   time.Now().UTC().Truncate(time.Second),
		ServerAddress: "localhost:50051",
	}

	if err := state.Write(path, s); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := state.Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil state")
	}
	if got.NodeID != s.NodeID {
		t.Errorf("NodeID: got %q want %q", got.NodeID, s.NodeID)
	}
	if got.ServerAddress != s.ServerAddress {
		t.Errorf("ServerAddress: got %q want %q", got.ServerAddress, s.ServerAddress)
	}
}

func TestStateFile_MissingFile(t *testing.T) {
	got, err := state.Read("/nonexistent/path/state.json")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil state for missing file, got: %+v", got)
	}
}

func TestStateFile_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := state.Read(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestStateFile_CreatesParentDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "deep", "state.json")

	s := &state.State{NodeID: "abc", GeneratedAt: time.Now().UTC()}
	if err := state.Write(path, s); err != nil {
		t.Fatalf("Write with deep path: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist at %s: %v", path, err)
	}
}

func TestStateFile_DoesNotStoreSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := &state.State{NodeID: "node-1", GeneratedAt: time.Now().UTC()}
	if err := state.Write(path, s); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, dangerous := range []string{"auth_token", "password", "secret", "token"} {
		if _, found := raw[dangerous]; found {
			t.Errorf("state file must not contain field %q", dangerous)
		}
	}
}

// --- Identity resolution tests ---

func TestIdentity_ResolveFromConfig(t *testing.T) {
	cfg := config.AgentConfig{
		NodeID:            "explicit-node-id",
		ServerAddress:     "localhost:50051",
		HeartbeatInterval: 30 * time.Second,
		StateFile:         "/tmp/nonexistent-state.json",
	}

	id, err := identity.Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if id.NodeID != "explicit-node-id" {
		t.Errorf("NodeID: got %q want %q", id.NodeID, "explicit-node-id")
	}
	if id.Source != identity.SourceConfig {
		t.Errorf("Source: got %q want %q", id.Source, identity.SourceConfig)
	}
}

func TestIdentity_ResolveFromStateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := &state.State{NodeID: "state-file-node", GeneratedAt: time.Now().UTC()}
	if err := state.Write(path, s); err != nil {
		t.Fatal(err)
	}

	cfg := config.AgentConfig{
		NodeID:            "",
		ServerAddress:     "localhost:50051",
		HeartbeatInterval: 30 * time.Second,
		StateFile:         path,
	}

	id, err := identity.Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if id.NodeID != "state-file-node" {
		t.Errorf("NodeID: got %q want %q", id.NodeID, "state-file-node")
	}
	if id.Source != identity.SourceStateFile {
		t.Errorf("Source: got %q want %q", id.Source, identity.SourceStateFile)
	}
}

func TestIdentity_GeneratesUUIDWhenNoSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent-state.json")

	cfg := config.AgentConfig{
		NodeID:            "",
		ServerAddress:     "localhost:50051",
		HeartbeatInterval: 30 * time.Second,
		StateFile:         path,
	}

	id, err := identity.Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if id.NodeID == "" {
		t.Fatal("expected non-empty generated node ID")
	}
	if id.Source != identity.SourceGenerated {
		t.Errorf("Source: got %q want %q", id.Source, identity.SourceGenerated)
	}

	// Generated ID must be persisted so it survives a restart
	got, err := state.Read(path)
	if err != nil {
		t.Fatalf("Read state after generation: %v", err)
	}
	if got == nil || got.NodeID != id.NodeID {
		t.Errorf("state file should persist generated node_id; got %+v", got)
	}
}

func TestIdentity_ConfigTakesPriorityOverStateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := &state.State{NodeID: "state-id", GeneratedAt: time.Now().UTC()}
	if err := state.Write(path, s); err != nil {
		t.Fatal(err)
	}

	cfg := config.AgentConfig{
		NodeID:            "config-id",
		ServerAddress:     "localhost:50051",
		HeartbeatInterval: 30 * time.Second,
		StateFile:         path,
	}

	id, err := identity.Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if id.NodeID != "config-id" {
		t.Errorf("config NodeID should take priority; got %q", id.NodeID)
	}
	if id.Source != identity.SourceConfig {
		t.Errorf("Source should be config; got %q", id.Source)
	}
}

// --- Config defaults tests ---

func TestAgentConfig_HeartbeatDefaultIs30s(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.Agent.HeartbeatInterval != 30*time.Second {
		t.Errorf("default HeartbeatInterval: got %v want 30s", cfg.Agent.HeartbeatInterval)
	}
}

func TestAgentConfig_StateFileHasDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.Agent.StateFile == "" {
		t.Error("default StateFile must not be empty")
	}
}

func TestAgentConfig_ServerAddressHasDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.Agent.ServerAddress == "" {
		t.Error("default ServerAddress must not be empty")
	}
}

// --- Server-side unknown node_id enforcement ---

func TestRepo_UpdateInventory_RejectsUnknownNodeID(t *testing.T) {
	// fakeServerRepo (from server_test.go) does not pre-populate any node,
	// so UpdateInventory must return ErrServerNotFound.
	repo := newFakeServerRepo()

	err := repo.UpdateInventory(context.Background(), "nonexistent-id", serverdomain.Inventory{
		UpdatedAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected ErrServerNotFound for unknown node_id, got nil")
	}
	if err != serverdomain.ErrServerNotFound {
		t.Errorf("expected ErrServerNotFound, got %v", err)
	}
}

func TestRepo_UpdateAgentInfo_RejectsUnknownNodeID(t *testing.T) {
	repo := newFakeServerRepo()

	err := repo.UpdateAgentInfo(context.Background(), "nonexistent-id", serverdomain.AgentInfo{
		Status:     serverdomain.AgentStatusConnected,
		LastSeenAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected ErrServerNotFound for unknown node_id, got nil")
	}
	if err != serverdomain.ErrServerNotFound {
		t.Errorf("expected ErrServerNotFound, got %v", err)
	}
}
