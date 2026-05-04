package main

import (
	"log"

	"github.com/spf13/cobra"

	"github.com/AFDEAPAC/swallow/config"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Start the agent (not yet implemented)",
	RunE:  runAgent,
}

func init() {
	agentCmd.Flags().String("controller", "", "API server address for the agent to connect to")
}

func runAgent(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(config.LoadOptions{
		ConfigFile: rootConfigFile,
		Role:       "agent",
	})
	if err != nil {
		return err
	}

	// Apply controller flag if explicitly set.
	if cmd.Flags().Changed("controller") {
		cfg.Agent.ControllerAddr, _ = cmd.Flags().GetString("controller")
	}

	logStartup("agent", rootConfigFile, "", "", "", cfg.Agent.ControllerAddr)
	log.Println("[swallow] agent mode is not yet implemented")
	return nil
}
