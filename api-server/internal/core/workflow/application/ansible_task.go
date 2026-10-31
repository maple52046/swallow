package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// HostKeyScanner learns the SSH host keys of a run's targets, so a freshly provisioned host (whose
// keys no one can know in advance) is verified by pinning what it presents right before the run.
// It returns known_hosts lines.
type HostKeyScanner interface {
	Scan(ctx context.Context, addresses []string, port int) (string, error)
}

// LiveRecorder records a running attempt's live fields through the core (Engine.UpdateLive).
type LiveRecorder interface {
	UpdateLive(ctx context.Context, ref TaskRef, external *workflowdomain.ExternalExecutionReference, startedAt *time.Time, live *workflowdomain.TaskLive) error
}

// AnsibleTask is one Ansible Task attempt to run: its Workflow's identity and the Task as stored.
type AnsibleTask struct {
	WorkflowID string
	Kind       workflowdomain.WorkflowKind
	PlatformID string
	SiteID     string
	Generation int64
	Task       workflowdomain.Task
	// Resumed is true when a later dispatch continues the attempt; an Ansible outcome cannot be
	// proven after its process stopped, so a resumed attempt is handed to the operator.
	Resumed bool
}

// AnsibleTaskRunner runs Ansible Tasks in the Ansible executor (decision 072), the only process
// that owns ansible-runner. For one attempt it checks the Server Lock, resolves the Site's
// automation configuration, credential, inventory, the targets' SSH host keys, and the Task's
// secrets; publishes the run reference; runs the playbook while streaming per-task events and
// Task.Live through the core; and returns the outcome with the playbook's result data, which never
// leaves this process. Cancellation of ctx stops ansible-runner.
type AnsibleTaskRunner struct {
	configurations workflowdomain.AutomationConfigurationRepository
	catalog        workflowdomain.PlaybookCatalog
	runner         workflowdomain.Runner
	events         workflowdomain.AnsibleEventRepository
	inventory      InventorySource
	protection     serverdomain.MutationGuard
	secrets        workflowdomain.OperationSecretRepository
	hostKeys       HostKeyScanner
	live           LiveRecorder
	artifactRoot   string
	streamInterval time.Duration
}

// NewAnsibleTaskRunner builds the runner. Every dependency is required: a missing Server Lock
// check, secret resolver, or event store would silently weaken a run, so construction fails
// instead (AR-07).
func NewAnsibleTaskRunner(
	configurations workflowdomain.AutomationConfigurationRepository,
	catalog workflowdomain.PlaybookCatalog,
	runner workflowdomain.Runner,
	events workflowdomain.AnsibleEventRepository,
	inventory InventorySource,
	protection serverdomain.MutationGuard,
	secrets workflowdomain.OperationSecretRepository,
	hostKeys HostKeyScanner,
	live LiveRecorder,
	artifactRoot string,
) (*AnsibleTaskRunner, error) {
	if configurations == nil || catalog == nil || runner == nil || events == nil || inventory == nil ||
		protection == nil || secrets == nil || hostKeys == nil || live == nil || artifactRoot == "" {
		return nil, errors.New("ansible task runner: every dependency is required")
	}
	return &AnsibleTaskRunner{
		configurations: configurations, catalog: catalog, runner: runner, events: events, inventory: inventory,
		protection: protection, secrets: secrets, hostKeys: hostKeys, live: live, artifactRoot: artifactRoot,
		streamInterval: streamInterval,
	}, nil
}

// AnsibleRunID is the run ID of one attempt, derived from the Workflow, Task, and attempt so the
// same attempt always names the same artifact directory.
func AnsibleRunID(workflowID, taskID string, attempt int) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(fmt.Sprintf("swallow:%s/%s/%d", workflowID, taskID, attempt))).String()
}

