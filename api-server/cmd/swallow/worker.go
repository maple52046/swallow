package main

import (
	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/config"
	"github.com/maple52046/swallow/internal/app"
)

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Run durable Operation workflows and activities",
	RunE: func(_ *cobra.Command, _ []string) error {
		cfg, err := config.Load(config.LoadOptions{ConfigFile: rootConfigFile})
		if err != nil {
			return err
		}
		return app.RunWorker(cfg.API)
	},
}
