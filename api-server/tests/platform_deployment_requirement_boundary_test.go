package tests

import (
	"net/http"
	"testing"
)

func TestSlurmDeploymentRequirementRequiresExplicitMinimumResourcesField(t *testing.T) {
	fixture := setupPlatform(t)
	response := doRequest(
		t,
		fixture.app,
		http.MethodPut,
		"/api/v1/platforms/deployment-requirements/slurm",
		map[string]any{},
		fixture.adminAuth(t),
	)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT without minimumResources status = %d, want 400", response.StatusCode)
	}
}
