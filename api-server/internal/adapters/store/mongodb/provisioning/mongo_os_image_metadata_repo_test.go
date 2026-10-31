package provisioning

import (
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// TestOSImageMetadataDocPersistsOverlayKey locks the persisted field names to the ones the
// unique index and the metadata queries rely on. A silent rename here would break the merge
// and let one image carry two names, so the schema is asserted explicitly rather than trusted.
func TestOSImageMetadataDocPersistsOverlayKey(t *testing.T) {
	doc := osImageMetadataDoc{
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
			t.Errorf("serialized metadata document missing key %q; got keys %v", key, decoded)
		}
	}
}

// TestToOSImageMetadataRoundTrips verifies the store mapper carries every metadata field into
// the domain type, so a read returns exactly what was stored.
func TestToOSImageMetadataRoundTrips(t *testing.T) {
	updated := time.Now().UTC()
	doc := osImageMetadataDoc{
		IntegrationID: "integration-1",
		ImageID:       "ubuntu/jammy",
		Architecture:  "amd64",
		DisplayName:   "Compute Baseline",
		OSSystem:      "Ubuntu LTS",
		Release:       "22.04",
		Tags:          []string{"gpu", "ml"},
		UpdatedAt:     updated,
	}
	got := toOSImageMetadata(&doc)
	want := &provisioningdomain.OSImageMetadata{
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
		t.Errorf("toOSImageMetadata() = %+v, want %+v", *got, *want)
	}
}