// Run executes one attempt and returns its outcome.
func (r *AnsibleTaskRunner) Run(ctx context.Context, input AnsibleTask) Outcome {
	task := input.Task
	if input.Resumed {
		return Outcome{Status: workflowdomain.TaskRequiresAttention, Error: &workflowdomain.NormalizedError{
			Code: "executor_outcome_unknown", Retryable: true,
			Message: "The executor stopped before recording the Task's outcome; retry the Task to run it again.",
		}}
	}
	playbook, _ := task.Parameters["playbook"].(string)
	if playbook == "" {
		return failedOutcome("invalid_step", "The Ansible Step has no playbook.", false)
	}
	targets := make([]string, 0, len(task.Targets))
	for _, target := range task.Targets {
		if target.Kind == "server" {
			targets = append(targets, target.ID)
		}
	}
	if err := r.protection.RequireUnlocked(ctx, targets); err != nil {
		return failedOutcome("server_locked", err.Error(), true)
	}
	configuration, err := r.configurations.FindBySiteID(ctx, input.SiteID)
	if err != nil {
		return failedOutcome("automation_configuration_unavailable", err.Error(), true)
	}
	if err := configuration.CheckRunnable(); err != nil {
		return failedOutcome("automation_configuration_unavailable",
			"Site automation must be enabled and the installation's Deployment Key must exist before this Step can run: "+err.Error(), true)
	}
	credential, err := r.configurations.Credential(ctx, input.SiteID)
	if err != nil {
		return failedOutcome("automation_configuration_unavailable", err.Error(), true)
	}
	playbookPath, err := r.catalog.Resolve(playbook)
	if err != nil {
		return failedOutcome("playbook_unavailable", err.Error(), false)
	}
	inventory, err := r.inventory.Inventory(ctx, input.SiteID)
	if err != nil {
		return failedOutcome("inventory_snapshot_failed", err.Error(), true)
	}
	knownHosts, err := r.knownHosts(ctx, inventory, targets, configuration.SSHPort, configuration.KnownHosts)
	if err != nil {
		return failedOutcome("host_key_unavailable", err.Error(), true)
	}
	secretVars := map[string]any{}
	for name, reference := range task.SecretRefs {
		value, err := r.secrets.Resolve(ctx, reference)
		if err != nil {
			return failedOutcome("secret_unavailable", "resolve secret reference: "+err.Error(), false)
		}
		secretVars[name] = value
	}

	runID := AnsibleRunID(input.WorkflowID, task.ID, task.Attempt)
	ref := TaskRef{WorkflowID: input.WorkflowID, Generation: input.Generation, TaskID: task.ID, Attempt: task.Attempt}
	reference := &workflowdomain.ExternalExecutionReference{Provider: "ansible-runner", ID: runID, Generation: task.Attempt}
	now := time.Now().UTC()
	// Publishing the reference first lets the API serve this Task's events and logs during the run.
	if err := r.live.UpdateLive(ctx, ref, reference, &now, nil); err != nil {
		return failedOutcome("dispatch_fenced", "The Workflow run was superseded before the playbook started.", false)
	}

	extraVars, _ := task.Parameters["extraVars"].(map[string]any)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type finished struct {
		result workflowdomain.RunnerResult
		err    error
	}
	done := make(chan finished, 1)
	go func() {
		result, err := r.runner.Run(runCtx, workflowdomain.RunnerInput{
			Operation: &workflowdomain.ExecutionOperation{
				ID: input.WorkflowID, SiteID: input.SiteID, Kind: input.Kind, PlatformID: input.PlatformID,
				TargetServerIDs: targets, ExtraVars: extraVars,
				Execution: workflowdomain.Execution{RunID: runID, Playbook: playbook, Status: workflowdomain.StatusRunning},
			},
			Configuration: &workflowdomain.AutomationConfiguration{
				SiteID: input.SiteID, Enabled: true, SSHUser: configuration.SSHUser, SSHPort: configuration.SSHPort, KnownHosts: knownHosts,
			},
			Credential: credential, Inventory: inventory, PlaybookPath: playbookPath, SecretVars: secretVars,
		})
		done <- finished{result: result, err: err}
	}()

	reader, canStream := r.runner.(workflowdomain.IncrementalEventReader)
	progress := &runProgressState{}
	ticker := time.NewTicker(r.streamInterval)
	defer ticker.Stop()
	for {
		select {
		case outcome := <-done:
			if canStream {
				r.stream(ctx, ref, runID, reader, progress)
			}
			if ctx.Err() != nil {
				return Outcome{Status: workflowdomain.TaskCanceled, ExternalExecution: reference}
			}
			if outcome.err != nil {
				result := failedOutcome("ansible_failed", outcome.err.Error(), true)
				result.ExternalExecution, result.Artifacts = reference, r.artifacts(runID)
				return result
			}
			return Outcome{
				Status: workflowdomain.TaskSucceeded, Progress: 100, ExternalExecution: reference,
				Artifacts: r.artifacts(runID), Data: outcome.result.Data,
			}
		case <-ticker.C:
			if canStream {
				r.stream(ctx, ref, runID, reader, progress)
			}
		}
	}
}

