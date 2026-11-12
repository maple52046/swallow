package domain

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strings"
)

// Template is a Workflow Template (glossary): a module-declared, data-only description of Jobs and
// Tasks that Compile turns, with the targets of one request, into Tasks and Job declarations of a
// Workflow's plan. A template never runs anything itself, and compiling it accepts nothing: the
// result is checked at acceptance like any other plan (PlanCatalog.Validate and the Workflow
// service), so a template cannot declare what a Go launcher could not.
//
// Templates are part of the module contract v1 (decision 075, 081): their JSON field names are a
// compatibility surface of every module.json.
type Template struct {
	// Version is the template's own version (at least 1); a Workflow compiled from it records it as
	// its definition version.
	Version int            `json:"version"`
	Jobs    []TemplateJob  `json:"jobs"`
	Tasks   []TemplateTask `json:"tasks"`
}

// TemplateJob declares one named Job of the template with its display label and retry granularity.
type TemplateJob struct {
	Name  string           `json:"name"`
	Label string           `json:"label"`
	Retry RetryGranularity `json:"retry"`
}

// TemplateTask is one Task of the template.
type TemplateTask struct {
	// ID identifies the Task within the template (kebab-case); Compile prefixes it.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Job names one of the template's Jobs; empty puts the Task in the implicit Job.
	Job     string          `json:"job,omitempty"`
	Content TemplateContent `json:"content"`
	// Targets says which of the request's targets the Task acts on.
	Targets TemplateTargets `json:"targets"`
	// DependsOn lists other Tasks of the template; a Task without one depends on the enclosing
	// plan's TemplateInput.After instead.
	DependsOn  []string     `json:"dependsOn,omitempty"`
	ContinueOn []TaskStatus `json:"continueOn,omitempty"`
	// TimeoutSeconds bounds one attempt of the Task (Task.TimeoutSeconds); 0 means no bound of its
	// own.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
	// Vars are static variables of a host automation content, handed to the playbook as trusted
	// variables beside the ones the launcher adds.
	Vars map[string]any `json:"vars,omitempty"`
}

// TemplateContent is what a Task runs, as a content type plus a reference (core-and-extensions §5.2),
// so the template assumes no particular host automation adapter.
type TemplateContent struct {
	// Type is ContentHostAutomation or ContentTask.
	Type string `json:"type"`
	// Playbook names a playbook of the release's allowlist, for ContentHostAutomation.
	Playbook string `json:"playbook,omitempty"`
	// Kind names a registered Task kind, for ContentTask.
	Kind string `json:"kind,omitempty"`
}

// The content types a template Task may run.
const (
	// ContentHostAutomation runs a playbook as the plan catalog's one Task kind of the host
	// automation Runner.
	ContentHostAutomation = "host-automation"
	// ContentTask runs a registered Task kind (for example wait-for-ssh) through that kind's Runner.
	ContentTask = "task"
)

// TemplateTargets names which of a request's targets a template Task acts on.
type TemplateTargets string

// The template target bindings.
const (
	// TargetsServers is the request's target Servers.
	TargetsServers TemplateTargets = "servers"
	// TargetsAccess is the Servers a Platform is entered through (its login or control-plane nodes,
	// as the Platform's module decides).
	TargetsAccess TemplateTargets = "access"
	// TargetsMembers is every member Server of the Platform.
	TargetsMembers TemplateTargets = "members"
)

// ErrInvalidTemplate means a template breaks the format or cannot be compiled for a request.
var ErrInvalidTemplate = errors.New("invalid Workflow template")

var templateIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Validate checks the template on its own: a version, uniquely named Jobs with a retry granularity,
// at least one Task, unique kebab-case Task ids each with a name, a declared Job (or none), a known
// content type with its reference, a known target binding, dependencies on other Tasks of the
// template without a cycle, continueOn outcomes that settle, and a non-negative timeout. Whether a
// playbook or Task kind exists is checked where it is known: playbooks against the owning module's
// manifest, Task kinds at compile time.
func (t Template) Validate() error {
	if t.Version < 1 {
		return fmt.Errorf("%w: version must be at least 1", ErrInvalidTemplate)
	}
	jobs := map[string]bool{}
	for _, job := range t.Jobs {
		if !templateIDPattern.MatchString(job.Name) {
			return fmt.Errorf("%w: Job name %q must be kebab-case", ErrInvalidTemplate, job.Name)
		}
		if jobs[job.Name] {
			return fmt.Errorf("%w: Job %q is declared twice", ErrInvalidTemplate, job.Name)
		}
		if job.Retry != RetryTask && job.Retry != RetryJob {
			return fmt.Errorf("%w: Job %q retry must be %q or %q", ErrInvalidTemplate, job.Name, RetryTask, RetryJob)
		}
		jobs[job.Name] = true
	}
	if len(t.Tasks) == 0 {
		return fmt.Errorf("%w: at least one Task is required", ErrInvalidTemplate)
	}
	ids := map[string]bool{}
	for _, task := range t.Tasks {
		if !templateIDPattern.MatchString(task.ID) {
			return fmt.Errorf("%w: Task id %q must be kebab-case", ErrInvalidTemplate, task.ID)
		}
		if ids[task.ID] {
			return fmt.Errorf("%w: Task %q is declared twice", ErrInvalidTemplate, task.ID)
		}
		ids[task.ID] = true
		if strings.TrimSpace(task.Name) == "" {
			return fmt.Errorf("%w: Task %q has no name", ErrInvalidTemplate, task.ID)
		}
		if task.Job != "" && !jobs[task.Job] {
			return fmt.Errorf("%w: Task %q names undeclared Job %q", ErrInvalidTemplate, task.ID, task.Job)
		}
		switch task.Content.Type {
		case ContentHostAutomation:
			if task.Content.Playbook == "" || task.Content.Kind != "" {
				return fmt.Errorf("%w: Task %q content %q needs a playbook and no kind", ErrInvalidTemplate, task.ID, task.Content.Type)
			}
		case ContentTask:
			if task.Content.Kind == "" || task.Content.Playbook != "" {
				return fmt.Errorf("%w: Task %q content %q needs a kind and no playbook", ErrInvalidTemplate, task.ID, task.Content.Type)
			}
			if len(task.Vars) > 0 {
				return fmt.Errorf("%w: Task %q: only host automation content takes vars", ErrInvalidTemplate, task.ID)
			}
		default:
			return fmt.Errorf("%w: Task %q content type %q is not %q or %q", ErrInvalidTemplate, task.ID,
				task.Content.Type, ContentHostAutomation, ContentTask)
		}
		switch task.Targets {
		case TargetsServers, TargetsAccess, TargetsMembers:
		default:
			return fmt.Errorf("%w: Task %q targets %q is not servers, access, or members", ErrInvalidTemplate, task.ID, task.Targets)
		}
		for _, outcome := range task.ContinueOn {
			switch outcome {
			case TaskSucceeded, TaskFailed, TaskSkipped, TaskCanceled:
			default:
				return fmt.Errorf("%w: Task %q continueOn %q is not a settled outcome", ErrInvalidTemplate, task.ID, outcome)
			}
		}
		if task.TimeoutSeconds < 0 {
			return fmt.Errorf("%w: Task %q timeoutSeconds is negative", ErrInvalidTemplate, task.ID)
		}
	}
	for _, task := range t.Tasks {
		for _, dependency := range task.DependsOn {
			if !ids[dependency] || dependency == task.ID {
				return fmt.Errorf("%w: Task %q depends on %q, which is not another Task of the template", ErrInvalidTemplate, task.ID, dependency)
			}
		}
	}
	return t.checkAcyclic()
}

// Playbooks returns the playbooks the template's Tasks run, in Task order.
func (t Template) Playbooks() []string {
	var playbooks []string
	for _, task := range t.Tasks {
		if task.Content.Type == ContentHostAutomation {
			playbooks = append(playbooks, task.Content.Playbook)
		}
	}
	return playbooks
}

