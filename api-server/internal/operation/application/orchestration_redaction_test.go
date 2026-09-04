package application

import (
	"testing"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// publicSteps must strip opaque secret references and the executor-only Parameters (which
// carry the frozen request and extraVars) so no operator-supplied plaintext crosses the
// HTTP boundary, while still normalizing nil collections to arrays.
func TestPublicStepsStripsSecretsAndParameters(t *testing.T) {
	public := publicSteps([]operationdomain.OperationStep{{
		ID:         "s1",
		SecretRefs: map[string]string{"userData": "opaque-ref"},
		Parameters: map[string]any{"extraVars": map[string]any{"password": "hunter2"}, "playbook": "deploy.yml"},
	}})
	if public[0].SecretRefs != nil {
		t.Error("secret references must be stripped from public steps")
	}
	if public[0].Parameters != nil {
		t.Error("step parameters must be redacted from public steps")
	}
	if public[0].DependsOn == nil || public[0].Targets == nil || public[0].Artifacts == nil {
		t.Error("nil collections must normalize to arrays")
	}
}

// redactSensitiveMap must drop sensitive keys at any depth while preserving the rest of the
// operator's intent snapshot.
func TestRedactSensitiveMapRemovesSecretsAtAnyDepth(t *testing.T) {
	redacted := redactSensitiveMap(map[string]any{
		"summary": "deploy prod",
		"request": map[string]any{
			"name":     "prod-k8s",
			"userData": "#cloud-config\nsecret",
			"extraVars": map[string]any{
				"vrrp_password": "s3cret",
			},
		},
		"targets": []any{
			map[string]any{"serverId": "s1", "credential": "leak"},
		},
	})

	request := redacted["request"].(map[string]any)
	if _, ok := request["userData"]; ok {
		t.Error("userData must be redacted")
	}
	if _, ok := request["extraVars"]; ok {
		t.Error("extraVars must be redacted")
	}
	if request["name"] != "prod-k8s" {
		t.Errorf("non-sensitive fields must survive, got %v", request["name"])
	}
	target := redacted["targets"].([]any)[0].(map[string]any)
	if _, ok := target["credential"]; ok {
		t.Error("credential must be redacted inside arrays")
	}
	if target["serverId"] != "s1" {
		t.Errorf("non-sensitive nested fields must survive, got %v", target["serverId"])
	}
}
