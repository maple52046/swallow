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

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply supported MongoDB schema migrations",
	RunE: func(_ *cobra.Command, _ []string) error {
		cfg, err := config.Load(config.LoadOptions{ConfigFile: rootConfigFile})
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.API.MongoURI))
		if err != nil {
			return err
		}
		defer client.Disconnect(context.Background())
		if err := migration.Migrate(ctx, client.Database(cfg.API.MongoDB)); err != nil {
			return err
		}
		fmt.Printf("database schema is at version %d\n", migration.CurrentSchemaVersion)
		return nil
	},
}
