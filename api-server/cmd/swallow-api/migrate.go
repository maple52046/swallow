package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/maple52046/swallow/config"
	"github.com/maple52046/swallow/internal/migration"
)

// migrateBackupConfirmed gates the destructive v2 -> v3 Platform rename behind an
// explicit operator acknowledgement that a verified backup exists.
var migrateBackupConfirmed bool

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply supported MongoDB schema migrations",
	RunE: func(_ *cobra.Command, _ []string) error {
		cfg, err := config.Load(config.LoadOptions{ConfigFile: rootConfigFile})
		if err != nil {
			return err
		}
		// Connecting has a short deadline so a wrong URI fails fast. The migration
		// itself deliberately runs without an artificial deadline: a large collection
		// rename must be allowed to finish, because an interrupted migration leaves the
		// schema marker behind and the binary would then refuse to start.
		connectCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		client, err := mongo.Connect(connectCtx, options.Client().ApplyURI(cfg.API.MongoURI))
		if err != nil {
			return err
		}
		defer client.Disconnect(context.Background())
		if err := migration.Migrate(context.Background(), client.Database(cfg.API.MongoDB), migrateBackupConfirmed); err != nil {
			return err
		}
		fmt.Printf("database schema is at version %d\n", migration.CurrentSchemaVersion)
		return nil
	},
}

func init() {
	migrateCmd.Flags().BoolVar(&migrateBackupConfirmed, "backup-confirmed", false,
		"confirm a verified backup exists before a destructive schema rename")
}
