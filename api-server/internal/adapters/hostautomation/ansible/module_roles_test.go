package ansible

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"
)

// roleTaskKeys are the task keywords that pull in another role at run time.
var roleTaskKeys = []string{
	"include_role", "import_role", "ansible.builtin.include_role", "ansible.builtin.import_role",
}

// referencedRoles collects the roles named by a list of tasks, descending into blocks.
func referencedRoles(tasks []any, into map[string]bool) {
	for _, item := range tasks {
		task, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range roleTaskKeys {
			switch value := task[key].(type) {
			case string:
				into[value] = true
			case map[string]any:
				if name, ok := value["name"].(string); ok {
					into[name] = true
				}
			}
		}
		for _, key := range []string{"block", "rescue", "always"} {
			if nested, ok := task[key].([]any); ok {
				referencedRoles(nested, into)
			}
		}
	}
}

// roleEntryName returns the role a play's roles: entry or a meta dependency names.
func roleEntryName(entry any) string {
	switch value := entry.(type) {
	case string:
		return value
	case map[string]any:
		for _, key := range []string{"role", "name"} {
			if name, ok := value[key].(string); ok {
				return name
			}
		}
	}
	return ""
}

func readYAML(t *testing.T, path string, into any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, into); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

// playbookRoles returns the roles a playbook's plays use directly.
func playbookRoles(t *testing.T, path string) map[string]bool {
	t.Helper()
	var plays []map[string]any
	readYAML(t, path, &plays)
	roles := map[string]bool{}
	for _, play := range plays {
		if entries, ok := play["roles"].([]any); ok {
			for _, entry := range entries {
				if name := roleEntryName(entry); name != "" {
					roles[name] = true
				}
			}
		}
		for _, key := range []string{"pre_tasks", "tasks", "post_tasks", "handlers"} {
			if tasks, ok := play[key].([]any); ok {
				referencedRoles(tasks, roles)
			}
		}
	}
	return roles
}

// nestedRoles returns the roles a role pulls in through its tasks, handlers, or meta dependencies.
func nestedRoles(t *testing.T, dir string) map[string]bool {
	t.Helper()
	roles := map[string]bool{}
	for _, sub := range []string{"tasks", "handlers"} {
		files, err := filepath.Glob(filepath.Join(dir, sub, "*.yml"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			var tasks []any
			readYAML(t, file, &tasks)
			referencedRoles(tasks, roles)
		}
	}
	meta := filepath.Join(dir, "meta", "main.yml")
	if _, err := os.Stat(meta); err == nil {
		var doc map[string]any
		readYAML(t, meta, &doc)
		if deps, ok := doc["dependencies"].([]any); ok {
			for _, dep := range deps {
				if name := roleEntryName(dep); name != "" {
					roles[name] = true
				}
			}
		}
	}
	return roles
}

// Every role a shipped playbook uses, directly or through another role, must resolve in the roles
// path its run gets (see rolesPath), so content that only worked by reaching into another module's
// roles fails here instead of on a host.
func TestShippedPlaybooksResolveRolesWithinTheirModule(t *testing.T) {
	raw, err := os.ReadFile(shippedManifest)
	if err != nil {
		t.Fatal(err)
	}
	var manifest playbookManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, entry := range manifest.Playbooks {
		t.Run(entry.Name, func(t *testing.T) {
			path, err := rolesPath(modulesRoot, entry.Path)
			if err != nil {
				t.Fatal(err)
			}
			searched := filepath.SplitList(path)
			pending := playbookRoles(t, filepath.Join(modulesRoot, entry.Path))
			resolved := map[string]bool{}
			for len(pending) > 0 {
				var name string
				for name = range pending {
					break
				}
				delete(pending, name)
				resolved[name] = true
				var dir string
				for _, candidate := range searched {
					if info, err := os.Stat(filepath.Join(candidate, name)); err == nil && info.IsDir() {
						dir = filepath.Join(candidate, name)
						break
					}
				}
				if dir == "" {
					t.Errorf("%s uses role %q, which is not in its roles path %v", entry.Path, name, searched)
					continue
				}
				for nested := range nestedRoles(t, dir) {
					if !resolved[nested] {
						pending[nested] = true
					}
				}
			}
		})
	}
}

// A role name must be defined by exactly one module (shared counts as one): with two definitions,
// which one a run uses would depend on the roles path order rather than on ownership.
func TestShippedRoleNamesAreUnique(t *testing.T) {
	owners := map[string][]string{}
	for _, module := range moduleDirs(t) {
		entries, err := os.ReadDir(filepath.Join(module, "roles"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				owners[entry.Name()] = append(owners[entry.Name()], module)
			}
		}
	}
	if len(owners) == 0 {
		t.Fatal("found no shipped roles; is modulesRoot right?")
	}
	var duplicated []string
	for name, modules := range owners {
		if len(modules) > 1 {
			duplicated = append(duplicated, name)
		}
	}
	sort.Strings(duplicated)
	for _, name := range duplicated {
		t.Errorf("role %q is defined by %v, want exactly one module", name, owners[name])
	}
}
