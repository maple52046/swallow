package main

import (
	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/config"
	"github.com/maple52046/swallow/internal/app"
)

var ansibleExecutorCmd = &cobra.Command{
	Use:   "ansible-executor",
	Short: "Run queued Ansible executions",
	RunE: func(_ *cobra.Command, _ []string) error {
		cfg, err := config.Load(config.LoadOptions{ConfigFile: rootConfigFile})
		if err != nil {
			return err
		}
		return app.RunAnsibleExecutor(cfg.API)
	},
}
