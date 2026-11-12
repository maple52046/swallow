package domain

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// ValidationDeclaration is one Validation Suite a module offers (glossary: Validation Suite), as its
// manifest declares it under "validations" (decision 081). The validation context reads it through
// the composition root; this package only checks its shape and that the template it names exists in
// the same module. Its JSON field names are part of the module contract v1.
type ValidationDeclaration struct {
	// ID is the suite's id, kebab-case and unique across the build's modules.
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	// Target is "servers" (one or more deployed Servers) or "platform" (one Platform).
	Target string `json:"target"`
	// PlatformType is the Platform type a "platform" suite validates; required for it, refused
	// otherwise. It is opaque data to the core.
	PlatformType string `json:"platformType,omitempty"`
	// Impact is the Validation Impact: "observe", "workload", or "disruptive".
	Impact string `json:"impact"`
	// BudgetSeconds bounds one report run of the suite (its Workflow's budget).
	BudgetSeconds int `json:"budgetSeconds"`
	// Template names the module's Workflow Template that runs the suite.
	Template string `json:"template"`
	// Parameters is the schema of the run parameters the suite accepts, in the spec schema subset
	// (SpecSchema); absent means the suite takes none.
	Parameters json.RawMessage              `json:"parameters,omitempty"`
	Checks     []ValidationCheckDeclaration `json:"checks"`
	// Experimental offers the suite as a preview whose checks may still change: consumers may hide
	// it, and it runs like any other suite.
	Experimental bool `json:"experimental,omitempty"`
}

// ValidationCheckDeclaration is one Validation Check of a declared suite.
type ValidationCheckDeclaration struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	// Gating says whether a deployment that composes the suite as a gate fails when this check
	// does not pass.
	Gating bool `json:"gating"`
}

// FailureCodeDeclaration classifies one failure code of a module's host automation: the code a
// role's failure message starts with, in brackets (decision 077). The Ansible Runner applies it to
// the diagnostic of a failed run whose message carries the code.
type FailureCodeDeclaration struct {
	// FailureClass is "transient", "permanent", or "unknown" (glossary: Failure Class).
	FailureClass string `json:"failureClass"`
	// Remediation is the next step, in the user's terms.
	Remediation string `json:"remediation"`
	// Retryable, when set, overrides whether a Task retry may be requested; false means a retry
	// cannot succeed until something outside the Task changes.
	Retryable *bool `json:"retryable,omitempty"`
}

// The Validation Suite target kinds and impacts a declaration may name; the validation domain owns
// their meaning.
var (
	validationTargets = map[string]bool{"servers": true, "platform": true}
	validationImpacts = map[string]bool{"observe": true, "workload": true, "disruptive": true}
	failureClasses    = map[string]bool{"transient": true, "permanent": true, "unknown": true}
)

