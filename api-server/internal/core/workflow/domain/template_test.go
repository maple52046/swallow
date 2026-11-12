package domain

import (
	"errors"
	"reflect"
	"testing"
)

// templateCatalog registers the Task kinds the tests' templates run: one host automation kind and
// one internal kind.
func templateCatalog() PlanCatalog {
	return PlanCatalog{TaskKinds: map[string]TaskKind{
		"run-playbook": {Runner: RunnerKindHostAutomation},
		"wait-for-ssh": {Runner: RunnerKindInternal},
	}}
}

func validTemplate() Template {
	return Template{
		Version: 2,
		Jobs:    []TemplateJob{{Name: "check", Label: "Check", Retry: RetryJob}},
		Tasks: []TemplateTask{
			{ID: "ready", Name: "Wait", Job: "check", Content: TemplateContent{Type: ContentTask, Kind: "wait-for-ssh"}, Targets: TargetsAccess},
			{ID: "probe", Name: "Probe", Job: "check", Content: TemplateContent{Type: ContentHostAutomation, Playbook: "validate-x"},
				Targets: TargetsMembers, DependsOn: []string{"ready"}, TimeoutSeconds: 600, Vars: map[string]any{"swallow_x": 1}},
		},
	}
}

// A template compiles into prefixed Tasks of the catalog's kinds and Runners, aimed at the targets
// their bindings name; an entry Task depends on the enclosing plan, the rest on their own Tasks.
func TestTemplateCompile(t *testing.T) {
	compiled, err := validTemplate().Compile(templateCatalog(), TemplateInput{
		IDPrefix: "v-", Access: []string{"s-1"}, Members: []string{"s-1", "s-2"}, After: []string{"install"},
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	wantJobs := []JobDeclaration{{Name: "check", Label: "Check", Retry: RetryJob}}
	if !reflect.DeepEqual(compiled.Jobs, wantJobs) {
		t.Errorf("Compile() jobs = %+v, want %+v", compiled.Jobs, wantJobs)
	}
	if len(compiled.Tasks) != 2 {
		t.Fatalf("Compile() tasks = %d, want 2", len(compiled.Tasks))
	}
	ready, probe := compiled.Tasks[0], compiled.Tasks[1]
	if ready.ID != "v-ready" || ready.Kind != "wait-for-ssh" || ready.Executor != RunnerKindInternal ||
		!reflect.DeepEqual(ready.DependsOn, []string{"install"}) ||
		!reflect.DeepEqual(ready.Targets, []ResourceReference{{Kind: "server", ID: "s-1"}}) || ready.Parameters != nil {
		t.Errorf("Compile() ready = %+v", ready)
	}
	if probe.ID != "v-probe" || probe.Kind != "run-playbook" || probe.Executor != RunnerKindHostAutomation ||
		!reflect.DeepEqual(probe.DependsOn, []string{"v-ready"}) || probe.TimeoutSeconds != 600 || len(probe.Targets) != 2 {
		t.Errorf("Compile() probe = %+v", probe)
	}
	if probe.Parameters["playbook"] != "validate-x" || !reflect.DeepEqual(probe.Parameters["vars"], map[string]any{"swallow_x": 1}) {
		t.Errorf("Compile() probe parameters = %v", probe.Parameters)
	}
}

// Compiling refuses what the request or the catalog cannot satisfy.
func TestTemplateCompileRefuses(t *testing.T) {
	tests := []struct {
		name    string
		catalog PlanCatalog
		input   TemplateInput
	}{
		{name: "binding without Servers", catalog: templateCatalog(), input: TemplateInput{Access: []string{"s-1"}}},
		{name: "unregistered Task kind", catalog: PlanCatalog{TaskKinds: map[string]TaskKind{"run-playbook": {Runner: RunnerKindHostAutomation}}},
			input: TemplateInput{Access: []string{"s-1"}, Members: []string{"s-1"}}},
		{name: "no host automation kind", catalog: PlanCatalog{TaskKinds: map[string]TaskKind{"wait-for-ssh": {Runner: RunnerKindInternal}}},
			input: TemplateInput{Access: []string{"s-1"}, Members: []string{"s-1"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validTemplate().Compile(tc.catalog, tc.input); !errors.Is(err, ErrInvalidTemplate) {
				t.Errorf("Compile() error = %v, want ErrInvalidTemplate", err)
			}
		})
	}
}

// Validate refuses every shape the format does not allow.
func TestTemplateValidate(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Template)
	}{
		{name: "no version", change: func(tp *Template) { tp.Version = 0 }},
		{name: "duplicate Job", change: func(tp *Template) { tp.Jobs = append(tp.Jobs, tp.Jobs[0]) }},
		{name: "Job without retry", change: func(tp *Template) { tp.Jobs[0].Retry = "" }},
		{name: "no Task", change: func(tp *Template) { tp.Tasks = nil }},
		{name: "duplicate Task", change: func(tp *Template) { tp.Tasks[1].ID = "ready" }},
		{name: "Task without name", change: func(tp *Template) { tp.Tasks[0].Name = " " }},
		{name: "undeclared Job", change: func(tp *Template) { tp.Tasks[0].Job = "other" }},
		{name: "unknown content", change: func(tp *Template) { tp.Tasks[0].Content.Type = "script" }},
		{name: "host automation without playbook", change: func(tp *Template) { tp.Tasks[1].Content.Playbook = "" }},
		{name: "task content with vars", change: func(tp *Template) { tp.Tasks[0].Vars = map[string]any{"x": 1} }},
		{name: "unknown targets", change: func(tp *Template) { tp.Tasks[0].Targets = "all" }},
		{name: "unsettled continueOn", change: func(tp *Template) { tp.Tasks[1].ContinueOn = []TaskStatus{TaskRunning} }},
		{name: "negative timeout", change: func(tp *Template) { tp.Tasks[1].TimeoutSeconds = -1 }},
		{name: "unknown dependency", change: func(tp *Template) { tp.Tasks[1].DependsOn = []string{"missing"} }},
		{name: "cycle", change: func(tp *Template) { tp.Tasks[0].DependsOn = []string{"probe"} }},
	}
	if err := validTemplate().Validate(); err != nil {
		t.Fatalf("Validate() of the valid template error = %v", err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			template := validTemplate()
			tc.change(&template)
			if err := template.Validate(); !errors.Is(err, ErrInvalidTemplate) {
				t.Errorf("Validate() error = %v, want ErrInvalidTemplate", err)
			}
		})
	}
}
