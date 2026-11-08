package ansible

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// ansible.builtin.file gives the directories it creates the task's owner and mode, so the first
// software to create its data directory under swallow's shared data directory would own the shared
// directory too, and a 0750 data directory (Grafana native, VictoriaMetrics) would lock every other
// software out of its own. Every role that keeps a data directory there therefore includes the shared
// role shared_data_dir, which keeps the shared directory root:root 0755, before any task a run could
// reach that creates that data directory.
func TestDataDirectoriesKeepTheSharedDirectoryRootOwned(t *testing.T) {
	sharedRole := roleDir(t, "shared_data_dir")
	var sharedDefaults map[string]any
	readYAML(t, filepath.Join(sharedRole, "defaults", "main.yml"), &sharedDefaults)
	root, _ := sharedDefaults["shared_data_dir_path"].(string)
	if root != "/var/lib/swallow" {
		t.Fatalf("shared_data_dir_path = %q, want /var/lib/swallow", root)
	}
	var sharedTasks []map[string]any
	readYAML(t, filepath.Join(sharedRole, "tasks", "main.yml"), &sharedTasks)
	if len(sharedTasks) != 1 {
		t.Fatalf("shared_data_dir has %d tasks, want the one that keeps the directory root-owned", len(sharedTasks))
	}
	args, _ := moduleArgs(sharedTasks[0], "file")
	if args["path"] != "{{ shared_data_dir_path }}" || args["state"] != "directory" ||
		args["owner"] != "root" || args["group"] != "root" || args["mode"] != "0755" {
		t.Errorf("shared_data_dir sets %v, want {{ shared_data_dir_path }} as a root:root 0755 directory", args)
	}

	found := map[string]bool{}
	for _, module := range moduleDirs(t) {
		defaultsFiles, err := filepath.Glob(filepath.Join(module, "roles", "*", "defaults", "main.yml"))
		if err != nil {
			t.Fatal(err)
		}
		for _, defaultsFile := range defaultsFiles {
			roleRoot := filepath.Dir(filepath.Dir(defaultsFile))
			role := filepath.Base(roleRoot)
			var defaults map[string]any
			readYAML(t, defaultsFile, &defaults)
			for name, value := range defaults {
				text, _ := value.(string)
				if !strings.HasSuffix(name, "_data_dir") || !strings.HasPrefix(text, root+"/") {
					continue
				}
				found[role] = true
				steps := roleTaskOrder(t, filepath.Join(roleRoot, "tasks"), "main.yml", nil)
				keeps, creates := -1, -1
				for i, step := range steps {
					if keeps < 0 && step.includesRole == "shared_data_dir" {
						keeps = i
					}
					if creates < 0 && step.createsDirectory == "{{ "+name+" }}" {
						creates = i
					}
				}
				if creates < 0 {
					t.Errorf("role %s keeps %s under %s but no task creates it", role, name, root)
				} else if keeps < 0 || keeps > creates {
					t.Errorf("role %s can create {{ %s }} (step %d) before it includes shared_data_dir (step %d)", role, name, creates, keeps)
				}
			}
		}
	}
	for _, role := range []string{"prometheus", "grafana", "victoriametrics"} {
		if !found[role] {
			t.Errorf("role %s keeps no *_data_dir under %s; the check would miss it", role, root)
		}
	}
}

// roleStep is one task of a role as this check reads it: the role it includes, or the directory it
// creates.
type roleStep struct {
	includesRole     string
	createsDirectory string
}

// templatedName is the Jinja expression in a templated task file name.
var templatedName = regexp.MustCompile(`\{\{[^}]*\}\}`)

// roleTaskOrder flattens the role's task file in the order a run reaches its tasks: blocks are entered,
// and an include_tasks or import_tasks is replaced by the task files it names. A templated name (for
// example "{{ method }}.yml") stands for every task file it can match, in name order, so a step's index
// is the earliest point any run could reach it. stack holds the files being expanded, so a pattern
// never expands a file into itself.
func roleTaskOrder(t *testing.T, dir, file string, stack []string) []roleStep {
	t.Helper()
	var tasks []map[string]any
	readYAML(t, filepath.Join(dir, file), &tasks)
	stack = append(stack, file)
	var steps []roleStep
	var walk func(list []map[string]any)
	walk = func(list []map[string]any) {
		for _, task := range list {
			for _, key := range []string{"block", "rescue", "always"} {
				if nested, ok := task[key].([]any); ok {
					walk(taskMaps(nested))
				}
			}
			if args, ok := moduleArgs(task, "file"); ok && args["state"] == "directory" {
				path, _ := args["path"].(string)
				steps = append(steps, roleStep{createsDirectory: path})
			}
			for _, verb := range []string{"include_role", "import_role"} {
				if args, ok := moduleArgs(task, verb); ok {
					name, _ := args["name"].(string)
					steps = append(steps, roleStep{includesRole: name})
				}
			}
			for _, verb := range []string{"include_tasks", "import_tasks"} {
				target, ok := includedFile(task, verb)
				if !ok {
					continue
				}
				for _, match := range taskFilesMatching(t, dir, target) {
					if !slices.Contains(stack, match) {
						steps = append(steps, roleTaskOrder(t, dir, match, stack)...)
					}
				}
			}
		}
	}
	walk(tasks)
	return steps
}

// moduleArgs returns a task's arguments for an ansible.builtin module, written with or without the
// collection prefix.
func moduleArgs(task map[string]any, module string) (map[string]any, bool) {
	for _, key := range []string{"ansible.builtin." + module, module} {
		if args, ok := task[key].(map[string]any); ok {
			return args, true
		}
	}
	return nil, false
}

// includedFile returns the task file an include_tasks or import_tasks names, in its free form or its
// file argument.
func includedFile(task map[string]any, verb string) (string, bool) {
	for _, key := range []string{"ansible.builtin." + verb, verb} {
		switch value := task[key].(type) {
		case string:
			return value, true
		case map[string]any:
			file, ok := value["file"].(string)
			return file, ok
		}
	}
	return "", false
}

// taskFilesMatching returns the task files of dir a possibly templated name can refer to, sorted.
func taskFilesMatching(t *testing.T, dir, name string) []string {
	t.Helper()
	pattern := templatedName.ReplaceAllString(name, "*")
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		t.Fatal(err)
	}
	files := make([]string, 0, len(matches))
	for _, match := range matches {
		if info, err := os.Stat(match); err == nil && !info.IsDir() {
			files = append(files, filepath.Base(match))
		}
	}
	sort.Strings(files)
	return files
}

// taskMaps keeps the tasks of a decoded YAML list.
func taskMaps(list []any) []map[string]any {
	tasks := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if task, ok := item.(map[string]any); ok {
			tasks = append(tasks, task)
		}
	}
	return tasks
}
