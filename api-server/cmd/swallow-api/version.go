package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/internal/adapters/store/mongodb/migration"
	"github.com/maple52046/swallow/internal/shared/version"
)

// versionJSON selects the machine-readable output of `swallow-api version`.
var versionJSON bool

// buildInfo is what `swallow-api version --json` prints. assemble-release.sh writes the release
// manifest's schema versions and upgrade sources from it, so the manifest always matches the
// binary the release ships.
type buildInfo struct {
	Version                       string `json:"version"`
	Commit                        string `json:"commit"`
	BuiltAt                       string `json:"builtAt"`
	SchemaVersion                 int    `json:"schemaVersion"`
	OldestUpgradableSchemaVersion int    `json:"oldestUpgradableSchemaVersion"`
}

// currentBuildInfo reads the linker-injected build metadata and the schema constants of this
// binary; it needs no configuration or database.
func currentBuildInfo() buildInfo {
	return buildInfo{
		Version:                       version.Version,
		Commit:                        version.Commit,
		BuiltAt:                       version.BuiltAt,
		SchemaVersion:                 migration.CurrentSchemaVersion,
		OldestUpgradableSchemaVersion: migration.OldestUpgradableSchemaVersion,
	}
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the build and the database schema version this binary serves",
	Long: "Print the release version, commit, and build time of this binary, the only MongoDB " +
		"schema version it serves, and the oldest schema `migrate` upgrades from. It reads no " +
		"configuration and does not connect to the database.",
	Args: cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		info := currentBuildInfo()
		if versionJSON {
			return json.NewEncoder(os.Stdout).Encode(info)
		}
		fmt.Printf("swallow-api %s (commit %s, built %s)\n", info.Version, info.Commit, info.BuiltAt)
		fmt.Printf("database schema %d (upgrades from %d)\n", info.SchemaVersion, info.OldestUpgradableSchemaVersion)
		return nil
	},
}

func init() {
	versionCmd.Flags().BoolVar(&versionJSON, "json", false, "print the version as one JSON object")
}
