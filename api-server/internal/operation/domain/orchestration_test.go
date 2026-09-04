package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOperationStepJSONCarriesOpaqueSecretReferencesForWorkflowExecution(t *testing.T) {
	encoded, err := json.Marshal(OperationStep{
		ID: "step-1", Kind: "provision-os", Name: "Provision OS",
		Executor: StepExecutorMAAS, SecretRefs: map[string]string{"userData": "secret-reference"},
	})
	if err != nil {
		t.Fatalf("marshal Step: %v", err)
	}
	body := string(encoded)
	if !strings.Contains(body, "secretRefs") || !strings.Contains(body, "secret-reference") {
		t.Fatalf("workflow Step JSON lost its opaque secret reference: %s", body)
	}
}