// stream tails the run's new events into the event store and refreshes Task.Live. It is
// best-effort: a failure is logged and the same window is read again on the next tick.
func (r *AnsibleTaskRunner) stream(ctx context.Context, ref TaskRef, runID string, reader workflowdomain.IncrementalEventReader, state *runProgressState) {
	delta, err := reader.EventsSince(runID, state.lastSeq)
	if err != nil {
		slog.Warn("read Ansible run events", "runId", runID, "error", err)
		return
	}
	if len(delta.Events) > 0 {
		if err := r.events.AppendEvents(ctx, delta.Events); err != nil {
			slog.Warn("append Ansible run events", "runId", runID, "error", err)
			return
		}
	}
	state.apply(delta)
	live := state.prog
	live.UpdatedAt = time.Now().UTC()
	if err := r.live.UpdateLive(ctx, ref, nil, nil, &live); err != nil && !errors.Is(err, workflowdomain.ErrTaskNotRunning) {
		slog.Warn("record Ansible Task progress", "runId", runID, "error", err)
	}
}

// knownHosts merges the targets' freshly scanned host keys with the Site's static entries; a run
// with targets and no entry at all cannot verify any host and fails.
func (r *AnsibleTaskRunner) knownHosts(ctx context.Context, inventory map[string]any, targets []string, port int, static string) (string, error) {
	scanned := ""
	if addresses := inventoryAddresses(inventory, targets); len(addresses) > 0 {
		if port <= 0 {
			port = 22
		}
		// A scan error still yields the keys it captured; only an empty result is fatal below.
		scanned, _ = r.hostKeys.Scan(ctx, addresses, port)
	}
	var blocks []string
	for _, block := range []string{scanned, static} {
		if strings.TrimSpace(block) != "" {
			blocks = append(blocks, strings.TrimSpace(block))
		}
	}
	if len(blocks) == 0 {
		if len(targets) > 0 {
			return "", errors.New("could not learn the target hosts' SSH host keys and no known_hosts is configured")
		}
		return "", nil
	}
	return strings.Join(blocks, "\n") + "\n", nil
}

// inventoryAddresses reads each target's connection address (ansible_host) from the inventory, so
// scanning reaches the addresses Ansible connects to.
func inventoryAddresses(inventory map[string]any, targets []string) []string {
	meta, _ := inventory["_meta"].(map[string]any)
	hostvars, _ := meta["hostvars"].(map[string]any)
	seen := map[string]bool{}
	var addresses []string
	for _, id := range targets {
		vars, _ := hostvars[id].(map[string]any)
		address, _ := vars["ansible_host"].(string)
		if address = strings.TrimSpace(address); address != "" && !seen[address] {
			seen[address] = true
			addresses = append(addresses, address)
		}
	}
	return addresses
}

// artifacts describes the run's retained output by metadata only; the bytes stay on this host and
// are served through the runner's readers.
func (r *AnsibleTaskRunner) artifacts(runID string) []workflowdomain.ArtifactMetadata {
	artifacts := []workflowdomain.ArtifactMetadata{}
	if filepath.Base(runID) != runID {
		return artifacts
	}
	for _, candidate := range []struct{ name, mediaType string }{
		{"stdout", "text/plain"}, {"process.log", "text/plain"}, {"job_events", "application/vnd.swallow.ansible-events"},
	} {
		path := filepath.Join(r.artifactRoot, runID, candidate.name)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		size := info.Size()
		if info.IsDir() {
			size = 0
			_ = filepath.WalkDir(path, func(_ string, entry os.DirEntry, err error) error {
				if err == nil && !entry.IsDir() {
					if fileInfo, statErr := entry.Info(); statErr == nil {
						size += fileInfo.Size()
					}
				}
				return nil
			})
		}
		artifacts = append(artifacts, workflowdomain.ArtifactMetadata{
			ID: candidate.name, Name: candidate.name, MediaType: candidate.mediaType, SizeBytes: size,
			RelativeKey: filepath.Join(runID, candidate.name), CreatedAt: info.ModTime().UTC(),
		})
	}
	return artifacts
}

func failedOutcome(code, message string, retryable bool) Outcome {
	if message == "" {
		message = "The Ansible Step failed."
	}
	return Outcome{Status: workflowdomain.TaskFailed, Error: &workflowdomain.NormalizedError{Code: code, Message: message, Retryable: retryable}}
}
