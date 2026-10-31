package app

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// projectWith applies the completions of type kind that task declares through the registered
// projection handler, as the engine does on one of the Task's transitions.
func projectWith(t *testing.T, handlers map[string]workflowapp.ProjectionHandler, kind, workflowID string, task workflowdomain.Task) {
	t.Helper()
	for _, completion := range task.Completion {
		if completion.Type != kind {
			continue
		}
		if err := handlers[kind].Project(context.Background(), workflowapp.TaskTransition{
			Workflow: &workflowdomain.Workflow{ID: workflowID}, Task: task, Completion: completion,
		}); err != nil {
			t.Fatalf("project %s: %v", kind, err)
		}
	}
}

// The engine acts only on a plan's declarations (decision 072): none of its sources may name a
// registered Task kind, completion type, or compensation.
func TestEngineSourcesNameNoKind(t *testing.T) {
	catalog := workflowPlanCatalog()
	names := map[string]bool{}
	for kind := range catalog.TaskKinds {
		names[kind] = true
	}
	for kind := range catalog.Completions {
		names[kind] = true
	}
	for kind := range catalog.Compensations {
		names[kind] = true
	}
	for _, path := range []string{
		"../core/workflow/domain/plan.go", "../core/workflow/domain/schedule.go", "../core/workflow/domain/dispatch.go",
		"../core/workflow/application/engine.go", "../core/workflow/application/dispatcher.go",
		"../adapters/executor/temporalworkflow/driver.go", "../adapters/executor/temporalworkflow/driver_activities.go",
		"../adapters/executor/temporalworkflow/executor.go",
	} {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if lit, ok := node.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if value, err := strconv.Unquote(lit.Value); err == nil && names[value] {
					t.Errorf("%s names %q; the engine must act on declarations, not kinds", path, value)
				}
			}
			return true
		})
	}
}

// Every Task and Completion literal a launcher writes uses a registered kind, its registered
// Runner, and a registered completion type, so a plan is never refused at acceptance for a typo.
func TestLauncherPlansMatchTheCatalog(t *testing.T) {
	catalog := workflowPlanCatalog()
	runners := map[string]workflowdomain.RunnerKind{
		"RunnerKindInternal": workflowdomain.RunnerKindInternal, "RunnerKindAnsible": workflowdomain.RunnerKindAnsible,
		"RunnerKindProvisioner": workflowdomain.RunnerKindProvisioner,
	}
	files := parseAppSources(t)
	constants := stringConstants(files)
	checkedTasks, checkedCompletions := 0, 0
	for path, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			fields := map[string]ast.Expr{}
			for _, element := range lit.Elts {
				if kv, ok := element.(*ast.KeyValueExpr); ok {
					if key, ok := kv.Key.(*ast.Ident); ok {
						fields[key.Name] = kv.Value
					}
				}
			}
			switch typeName(lit.Type) {
			case "workflowdomain.Task":
				kind, ok := resolveString(fields["Kind"], constants)
				if !ok {
					return true
				}
				registered, known := catalog.TaskKinds[kind]
				if !known {
					t.Errorf("%s: Task kind %q is not registered", path, kind)
					return true
				}
				if selector, ok := fields["Executor"].(*ast.SelectorExpr); ok && runners[selector.Sel.Name] != registered.Runner {
					t.Errorf("%s: Task kind %q runs on %s, not %s", path, kind, registered.Runner, selector.Sel.Name)
				}
				checkedTasks++
			case "workflowdomain.Completion", "":
				if typeExpr, ok := fields["Type"]; ok {
					if value, ok := resolveString(typeExpr, constants); ok {
						if _, known := catalog.Completions[value]; !known {
							t.Errorf("%s: completion type %q is not registered", path, value)
						}
						checkedCompletions++
					}
				}
			}
			return true
		})
	}
	if checkedTasks < 15 || checkedCompletions < 8 {
		t.Fatalf("checked %d Task and %d Completion literals; the scan is missing launchers", checkedTasks, checkedCompletions)
	}
}

func parseAppSources(t *testing.T) map[string]*ast.File {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]*ast.File{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files[path] = file
	}
	return files
}

// stringConstants collects the package's string constants declared with a literal value.
func stringConstants(files map[string]*ast.File) map[string]string {
	constants := map[string]string{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				value := spec.(*ast.ValueSpec)
				for i, name := range value.Names {
					if i < len(value.Values) {
						if lit, ok := value.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							if unquoted, err := strconv.Unquote(lit.Value); err == nil {
								constants[name.Name] = unquoted
							}
						}
					}
				}
			}
		}
	}
	return constants
}

func resolveString(expr ast.Expr, constants map[string]string) (string, bool) {
	switch value := expr.(type) {
	case *ast.BasicLit:
		unquoted, err := strconv.Unquote(value.Value)
		return unquoted, err == nil
	case *ast.Ident:
		resolved, ok := constants[value.Name]
		return resolved, ok
	}
	return "", false
}

func typeName(expr ast.Expr) string {
	if selector, ok := expr.(*ast.SelectorExpr); ok {
		if pkg, ok := selector.X.(*ast.Ident); ok {
			return pkg.Name + "." + selector.Sel.Name
		}
	}
	return ""
}
