package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// LocalRunner executes the pinned ansible-runner binary without a shell.
type LocalRunner struct {
	command      string
	projectRoot  string
	runtimeRoot  string
	artifactRoot string
}

// NewLocalRunner constructs a runner whose mutable and persistent roots are explicit.
func NewLocalRunner(command, projectRoot, runtimeRoot, artifactRoot string) *LocalRunner {
	return &LocalRunner{
		command: command, projectRoot: projectRoot,
		runtimeRoot: runtimeRoot, artifactRoot: artifactRoot,
	}
}

// resultFileVar names the extra var that tells a playbook where to write its captured
// result. The path is inside the private runtime directory, so a credential written there
// is read by swallow and then deleted with that directory, never reaching an artifact.
const resultFileVar = "swallow_result_path"

// Run materializes secrets only in a private runtime directory and removes it afterward.
func (r *LocalRunner) Run(ctx context.Context, input operationdomain.RunnerInput) (operationdomain.RunnerResult, error) {
	var result operationdomain.RunnerResult
	if input.Credential.SSHPrivateKey == "" {
		return result, operationdomain.ErrAutomationCredentialMissing
	}
	if strings.TrimSpace(input.Configuration.KnownHosts) == "" {
		return result, fmt.Errorf("knownHosts is required; SSH host-key verification cannot be disabled")
	}
	if err := os.MkdirAll(r.runtimeRoot, 0o700); err != nil {
		return result, err
	}
	if err := os.Chmod(r.runtimeRoot, 0o700); err != nil {
		return result, err
	}
	if err := os.MkdirAll(r.artifactRoot, 0o750); err != nil {
		return result, err
	}

	privateDir, err := os.MkdirTemp(r.runtimeRoot, executionRunIDPrefix(input))
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(privateDir)
	if err := os.Chmod(privateDir, 0o700); err != nil {
		return result, err
	}

	inventory := cloneInventory(input.Inventory)
	setConnectionVars(inventory, input.Configuration)
	envDir := filepath.Join(privateDir, "env")
	if err := os.Mkdir(envDir, 0o700); err != nil {
		return result, err
	}
	inventoryPath := filepath.Join(privateDir, "inventory.json")
	// The discovery projection is in the Ansible dynamic-inventory schema (_meta, all, and
	// group host lists), which Ansible only accepts from an executable inventory script, not
	// a static file. So the JSON is written to a file and exposed through a tiny script that
	// prints it for --list; a static file in this schema would parse as zero hosts.
	inventoryScriptPath := filepath.Join(privateDir, "inventory-source")
	// ansible-runner reads the SSH key from env/ssh_key in the private data dir and starts
	// an ssh-agent with it; it has no --private-key flag. Writing it anywhere else, or
	// passing it as an argument, does not authenticate the run.
	privateKeyPath := filepath.Join(envDir, "ssh_key")
	knownHostsPath := filepath.Join(privateDir, "known_hosts")
	extraVarsPath := filepath.Join(privateDir, "extravars.json")
	resultPath := filepath.Join(privateDir, "result.json")
	passwordsPath := filepath.Join(envDir, "passwords")

	// Extra vars are assembled here rather than persisted: the operation's own vars,
	// plus run-time secrets and the result path, live only in this private file.
	extraVars := mergeExtraVars(input.Operation.ExtraVars, input.SecretVars)
	extraVars[resultFileVar] = resultPath

	if err := writeJSONFile(inventoryPath, inventory, 0o600); err != nil {
		return result, err
	}
	if err := writeInventoryScript(inventoryScriptPath, inventoryPath); err != nil {
		return result, err
	}
	if err := os.WriteFile(privateKeyPath, []byte(input.Credential.SSHPrivateKey), 0o600); err != nil {
		return result, err
	}
	if err := os.WriteFile(knownHostsPath, []byte(input.Configuration.KnownHosts), 0o600); err != nil {
		return result, err
	}
	if err := writeJSONFile(extraVarsPath, extraVars, 0o600); err != nil {
		return result, err
	}
	if input.Credential.BecomePassword != "" {
		if err := writeJSONFile(passwordsPath,
			map[string]string{"BECOME password.*": input.Credential.BecomePassword}, 0o600); err != nil {
			return result, err
		}
	}

	runArtifacts := filepath.Join(r.artifactRoot, input.Operation.Execution.RunID)
	if err := os.MkdirAll(runArtifacts, 0o750); err != nil {
		return result, err
	}
	logFile, err := os.OpenFile(filepath.Join(runArtifacts, "process.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return result, err
	}
	defer logFile.Close()

	args := []string{
		"run", privateDir,
		"--project-dir", r.projectRoot,
		"--artifact-dir", r.artifactRoot,
		"--ident", input.Operation.Execution.RunID,
		"--inventory", inventoryScriptPath,
		"--playbook", input.PlaybookPath,
		"--limit", strings.Join(input.Operation.TargetServerIDs, ","),
		"--cmdline", "--extra-vars @" + extraVarsPath,
	}
	command := exec.CommandContext(ctx, r.command, args...)
	command.Stdout = logFile
	command.Stderr = logFile
	command.Env = append(os.Environ(),
		"ANSIBLE_HOST_KEY_CHECKING=True",
		"ANSIBLE_SSH_ARGS=-o UserKnownHostsFile="+knownHostsPath,
	)
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, fmt.Errorf("ansible-runner: %w", err)
	}

	// Read the captured result before the deferred RemoveAll deletes the private dir.
	result.Data = readResultFile(resultPath)
	return result, nil
}

