// Package migration owns the explicit MongoDB schema version.
package migration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CurrentSchemaVersion is the only schema this binary can serve.
const CurrentSchemaVersion = 2

type metadata struct {
	ID        string    `bson:"_id"`
	Version   int       `bson:"version"`
	UpdatedAt time.Time `bson:"updatedAt"`
}

// EnsureInitialized creates the marker for a fresh database but never upgrades one.
func EnsureInitialized(ctx context.Context, db *mongo.Database) error {
	col := db.Collection("schema_metadata")
	_, err := col.UpdateOne(ctx, bson.M{"_id": "database"}, bson.M{
		"$setOnInsert": metadata{
			ID: "database", Version: CurrentSchemaVersion, UpdatedAt: time.Now().UTC(),
		},
	}, options.Update().SetUpsert(true))
	if err != nil {
		return err
	}
	return Check(ctx, db)
}

// Check refuses newer, older, or missing schemas.
func Check(ctx context.Context, db *mongo.Database) error {
	var state metadata
	err := db.Collection("schema_metadata").FindOne(ctx, bson.M{"_id": "database"}).Decode(&state)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return fmt.Errorf("database schema is not initialized; run swallow migrate")
	}
	if err != nil {
		return err
	}
	if state.Version != CurrentSchemaVersion {
		return fmt.Errorf("database schema version %d is incompatible with binary schema version %d",
			state.Version, CurrentSchemaVersion)
	}
	return nil
}

// Migrate initializes a fresh database. Versioned upgrade steps will be appended here.
func Migrate(ctx context.Context, db *mongo.Database) error {
	return EnsureInitialized(ctx, db)
}
