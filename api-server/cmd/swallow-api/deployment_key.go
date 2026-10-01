package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/config"
	"github.com/maple52046/swallow/internal/app"
)

// deploymentKeyCmd groups installation-time operations on the Deployment Key (decision 039).
var deploymentKeyCmd = &cobra.Command{
	Use:   "deployment-key",
	Short: "Manage the installation's Deployment Key",
}

// deploymentKeyEnsureCmd is the installation step swallowctl runs after `migrate`. It is
// idempotent and prints only the public fingerprint.
var deploymentKeyEnsureCmd = &cobra.Command{
	Use:   "ensure",
	Short: "Create the Deployment Key if the installation has none (idempotent)",
	Long: "Create the installation's ed25519 Deployment Key if none exists, sealed with the " +
		"configured credential key. An existing key is left unchanged. Run it after `migrate`, " +
		"with the same configuration as the API. The private key is never printed.",
	RunE: func(_ *cobra.Command, _ []string) error {
		cfg, err := config.Load(config.LoadOptions{ConfigFile: rootConfigFile})
		if err != nil {
			return err
		}
		result, err := app.EnsureDeploymentKey(cfg.API)
		if err != nil {
			return err
		}
		if result.Created {
			fmt.Printf("deployment key created: %s\n", result.Fingerprint)
		} else {
			fmt.Printf("deployment key already exists: %s\n", result.Fingerprint)
		}
		return nil
	},
}

func init() {
	deploymentKeyCmd.AddCommand(deploymentKeyEnsureCmd)
}
