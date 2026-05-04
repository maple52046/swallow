package main

import (
	"github.com/spf13/cobra"

	"github.com/AFDEAPAC/swallow/config"
	"github.com/AFDEAPAC/swallow/internal/app"
)

var apiCmd = &cobra.Command{
	Use:   "api",
	Short: "Start the HTTP API server",
	RunE:  runAPI,
}

func init() {
	apiCmd.Flags().String("addr", "", "HTTP listen address (e.g. :3000)")
	apiCmd.Flags().String("mongo-uri", "", "MongoDB connection URI")
	apiCmd.Flags().String("mongo-db", "", "MongoDB database name")
	apiCmd.Flags().String("jwt-secret", "", "JWT signing secret")
	apiCmd.Flags().Int("jwt-expiry", 0, "JWT token expiry in hours")
}

func runAPI(cmd *cobra.Command, _ []string) error {
	overlay := buildAPIFlagOverlay(cmd)

	cfg, err := config.Load(config.LoadOptions{
		ConfigFile:  rootConfigFile,
		FlagOverlay: overlay,
		Role:        "api",
	})
	if err != nil {
		return err
	}

	logStartup("api", rootConfigFile, cfg.API.Addr, cfg.API.MongoURI, cfg.API.MongoDB, "")
	return app.RunAPI(cfg.API)
}

// buildAPIFlagOverlay inspects which flags were explicitly set by the user
// (via pflag.Changed) and copies only those values into an overlay struct.
// This prevents zero-value flags from silently overwriting config file settings.
func buildAPIFlagOverlay(cmd *cobra.Command) *config.APIConfig {
	overlay := &config.APIConfig{}
	changed := false

	if cmd.Flags().Changed("addr") {
		overlay.Addr, _ = cmd.Flags().GetString("addr")
		changed = true
	}
	if cmd.Flags().Changed("mongo-uri") {
		overlay.MongoURI, _ = cmd.Flags().GetString("mongo-uri")
		changed = true
	}
	if cmd.Flags().Changed("mongo-db") {
		overlay.MongoDB, _ = cmd.Flags().GetString("mongo-db")
		changed = true
	}
	if cmd.Flags().Changed("jwt-secret") {
		overlay.JWTSecret, _ = cmd.Flags().GetString("jwt-secret")
		changed = true
	}
	if cmd.Flags().Changed("jwt-expiry") {
		overlay.JWTExpiryHours, _ = cmd.Flags().GetInt("jwt-expiry")
		changed = true
	}

	if !changed {
		return nil
	}
	return overlay
}
