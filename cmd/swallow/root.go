package main

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
)

// rootConfigFile is populated by the persistent --config flag and read by subcommands.
var rootConfigFile string

var rootCmd = &cobra.Command{
	Use:   "swallow",
	Short: "GPU Datacenter Management backend",
	Long:  "swallow is the backend service for the GDCM platform. Run 'swallow api' to start the API server.",
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&rootConfigFile, "config", "c", "", "path to YAML config file")

	rootCmd.AddCommand(apiCmd)
	rootCmd.AddCommand(agentCmd)
}

// logStartup prints non-sensitive effective config values at startup so that the
// active configuration is observable without exposing credentials in logs.
func logStartup(mode string, configFile, addr, mongoURI, mongoDB, controllerAddr string) {
	dbHost := redactURI(mongoURI)

	switch mode {
	case "api":
		log.Printf("[swallow] mode=api addr=%s mongo-host=%s mongo-db=%s config-file=%q",
			addr, dbHost, mongoDB, configFile)
	case "agent":
		log.Printf("[swallow] mode=agent controller=%s config-file=%q",
			controllerAddr, configFile)
	}
}

// redactURI returns only the host portion of a MongoDB URI to avoid logging credentials.
func redactURI(uri string) string {
	// Extract host by finding the "@" separator; if absent, return the full URI as-is
	// since it contains no credentials (e.g. "mongodb://localhost:27017").
	for i := len(uri) - 1; i >= 0; i-- {
		if uri[i] == '@' {
			rest := uri[i+1:]
			// strip trailing /dbname and query string
			for j, c := range rest {
				if c == '/' || c == '?' {
					return rest[:j]
				}
			}
			return rest
		}
	}
	// no credentials in URI; return host portion only
	trimmed := uri
	if len(trimmed) >= 10 && trimmed[:10] == "mongodb://" {
		trimmed = trimmed[10:]
	}
	for j, c := range trimmed {
		if c == '/' || c == '?' {
			return trimmed[:j]
		}
	}
	return fmt.Sprintf("%s", trimmed)
}