func (t Template) checkAcyclic() error {
	byID := make(map[string]TemplateTask, len(t.Tasks))
	for _, task := range t.Tasks {
		byID[task.ID] = task
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("%w: Task dependencies contain a cycle through %q", ErrInvalidTemplate, id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, dependency := range byID[id].DependsOn {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		delete(visiting, id)
		visited[id] = true
		return nil
	}
	for _, task := range t.Tasks {
		if err := visit(task.ID); err != nil {
			return err
		}
	}
	return nil
}

// TemplateInput is what one request binds a template to.
type TemplateInput struct {
	// IDPrefix is prepended to every Task id, so a template composed into a larger plan cannot
	// collide with the plan's own Tasks; Job names are not prefixed.
	IDPrefix string
	// Servers, Access, and Members are the Server ids the target bindings resolve to.
	Servers []string
	Access  []string
	Members []string
	// After lists Tasks of the enclosing plan that every Task without a dependency of its own
	// depends on, so composed Jobs run after the plan's earlier Jobs.
	After []string
}

// CompiledTemplate is a compiled template: Tasks ready to join a plan (a host automation Task's
// Parameters carry its "playbook" and, when the template declares them, its static "vars", which
// the launcher turns into the Task's frozen extra variables), and the template's Job declarations.
type CompiledTemplate struct {
	Tasks []Task
	Jobs  []JobDeclaration
}

// HostAutomationTaskKind returns the one Task kind the catalog runs with the host automation
// Runner, which every host automation content compiles to. A catalog with none, or with several,
// cannot run host automation content.
func (c PlanCatalog) HostAutomationTaskKind() (string, bool) {
	found := ""
	for kind, declared := range c.TaskKinds {
		if declared.Runner != RunnerKindHostAutomation {
			continue
		}
		if found != "" {
			return "", false
		}
		found = kind
	}
	return found, found != ""
}

// Compile binds the template to input. Each Task gets its kind and Runner from catalog (a host
// automation content runs as the catalog's host automation Task kind; a task content as its kind),
// and its targets from the binding it names; a binding that resolves to no Server, or a Task kind the
// catalog does not register, is ErrInvalidTemplate. Compile accepts nothing and has no side effect.
func (t Template) Compile(catalog PlanCatalog, input TemplateInput) (CompiledTemplate, error) {
	if err := t.Validate(); err != nil {
		return CompiledTemplate{}, err
	}
	compiled := CompiledTemplate{}
	for _, job := range t.Jobs {
		compiled.Jobs = append(compiled.Jobs, JobDeclaration{Name: job.Name, Label: job.Label, Retry: job.Retry})
	}
	for _, task := range t.Tasks {
		kind := task.Content.Kind
		if task.Content.Type == ContentHostAutomation {
			hostAutomation, ok := catalog.HostAutomationTaskKind()
			if !ok {
				return CompiledTemplate{}, fmt.Errorf("%w: Task %q needs host automation, which the plan catalog does not run", ErrInvalidTemplate, task.ID)
			}
			kind = hostAutomation
		}
		declared, ok := catalog.TaskKinds[kind]
		if !ok {
			return CompiledTemplate{}, fmt.Errorf("%w: Task %q runs unregistered Task kind %q", ErrInvalidTemplate, task.ID, kind)
		}
		servers := input.bound(task.Targets)
		if len(servers) == 0 {
			return CompiledTemplate{}, fmt.Errorf("%w: Task %q targets %s, which this request has none of", ErrInvalidTemplate, task.ID, task.Targets)
		}
		targets := make([]ResourceReference, 0, len(servers))
		for _, id := range servers {
			targets = append(targets, ResourceReference{Kind: "server", ID: id})
		}
		dependsOn := make([]string, 0, len(task.DependsOn))
		for _, dependency := range task.DependsOn {
			dependsOn = append(dependsOn, input.IDPrefix+dependency)
		}
		if len(dependsOn) == 0 {
			dependsOn = append(dependsOn, input.After...)
		}
		compiledTask := Task{
			ID: input.IDPrefix + task.ID, Kind: kind, Name: task.Name, Job: task.Job, Executor: declared.Runner,
			DependsOn: dependsOn, ContinueOn: append([]TaskStatus(nil), task.ContinueOn...), Targets: targets,
			TimeoutSeconds: task.TimeoutSeconds,
		}
		if task.Content.Type == ContentHostAutomation {
			compiledTask.Parameters = map[string]any{"playbook": task.Content.Playbook}
			if len(task.Vars) > 0 {
				compiledTask.Parameters["vars"] = maps.Clone(task.Vars)
			}
		}
		compiled.Tasks = append(compiled.Tasks, compiledTask)
	}
	return compiled, nil
}

func (i TemplateInput) bound(targets TemplateTargets) []string {
	switch targets {
	case TargetsServers:
		return i.Servers
	case TargetsAccess:
		return i.Access
	case TargetsMembers:
		return i.Members
	}
	return nil
}
