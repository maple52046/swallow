package domain

import (
	"encoding/json"
	"errors"
	"testing"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// validationManifest is a Validation module that declares one suite, its template, and a failure
// code.
func validationManifest(id string) Manifest {
	manifest := validManifest(KindValidation, id)
	manifest.Playbooks = []string{"validate-" + id}
	manifest.Templates = map[string]workflowdomain.Template{
		id: {Version: 1, Jobs: []workflowdomain.TemplateJob{{Name: "validate", Label: "Validate", Retry: workflowdomain.RetryJob}},
			Tasks: []workflowdomain.TemplateTask{{
				ID: "check", Name: "Check", Job: "validate", Targets: workflowdomain.TargetsServers,
				Content: workflowdomain.TemplateContent{Type: workflowdomain.ContentHostAutomation, Playbook: "validate-" + id},
			}}},
	}
	manifest.Validations = []ValidationDeclaration{{
		ID: id + "-baseline", Title: "Baseline", Target: "servers", Impact: "observe", BudgetSeconds: 600, Template: id,
		Checks: []ValidationCheckDeclaration{{ID: "time-sync", Title: "Time sync"}},
	}}
	manifest.FailureCodes = map[string]FailureCodeDeclaration{
		id + "_unreachable": {FailureClass: "transient", Remediation: "Check the network."},
	}
	return manifest
}

// A module of the validation kind declares suites, templates, and failure codes, and the registry
// returns them with the template each suite runs.
func TestValidationDeclarationsLoad(t *testing.T) {
	registry, err := NewRegistry(Module{Manifest: validationManifest("host")})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	validations := registry.Validations()
	if len(validations) != 1 || validations[0].Declaration.ID != "host-baseline" || validations[0].Module.Ref() != "validation/host" ||
		len(validations[0].Template.Tasks) != 1 {
		t.Errorf("Validations() = %+v", validations)
	}
	if _, ok := registry.Template(KindValidation, "host", "host"); !ok {
		t.Error("Template(validation/host, host) found nothing")
	}
	if declared, ok := registry.FailureCodes()["host_unreachable"]; !ok || declared.FailureClass != "transient" {
		t.Errorf("FailureCodes() = %v", registry.FailureCodes())
	}
}

func TestValidationDeclarationsRefuseMalformedShapes(t *testing.T) {
	cases := map[string]func(*Manifest){
		"template runs another module's playbook": func(m *Manifest) {
			template := m.Templates["host"]
			template.Tasks[0].Content.Playbook = "deploy-slurm"
			m.Templates["host"] = template
		},
		"malformed template":      func(m *Manifest) { m.Templates["host"] = workflowdomain.Template{} },
		"template name not kebab": func(m *Manifest) { m.Templates["Host"] = m.Templates["host"] },
		"suite without title":     func(m *Manifest) { m.Validations[0].Title = "" },
		"unknown target":          func(m *Manifest) { m.Validations[0].Target = "rack" },
		"platform without type":   func(m *Manifest) { m.Validations[0].Target = "platform" },
		"servers with a type":     func(m *Manifest) { m.Validations[0].PlatformType = "slurm" },
		"unknown impact":          func(m *Manifest) { m.Validations[0].Impact = "harmless" },
		"no budget":               func(m *Manifest) { m.Validations[0].BudgetSeconds = 0 },
		"undeclared template":     func(m *Manifest) { m.Validations[0].Template = "missing" },
		"malformed parameters":    func(m *Manifest) { m.Validations[0].Parameters = json.RawMessage(`{"type":"array"}`) },
		"no check":                func(m *Manifest) { m.Validations[0].Checks = nil },
		"duplicate check": func(m *Manifest) {
			m.Validations[0].Checks = append(m.Validations[0].Checks, m.Validations[0].Checks[0])
		},
		"duplicate suite": func(m *Manifest) { m.Validations = append(m.Validations, m.Validations[0]) },
		"failure code not snake": func(m *Manifest) {
			m.FailureCodes["Host-Down"] = FailureCodeDeclaration{FailureClass: "transient", Remediation: "x"}
		},
		"unknown failure class": func(m *Manifest) {
			m.FailureCodes["host_unreachable"] = FailureCodeDeclaration{FailureClass: "fatal", Remediation: "x"}
		},
		"failure without next step": func(m *Manifest) {
			m.FailureCodes["host_unreachable"] = FailureCodeDeclaration{FailureClass: "transient"}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			manifest := validationManifest("host")
			mutate(&manifest)
			if err := manifest.Validate(); !errors.Is(err, ErrInvalidManifest) {
				t.Errorf("Validate() = %v, want ErrInvalidManifest", err)
			}
		})
	}
}

// Suite ids and failure codes are owned: two modules cannot declare the same one.
func TestValidationClaimsAreOwned(t *testing.T) {
	cases := map[string]func(a, b *Manifest){
		"same suite": func(a, b *Manifest) { b.Validations[0].ID = a.Validations[0].ID },
		"same failure code": func(a, b *Manifest) {
			b.FailureCodes = map[string]FailureCodeDeclaration{"host_unreachable": a.FailureCodes["host_unreachable"]}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			a, b := validationManifest("host"), validationManifest("network")
			mutate(&a, &b)
			if _, err := NewRegistry(Module{Manifest: a}, Module{Manifest: b}); !errors.Is(err, ErrModuleConflict) {
				t.Errorf("NewRegistry() = %v, want ErrModuleConflict", err)
			}
		})
	}
}
