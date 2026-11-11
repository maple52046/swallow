package migration

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// migrateV14ToV15 replaces each Local Repository spec's boolean serveDeployments with its mode, so
// every Site keeps fetching where it did: true becomes offline (only from the Local Repository,
// refused while it is not ready) and false, or no value, upstream. Each observed source gains
// serving, true where its last observed state was ready, so an offline Site that was ready before
// the upgrade stays ready until the controller's next pass reports serving itself. A record that
// already has a mode is skipped, and each record is rewritten in one update, so the run is
// idempotent and resumes after an interruption.
func migrateV14ToV15(ctx context.Context, db *mongo.Database) error {
	col := db.Collection("local_repositories")
	cursor, err := col.Find(ctx, bson.M{"spec.mode": bson.M{"$exists": false}})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var doc struct {
			ID   string `bson:"_id"`
			Spec struct {
				ServeDeployments bool `bson:"serveDeployments"`
			} `bson:"spec"`
			Status struct {
				Sources []bson.Raw `bson:"sources"`
			} `bson:"status"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return err
		}
		mode := "upstream"
		if doc.Spec.ServeDeployments {
			mode = "offline"
		}
		set := bson.M{"spec.mode": mode}
		update := options.Update()
		// An array filter on a document without the array fails the whole update, so serving is set
		// only where sources were observed.
		if len(doc.Status.Sources) > 0 {
			set["status.sources.$[served].serving"] = true
			set["status.sources.$[unserved].serving"] = false
			update.SetArrayFilters(options.ArrayFilters{Filters: []any{
				bson.M{"served.state": "ready"},
				bson.M{"unserved.state": bson.M{"$ne": "ready"}},
			}})
		}
		if _, err := col.UpdateOne(ctx, bson.M{"_id": doc.ID},
			bson.M{"$set": set, "$unset": bson.M{"spec.serveDeployments": ""}}, update); err != nil {
			return err
		}
	}
	return cursor.Err()
}
