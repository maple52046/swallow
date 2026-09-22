package main

import (
	"log"
	"strings"

	"github.com/spf13/cobra"
)

// rootConfigFile is populated by the persistent --config flag and read by subcommands.
var rootConfigFile string

var rootCmd = &cobra.Command{
	Use:   "swallow-api",
	Short: "GPU Datacenter Management backend",
	Long:  "swallow-api is the backend service for the Swallow platform. Run 'swallow-api api' to start the API server.",
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&rootConfigFile, "config", "c", "", "path to YAML config file")

	rootCmd.AddCommand(apiCmd)
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(workerCmd)
	rootCmd.AddCommand(ansibleExecutorCmd)
}

// logStartup prints non-sensitive effective config values at startup so that the
// active configuration is observable without exposing credentials in logs.
func logStartup(configFile, addr, mongoURI, mongoDB string) {
	log.Printf("[swallow-api] addr=%s mongo-host=%s mongo-db=%s config-file=%q",
		addr, redactURI(mongoURI), mongoDB, configFile)
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
	trimmed := strings.TrimPrefix(uri, "mongodb://")
	for j, c := range trimmed {
		if c == '/' || c == '?' {
			return trimmed[:j]
		}
	}
	return trimmed
}
