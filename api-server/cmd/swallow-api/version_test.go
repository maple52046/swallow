package main

import (
	"encoding/json"
	"testing"

	"github.com/maple52046/swallow/internal/adapters/store/mongodb/migration"
)

// assemble-release.sh reads these keys; renaming one breaks every release manifest.
func TestBuildInfoJSONCarriesTheSchemaVersions(t *testing.T) {
	raw, err := json.Marshal(currentBuildInfo())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"version", "commit", "builtAt"} {
		if _, ok := got[key].(string); !ok {
			t.Errorf("%s = %#v, want a string", key, got[key])
		}
	}
	if got["schemaVersion"] != float64(migration.CurrentSchemaVersion) {
		t.Errorf("schemaVersion = %v, want %d", got["schemaVersion"], migration.CurrentSchemaVersion)
	}
	if got["oldestUpgradableSchemaVersion"] != float64(migration.OldestUpgradableSchemaVersion) {
		t.Errorf("oldestUpgradableSchemaVersion = %v, want %d",
			got["oldestUpgradableSchemaVersion"], migration.OldestUpgradableSchemaVersion)
	}
	if migration.OldestUpgradableSchemaVersion > migration.CurrentSchemaVersion {
		t.Errorf("oldest upgradable schema %d is newer than the current schema %d",
			migration.OldestUpgradableSchemaVersion, migration.CurrentSchemaVersion)
	}
}
