package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

// Run materializes secrets only in a private runtime directory and removes it afterward.
func (r *LocalRunner) Run(ctx context.Context, input operationdomain.RunnerInput) error {
	if input.Credential.SSHPrivateKey == "" {
		return operationdomain.ErrAutomationCredentialMissing
	}
	if strings.TrimSpace(input.Configuration.KnownHosts) == "" {
		return fmt.Errorf("knownHosts is required; SSH host-key verification cannot be disabled")
	}
	if err := os.MkdirAll(r.runtimeRoot, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(r.runtimeRoot, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(r.artifactRoot, 0o750); err != nil {
		return err
	}

	privateDir, err := os.MkdirTemp(r.runtimeRoot, executionRunIDPrefix(input))
	if err != nil {
		return err
	}
	defer os.RemoveAll(privateDir)
	if err := os.Chmod(privateDir, 0o700); err != nil {
		return err
	}

	inventory := cloneInventory(input.Inventory)
	setConnectionVars(inventory, input.Configuration)
	envDir := filepath.Join(privateDir, "env")
	if err := os.Mkdir(envDir, 0o700); err != nil {
		return err
	}
	inventoryPath := filepath.Join(privateDir, "inventory.json")
	privateKeyPath := filepath.Join(privateDir, "ssh_key")
	knownHostsPath := filepath.Join(privateDir, "known_hosts")
	extraVarsPath := filepath.Join(privateDir, "extravars.json")
	passwordsPath := filepath.Join(envDir, "passwords")

	if err := writeJSONFile(inventoryPath, inventory, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(privateKeyPath, []byte(input.Credential.SSHPrivateKey), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(knownHostsPath, []byte(input.Configuration.KnownHosts), 0o600); err != nil {
		return err
	}
	if err := writeJSONFile(extraVarsPath, input.Operation.ExtraVars, 0o600); err != nil {
		return err
	}
	if input.Credential.BecomePassword != "" {
		if err := writeJSONFile(passwordsPath,
			map[string]string{"BECOME password.*": input.Credential.BecomePassword}, 0o600); err != nil {
			return err
		}
	}

	runArtifacts := filepath.Join(r.artifactRoot, input.Operation.Execution.RunID)
	if err := os.MkdirAll(runArtifacts, 0o750); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(runArtifacts, "process.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer logFile.Close()

	args := []string{
		"run", privateDir,
		"--project-dir", r.projectRoot,
		"--artifact-dir", r.artifactRoot,
		"--ident", input.Operation.Execution.RunID,
		"--inventory", inventoryPath,
		"--private-key", privateKeyPath,
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
			return ctx.Err()
		}
		return fmt.Errorf("ansible-runner: %w", err)
	}
	return nil
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
