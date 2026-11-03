package migration

import (
	"context"
	"encoding/json"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// platformGrafanaLink is the Monitoring Link swallow records once a Platform's own Grafana is
// installed; a Platform without it has never had one.
const platformGrafanaLink = "Platform Grafana"

// migrateV12ToV13 records that every Platform without a Platform Grafana has its monitoring
// dashboards off, in its desired and its applied spec. The setting is on when omitted, so a Platform
// deployed before it existed read as having dashboards it never got, and turning them on was no
// change and installed nothing. With both specs false, the user's turning them on is a change its
// module realizes. A Platform with the link keeps its settings; the run is idempotent.
func migrateV12ToV13(ctx context.Context, db *mongo.Database) error {
	cursor, err := db.Collection("platforms").Find(ctx, bson.M{"monitoringLinks.name": bson.M{"$ne": platformGrafanaLink}})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var doc struct {
			ID   string `bson:"_id"`
			Spec *struct {
				Settings string `bson:"settings"`
			} `bson:"spec"`
			Status *struct {
				Applied *struct {
					Settings string `bson:"settings"`
				} `bson:"applied"`
			} `bson:"status"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return err
		}
		set := bson.M{}
		if doc.Spec != nil {
			settings, err := dashboardsOff(doc.Spec.Settings)
			if err != nil {
				return fmt.Errorf("platform %s spec: %w", doc.ID, err)
			}
			set["spec.settings"] = settings
		}
		if doc.Status != nil && doc.Status.Applied != nil {
			settings, err := dashboardsOff(doc.Status.Applied.Settings)
			if err != nil {
				return fmt.Errorf("platform %s applied spec: %w", doc.ID, err)
			}
			set["status.applied.settings"] = settings
		}
		if len(set) == 0 {
			continue
		}
		if _, err := db.Collection("platforms").UpdateOne(ctx, bson.M{"_id": doc.ID}, bson.M{"$set": set}); err != nil {
			return err
		}
	}
	return cursor.Err()
}

// dashboardsOff returns a Platform's settings JSON with monitoringDashboards false.
func dashboardsOff(raw string) (string, error) {
	settings := map[string]any{}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &settings); err != nil {
			return "", err
		}
	}
	settings["monitoringDashboards"] = false
	encoded, err := json.Marshal(settings)
	return string(encoded), err
}
