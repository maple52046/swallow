package migration

import (
	"context"

	"go.mongodb.org/mongo-driver/mongo"
)

// migrateV9ToV10 retires the schema-v2 Operation records (decision 074): it drops the
// `operations` collection, which since v4 held only them. No binary reads or runs them any more,
// and nothing else refers to them. Dropping a missing collection is a no-op, so an interrupted
// run resumes safely. The user confirms a verified backup first, because the records are not
// kept anywhere else.
func migrateV9ToV10(ctx context.Context, db *mongo.Database) error {
	return db.Collection("operations").Drop(ctx)
}
