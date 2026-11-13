package command

import (
	"net/http"
	"testing"
)

// A Zone list is filtered by Site, while a Pool belongs to the installation: its list sends no
// siteId and the command has no --site-id flag (infrastructure.md).
func TestInfrastructureListsZonesBySiteAndPoolsForTheInstallation(t *testing.T) {
	var queries []string
	useTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	})

	if err := runCommand(t, newInfrastructureCommand(), "zones", "list", "--site-id", "site-a"); err != nil {
		t.Fatalf("zones list: %v", err)
	}
	if err := runCommand(t, newInfrastructureCommand(), "pools", "list"); err != nil {
		t.Fatalf("pools list: %v", err)
	}
	want := []string{"/api/v1/infrastructure/zones?siteId=site-a", "/api/v1/infrastructure/pools?"}
	if len(queries) != 2 || queries[0] != want[0] || queries[1] != want[1] {
		t.Errorf("requests = %v, want %v", queries, want)
	}
	if err := runCommand(t, newInfrastructureCommand(), "pools", "list", "--site-id", "site-a"); err == nil {
		t.Error("pools list --site-id: got nil error, want an unknown-flag error")
	}
}
