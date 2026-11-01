package application

import (
	"encoding/json"
	"strings"
	"testing"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

func TestOperationProjectionDoesNotExposeSecretReferences(t *testing.T) {
	operation := &workflowdomain.Workflow{
		Intent: map[string]any{"summary": "Deploy OS"},
		Steps: []workflowdomain.Task{{
			ID: "provision-a", Kind: "provision-os", Name: "Provision OS",
			SecretRefs: map[string]string{"userData": "opaque-reference"},
		}},
	}
	item := toWorkflowItem(operation, workflowdomain.PlanCatalog{})
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

// The projection lists every Job the Tasks name, in declaration order and each with a label: a
// declaration stored before Jobs had labels reads the catalog's, a name the catalog lacks reads
// title-cased, and a Job no declaration lists retries by Task.
func TestWorkflowProjectionLabelsEveryJob(t *testing.T) {
	catalog := workflowdomain.PlanCatalog{JobLabels: map[string]string{"ensure-os": "Provision OS"}}
	operation := &workflowdomain.Workflow{
		Intent: map[string]any{},
		Steps: []workflowdomain.Task{
			{ID: "provision-a", Job: "ensure-os"}, {ID: "install", Job: "configure-k0s"},
			{ID: "check", Job: "post-checks"}, {ID: "note"},
		},
		Jobs: []workflowdomain.JobDeclaration{
			{Name: "ensure-os", Retry: workflowdomain.RetryJob},
			{Name: "configure-k0s", Label: "Configure Kubernetes", Retry: workflowdomain.RetryJob},
		},
	}
	got := toWorkflowItem(operation, catalog).Jobs
	want := []WorkflowJobItem{
		{Name: "ensure-os", Label: "Provision OS", Retry: "job"},
		{Name: "configure-k0s", Label: "Configure Kubernetes", Retry: "job"},
		{Name: "post-checks", Label: "Post Checks", Retry: "task"},
	}
	if len(got) != len(want) {
		t.Fatalf("Jobs = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Jobs[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	if operation.Jobs[0].Label != "" {
		t.Error("the projection labelled the stored declaration")
	}
}
