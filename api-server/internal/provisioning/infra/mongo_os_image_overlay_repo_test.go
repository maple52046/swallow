package infra

import (
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// TestOSImageOverlayDocPersistsOverlayKey locks the persisted field names to the ones the
// unique index and the overlay queries rely on. A silent rename here would break the merge
// and let one image carry two names, so the schema is asserted explicitly rather than trusted.
func TestOSImageOverlayDocPersistsOverlayKey(t *testing.T) {
	doc := osImageOverlayDoc{
		IntegrationID: "integration-1",
		ImageID:       "ubuntu/jammy",
		Architecture:  "amd64",
		DisplayName:   "Compute Baseline",
		OSSystem:      "Ubuntu LTS",
		Release:       "22.04",
		Tags:          []string{"gpu", "ml"},
		UpdatedAt:     time.Now().UTC(),
	}
	raw, err := bson.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal BSON: %v", err)
	}
	var decoded bson.M
	if err := bson.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal BSON: %v", err)
	}
	for _, key := range []string{"integrationId", "imageId", "architecture", "displayName", "osSystem", "release", "tags", "updatedAt"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("serialized overlay document missing key %q; got keys %v", key, decoded)
		}
	}
}

// TestToOSImageOverlayRoundTrips verifies the infra mapper carries every overlay field into
// the domain type, so a read returns exactly what was stored.
func TestToOSImageOverlayRoundTrips(t *testing.T) {
	updated := time.Now().UTC()
	doc := osImageOverlayDoc{
		IntegrationID: "integration-1",
		ImageID:       "ubuntu/jammy",
		Architecture:  "amd64",
		DisplayName:   "Compute Baseline",
		OSSystem:      "Ubuntu LTS",
		Release:       "22.04",
		Tags:          []string{"gpu", "ml"},
		UpdatedAt:     updated,
	}
	got := toOSImageOverlay(&doc)
	want := &provisioningdomain.OSImageOverlay{
		IntegrationID: "integration-1",
		ImageID:       "ubuntu/jammy",
		Architecture:  "amd64",
		DisplayName:   "Compute Baseline",
		OSSystem:      "Ubuntu LTS",
		Release:       "22.04",
		Tags:          []string{"gpu", "ml"},
		UpdatedAt:     updated,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("toOSImageOverlay() = %+v, want %+v", *got, *want)
	}
}
