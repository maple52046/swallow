package tests

import (
	"net/http"
	"testing"
)

func TestSlurmDeploymentRequirementCanonicalRoundTripAndDisable(t *testing.T) {
	fixture := setupPlatform(t)
	auth := fixture.adminAuth(t)
	path := "/api/v1/platforms/deployment-requirements/slurm"

	response := doRequest(t, fixture.app, http.MethodGet, path, nil, auth)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("initial GET status = %d: %s", response.StatusCode, rawBody(t, response))
	}
	initial := parseBody(t, response)
	if initial["platformType"] != "slurm" || initial["minimumResources"] != nil || initial["updatedAt"] != nil {
		t.Fatalf("initial body = %#v, want disabled representation", initial)
	}

	response = doRequest(t, fixture.app, http.MethodPut, path, map[string]any{
		"minimumResources": map[string]any{"cpuCores": 4, "memoryMiB": 24576, "storageGB": 80},
	}, auth)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d: %s", response.StatusCode, rawBody(t, response))
	}
	updated := parseBody(t, response)
	minimum := updated["minimumResources"].(map[string]any)
	if minimum["cpuCores"] != float64(4) || minimum["memoryMiB"] != float64(24576) || minimum["storageGB"] != float64(80) || updated["updatedAt"] == nil {
		t.Fatalf("updated body = %#v", updated)
	}

	response = doRequest(t, fixture.app, http.MethodPut, path, map[string]any{"minimumResources": nil}, auth)
	if response.StatusCode != http.StatusOK || parseBody(t, response)["minimumResources"] != nil {
		t.Fatalf("disable status = %d body = %s", response.StatusCode, rawBody(t, response))
	}
}

func TestSlurmDeploymentRequirementRejectsInvalidOrLegacyRequests(t *testing.T) {
	fixture := setupPlatform(t)
	auth := fixture.adminAuth(t)
	path := "/api/v1/platforms/deployment-requirements/slurm"

	for _, minimum := range []map[string]any{
		{"cpuCores": 0, "memoryMiB": 24576, "storageGB": 80},
		{"cpuCores": 4.5, "memoryMiB": 24576, "storageGB": 80},
		{"cpuCores": 4, "memoryMiB": 0, "storageGB": 80},
		{"cpuCores": 4, "memoryMiB": 24576, "storageGB": 0},
	} {
		response := doRequest(t, fixture.app, http.MethodPut, path, map[string]any{"minimumResources": minimum}, auth)
		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("minimum %#v status = %d, want 400", minimum, response.StatusCode)
		}
	}

	response := doRequest(t, fixture.app, http.MethodGet, "/api/v1/clusters/deployment-requirements/slurm", nil, auth)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("legacy requirement route status = %d, want 404", response.StatusCode)
	}
}