var failureCodePattern = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*$`)

// validateTemplates checks every template on its own and that each playbook it runs is one the
// module lists, so a template cannot reach another module's content.
func (m Manifest) validateTemplates() error {
	playbooks := map[string]bool{}
	for _, playbook := range m.Playbooks {
		playbooks[playbook] = true
	}
	for name, template := range m.Templates {
		if !moduleIDPattern.MatchString(name) {
			return fmt.Errorf("%w: module %s template name %q must be kebab-case", ErrInvalidManifest, m.Ref(), name)
		}
		if err := template.Validate(); err != nil {
			return fmt.Errorf("%w: module %s template %q: %w", ErrInvalidManifest, m.Ref(), name, err)
		}
		for _, playbook := range template.Playbooks() {
			if !playbooks[playbook] {
				return fmt.Errorf("%w: module %s template %q runs playbook %q, which the module does not list",
					ErrInvalidManifest, m.Ref(), name, playbook)
			}
		}
	}
	return nil
}

// validateValidations checks each declared suite: a kebab-case id unique in the module, a title, a
// known target and impact, a platform type exactly when the target is a Platform, a positive budget,
// a template the module declares, a parameter schema the spec schema reader accepts, and at least
// one check with unique kebab-case ids and titles.
func (m Manifest) validateValidations() error {
	ids := map[string]bool{}
	for _, suite := range m.Validations {
		if !moduleIDPattern.MatchString(suite.ID) {
			return fmt.Errorf("%w: module %s suite id %q must be kebab-case", ErrInvalidManifest, m.Ref(), suite.ID)
		}
		if ids[suite.ID] {
			return fmt.Errorf("%w: module %s declares suite %q twice", ErrInvalidManifest, m.Ref(), suite.ID)
		}
		ids[suite.ID] = true
		if strings.TrimSpace(suite.Title) == "" {
			return fmt.Errorf("%w: module %s suite %q has no title", ErrInvalidManifest, m.Ref(), suite.ID)
		}
		if !validationTargets[suite.Target] {
			return fmt.Errorf("%w: module %s suite %q target %q is not servers or platform", ErrInvalidManifest, m.Ref(), suite.ID, suite.Target)
		}
		if (suite.Target == "platform") != (suite.PlatformType != "") {
			return fmt.Errorf("%w: module %s suite %q needs a platformType exactly when it targets a platform", ErrInvalidManifest, m.Ref(), suite.ID)
		}
		if !validationImpacts[suite.Impact] {
			return fmt.Errorf("%w: module %s suite %q impact %q is not observe, workload, or disruptive", ErrInvalidManifest, m.Ref(), suite.ID, suite.Impact)
		}
		if suite.BudgetSeconds <= 0 {
			return fmt.Errorf("%w: module %s suite %q needs a positive budgetSeconds", ErrInvalidManifest, m.Ref(), suite.ID)
		}
		if _, ok := m.Templates[suite.Template]; !ok {
			return fmt.Errorf("%w: module %s suite %q names template %q, which the module does not declare", ErrInvalidManifest, m.Ref(), suite.ID, suite.Template)
		}
		if len(suite.Parameters) > 0 {
			if _, err := ParseSpecSchema(suite.Parameters); err != nil {
				return fmt.Errorf("%w: module %s suite %q parameters: %v", ErrInvalidManifest, m.Ref(), suite.ID, err)
			}
		}
		if len(suite.Checks) == 0 {
			return fmt.Errorf("%w: module %s suite %q declares no check", ErrInvalidManifest, m.Ref(), suite.ID)
		}
		checks := map[string]bool{}
		for _, check := range suite.Checks {
			if !moduleIDPattern.MatchString(check.ID) || strings.TrimSpace(check.Title) == "" {
				return fmt.Errorf("%w: module %s suite %q check %q needs a kebab-case id and a title", ErrInvalidManifest, m.Ref(), suite.ID, check.ID)
			}
			if checks[check.ID] {
				return fmt.Errorf("%w: module %s suite %q declares check %q twice", ErrInvalidManifest, m.Ref(), suite.ID, check.ID)
			}
			checks[check.ID] = true
		}
	}
	return nil
}

// validateFailureCodes checks each declared failure code: a snake_case code, a known Failure Class,
// and a remediation.
func (m Manifest) validateFailureCodes() error {
	for code, declaration := range m.FailureCodes {
		if !failureCodePattern.MatchString(code) {
			return fmt.Errorf("%w: module %s failure code %q must be snake_case", ErrInvalidManifest, m.Ref(), code)
		}
		if !failureClasses[declaration.FailureClass] {
			return fmt.Errorf("%w: module %s failure code %q class %q is not transient, permanent, or unknown",
				ErrInvalidManifest, m.Ref(), code, declaration.FailureClass)
		}
		if strings.TrimSpace(declaration.Remediation) == "" {
			return fmt.Errorf("%w: module %s failure code %q has no remediation", ErrInvalidManifest, m.Ref(), code)
		}
	}
	return nil
}

// ModuleValidation is one declared suite with the module that declares it and the template that
// runs it.
type ModuleValidation struct {
	Module      Manifest
	Declaration ValidationDeclaration
	Template    workflowdomain.Template
}

// Validations returns every suite the build's modules declare, in module order and then in each
// module's declaration order.
func (r *Registry) Validations() []ModuleValidation {
	var validations []ModuleValidation
	for _, module := range r.modules {
		for _, declaration := range module.Manifest.Validations {
			validations = append(validations, ModuleValidation{
				Module: module.Manifest, Declaration: declaration, Template: module.Manifest.Templates[declaration.Template],
			})
		}
	}
	return validations
}

// Template returns the template name of module "<kind>/<id>".
func (r *Registry) Template(kind Kind, id, name string) (workflowdomain.Template, bool) {
	module, ok := r.Module(kind, id)
	if !ok {
		return workflowdomain.Template{}, false
	}
	template, ok := module.Manifest.Templates[name]
	return template, ok
}

// FailureCodes returns every failure code the build's modules declare; the registry refused two
// modules declaring the same code.
func (r *Registry) FailureCodes() map[string]FailureCodeDeclaration {
	codes := map[string]FailureCodeDeclaration{}
	for _, module := range r.modules {
		for code, declaration := range module.Manifest.FailureCodes {
			codes[code] = declaration
		}
	}
	return codes
}
