package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// v14LandingPaths is each dashboard bundle's landing dashboard as the Grafana roles report it from
// this release on (grafana_provisioning_landing_dashboards): /d/<uid>/<slug>. It is the v14 snapshot
// of that table, not a second source for new links: a role run writes those.
var v14LandingPaths = map[string]string{
	"device-metrics-exporter": "/d/fe0ayyeeoxvy8f/overview",
	"omnistat":                "/d/edqnz7pce7togc-rms/global-rms",
	"node-exporter":           "/d/swallow-node-exporter/servers",
}

// v14RewritablePath matches the paths a swallow-written Platform Grafana link had before v14: the
// Grafana root, or a landing dashboard without its slug.
var v14RewritablePath = regexp.MustCompile(`^(/|/d/[A-Za-z0-9_-]+/?)?$`)

// migrateV13ToV14 points every Platform Grafana Monitoring Link that still opens the Grafana root
// (or a landing dashboard without its slug) at the landing dashboard of the bundles that Grafana
// carries, so a Platform deployed before this release opens the same page a new deploy records,
// without being deployed again. The bundles follow the Platform's applied settings, else its spec:
// a Kubernetes Platform whose GPU Operator runs its metrics exporter lands on the Device Metrics
// Exporter Overview, a Slurm Platform on the Device Metrics Exporter or Omnistat it runs, and any
// other on Servers. A link a user pointed at another page, and a Platform of another type, are left
// as they are; the run is idempotent.
func migrateV13ToV14(ctx context.Context, db *mongo.Database) error {
	cursor, err := db.Collection("platforms").Find(ctx, bson.M{"monitoringLinks.name": platformGrafanaLink})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var doc struct {
			ID    string `bson:"_id"`
			Type  string `bson:"type"`
			Links []struct {
				Name string `bson:"name"`
				URL  string `bson:"url"`
			} `bson:"monitoringLinks"`
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
		raw := ""
		if doc.Spec != nil {
			raw = doc.Spec.Settings
		}
		if doc.Status != nil && doc.Status.Applied != nil && doc.Status.Applied.Settings != "" {
			raw = doc.Status.Applied.Settings
		}
		bundle, err := v14LandingBundle(doc.Type, raw)
		if err != nil {
			return fmt.Errorf("platform %s settings: %w", doc.ID, err)
		}
		if bundle == "" {
			continue
		}
		for _, link := range doc.Links {
			if link.Name != platformGrafanaLink {
				continue
			}
			target, ok := v14LandingURL(link.URL, v14LandingPaths[bundle])
			if !ok || target == link.URL {
				continue
			}
			if _, err := db.Collection("platforms").UpdateOne(ctx, bson.M{"_id": doc.ID},
				bson.M{"$set": bson.M{"monitoringLinks.$[link].url": target}},
				options.Update().SetArrayFilters(options.ArrayFilters{Filters: []any{bson.M{"link.name": platformGrafanaLink}}}),
			); err != nil {
				return err
			}
		}
	}
	return cursor.Err()
}

// v14LandingBundle returns the bundle whose dashboard a Platform's Grafana lands on, following the
// bundles its deploy provisions, or "" for a Platform type that has no Platform Grafana.
func v14LandingBundle(platformType, raw string) (string, error) {
	var settings struct {
		GPUOperator *struct {
			Enabled         bool `json:"enabled"`
			MetricsExporter bool `json:"metricsExporter"`
		} `json:"gpuOperator"`
		Omnistat *struct {
			Enabled bool `json:"enabled"`
		} `json:"omnistat"`
		DeviceMetricsExporter *struct {
			Enabled bool `json:"enabled"`
		} `json:"deviceMetricsExporter"`
	}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &settings); err != nil {
			return "", err
		}
	}
	switch platformType {
	case "kubernetes":
		if settings.GPUOperator != nil && settings.GPUOperator.Enabled && settings.GPUOperator.MetricsExporter {
			return "device-metrics-exporter", nil
		}
		return "node-exporter", nil
	case "slurm":
		switch {
		case settings.DeviceMetricsExporter != nil && settings.DeviceMetricsExporter.Enabled:
			return "device-metrics-exporter", nil
		case settings.Omnistat != nil && settings.Omnistat.Enabled:
			return "omnistat", nil
		}
		return "node-exporter", nil
	default:
		return "", nil
	}
}

// v14LandingURL returns link with its path replaced by landing when the path is one swallow wrote
// before v14, keeping its scheme and host; ok is false for a link it must not touch.
func v14LandingURL(link, landing string) (string, bool) {
	parsed, err := url.Parse(link)
	if err != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		!v14RewritablePath.MatchString(parsed.Path) {
		return "", false
	}
	parsed.Path = landing
	parsed.RawPath = ""
	return parsed.String(), true
}
