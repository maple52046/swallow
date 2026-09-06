package application

import (
	"encoding/json"
	"strings"
	"testing"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

func TestOperationProjectionDoesNotExposeSecretReferences(t *testing.T) {
	operation := &operationdomain.Workflow{
		Intent: map[string]any{"summary": "Deploy OS"},
		Steps: []operationdomain.Task{{
			ID: "provision-a", Kind: "provision-os", Name: "Provision OS",
			SecretRefs: map[string]string{"userData": "opaque-reference"},
		}},
	}
	item := toWorkflowItem(operation)
	if item.Steps[0].DependsOn == nil || item.Steps[0].Targets == nil || item.Steps[0].Artifacts == nil {
		t.Fatal("public Operation must encode empty Step collections as arrays")
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal public Operation: %v", err)
	}
	body := string(encoded)
	if strings.Contains(body, "secretRefs") || strings.Contains(body, "opaque-reference") {
		t.Fatalf("public Operation exposed a secret reference: %s", body)
	}
	if strings.Contains(body, `"dependsOn":null`) || strings.Contains(body, `"targets":null`) || strings.Contains(body, `"artifacts":null`) {
		t.Fatalf("public Operation exposed a nullable Step collection: %s", body)
	}
	if operation.Steps[0].SecretRefs["userData"] != "opaque-reference" {
		t.Fatal("public projection mutated the workflow Step")
	}
}