// Logs returns locally retained output without exposing temporary credentials.
func (r *LocalRunner) Logs(_ context.Context, runID string) (string, error) {
	file, err := os.Open(filepath.Join(r.artifactRoot, filepath.Base(runID), "stdout"))
	if os.IsNotExist(err) {
		file, err = os.Open(filepath.Join(r.artifactRoot, filepath.Base(runID), "process.log"))
		if os.IsNotExist(err) {
			return "", nil
		}
	}
	if err != nil {
		return "", err
	}
	defer file.Close()
	raw, err := io.ReadAll(file)
	return string(raw), err
}

// Events projects a run's task results from ansible-runner's job_events directory.
//
// ansible-runner writes one JSON file per event, named with a leading ordinal, so sorting
// by that ordinal reproduces execution order. Only per-task host results are surfaced;
// task output (which can contain secrets) is deliberately dropped.
func (r *LocalRunner) Events(_ context.Context, runID string) ([]operationdomain.TaskEvent, error) {
	dir := filepath.Join(r.artifactRoot, filepath.Base(runID), "job_events")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []operationdomain.TaskEvent{}, nil
	}
	if err != nil {
		return nil, err
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			files = append(files, entry.Name())
		}
	}
	sort.Slice(files, func(i, j int) bool {
		return eventOrdinal(files[i]) < eventOrdinal(files[j])
	})

	events := make([]operationdomain.TaskEvent, 0, len(files))
	for _, name := range files {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if event, ok := parseTaskEvent(raw); ok {
			events = append(events, event)
		}
	}
	return events, nil
}

// jobEventJSON is the subset of an ansible-runner job event swallow reads.
type jobEventJSON struct {
	Event     string `json:"event"`
	EventData struct {
		Play  string `json:"play"`
		Task  string `json:"task"`
		Host  string `json:"host"`
		Start string `json:"start"`
		End   string `json:"end"`
		Res   struct {
			Changed bool `json:"changed"`
		} `json:"res"`
	} `json:"event_data"`
}

// taskEventStatus maps a runner event name to a member-visible status, and reports whether
// the event is a per-task host result worth surfacing at all.
var taskEventStatus = map[string]string{
	"runner_on_ok":          "ok",
	"runner_on_failed":      "failed",
	"runner_on_skipped":     "skipped",
	"runner_on_unreachable": "unreachable",
}

func parseTaskEvent(raw []byte) (operationdomain.TaskEvent, bool) {
	var doc jobEventJSON
	if err := json.Unmarshal(raw, &doc); err != nil {
		return operationdomain.TaskEvent{}, false
	}
	status, ok := taskEventStatus[doc.Event]
	if !ok || doc.EventData.Host == "" {
		return operationdomain.TaskEvent{}, false
	}
	return operationdomain.TaskEvent{
		Play:      doc.EventData.Play,
		Task:      doc.EventData.Task,
		Host:      doc.EventData.Host,
		Status:    status,
		Changed:   doc.EventData.Res.Changed,
		StartedAt: parseEventTime(doc.EventData.Start),
		EndedAt:   parseEventTime(doc.EventData.End),
	}, true
}

func parseEventTime(value string) *time.Time {
	if value == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return &t
}

// eventOrdinal extracts the leading counter from a job-event filename such as
// "12-<uuid>.json". A name without one sorts last so it never displaces ordered events.
func eventOrdinal(name string) int {
	digits, _, _ := strings.Cut(name, "-")
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 1 << 30
	}
	return n
}

// mergeExtraVars combines the operation's persisted vars with run-time secret vars.
// Secret vars win on a key collision, and neither input is mutated.
func mergeExtraVars(base, secret map[string]any) map[string]any {
	merged := make(map[string]any, len(base)+len(secret)+1)
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range secret {
		merged[key] = value
	}
	return merged
}

// readResultFile returns the JSON object a playbook wrote, or nil. A missing or malformed
// file is not an error: a playbook that captured nothing is the common case.
func readResultFile(path string) map[string]any {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil
	}
	return data
}

// writeInventoryScript writes an executable inventory source that prints the dynamic
// inventory JSON for --list and an empty object for --host. Ansible's script inventory
// plugin expects exactly this contract, and _meta.hostvars in the JSON means --host is
// never actually invoked per host.
func writeInventoryScript(scriptPath, jsonPath string) error {
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--host\" ]; then\n" +
		"  echo '{}'\n" +
		"else\n" +
		"  cat " + shellQuote(jsonPath) + "\n" +
		"fi\n"
	return os.WriteFile(scriptPath, []byte(script), 0o700)
}

// shellQuote single-quotes a path for safe embedding in the inventory script. Paths come
// from MkdirTemp so they contain no single quotes, but quoting keeps the script correct if
// that ever changes.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func writeJSONFile(path string, value any, mode os.FileMode) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, mode)
}

func cloneInventory(source map[string]any) map[string]any {
	raw, _ := json.Marshal(source)
	var cloned map[string]any
	_ = json.Unmarshal(raw, &cloned)
	return cloned
}

func setConnectionVars(inventory map[string]any, configuration *operationdomain.AutomationConfiguration) {
	meta, ok := inventory["_meta"].(map[string]any)
	if !ok {
		return
	}
	hostvars, ok := meta["hostvars"].(map[string]any)
	if !ok {
		return
	}
	for _, raw := range hostvars {
		vars, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		vars["ansible_user"] = configuration.SSHUser
		vars["ansible_port"] = configuration.SSHPort
	}
}

// executionRunIDPrefix returns a safe identifier prefix for temporary files.
func executionRunIDPrefix(input operationdomain.RunnerInput) string {
	return "run-" + strings.ReplaceAll(input.Operation.Execution.RunID, string(filepath.Separator), "_") + "-"
}
