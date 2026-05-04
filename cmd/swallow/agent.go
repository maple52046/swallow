package main

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/AFDEAPAC/swallow/config"
	"github.com/AFDEAPAC/swallow/internal/app"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Start the agent runtime",
	Long: `Start the swallow agent on this node.

The agent resolves a node ID, detects local environment, updates the API server
inventory, and maintains a gRPC bidirectional stream with periodic heartbeats.

Node ID resolution order:
  1. --node-id flag or GDCM_AGENT_NODE_ID env var
  2. Local state file (stateFile config field)
  3. Auto-generated UUID written to the state file

The agent exits with a non-zero status when node_id is not registered,
auth token is invalid, or configuration is missing required fields.`,
	RunE: runAgent,
}

func init() {
	agentCmd.Flags().String("node-id", "", "node ID to use (overrides config and state file)")
	agentCmd.Flags().String("server-address", "", "gRPC address of the API server (e.g. localhost:50051)")
	agentCmd.Flags().Duration("heartbeat", 0, "heartbeat interval (e.g. 30s)")
	agentCmd.Flags().String("state-file", "", "path to the local agent state file")
}

func runAgent(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(config.LoadOptions{
		ConfigFile: rootConfigFile,
		Role:       "agent",
	})
	if err != nil {
		return err
	}

	// Apply CLI flags that were explicitly set (pflag.Changed ensures zero values are ignored)
	if cmd.Flags().Changed("node-id") {
		cfg.Agent.NodeID, _ = cmd.Flags().GetString("node-id")
	}
	if cmd.Flags().Changed("server-address") {
		cfg.Agent.ServerAddress, _ = cmd.Flags().GetString("server-address")
	}
	if cmd.Flags().Changed("heartbeat") {
		cfg.Agent.HeartbeatInterval, _ = cmd.Flags().GetDuration("heartbeat")
	}
	if cmd.Flags().Changed("state-file") {
		cfg.Agent.StateFile, _ = cmd.Flags().GetString("state-file")
	}

	logAgentStartup(cfg.Agent, rootConfigFile)
	return app.RunAgent(cfg.Agent)
}

// logAgentStartup prints non-sensitive agent config values at startup.
// Auth token is never logged.
func logAgentStartup(cfg config.AgentConfig, configFile string) {
	interval := cfg.HeartbeatInterval
	if interval == 0 {
		interval = 30 * time.Second
	}
	logStartup("agent", configFile, "", "", "", cfg.ServerAddress)
}
