package migration

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// retiredMappedKinds are the Workflow kinds a Site's playbookMappings may still name although the
// release no longer accepts them: the host exporters became the Node Exporter's Managed Software.
var retiredMappedKinds = []string{"install-exporters", "uninstall-exporters"}

// migrateV10ToV11 removes the retired Workflow kinds from every Site automation configuration's
// playbookMappings. Saving a configuration validates each mapped kind, so a stored mapping of a
// kind the release no longer accepts would make every later save fail. The mappings select
// nothing any more, so removing them loses no intent; unsetting a missing key is a no-op, so an
// interrupted run resumes safely.
func migrateV10ToV11(ctx context.Context, db *mongo.Database) error {
	unset := bson.M{}
	for _, kind := range retiredMappedKinds {
		unset["playbookMappings."+kind] = ""
	}
	_, err := db.Collection("automation_configurations").UpdateMany(ctx, bson.M{}, bson.M{"$unset": unset})
	return err
}
