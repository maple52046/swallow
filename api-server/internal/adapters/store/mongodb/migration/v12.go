package migration

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// retiredKubernetesExporterKinds are the Workflow kinds a Site's playbookMappings may still name
// although the release no longer accepts them: the Kubernetes exporter DaemonSets were retired.
var retiredKubernetesExporterKinds = []string{"deploy-k8s-exporters", "remove-k8s-exporters"}

// migrateV11ToV12 retires the per-Platform exporter owner: it unsets `exporterOwner` on every
// Platform and removes the retired Kubernetes exporter kinds from every Site automation
// configuration's playbookMappings. Saving a configuration validates each mapped kind, so a stored
// mapping of a kind the release no longer accepts would make every later save fail. Neither the
// field nor the mappings select anything any more, so removing them loses no intent; unsetting a
// missing field is a no-op, so an interrupted run resumes safely.
func migrateV11ToV12(ctx context.Context, db *mongo.Database) error {
	if _, err := db.Collection("platforms").UpdateMany(ctx,
		bson.M{"exporterOwner": bson.M{"$exists": true}},
		bson.M{"$unset": bson.M{"exporterOwner": ""}},
	); err != nil {
		return err
	}
	unset := bson.M{}
	for _, kind := range retiredKubernetesExporterKinds {
		unset["playbookMappings."+kind] = ""
	}
	_, err := db.Collection("automation_configurations").UpdateMany(ctx, bson.M{}, bson.M{"$unset": unset})
	return err
}
